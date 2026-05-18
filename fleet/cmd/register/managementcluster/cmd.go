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

package managementcluster

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	"github.com/spf13/cobra"

	"github.com/Azure/ARO-HCP/internal/api"
	"github.com/Azure/ARO-HCP/internal/api/fleet"
	"github.com/Azure/ARO-HCP/internal/database"
)

func NewManagementClusterCommand() (*cobra.Command, error) {
	opts := DefaultManagementClusterOptions()
	cmd := &cobra.Command{
		Use:   "managementcluster",
		Short: "Register or update a management cluster in CosmosDB",
		RunE: func(cmd *cobra.Command, args []string) error {
			return run(cmd.Context(), opts)
		},
	}
	if err := BindManagementClusterOptions(opts, cmd); err != nil {
		return nil, err
	}
	return cmd, nil
}

func run(ctx context.Context, rawOpts *RawManagementClusterOptions) error {
	logger := logr.FromContextOrDiscard(ctx)

	validated, err := rawOpts.Validate(ctx)
	if err != nil {
		return err
	}
	completed, err := validated.Complete(ctx)
	if err != nil {
		return err
	}

	stampsCRUD := completed.fleetDBClient.Stamps()

	// Verify parent stamp exists
	if _, err := stampsCRUD.Get(ctx, completed.stampIdentifier); err != nil {
		if database.IsNotFoundError(err) {
			return fmt.Errorf("parent stamp %q not found: register the stamp first", completed.stampIdentifier)
		}
		return fmt.Errorf("failed to get parent stamp: %w", err)
	}

	mcResourceID, _ := fleet.ToManagementClusterResourceID(completed.stampIdentifier)
	newMC := &fleet.ManagementCluster{
		CosmosMetadata: api.CosmosMetadata{ResourceID: mcResourceID},
		ResourceID:     mcResourceID,
		Spec: fleet.ManagementClusterSpec{
			SchedulingPolicy: completed.schedulingPolicy,
		},
		Status: fleet.ManagementClusterStatus{
			AKSResourceID:                                        completed.aksResourceID,
			PublicDNSZoneResourceID:                              completed.publicDNSZoneResourceID,
			HostedClustersSecretsKeyVaultURL:                     completed.hostedClustersSecretsKeyVaultURL,
			HostedClustersManagedIdentitiesKeyVaultURL:           completed.hostedClustersManagedIdentitiesKeyVaultURL,
			HostedClustersSecretsKeyVaultManagedIdentityClientID: completed.hostedClustersSecretsKeyVaultManagedIdentityClientID,
			ClusterServiceProvisionShardID:                       completed.provisionShardID,
			MaestroConsumerName:                                  completed.maestroConsumerName,
			MaestroRESTAPIURL:                                    completed.maestroRESTAPIURL,
			MaestroGRPCTarget:                                    completed.maestroGRPCTarget,
		},
	}

	mcCRUD := stampsCRUD.ManagementClusters(completed.stampIdentifier)

	existing, err := mcCRUD.Get(ctx, fleet.ManagementClusterResourceName)
	if err != nil {
		if !database.IsNotFoundError(err) {
			return fmt.Errorf("failed to get management cluster: %w", err)
		}

		logger.Info("Creating management cluster", "stampIdentifier", completed.stampIdentifier)
		if _, err := mcCRUD.Create(ctx, newMC, nil); err != nil {
			return fmt.Errorf("failed to create management cluster: %w", err)
		}
		logger.Info("Management cluster created", "stampIdentifier", completed.stampIdentifier)
		return nil
	}

	logger.Info("Updating existing management cluster", "stampIdentifier", completed.stampIdentifier)
	updated := existing.DeepCopy()
	updated.Spec = newMC.Spec
	updated.Status.AKSResourceID = newMC.Status.AKSResourceID
	updated.Status.PublicDNSZoneResourceID = newMC.Status.PublicDNSZoneResourceID
	updated.Status.HostedClustersSecretsKeyVaultURL = newMC.Status.HostedClustersSecretsKeyVaultURL
	updated.Status.HostedClustersManagedIdentitiesKeyVaultURL = newMC.Status.HostedClustersManagedIdentitiesKeyVaultURL
	updated.Status.HostedClustersSecretsKeyVaultManagedIdentityClientID = newMC.Status.HostedClustersSecretsKeyVaultManagedIdentityClientID
	updated.Status.ClusterServiceProvisionShardID = newMC.Status.ClusterServiceProvisionShardID
	updated.Status.MaestroConsumerName = newMC.Status.MaestroConsumerName
	updated.Status.MaestroRESTAPIURL = newMC.Status.MaestroRESTAPIURL
	updated.Status.MaestroGRPCTarget = newMC.Status.MaestroGRPCTarget

	if _, err := mcCRUD.Replace(ctx, updated, existing, nil); err != nil {
		return fmt.Errorf("failed to update management cluster: %w", err)
	}
	logger.Info("Management cluster updated", "stampIdentifier", completed.stampIdentifier)
	return nil
}
