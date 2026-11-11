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

package framework

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"net/http"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/runtime"

	"github.com/Azure/ARO-HCP/internal/api/coreapi"
	"github.com/Azure/ARO-HCP/test/util/timing"
)

// capacityRetryInterval is the minimum wait before a rejected cluster create is sent again.
const capacityRetryInterval = time.Minute

// isCapacityRejection reports whether the RP rejected a cluster create because
// regional capacity is exhausted. Nothing was persisted, so the create is safe
// to send again.
func isCapacityRejection(err error) bool {
	var responseError *azcore.ResponseError
	return errors.As(err, &responseError) && responseError.StatusCode == http.StatusBadRequest && responseError.ErrorCode == coreapi.CloudErrorCodeCapacityExceeded
}

// capacityRetryDelay adds up to 50% jitter so that creates rejected together
// do not return in one wave and overshoot the limit together.
func capacityRetryDelay() time.Duration {
	return capacityRetryInterval + time.Duration(rand.Int64N(int64(capacityRetryInterval/2)+1))
}

// operationBudget preserves one operation deadline across request submission,
// SDK retries, recovery and polling. Only explicit quota backoff extends it.
// It is owned by the synchronous create helper, never shared between goroutines.
type operationBudget struct {
	deadline time.Time
}

func newOperationBudget(timeout time.Duration) *operationBudget {
	budget := &operationBudget{}
	if timeout > 0 {
		budget.deadline = time.Now().Add(timeout)
	}
	return budget
}

func (b *operationBudget) context(ctx context.Context) (context.Context, context.CancelFunc) {
	if b.deadline.IsZero() {
		return context.WithCancel(ctx)
	}
	return context.WithDeadlineCause(ctx, b.deadline, fmt.Errorf("operation timeout exceeded: %w", context.DeadlineExceeded))
}

func (b *operationBudget) remaining(ctx context.Context) (time.Duration, error) {
	if ctx.Err() != nil {
		return 0, errors.Join(ctx.Err(), context.Cause(ctx))
	}
	if b.deadline.IsZero() {
		return 0, nil
	}
	remaining := time.Until(b.deadline)
	if remaining <= 0 {
		return 0, fmt.Errorf("operation timeout exceeded: %w", context.DeadlineExceeded)
	}
	return remaining, nil
}

func waitForCapacity[T any](ctx context.Context, budget *operationBudget, timeout time.Duration, retryable func(error) bool, create func(context.Context) (T, error)) (T, error) {
	recorder := beginOperation()
	remainingWait := timeout
	var zero T
	var lastErr error
	for {
		if _, err := budget.remaining(ctx); err != nil {
			return zero, errors.Join(err, lastErr)
		}
		operationCtx, cancelOperation := budget.context(ctx)
		result, err := create(operationCtx)
		operationErr := errors.Join(operationCtx.Err(), context.Cause(operationCtx))
		cancelOperation()
		if operationErr != nil {
			return zero, errors.Join(operationErr, err)
		}
		if err == nil || !retryable(err) {
			return result, err
		}
		lastErr = err
		waitCtx, cancelWait := context.WithTimeoutCause(ctx, remainingWait, errors.New("timed out waiting for regional HCP cluster capacity"))
		start := time.Now()
		timer := time.NewTimer(capacityRetryDelay())
		select {
		case <-waitCtx.Done():
			timer.Stop()
		case <-timer.C:
		}
		waited := time.Since(start)
		remainingWait -= waited
		if !budget.deadline.IsZero() {
			budget.deadline = budget.deadline.Add(waited)
		}
		waitErr := errors.Join(waitCtx.Err(), context.Cause(waitCtx))
		cancelWait()
		recorder.Record(timing.KindCapacityWait, "Backoff for regional SWIFT/minimal cluster capacity", start, start.Add(waited), waitErr)
		if waitErr != nil {
			return zero, fmt.Errorf("waiting for regional HCP cluster capacity: %w", errors.Join(waitErr, lastErr))
		}
		if remainingWait <= 0 {
			return zero, fmt.Errorf("quota backoff budget exhausted: %w", errors.Join(context.DeadlineExceeded, lastErr))
		}
	}
}

func capacityOnly(e coreapi.CloudErrorBody) bool {
	if len(e.Details) == 0 {
		return e.Code == coreapi.CloudErrorCodeCapacityExceeded
	}
	for _, detail := range e.Details {
		if !capacityOnly(detail) {
			return false
		}
	}
	return true
}

// A failed deployment wraps the RP rejection. Require a structured error tree
// whose leaves are all capacity rejections; messages mentioning capacity and
// mixed failures are not sufficient.
func isDeploymentCapacityRejection(err error) bool {
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		for _, cause := range joined.Unwrap() {
			if !isDeploymentCapacityRejection(cause) {
				return false
			}
		}
		return len(joined.Unwrap()) > 0
	}
	if wrapped, ok := err.(interface{ Unwrap() error }); ok {
		return isDeploymentCapacityRejection(wrapped.Unwrap())
	}
	var responseError *azcore.ResponseError
	if !errors.As(err, &responseError) {
		return false
	}
	if responseError.RawResponse == nil {
		return isCapacityRejection(err)
	}
	payload, err := runtime.Payload(responseError.RawResponse)
	if err != nil {
		return false
	}
	var envelope struct {
		Error      coreapi.CloudErrorBody `json:"error"`
		Properties struct {
			Error coreapi.CloudErrorBody `json:"error"`
		} `json:"properties"`
	}
	if json.Unmarshal(payload, &envelope) != nil {
		return false
	}
	if len(envelope.Error.Code) > 0 {
		return capacityOnly(envelope.Error) && (len(envelope.Properties.Error.Code) == 0 || capacityOnly(envelope.Properties.Error))
	}
	return capacityOnly(envelope.Properties.Error)
}

func deployWithCapacityWait[T any](ctx context.Context, timeout time.Duration, description string, begin func(context.Context) (*runtime.Poller[T], error), getState func(context.Context) (string, error)) (T, error) {
	// A transient polling rejection does not mean the deployment failed.
	// Only terminal, capacity-only failures allow another deployment PUT.
	budget := newOperationBudget(timeout)
	if timeout <= 0 {
		budget.deadline = time.Now().Add(timeout)
	}
	canRetry := false
	return waitForCapacity(ctx, budget, ClusterCapacityWaitTimeout, func(err error) bool {
		return canRetry && isDeploymentCapacityRejection(err)
	}, func(capacityCtx context.Context) (T, error) {
		var zero T
		canRetry = false
		poller, err := beginDeploymentWithResubmit(capacityCtx, description, begin, getState)
		if err != nil {
			return zero, err
		}
		result, err := poller.PollUntilDone(capacityCtx, &runtime.PollUntilDoneOptions{Frequency: StandardPollInterval})
		canRetry = poller.Done()
		return result, err
	})
}
