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
	"errors"
	"fmt"
	"time"

	"github.com/go-logr/logr"

	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	utilsclock "k8s.io/utils/clock"

	azcorearm "github.com/Azure/azure-sdk-for-go/sdk/azcore/arm"

	"github.com/Azure/ARO-HCP/internal/api"
	"github.com/Azure/ARO-HCP/internal/api/arm"
	fleetapi "github.com/Azure/ARO-HCP/internal/api/fleet"
	"github.com/Azure/ARO-HCP/internal/controllerutils"
	"github.com/Azure/ARO-HCP/internal/utils"
)

const DefaultManagementClusterCooldownPeriod = 10 * time.Minute

// ManagementClusterKey identifies a ManagementCluster in the workqueue.
// Since each Stamp has exactly one ManagementCluster (named "default"),
// the stamp identifier is sufficient to uniquely identify the MC.
type ManagementClusterKey struct {
	StampIdentifier string
}

func (k ManagementClusterKey) String() string {
	return k.StampIdentifier
}

func (k ManagementClusterKey) GetResourceID() *azcorearm.ResourceID {
	return api.Must(fleetapi.ToManagementClusterResourceID(k.StampIdentifier))
}

func (k ManagementClusterKey) AddLoggerValues(logger logr.Logger) logr.Logger {
	return logger.WithValues(
		utils.LogValues{}.
			AddLogValuesForResourceID(k.GetResourceID())...)
}

type stampScoped interface {
	GetStampIdentifier() string
}

// ManagementClusterKeyFromObject extracts the workqueue key from any fleet
// object that carries a stamp identifier (*fleet.ManagementCluster, *fleet.Stamp).
func ManagementClusterKeyFromObject(obj any) (ManagementClusterKey, error) {
	s, ok := obj.(stampScoped)
	if !ok {
		return ManagementClusterKey{}, fmt.Errorf("object %T does not implement stampScoped", obj)
	}
	id := s.GetStampIdentifier()
	if len(id) == 0 {
		return ManagementClusterKey{}, fmt.Errorf("object %T has empty stamp identifier", obj)
	}
	return ManagementClusterKey{StampIdentifier: id}, nil
}

// ManagementClusterSyncer is the interface that concrete controllers implement.
type ManagementClusterSyncer interface {
	SyncOnce(ctx context.Context, key ManagementClusterKey) error
}

// Notifier is the subset of cache.SharedIndexInformer needed to register
// event handlers. Any informer satisfies this interface.
type Notifier interface {
	AddEventHandlerWithOptions(handler cache.ResourceEventHandler, options cache.HandlerOptions) (cache.ResourceEventHandlerRegistration, error)
}

// ManagementClusterWatchingControllerConfig tunes the controller's cooldown behavior.
type ManagementClusterWatchingControllerConfig struct {
	CooldownPeriod time.Duration
	Clock          utilsclock.PassiveClock
}

func (c ManagementClusterWatchingControllerConfig) withDefaults() ManagementClusterWatchingControllerConfig {
	if c.CooldownPeriod == 0 {
		c.CooldownPeriod = DefaultManagementClusterCooldownPeriod
	}
	if c.Clock == nil {
		c.Clock = utilsclock.RealClock{}
	}
	return c
}

// ManagementClusterWatchingController is a controller base that watches
// fleet informers, handles etag-based change detection with cooldown gating,
// and delegates reconciliation to a ManagementClusterSyncer.
type ManagementClusterWatchingController struct {
	name     string
	syncer   ManagementClusterSyncer
	queue    workqueue.TypedRateLimitingInterface[ManagementClusterKey]
	cooldown controllerutils.CooldownChecker
}

// NewManagementClusterWatchingController creates a controller and delegates
// reconciliation to the syncer. Call QueueForInformers to register informers.
func NewManagementClusterWatchingController(
	name string,
	syncer ManagementClusterSyncer,
	cfg ManagementClusterWatchingControllerConfig,
) *ManagementClusterWatchingController {
	cfg = cfg.withDefaults()
	cooldownChecker := controllerutils.NewTimeBasedCooldownChecker(cfg.CooldownPeriod)
	cooldownChecker.SetClock(cfg.Clock)

	return &ManagementClusterWatchingController{
		name:   name,
		syncer: syncer,
		queue: workqueue.NewTypedRateLimitingQueueWithConfig(
			workqueue.DefaultTypedControllerRateLimiter[ManagementClusterKey](),
			workqueue.TypedRateLimitingQueueConfig[ManagementClusterKey]{Name: name},
		),
		cooldown: cooldownChecker,
	}
}

// QueueForInformers registers notifiers whose objects feed into the workqueue.
// Objects must implement both stampScoped (for key extraction) and
// arm.CosmosPersistable (for etag-based change detection). Add events enqueue
// immediately. Update events enqueue immediately when the Cosmos etag changed
// and consult the cooldown gate otherwise.
func (c *ManagementClusterWatchingController) QueueForInformers(resyncDuration time.Duration, notifiers ...Notifier) error {
	errs := []error{}
	for _, notifier := range notifiers {
		_, err := notifier.AddEventHandlerWithOptions(
			cache.ResourceEventHandlerFuncs{
				AddFunc:    c.handleAdd,
				UpdateFunc: c.handleUpdate,
			},
			cache.HandlerOptions{
				ResyncPeriod: &resyncDuration,
			},
		)
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

func (c *ManagementClusterWatchingController) handleAdd(obj any) {
	c.enqueue(obj, true)
}

func (c *ManagementClusterWatchingController) handleUpdate(oldObj, newObj any) {
	oldPersistable, oldOK := oldObj.(arm.CosmosPersistable)
	newPersistable, newOK := newObj.(arm.CosmosPersistable)
	if !oldOK || !newOK {
		utilruntime.HandleError(fmt.Errorf("update handler: expected CosmosPersistable, got old=%T new=%T", oldObj, newObj))
		return
	}
	changed := oldPersistable.GetCosmosData().GetEtag() != newPersistable.GetCosmosData().GetEtag()
	c.enqueue(newObj, changed)
}

// enqueue extracts the ManagementClusterKey and adds it to the workqueue.
// When changed is true, the key is enqueued immediately. Otherwise, the
// cooldown gate decides.
func (c *ManagementClusterWatchingController) enqueue(obj any, changed bool) {
	key, err := ManagementClusterKeyFromObject(obj)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("enqueue key extraction: %w", err))
		return
	}
	if changed {
		c.queue.Add(key)
		return
	}
	if !c.cooldown.CanSync(context.TODO(), key) {
		return
	}
	c.queue.Add(key)
}

func (c *ManagementClusterWatchingController) Run(ctx context.Context, threadiness int) {
	defer utilruntime.HandleCrash()
	defer c.queue.ShutDown()

	ctx = utils.ContextWithControllerName(ctx, c.name)
	logger := utils.LoggerFromContext(ctx).WithValues(utils.LogValues{}.AddControllerName(c.name)...)
	ctx = utils.ContextWithLogger(ctx, logger)
	logger.Info("starting controller")
	defer logger.Info("stopped controller")

	for i := 0; i < threadiness; i++ {
		go wait.UntilWithContext(ctx, c.runWorker, time.Second)
	}
	<-ctx.Done()
}

func (c *ManagementClusterWatchingController) runWorker(ctx context.Context) {
	for c.processNext(ctx) {
	}
}

func (c *ManagementClusterWatchingController) processNext(ctx context.Context) bool {
	key, shutdown := c.queue.Get()
	if shutdown {
		return false
	}
	defer c.queue.Done(key)

	logger := key.AddLoggerValues(utils.LoggerFromContext(ctx))
	ctx = utils.ContextWithLogger(ctx, logger)

	ReconcileTotal.WithLabelValues(c.name).Inc()
	if err := c.syncer.SyncOnce(ctx, key); err != nil {
		utilruntime.HandleErrorWithContext(ctx, err, "sync error; requeuing", "key", key)
		c.queue.AddRateLimited(key)
		return true
	}
	c.queue.Forget(key)
	return true
}
