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

package admission

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	azcorearm "github.com/Azure/azure-sdk-for-go/sdk/azcore/arm"

	"github.com/Azure/ARO-HCP/internal/api/coreapi"
	"github.com/Azure/ARO-HCP/internal/api/metadataapi"
	"github.com/Azure/ARO-HCP/internal/apitesting/coreapitesting"
)

func TestAdmitClusterCapacity(t *testing.T) {
	swiftSubnet := metadataapi.Must(azcorearm.ParseResourceID(coreapitesting.TestVnetIntegrationSubnetResourceID))
	cluster := func(location string, sizing coreapi.ControlPlanePodSizing, swiftSubnet *azcorearm.ResourceID) *coreapi.Cluster {
		c := &coreapi.Cluster{}
		c.Location = location
		c.ServiceProviderProperties.ExperimentalFeatures.ControlPlanePodSizing = sizing
		c.CustomerProperties.Platform.VnetIntegrationSubnetID = swiftSubnet
		return c
	}
	failed := cluster("westus3", coreapi.MinimalControlPlanePodSizing, swiftSubnet)
	failed.ServiceProviderProperties.ProvisioningState = coreapi.ProvisioningStateFailed
	provisioning := cluster("westus3", coreapi.MinimalControlPlanePodSizing, swiftSubnet)
	provisioning.ServiceProviderProperties.ProvisioningState = coreapi.ProvisioningStateProvisioning
	deleting := cluster("westus3", coreapi.MinimalControlPlanePodSizing, swiftSubnet)
	deleting.ServiceProviderProperties.ProvisioningState = coreapi.ProvisioningStateDeleting
	deleting.ServiceProviderProperties.DeletionTimestamp = &metav1.Time{}

	for _, tt := range []struct {
		name     string
		clusters []*coreapi.Cluster
		limit    int
		newObj   *coreapi.Cluster
		rejected bool
	}{
		{
			name:   "empty inventory is admitted",
			limit:  2,
			newObj: cluster("westus3", coreapi.MinimalControlPlanePodSizing, swiftSubnet),
		},
		{
			name:     "zero limit rejects empty inventory",
			limit:    0,
			newObj:   cluster("westus3", coreapi.MinimalControlPlanePodSizing, swiftSubnet),
			rejected: true,
		},
		{
			name: "one below the limit is admitted",
			clusters: []*coreapi.Cluster{
				cluster("westus3", coreapi.MinimalControlPlanePodSizing, swiftSubnet),
			},
			limit:  2,
			newObj: cluster("westus3", coreapi.MinimalControlPlanePodSizing, swiftSubnet),
		},
		{
			name: "at the limit is rejected",
			clusters: []*coreapi.Cluster{
				cluster("westus3", coreapi.MinimalControlPlanePodSizing, swiftSubnet),
				cluster("westus3", coreapi.MinimalControlPlanePodSizing, swiftSubnet),
			},
			limit:    2,
			newObj:   cluster("westus3", coreapi.MinimalControlPlanePodSizing, swiftSubnet),
			rejected: true,
		},
		{
			name: "above the limit is rejected",
			clusters: []*coreapi.Cluster{
				cluster("westus3", coreapi.MinimalControlPlanePodSizing, swiftSubnet),
				cluster("westus3", coreapi.MinimalControlPlanePodSizing, swiftSubnet),
				cluster("westus3", coreapi.MinimalControlPlanePodSizing, swiftSubnet),
			},
			limit:    2,
			newObj:   cluster("westus3", coreapi.MinimalControlPlanePodSizing, swiftSubnet),
			rejected: true,
		},
		{
			name: "cluster in another region does not count",
			clusters: []*coreapi.Cluster{
				cluster("westus3", coreapi.MinimalControlPlanePodSizing, swiftSubnet),
				cluster("eastus", coreapi.MinimalControlPlanePodSizing, swiftSubnet),
			},
			limit:  2,
			newObj: cluster("westus3", coreapi.MinimalControlPlanePodSizing, swiftSubnet),
		},
		{
			name: "region comparison ignores case",
			clusters: []*coreapi.Cluster{
				cluster("westus3", coreapi.MinimalControlPlanePodSizing, swiftSubnet),
				cluster("WESTUS3", coreapi.MinimalControlPlanePodSizing, swiftSubnet),
			},
			limit:    2,
			newObj:   cluster("westus3", coreapi.MinimalControlPlanePodSizing, swiftSubnet),
			rejected: true,
		},
		{
			name: "default sized cluster does not count",
			clusters: []*coreapi.Cluster{
				cluster("westus3", coreapi.MinimalControlPlanePodSizing, swiftSubnet),
				cluster("westus3", coreapi.DefaultControlPlanePodSizing, swiftSubnet),
			},
			limit:  2,
			newObj: cluster("westus3", coreapi.MinimalControlPlanePodSizing, swiftSubnet),
		},
		{
			name: "cluster without SWIFT does not count",
			clusters: []*coreapi.Cluster{
				cluster("westus3", coreapi.MinimalControlPlanePodSizing, swiftSubnet),
				cluster("westus3", coreapi.MinimalControlPlanePodSizing, nil),
			},
			limit:  2,
			newObj: cluster("westus3", coreapi.MinimalControlPlanePodSizing, swiftSubnet),
		},
		{
			name: "failed cluster counts",
			clusters: []*coreapi.Cluster{
				cluster("westus3", coreapi.MinimalControlPlanePodSizing, swiftSubnet),
				failed,
			},
			limit:    2,
			newObj:   cluster("westus3", coreapi.MinimalControlPlanePodSizing, swiftSubnet),
			rejected: true,
		},
		{
			name: "provisioning cluster counts",
			clusters: []*coreapi.Cluster{
				cluster("westus3", coreapi.MinimalControlPlanePodSizing, swiftSubnet),
				provisioning,
			},
			limit:    2,
			newObj:   cluster("westus3", coreapi.MinimalControlPlanePodSizing, swiftSubnet),
			rejected: true,
		},
		{
			name: "deleting cluster counts",
			clusters: []*coreapi.Cluster{
				cluster("westus3", coreapi.MinimalControlPlanePodSizing, swiftSubnet),
				deleting,
			},
			limit:    2,
			newObj:   cluster("westus3", coreapi.MinimalControlPlanePodSizing, swiftSubnet),
			rejected: true,
		},
		{
			name: "default sized create is admitted at the limit",
			clusters: []*coreapi.Cluster{
				cluster("westus3", coreapi.MinimalControlPlanePodSizing, swiftSubnet),
				cluster("westus3", coreapi.MinimalControlPlanePodSizing, swiftSubnet),
			},
			limit:  2,
			newObj: cluster("westus3", coreapi.DefaultControlPlanePodSizing, swiftSubnet),
		},
		{
			name: "create without SWIFT is admitted at the limit",
			clusters: []*coreapi.Cluster{
				cluster("westus3", coreapi.MinimalControlPlanePodSizing, swiftSubnet),
				cluster("westus3", coreapi.MinimalControlPlanePodSizing, swiftSubnet),
			},
			limit:  2,
			newObj: cluster("westus3", coreapi.MinimalControlPlanePodSizing, nil),
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			err := AdmitClusterCapacity(tt.clusters, tt.limit, tt.newObj)
			if !tt.rejected {
				require.NoError(t, err)
				return
			}
			var cloudError *coreapi.CloudError
			require.ErrorAs(t, err, &cloudError)
			require.Equal(t, http.StatusBadRequest, cloudError.StatusCode)
			require.Equal(t, coreapi.CloudErrorCodeCapacityExceeded, cloudError.Code)
			require.Empty(t, cloudError.Target)
		})
	}
}
