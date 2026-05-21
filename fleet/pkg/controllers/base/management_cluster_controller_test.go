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

package base

import (
	"context"
	"fmt"
	"testing"
	"time"

	"k8s.io/client-go/util/workqueue"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"

	"github.com/Azure/ARO-HCP/internal/api"
	fleetapi "github.com/Azure/ARO-HCP/internal/api/fleet"
	"github.com/Azure/ARO-HCP/internal/controllerutils"
)

func TestManagementClusterKeyFromObject(t *testing.T) {
	tests := []struct {
		name      string
		obj       any
		wantKey   ManagementClusterKey
		wantError bool
	}{
		{
			name: "ManagementCluster",
			obj: func() *fleetapi.ManagementCluster {
				rid, _ := fleetapi.ToManagementClusterResourceID("abc")
				return &fleetapi.ManagementCluster{
					CosmosMetadata: api.CosmosMetadata{ResourceID: rid},
				}
			}(),
			wantKey: ManagementClusterKey{StampIdentifier: "abc"},
		},
		{
			name: "Stamp",
			obj: func() *fleetapi.Stamp {
				rid, _ := fleetapi.ToStampResourceID("xyz")
				return &fleetapi.Stamp{
					CosmosMetadata: api.CosmosMetadata{ResourceID: rid},
				}
			}(),
			wantKey: ManagementClusterKey{StampIdentifier: "xyz"},
		},
		{
			name:      "wrong type",
			obj:       "not a fleet object",
			wantError: true,
		},
		{
			name: "ManagementCluster with nil resource ID",
			obj: &fleetapi.ManagementCluster{
				CosmosMetadata: api.CosmosMetadata{ResourceID: nil},
			},
			wantError: true,
		},
		{
			name: "Stamp with nil resource ID",
			obj: &fleetapi.Stamp{
				CosmosMetadata: api.CosmosMetadata{ResourceID: nil},
			},
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key, err := ManagementClusterKeyFromObject(tt.obj)
			if tt.wantError {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if key != tt.wantKey {
				t.Errorf("got key %v, want %v", key, tt.wantKey)
			}
		})
	}
}

func TestManagementClusterKeyGetResourceID(t *testing.T) {
	key := ManagementClusterKey{StampIdentifier: "s1"}
	rid := key.GetResourceID()
	if rid == nil {
		t.Fatal("expected non-nil resource ID")
	}
	want := "/providers/microsoft.redhatopenshift/stamps/s1/managementclusters/default"
	if rid.String() != want {
		t.Errorf("got %q, want %q", rid.String(), want)
	}
}

func TestHandleAdd(t *testing.T) {
	queue := workqueue.NewTypedRateLimitingQueue(workqueue.DefaultTypedControllerRateLimiter[ManagementClusterKey]())
	defer queue.ShutDown()

	controller := &ManagementClusterWatchingController{queue: queue}

	rid, _ := fleetapi.ToManagementClusterResourceID("s1")
	managementCluster := &fleetapi.ManagementCluster{
		CosmosMetadata: api.CosmosMetadata{ResourceID: rid},
	}

	controller.handleAdd(managementCluster)

	if queue.Len() != 1 {
		t.Fatalf("expected queue length 1, got %d", queue.Len())
	}
	key, _ := queue.Get()
	if key.StampIdentifier != "s1" {
		t.Errorf("got %q, want %q", key.StampIdentifier, "s1")
	}
}

func TestHandleAdd_Stamp(t *testing.T) {
	queue := workqueue.NewTypedRateLimitingQueue(workqueue.DefaultTypedControllerRateLimiter[ManagementClusterKey]())
	defer queue.ShutDown()

	controller := &ManagementClusterWatchingController{queue: queue}

	rid, _ := fleetapi.ToStampResourceID("s2")
	stamp := &fleetapi.Stamp{
		CosmosMetadata: api.CosmosMetadata{ResourceID: rid},
	}

	controller.handleAdd(stamp)

	if queue.Len() != 1 {
		t.Fatalf("expected queue length 1, got %d", queue.Len())
	}
	key, _ := queue.Get()
	if key.StampIdentifier != "s2" {
		t.Errorf("got %q, want %q", key.StampIdentifier, "s2")
	}
}

func TestHandleAdd_InvalidObject(t *testing.T) {
	queue := workqueue.NewTypedRateLimitingQueue(workqueue.DefaultTypedControllerRateLimiter[ManagementClusterKey]())
	defer queue.ShutDown()

	controller := &ManagementClusterWatchingController{queue: queue}

	controller.handleAdd("not a fleet object")

	if queue.Len() != 0 {
		t.Fatalf("expected queue length 0 for invalid object, got %d", queue.Len())
	}
}

func testManagementCluster(stampID string, etag azcore.ETag) *fleetapi.ManagementCluster {
	rid, _ := fleetapi.ToManagementClusterResourceID(stampID)
	mc := &fleetapi.ManagementCluster{
		CosmosMetadata: api.CosmosMetadata{ResourceID: rid},
	}
	mc.SetEtag(etag)
	return mc
}

func TestHandleUpdate_EtagChanged(t *testing.T) {
	queue := workqueue.NewTypedRateLimitingQueue(workqueue.DefaultTypedControllerRateLimiter[ManagementClusterKey]())
	defer queue.ShutDown()

	cooldown := controllerutils.NewTimeBasedCooldownChecker(1 * time.Hour)
	controller := &ManagementClusterWatchingController{queue: queue, cooldown: cooldown}

	oldMC := testManagementCluster("s1", "etag-1")
	newMC := testManagementCluster("s1", "etag-2")

	controller.handleUpdate(oldMC, newMC)

	if queue.Len() != 1 {
		t.Fatalf("expected queue length 1 for etag change, got %d", queue.Len())
	}
}

func TestHandleUpdate_EtagUnchanged_CooldownRejects(t *testing.T) {
	queue := workqueue.NewTypedRateLimitingQueue(workqueue.DefaultTypedControllerRateLimiter[ManagementClusterKey]())
	defer queue.ShutDown()

	cooldown := controllerutils.NewTimeBasedCooldownChecker(1 * time.Hour)
	controller := &ManagementClusterWatchingController{queue: queue, cooldown: cooldown}

	mc := testManagementCluster("s1", "etag-1")

	// First call passes cooldown, second is rejected.
	controller.handleUpdate(mc, mc)
	if queue.Len() != 1 {
		t.Fatalf("expected first call to enqueue, got queue length %d", queue.Len())
	}
	queue.Get() // drain
	queue.Done(ManagementClusterKey{StampIdentifier: "s1"})

	controller.handleUpdate(mc, mc)
	if queue.Len() != 0 {
		t.Fatalf("expected second call to be rejected by cooldown, got queue length %d", queue.Len())
	}
}

func TestHandleUpdate_InvalidObjects(t *testing.T) {
	queue := workqueue.NewTypedRateLimitingQueue(workqueue.DefaultTypedControllerRateLimiter[ManagementClusterKey]())
	defer queue.ShutDown()

	cooldown := controllerutils.NewTimeBasedCooldownChecker(0)
	controller := &ManagementClusterWatchingController{queue: queue, cooldown: cooldown}

	controller.handleUpdate("not a MC", "also not a MC")

	if queue.Len() != 0 {
		t.Fatalf("expected queue length 0 for invalid objects, got %d", queue.Len())
	}
}

type fakeSyncer struct {
	called []ManagementClusterKey
	err    error
}

func (f *fakeSyncer) SyncOnce(_ context.Context, key ManagementClusterKey) error {
	f.called = append(f.called, key)
	return f.err
}

func TestProcessNext_Success(t *testing.T) {
	queue := workqueue.NewTypedRateLimitingQueue(workqueue.DefaultTypedControllerRateLimiter[ManagementClusterKey]())
	defer queue.ShutDown()

	syncer := &fakeSyncer{}
	controller := &ManagementClusterWatchingController{
		name:   "test",
		syncer: syncer,
		queue:  queue,
	}

	queue.Add(ManagementClusterKey{StampIdentifier: "s1"})

	ctx := context.Background()
	ok := controller.processNext(ctx)
	if !ok {
		t.Fatal("expected processNext to return true")
	}
	if len(syncer.called) != 1 {
		t.Fatalf("expected 1 SyncOnce call, got %d", len(syncer.called))
	}
	if syncer.called[0].StampIdentifier != "s1" {
		t.Errorf("got %q, want %q", syncer.called[0].StampIdentifier, "s1")
	}
}

func TestProcessNext_Error_ReturnsTrue(t *testing.T) {
	queue := workqueue.NewTypedRateLimitingQueue(workqueue.DefaultTypedControllerRateLimiter[ManagementClusterKey]())
	defer queue.ShutDown()

	syncer := &fakeSyncer{err: fmt.Errorf("sync failed")}
	controller := &ManagementClusterWatchingController{
		name:   "test",
		syncer: syncer,
		queue:  queue,
	}

	queue.Add(ManagementClusterKey{StampIdentifier: "s1"})

	ctx := context.Background()
	ok := controller.processNext(ctx)
	if !ok {
		t.Fatal("expected processNext to return true (continues processing)")
	}
	if len(syncer.called) != 1 {
		t.Fatalf("expected 1 SyncOnce call, got %d", len(syncer.called))
	}
}
