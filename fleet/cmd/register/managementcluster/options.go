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

	azcorearm "github.com/Azure/azure-sdk-for-go/sdk/azcore/arm"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/cloud"
	"github.com/spf13/cobra"

	"github.com/Azure/ARO-HCP/internal/api"
	"github.com/Azure/ARO-HCP/internal/api/fleet"
	"github.com/Azure/ARO-HCP/internal/azsdk"
	"github.com/Azure/ARO-HCP/internal/database"
)

type RawManagementClusterOptions struct {
	CosmosURL                                           string
	CosmosName                                          string
	StampIdentifier                                     string
	SchedulingPolicy                                    string
	AKSResourceID                                       string
	PublicDNSZoneResourceID                              string
	HostedClustersSecretsKeyVaultURL                     string
	HostedClustersManagedIdentitiesKeyVaultURL           string
	HostedClustersSecretsKeyVaultManagedIdentityClientID string
	ClusterServiceProvisionShardID                       string
	MaestroConsumerName                                  string
	MaestroRESTAPIURL                                    string
	MaestroGRPCTarget                                    string
}

func DefaultManagementClusterOptions() *RawManagementClusterOptions {
	return &RawManagementClusterOptions{}
}

func BindManagementClusterOptions(opts *RawManagementClusterOptions, cmd *cobra.Command) error {
	cmd.Flags().StringVar(&opts.CosmosURL, "cosmos-url", opts.CosmosURL, "CosmosDB endpoint URL")
	cmd.Flags().StringVar(&opts.CosmosName, "cosmos-name", opts.CosmosName, "CosmosDB database name")
	cmd.Flags().StringVar(&opts.StampIdentifier, "stamp-identifier", opts.StampIdentifier, "parent stamp identifier")
	cmd.Flags().StringVar(&opts.SchedulingPolicy, "scheduling-policy", opts.SchedulingPolicy, "scheduling policy (Schedulable or Unschedulable)")
	cmd.Flags().StringVar(&opts.AKSResourceID, "aks-resource-id", opts.AKSResourceID, "AKS cluster ARM resource ID")
	cmd.Flags().StringVar(&opts.PublicDNSZoneResourceID, "public-dns-zone-resource-id", opts.PublicDNSZoneResourceID, "public DNS zone ARM resource ID")
	cmd.Flags().StringVar(&opts.HostedClustersSecretsKeyVaultURL, "hosted-clusters-secrets-keyvault-url", opts.HostedClustersSecretsKeyVaultURL, "key vault URL for hosted cluster secrets")
	cmd.Flags().StringVar(&opts.HostedClustersManagedIdentitiesKeyVaultURL, "hosted-clusters-managed-identities-keyvault-url", opts.HostedClustersManagedIdentitiesKeyVaultURL, "key vault URL for hosted cluster managed identities")
	cmd.Flags().StringVar(&opts.HostedClustersSecretsKeyVaultManagedIdentityClientID, "hosted-clusters-secrets-keyvault-mi-client-id", opts.HostedClustersSecretsKeyVaultManagedIdentityClientID, "client ID of the managed identity for the secrets key vault")
	cmd.Flags().StringVar(&opts.ClusterServiceProvisionShardID, "cluster-service-provision-shard-id", opts.ClusterServiceProvisionShardID, "Cluster Service provision shard ID")
	cmd.Flags().StringVar(&opts.MaestroConsumerName, "maestro-consumer-name", opts.MaestroConsumerName, "Maestro consumer name")
	cmd.Flags().StringVar(&opts.MaestroRESTAPIURL, "maestro-rest-api-url", opts.MaestroRESTAPIURL, "Maestro REST API URL")
	cmd.Flags().StringVar(&opts.MaestroGRPCTarget, "maestro-grpc-target", opts.MaestroGRPCTarget, "Maestro gRPC dial target (host:port)")

	for _, flag := range []string{
		"cosmos-url",
		"cosmos-name",
		"stamp-identifier",
		"scheduling-policy",
		"aks-resource-id",
		"public-dns-zone-resource-id",
		"hosted-clusters-secrets-keyvault-url",
		"hosted-clusters-managed-identities-keyvault-url",
		"hosted-clusters-secrets-keyvault-mi-client-id",
		"cluster-service-provision-shard-id",
		"maestro-consumer-name",
		"maestro-rest-api-url",
		"maestro-grpc-target",
	} {
		if err := cmd.MarkFlagRequired(flag); err != nil {
			return err
		}
	}

	return nil
}

type validatedManagementClusterOptions struct {
	*RawManagementClusterOptions
	aksResourceID           *azcorearm.ResourceID
	publicDNSZoneResourceID *azcorearm.ResourceID
	provisionShardID        *api.InternalID
	schedulingPolicy        fleet.ManagementClusterSchedulingPolicy
}

type ValidatedManagementClusterOptions struct {
	*validatedManagementClusterOptions
}

func (o *RawManagementClusterOptions) Validate(ctx context.Context) (*ValidatedManagementClusterOptions, error) {
	if len(o.CosmosURL) == 0 {
		return nil, fmt.Errorf("cosmos-url is required")
	}
	if len(o.CosmosName) == 0 {
		return nil, fmt.Errorf("cosmos-name is required")
	}
	if len(o.StampIdentifier) == 0 {
		return nil, fmt.Errorf("stamp-identifier is required")
	}
	if _, err := fleet.ToStampResourceID(o.StampIdentifier); err != nil {
		return nil, fmt.Errorf("invalid stamp identifier %q: %w", o.StampIdentifier, err)
	}

	schedulingPolicy := fleet.ManagementClusterSchedulingPolicy(o.SchedulingPolicy)
	if !fleet.ValidManagementClusterSchedulingPolicies.Has(schedulingPolicy) {
		return nil, fmt.Errorf("invalid scheduling policy %q: must be Schedulable or Unschedulable", o.SchedulingPolicy)
	}

	aksID, err := azcorearm.ParseResourceID(o.AKSResourceID)
	if err != nil {
		return nil, fmt.Errorf("invalid aks-resource-id: %w", err)
	}

	dnsID, err := azcorearm.ParseResourceID(o.PublicDNSZoneResourceID)
	if err != nil {
		return nil, fmt.Errorf("invalid public-dns-zone-resource-id: %w", err)
	}

	shardID, err := api.NewInternalID(o.ClusterServiceProvisionShardID)
	if err != nil {
		return nil, fmt.Errorf("invalid cluster-service-provision-shard-id: %w", err)
	}

	return &ValidatedManagementClusterOptions{
		validatedManagementClusterOptions: &validatedManagementClusterOptions{
			RawManagementClusterOptions: o,
			aksResourceID:              aksID,
			publicDNSZoneResourceID:    dnsID,
			provisionShardID:           &shardID,
			schedulingPolicy:           schedulingPolicy,
		},
	}, nil
}

type managementClusterOptions struct {
	fleetDBClient                                       database.FleetDBClient
	stampIdentifier                                     string
	schedulingPolicy                                    fleet.ManagementClusterSchedulingPolicy
	aksResourceID                                       *azcorearm.ResourceID
	publicDNSZoneResourceID                              *azcorearm.ResourceID
	hostedClustersSecretsKeyVaultURL                     string
	hostedClustersManagedIdentitiesKeyVaultURL           string
	hostedClustersSecretsKeyVaultManagedIdentityClientID string
	provisionShardID                                     *api.InternalID
	maestroConsumerName                                  string
	maestroRESTAPIURL                                    string
	maestroGRPCTarget                                    string
}

type ManagementClusterOptions struct {
	*managementClusterOptions
}

func (o *ValidatedManagementClusterOptions) Complete(ctx context.Context) (*ManagementClusterOptions, error) {
	clientOpts := azsdk.NewClientOptions(azsdk.ComponentFleet)
	// FIXME Cloud should be determined by other means.
	clientOpts.Cloud = cloud.AzurePublic

	dbClient, err := database.NewCosmosDatabaseClient(o.CosmosURL, o.CosmosName, clientOpts)
	if err != nil {
		return nil, fmt.Errorf("failed to create CosmosDB client: %w", err)
	}

	fleetDBClient, err := database.NewFleetDBClient(dbClient)
	if err != nil {
		return nil, fmt.Errorf("failed to create fleet DB client: %w", err)
	}

	return &ManagementClusterOptions{
		managementClusterOptions: &managementClusterOptions{
			fleetDBClient:                                       fleetDBClient,
			stampIdentifier:                                     o.StampIdentifier,
			schedulingPolicy:                                    o.schedulingPolicy,
			aksResourceID:                                       o.aksResourceID,
			publicDNSZoneResourceID:                              o.publicDNSZoneResourceID,
			hostedClustersSecretsKeyVaultURL:                     o.HostedClustersSecretsKeyVaultURL,
			hostedClustersManagedIdentitiesKeyVaultURL:           o.HostedClustersManagedIdentitiesKeyVaultURL,
			hostedClustersSecretsKeyVaultManagedIdentityClientID: o.HostedClustersSecretsKeyVaultManagedIdentityClientID,
			provisionShardID:                                     o.provisionShardID,
			maestroConsumerName:                                  o.MaestroConsumerName,
			maestroRESTAPIURL:                                    o.MaestroRESTAPIURL,
			maestroGRPCTarget:                                    o.MaestroGRPCTarget,
		},
	}, nil
}
