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
	"strings"

	"github.com/Azure/ARO-HCP/internal/api/coreapi"
)

// AdmitClusterCapacity rejects the creation of a SWIFT-enabled minimal cluster
// once limit such clusters exist in its region. Other clusters are always
// admitted. Every matching cluster counts, including failed, provisioning and
// deleting ones.
//
// Best-effort only: clusters is a snapshot and nothing is reserved, so
// concurrent creates can exceed the limit.
func AdmitClusterCapacity(clusters []*coreapi.Cluster, limit int, newObj *coreapi.Cluster) error {
	if newObj.ServiceProviderProperties.ExperimentalFeatures.ControlPlanePodSizing != coreapi.MinimalControlPlanePodSizing ||
		newObj.CustomerProperties.Platform.VnetIntegrationSubnetID == nil {
		return nil
	}

	count := 0
	for _, cluster := range clusters {
		if strings.EqualFold(cluster.Location, newObj.Location) &&
			cluster.ServiceProviderProperties.ExperimentalFeatures.ControlPlanePodSizing == coreapi.MinimalControlPlanePodSizing &&
			cluster.CustomerProperties.Platform.VnetIntegrationSubnetID != nil {
			count++
		}
	}
	if count >= limit {
		return coreapi.NewCloudError(http.StatusBadRequest, coreapi.CloudErrorCodeCapacityExceeded, "",
			"regional capacity for SWIFT-enabled minimal clusters is currently exhausted; retry later")
	}
	return nil
}
