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

package compute

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/go-logr/logr"
	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Azure/ARO-HCP/fleet/pkg/azure/skucache"
)

type desiredPoolsResult struct {
	Pools    []Pool              `json:"pools"`
	Failures []AllocationFailure `json:"failures,omitempty"`
}

func assertGolden(t *testing.T, got string) {
	t.Helper()
	golden := filepath.Join("testdata", t.Name()+".json")

	if os.Getenv("UPDATE_GOLDEN") != "" {
		require.NoError(t, os.MkdirAll(filepath.Dir(golden), 0o755))
		require.NoError(t, os.WriteFile(golden, []byte(got), 0o644))
		return
	}

	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("golden file not found: %s (run with UPDATE_GOLDEN=1 to create)", golden)
	}
	if diff := cmp.Diff(string(want), got); diff != "" {
		t.Errorf("golden file mismatch (-want +got):\n%s", diff)
	}
}

var (
	allZones = []string{"1", "2", "3"}

	e32dsv6 = &skucache.SKUMetadata{
		Name:                     "Standard_E32ds_v6",
		Family:                   "standardEDSv6Family",
		VCPUs:                    32,
		MemoryGB:                 256,
		SecondaryNICs:            7,
		EphemeralOSDiskSupported: true,
		EphemeralDiskSizeGB:      1792,
		Zones:                    allZones,
	}

	e16dsv6 = &skucache.SKUMetadata{
		Name:                     "Standard_E16ds_v6",
		Family:                   "standardEDSv6Family",
		VCPUs:                    16,
		MemoryGB:                 128,
		SecondaryNICs:            7,
		EphemeralOSDiskSupported: true,
		EphemeralDiskSizeGB:      896,
		Zones:                    allZones,
	}

	e8dsv6 = &skucache.SKUMetadata{
		Name:                     "Standard_E8ds_v6",
		Family:                   "standardEDSv6Family",
		VCPUs:                    8,
		MemoryGB:                 64,
		SecondaryNICs:            3,
		EphemeralOSDiskSupported: true,
		EphemeralDiskSizeGB:      448,
		Zones:                    allZones,
	}

	d4dsv6 = &skucache.SKUMetadata{
		Name:                     "Standard_D4ds_v6",
		Family:                   "standardDDSv6Family",
		VCPUs:                    4,
		MemoryGB:                 16,
		SecondaryNICs:            1,
		EphemeralOSDiskSupported: true,
		EphemeralDiskSizeGB:      200,
		Zones:                    allZones,
	}

	// e32dsv6Zone12 is a zone-restricted variant of e32dsv6, available only in
	// zones 1 and 2. It exercises per-mode zone eligibility.
	e32dsv6Zone12 = &skucache.SKUMetadata{
		Name:                     "Standard_E32ds_v6",
		Family:                   "standardEDSv6Family",
		VCPUs:                    32,
		MemoryGB:                 256,
		SecondaryNICs:            7,
		EphemeralOSDiskSupported: true,
		EphemeralDiskSizeGB:      1792,
		Zones:                    []string{"1", "2"},
	}

	// e8dsv6SmallDisk is an 8-core EDSv6 SKU whose ephemeral disk (64 GB) is too
	// small to hold a 128 GB OS disk, so allocateTier skips it and falls through
	// to the next family.
	e8dsv6SmallDisk = &skucache.SKUMetadata{
		Name:                     "Standard_E8ds_v6",
		Family:                   "standardEDSv6Family",
		VCPUs:                    8,
		MemoryGB:                 64,
		SecondaryNICs:            3,
		EphemeralOSDiskSupported: true,
		EphemeralDiskSizeGB:      64,
		Zones:                    allZones,
	}

	// d8dsv6 is an 8-core DDSv6 SKU with a disk large enough for a 128 GB OS
	// disk; it serves as the fallback family for e8dsv6SmallDisk.
	d8dsv6 = &skucache.SKUMetadata{
		Name:                     "Standard_D8ds_v6",
		Family:                   "standardDDSv6Family",
		VCPUs:                    8,
		MemoryGB:                 32,
		SecondaryNICs:            2,
		EphemeralOSDiskSupported: true,
		EphemeralDiskSizeGB:      300,
		Zones:                    allZones,
	}

	// e32NoEphemeral is a 32-core EDSv6 SKU without ephemeral OS disk support, so
	// BuildEligibleSKUIndex excludes it and no family has an eligible SKU.
	e32NoEphemeral = &skucache.SKUMetadata{
		Name:                     "Standard_E32ds_v6",
		Family:                   "standardEDSv6Family",
		VCPUs:                    32,
		MemoryGB:                 256,
		SecondaryNICs:            7,
		EphemeralOSDiskSupported: false,
		EphemeralDiskSizeGB:      0,
		Zones:                    allZones,
	}
)

func TestComputeDesiredPools(t *testing.T) {
	tests := []struct {
		name          string
		tiers         []TierConfig
		familyBudgets map[VMFamily]int64
		skuMetadata   map[string]*skucache.SKUMetadata
	}{
		{
			name: "single tier single family",
			tiers: []TierConfig{
				{Name: "wrk", Role: PoolRoleWorker, PoolMode: PoolModePerZone, Cores: 32, OSDiskSizeGB: 512, MaxNodes: 10, FamilyPriority: []VMFamily{"standardEDSv6Family"}, MaxPods: 225, PoolCount: 3, EnableSwift: true},
			},
			familyBudgets: map[VMFamily]int64{"standardEDSv6Family": 224},
			skuMetadata:   map[string]*skucache.SKUMetadata{"Standard_E32ds_v6": e32dsv6},
		},
		{
			name: "insufficient quota",
			tiers: []TierConfig{
				{Name: "wrk", Role: PoolRoleWorker, PoolMode: PoolModePerZone, Cores: 32, OSDiskSizeGB: 512, MaxNodes: 10, FamilyPriority: []VMFamily{"standardEDSv6Family"}, MaxPods: 225, PoolCount: 3},
			},
			familyBudgets: map[VMFamily]int64{"standardEDSv6Family": 0},
			skuMetadata:   map[string]*skucache.SKUMetadata{"Standard_E32ds_v6": e32dsv6},
		},
		{
			name: "multi role production like",
			tiers: []TierConfig{
				{Name: "sys", Role: PoolRoleSystem, PoolMode: PoolModeRegional, Cores: 8, OSDiskSizeGB: 128, MaxNodes: 3, FamilyPriority: []VMFamily{"standardEDSv6Family"}, MaxPods: 100, Taints: []string{TaintCriticalAddonsOnly}, PoolCount: 1},
				{Name: "inf", Role: PoolRoleInfra, PoolMode: PoolModePerZone, Cores: 32, OSDiskSizeGB: 128, MaxNodes: 1, FamilyPriority: []VMFamily{"standardEDSv6Family"}, MaxPods: 225, Taints: []string{TaintInfra}, PoolCount: 3},
				{Name: "wrk16", Role: PoolRoleWorker, PoolMode: PoolModePerZone, Cores: 16, OSDiskSizeGB: 256, MaxNodes: 2, FamilyPriority: []VMFamily{"standardEDSv6Family"}, MaxPods: 225, PoolCount: 3, EnableSwift: true},
				{Name: "wrk32", Role: PoolRoleWorker, PoolMode: PoolModePerZone, Cores: 32, OSDiskSizeGB: 512, MaxNodes: 8, FamilyPriority: []VMFamily{"standardEDSv6Family"}, MaxPods: 225, PoolCount: 3, EnableSwift: true},
			},
			familyBudgets: map[VMFamily]int64{"standardEDSv6Family": 1000},
			skuMetadata: map[string]*skucache.SKUMetadata{
				"Standard_E32ds_v6": e32dsv6,
				"Standard_E16ds_v6": e16dsv6,
				"Standard_E8ds_v6":  e8dsv6,
			},
		},
		{
			name: "family fallback",
			tiers: []TierConfig{
				{Name: "sys", Role: PoolRoleSystem, PoolMode: PoolModeRegional, Cores: 4, OSDiskSizeGB: 32, MaxNodes: 3, FamilyPriority: []VMFamily{"standardEDSv6Family", "standardDDSv6Family"}, MaxPods: 100, PoolCount: 1},
			},
			familyBudgets: map[VMFamily]int64{"standardEDSv6Family": 0, "standardDDSv6Family": 100},
			skuMetadata:   map[string]*skucache.SKUMetadata{"Standard_D4ds_v6": d4dsv6},
		},
		{
			name: "surge reservation",
			tiers: []TierConfig{
				{Name: "wrk16", Role: PoolRoleWorker, PoolMode: PoolModePerZone, Cores: 16, OSDiskSizeGB: 256, MaxNodes: 2, FamilyPriority: []VMFamily{"standardEDSv6Family"}, MaxPods: 225, PoolCount: 3},
				{Name: "wrk32", Role: PoolRoleWorker, PoolMode: PoolModePerZone, Cores: 32, OSDiskSizeGB: 512, MaxNodes: 8, FamilyPriority: []VMFamily{"standardEDSv6Family"}, MaxPods: 225, PoolCount: 3},
			},
			familyBudgets: map[VMFamily]int64{"standardEDSv6Family": 232},
			skuMetadata: map[string]*skucache.SKUMetadata{
				"Standard_E32ds_v6": e32dsv6,
				"Standard_E16ds_v6": e16dsv6,
			},
		},
		{
			name:          "no tiers",
			tiers:         []TierConfig{},
			familyBudgets: map[VMFamily]int64{"standardEDSv6Family": 100},
			skuMetadata:   map[string]*skucache.SKUMetadata{"Standard_E32ds_v6": e32dsv6},
		},
		{
			name: "regional sets no zones and accepts zone restricted sku",
			tiers: []TierConfig{
				{Name: "ovfl", Role: PoolRoleWorker, PoolMode: PoolModeRegional, Cores: 32, OSDiskSizeGB: 512, MaxNodes: 10, FamilyPriority: []VMFamily{"standardEDSv6Family"}, MaxPods: 225, PoolCount: 1, EnableSwift: true},
			},
			familyBudgets: map[VMFamily]int64{"standardEDSv6Family": 224},
			skuMetadata:   map[string]*skucache.SKUMetadata{"Standard_E32ds_v6": e32dsv6Zone12},
		},
		{
			name: "per zone poolcount two uses zone restricted sku",
			tiers: []TierConfig{
				{Name: "wrk", Role: PoolRoleWorker, PoolMode: PoolModePerZone, Cores: 32, OSDiskSizeGB: 512, MaxNodes: 10, FamilyPriority: []VMFamily{"standardEDSv6Family"}, MaxPods: 225, PoolCount: 2, EnableSwift: true},
			},
			familyBudgets: map[VMFamily]int64{"standardEDSv6Family": 224},
			skuMetadata:   map[string]*skucache.SKUMetadata{"Standard_E32ds_v6": e32dsv6Zone12},
		},
		{
			name: "per zone poolcount three rejects zone restricted sku",
			tiers: []TierConfig{
				{Name: "wrk", Role: PoolRoleWorker, PoolMode: PoolModePerZone, Cores: 32, OSDiskSizeGB: 512, MaxNodes: 10, FamilyPriority: []VMFamily{"standardEDSv6Family"}, MaxPods: 225, PoolCount: 3, EnableSwift: true},
			},
			familyBudgets: map[VMFamily]int64{"standardEDSv6Family": 224},
			skuMetadata:   map[string]*skucache.SKUMetadata{"Standard_E32ds_v6": e32dsv6Zone12},
		},
		{
			// family[0] (EDSv6) SKU has an ephemeral disk too small for the tier's
			// 128 GB OS disk, so it is skipped and allocation falls through to
			// family[1] (DDSv6), which is viable.
			name: "family fallback ephemeral disk too small",
			tiers: []TierConfig{
				{Name: "sys", Role: PoolRoleSystem, PoolMode: PoolModeRegional, Cores: 8, OSDiskSizeGB: 128, MaxNodes: 3, FamilyPriority: []VMFamily{"standardEDSv6Family", "standardDDSv6Family"}, MaxPods: 100, PoolCount: 1},
			},
			familyBudgets: map[VMFamily]int64{"standardEDSv6Family": 100, "standardDDSv6Family": 100},
			skuMetadata: map[string]*skucache.SKUMetadata{
				"Standard_E8ds_v6": e8dsv6SmallDisk,
				"Standard_D8ds_v6": d8dsv6,
			},
		},
		{
			// Empty family priority list yields a NoEligibleFamily failure.
			name: "no eligible family",
			tiers: []TierConfig{
				{Name: "wrk", Role: PoolRoleWorker, PoolMode: PoolModePerZone, Cores: 32, OSDiskSizeGB: 512, MaxNodes: 10, FamilyPriority: []VMFamily{}, MaxPods: 225, PoolCount: 3},
			},
			familyBudgets: map[VMFamily]int64{"standardEDSv6Family": 224},
			skuMetadata:   map[string]*skucache.SKUMetadata{"Standard_E32ds_v6": e32dsv6},
		},
		{
			// The only family's SKU lacks ephemeral OS disk support, so the SKU
			// index excludes it and no family has an eligible SKU: NoEligibleSKU.
			name: "no eligible sku",
			tiers: []TierConfig{
				{Name: "wrk", Role: PoolRoleWorker, PoolMode: PoolModePerZone, Cores: 32, OSDiskSizeGB: 512, MaxNodes: 10, FamilyPriority: []VMFamily{"standardEDSv6Family"}, MaxPods: 225, PoolCount: 3},
			},
			familyBudgets: map[VMFamily]int64{"standardEDSv6Family": 224},
			skuMetadata:   map[string]*skucache.SKUMetadata{"Standard_E32ds_v6": e32NoEphemeral},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			skuIndex := BuildEligibleSKUIndex(test.skuMetadata)
			pools, failures := ComputeDesiredPools(logr.Discard(), test.tiers, allZones, test.familyBudgets, skuIndex)

			result := desiredPoolsResult{Pools: pools, Failures: failures}
			b, err := json.MarshalIndent(result, "", "  ")
			require.NoError(t, err)
			assertGolden(t, string(b)+"\n")
		})
	}
}

// TestPoolName pins the name format: <symbolicName><zone><hash6>, where the
// leading segment is the tier's symbolic name and the trailing 6 hex chars hash
// the AKS-immutable fields. The exact hash values are pinned by the golden
// fixtures in TestComputeDesiredPools and TestResolveDesiredPools, which would
// catch any hash-algorithm drift; this test pins structure and field
// sensitivity, since a name change would make the controller treat existing AKS
// pools as unrecognized, creating duplicates and orphaning the originals.
func TestPoolName(t *testing.T) {
	name := poolName("wrk16", "1", "Standard_E16ds_v6", 256, 225, true)

	assert.Equal(t, "wrk161", name[:len("wrk16")+1], "name must start with symbolic name followed by the zone digit")
	assert.Len(t, name, len("wrk16")+1+6, "name must be <symbolicName><zone><hash6>")

	assert.Equal(t, name, poolName("wrk16", "1", "Standard_E16ds_v6", 256, 225, true), "poolName must be deterministic")

	// Each AKS-immutable field must alter the hash so the pool is renamed and
	// replaced when it changes.
	hash := func(n string) string { return n[len(n)-6:] }
	base := hash(name)
	assert.NotEqual(t, base, hash(poolName("wrk16", "1", "Standard_E32ds_v6", 256, 225, true)), "VMSize change must alter hash")
	assert.NotEqual(t, base, hash(poolName("wrk16", "1", "Standard_E16ds_v6", 512, 225, true)), "OSDiskSizeGB change must alter hash")
	assert.NotEqual(t, base, hash(poolName("wrk16", "1", "Standard_E16ds_v6", 256, 250, true)), "MaxPods change must alter hash")
	assert.NotEqual(t, base, hash(poolName("wrk16", "1", "Standard_E16ds_v6", 256, 225, false)), "EnableSwift change must alter hash")
}

// TestPoolName_SameSpecAcrossZonesSharesHashSuffix verifies that the hash
// portion of the name (used to detect identical pool specs) ignores zone, so
// per-zone pools of the same spec are recognized as the same spec.
func TestPoolName_SameSpecAcrossZonesSharesHashSuffix(t *testing.T) {
	zone1 := poolName("wrk", "1", "Standard_E16ds_v6", 100, 225, true)
	zone2 := poolName("wrk", "2", "Standard_E16ds_v6", 100, 225, true)

	assert.NotEqual(t, zone1, zone2, "names should differ by zone digit")
	assert.Equal(t, zone1[len(zone1)-6:], zone2[len(zone2)-6:], "hash suffix should be identical across zones for the same spec")
}

func TestSeedMinCount(t *testing.T) {
	tests := []struct {
		name     string
		minNodes int64
		maxCount int64
		want     int32
	}{
		{name: "zero means one", minNodes: 0, maxCount: 5, want: 1},
		{name: "one stays one", minNodes: 1, maxCount: 5, want: 1},
		{name: "within max preserved", minNodes: 3, maxCount: 5, want: 3},
		{name: "above max clamped", minNodes: 5, maxCount: 3, want: 3},
		{name: "equal to max", minNodes: 5, maxCount: 5, want: 5},
		{name: "clamped to single node", minNodes: 3, maxCount: 1, want: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, seedMinCount(tt.minNodes, tt.maxCount))
		})
	}
}
