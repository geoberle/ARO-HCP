// Copyright 2026 Microsoft Corporation
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package clustersserviceregistration

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"

	arohcpv1alpha1 "github.com/openshift-online/ocm-api-model/clientapi/arohcp/v1alpha1"
	ocmerrors "github.com/openshift-online/ocm-sdk-go/errors"

	fleetcontrollers "github.com/Azure/ARO-HCP/fleet/pkg/controllers/base"
	"github.com/Azure/ARO-HCP/internal/api"
	"github.com/Azure/ARO-HCP/internal/api/fleet"
	"github.com/Azure/ARO-HCP/internal/database"
	"github.com/Azure/ARO-HCP/internal/database/listers"
	"github.com/Azure/ARO-HCP/internal/ocm"
	"github.com/Azure/ARO-HCP/internal/utils"
)

const defaultInformerResyncPeriod = 5 * time.Minute

type clustersServiceRegistrationSyncer struct {
	fleetDBClient         database.FleetDBClient
	clustersServiceClient ocm.ClusterServiceClientSpec
	stampLister           listers.StampLister
	region                string
}

// NewClustersServiceRegistrationController creates a ManagementClusterWatchingController
// that reconciles ClustersService provision shards from ManagementCluster documents.
func NewClustersServiceRegistrationController(
	managementClusterInformer cache.SharedIndexInformer,
	stampInformer cache.SharedIndexInformer,
	fleetDBClient database.FleetDBClient,
	clustersServiceClient ocm.ClusterServiceClientSpec,
	stampLister listers.StampLister,
	region string,
	cfg fleetcontrollers.ManagementClusterWatchingControllerConfig,
) (*fleetcontrollers.ManagementClusterWatchingController, error) {
	syncer := &clustersServiceRegistrationSyncer{
		fleetDBClient:         fleetDBClient,
		clustersServiceClient: clustersServiceClient,
		stampLister:           stampLister,
		region:                region,
	}

	controller := fleetcontrollers.NewManagementClusterWatchingController(
		"ClustersServiceRegistrationController",
		syncer,
		cfg,
	)

	if err := controller.QueueForInformers(defaultInformerResyncPeriod, managementClusterInformer, stampInformer); err != nil {
		return nil, err
	}

	return controller, nil
}

func (s *clustersServiceRegistrationSyncer) SyncOnce(ctx context.Context, key fleetcontrollers.ManagementClusterKey) error {
	logger := utils.LoggerFromContext(ctx)

	managementClusterCRUD := s.fleetDBClient.Stamps().ManagementClusters(key.StampIdentifier)
	managementCluster, err := managementClusterCRUD.Get(ctx, fleet.ManagementClusterResourceName)
	if err != nil {
		if database.IsNotFoundError(err) {
			return nil
		}
		return fmt.Errorf("fetching management cluster: %w", err)
	}

	stamp, err := s.stampLister.Get(ctx, key.StampIdentifier)
	if err != nil {
		return fmt.Errorf("looking up stamp %q: %w", key.StampIdentifier, err)
	}

	if !apimeta.IsStatusConditionTrue(stamp.Status.Conditions, string(fleet.StampConditionApproved)) {
		logger.Info("stamp not approved, skipping CS registration")
		apimeta.SetStatusCondition(&managementCluster.Status.Conditions, metav1.Condition{
			Type:               string(fleet.ManagementClusterConditionClustersServiceRegistered),
			Status:             metav1.ConditionFalse,
			Reason:             string(fleet.ManagementClusterConditionReasonStampNotApproved),
			Message:            "Parent stamp is not approved",
			LastTransitionTime: metav1.Now(),
		})
		if _, err := managementClusterCRUD.Replace(ctx, managementCluster, managementCluster, nil); err != nil {
			return fmt.Errorf("writing unapproved condition: %w", err)
		}
		return nil
	}

	shardID, err := s.reconcileProvisionShard(ctx, managementCluster)
	if err != nil {
		apimeta.SetStatusCondition(&managementCluster.Status.Conditions, metav1.Condition{
			Type:               string(fleet.ManagementClusterConditionClustersServiceRegistered),
			Status:             metav1.ConditionFalse,
			Reason:             string(fleet.ManagementClusterConditionReasonRegistrationFailed),
			Message:            fmt.Sprintf("ClustersService registration failed: %v", err),
			LastTransitionTime: metav1.Now(),
		})
		if _, writeErr := managementClusterCRUD.Replace(ctx, managementCluster, managementCluster, nil); writeErr != nil {
			logger.Error(writeErr, "failed to write failure condition")
		}
		return fmt.Errorf("reconciling provision shard: %w", err)
	}

	managementCluster.Status.ClusterServiceProvisionShardID = shardID

	reason, message := shardConditionForPolicy(managementCluster.Spec.SchedulingPolicy)
	apimeta.SetStatusCondition(&managementCluster.Status.Conditions, metav1.Condition{
		Type:               string(fleet.ManagementClusterConditionClustersServiceRegistered),
		Status:             metav1.ConditionTrue,
		Reason:             string(reason),
		Message:            message,
		LastTransitionTime: metav1.Now(),
	})

	if _, err := managementClusterCRUD.Replace(ctx, managementCluster, managementCluster, nil); err != nil {
		return fmt.Errorf("writing success condition: %w", err)
	}

	logger.Info("CS registration synced", "provisionShardID", shardID)
	return nil
}

func (s *clustersServiceRegistrationSyncer) reconcileProvisionShard(
	ctx context.Context,
	managementCluster *fleet.ManagementCluster,
) (*api.InternalID, error) {
	existingID, err := s.findExistingProvisionShard(ctx, managementCluster)
	if err != nil {
		return nil, err
	}

	if existingID != nil {
		builder := buildProvisionShardForUpdate(managementCluster)
		updated, err := s.clustersServiceClient.UpdateProvisionShard(ctx, *existingID, builder)
		if err != nil {
			return nil, fmt.Errorf("updating provision shard: %w", err)
		}
		shardID, err := api.NewInternalID(updated.HREF())
		if err != nil {
			return nil, fmt.Errorf("parsing updated provision shard HREF: %w", err)
		}
		return &shardID, nil
	}

	createBuilder := buildProvisionShardForCreate(managementCluster, s.region)
	created, err := s.clustersServiceClient.PostProvisionShard(ctx, createBuilder)
	if err != nil {
		return nil, fmt.Errorf("creating provision shard: %w", err)
	}
	createdID, err := api.NewInternalID(created.HREF())
	if err != nil {
		return nil, fmt.Errorf("parsing created provision shard HREF: %w", err)
	}

	desiredStatus := schedulingPolicyToShardStatus(managementCluster.Spec.SchedulingPolicy)
	if desiredStatus != ocm.CSProvisionShardStatusMaintenance {
		updateBuilder := buildProvisionShardForUpdate(managementCluster)
		if _, err := s.clustersServiceClient.UpdateProvisionShard(ctx, createdID, updateBuilder); err != nil {
			return nil, fmt.Errorf("setting provision shard status after create: %w", err)
		}
	}

	return &createdID, nil
}

func (s *clustersServiceRegistrationSyncer) findExistingProvisionShard(
	ctx context.Context,
	managementCluster *fleet.ManagementCluster,
) (*api.InternalID, error) {
	if managementCluster.Status.ClusterServiceProvisionShardID != nil {
		storedID := *managementCluster.Status.ClusterServiceProvisionShardID
		_, err := s.clustersServiceClient.GetProvisionShard(ctx, storedID)
		if err == nil {
			return &storedID, nil
		}
		var ocmError *ocmerrors.Error
		if !errors.As(err, &ocmError) || ocmError.Status() != http.StatusNotFound {
			return nil, fmt.Errorf("getting provision shard: %w", err)
		}
	}
	existingShardID, err := s.findProvisionShardByAKSResourceID(ctx, managementCluster.Status.AKSResourceID.String())
	if err != nil {
		return nil, fmt.Errorf("searching for provision shard by AKS resource ID: %w", err)
	}
	return existingShardID, nil
}

func (s *clustersServiceRegistrationSyncer) findProvisionShardByAKSResourceID(ctx context.Context, aksResourceID string) (*api.InternalID, error) {
	iter := s.clustersServiceClient.ListProvisionShards()
	for shard := range iter.Items(ctx) {
		if strings.EqualFold(shard.AzureShard().AksManagementClusterResourceId(), aksResourceID) {
			shardID, err := api.NewInternalID(shard.HREF())
			if err != nil {
				return nil, fmt.Errorf("parsing provision shard HREF: %w", err)
			}
			return &shardID, nil
		}
	}
	if err := iter.GetError(); err != nil {
		return nil, err
	}
	return nil, nil
}

func baseProvisionShardBuilder(managementCluster *fleet.ManagementCluster, region string) *arohcpv1alpha1.ProvisionShardBuilder {
	return arohcpv1alpha1.NewProvisionShard().
		CloudProvider(arohcpv1alpha1.NewCloudProvider().ID(ocm.CSCloudProvider)).
		Region(arohcpv1alpha1.NewCloudRegion().ID(region)).
		AzureShard(arohcpv1alpha1.NewAzureShard().
			AksManagementClusterResourceId(managementCluster.Status.AKSResourceID.String()).
			PublicDnsZoneResourceId(managementCluster.Status.PublicDNSZoneResourceID.String()).
			CxSecretsKeyVaultUrl(managementCluster.Status.HostedClustersSecretsKeyVaultURL).
			CxManagedIdentitiesKeyVaultUrl(managementCluster.Status.HostedClustersManagedIdentitiesKeyVaultURL).
			CxSecretsKeyVaultManagedIdentityClientId(managementCluster.Status.HostedClustersSecretsKeyVaultManagedIdentityClientID),
		).
		MaestroConfig(arohcpv1alpha1.NewProvisionShardMaestroConfig().
			ConsumerName(managementCluster.Status.MaestroConsumerName).
			RestApiConfig(arohcpv1alpha1.NewProvisionShardMaestroRestApiConfig().
				Url(managementCluster.Status.MaestroRESTAPIURL),
			).
			GrpcApiConfig(arohcpv1alpha1.NewProvisionShardMaestroGrpcApiConfig().
				Url(managementCluster.Status.MaestroGRPCTarget),
			),
		).
		Topology(ocm.CSProvisionShardTopologyShared)
}

func schedulingPolicyToShardStatus(policy fleet.ManagementClusterSchedulingPolicy) string {
	if policy == fleet.ManagementClusterSchedulingPolicyUnschedulable {
		return ocm.CSProvisionShardStatusMaintenance
	}
	return ocm.CSProvisionShardStatusActive
}

func buildProvisionShardForCreate(managementCluster *fleet.ManagementCluster, region string) *arohcpv1alpha1.ProvisionShardBuilder {
	builder := baseProvisionShardBuilder(managementCluster, region)
	if managementCluster.Spec.SchedulingPolicy == fleet.ManagementClusterSchedulingPolicyUnschedulable {
		builder.Status(ocm.CSProvisionShardStatusMaintenance)
	}
	return builder
}

func buildProvisionShardForUpdate(managementCluster *fleet.ManagementCluster) *arohcpv1alpha1.ProvisionShardBuilder {
	return arohcpv1alpha1.NewProvisionShard().
		Topology(ocm.CSProvisionShardTopologyShared).
		Status(schedulingPolicyToShardStatus(managementCluster.Spec.SchedulingPolicy))
}

func shardConditionForPolicy(policy fleet.ManagementClusterSchedulingPolicy) (fleet.ManagementClusterConditionReason, string) {
	switch policy {
	case fleet.ManagementClusterSchedulingPolicySchedulable:
		return fleet.ManagementClusterConditionReasonProvisionShardActive, "Provision shard is active"
	case fleet.ManagementClusterSchedulingPolicyUnschedulable:
		return fleet.ManagementClusterConditionReasonProvisionShardMaintenance, "Provision shard is in maintenance"
	default:
		return fleet.ManagementClusterConditionReasonProvisionShardStatusUnknown, fmt.Sprintf("Unknown scheduling policy %q", policy)
	}
}
