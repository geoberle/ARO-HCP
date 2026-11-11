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
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/go-logr/logr"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	azcorearm "github.com/Azure/azure-sdk-for-go/sdk/azcore/arm"
	azfake "github.com/Azure/azure-sdk-for-go/sdk/azcore/fake"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/runtime"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/resources/armresources"

	"github.com/Azure/ARO-HCP/internal/api/coreapi"
	sdk20240610 "github.com/Azure/ARO-HCP/test/sdk/v20240610preview/resourcemanager/redhatopenshifthcp/armredhatopenshifthcp"
	sdk20251223 "github.com/Azure/ARO-HCP/test/sdk/v20251223preview/resourcemanager/redhatopenshifthcp/armredhatopenshifthcp"
	sdk20260630 "github.com/Azure/ARO-HCP/test/sdk/v20260630preview/resourcemanager/redhatopenshifthcp/armredhatopenshifthcp"
	sdk20260901 "github.com/Azure/ARO-HCP/test/sdk/v20260901preview/resourcemanager/redhatopenshifthcp/armredhatopenshifthcp"
	sdk20261001 "github.com/Azure/ARO-HCP/test/sdk/v20261001/resourcemanager/redhatopenshifthcp/armredhatopenshifthcp"
	"github.com/Azure/ARO-HCP/test/util/timing"
)

func capacityHTTPResponse(req *http.Request, status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body)), Request: req}
}

func TestClusterCapacityWaitPreservesProvisioningTimeout(t *testing.T) {
	creates := map[string]func(context.Context, *azcorearm.ClientOptions) error{
		"20240610": func(ctx context.Context, options *azcorearm.ClientOptions) error {
			client, err := sdk20240610.NewHcpOpenShiftClustersClient(fakeSubscriptionID, &azfake.TokenCredential{}, options)
			if err != nil {
				return err
			}
			_, err = CreateHCPClusterAndWait20240610(ctx, logr.Discard(), client, "rg", "cluster", sdk20240610.HcpOpenShiftCluster{Properties: &sdk20240610.HcpOpenShiftClusterProperties{Version: &sdk20240610.VersionProfile{}}}, time.Second)
			return err
		},
		"20251223": func(ctx context.Context, options *azcorearm.ClientOptions) error {
			client, err := sdk20251223.NewHcpOpenShiftClustersClient(fakeSubscriptionID, &azfake.TokenCredential{}, options)
			if err != nil {
				return err
			}
			_, err = CreateHCPClusterAndWait20251223(ctx, logr.Discard(), client, "rg", "cluster", sdk20251223.HcpOpenShiftCluster{Properties: &sdk20251223.HcpOpenShiftClusterProperties{Version: &sdk20251223.VersionProfile{}}}, time.Second)
			return err
		},
		"20260630": func(ctx context.Context, options *azcorearm.ClientOptions) error {
			client, err := sdk20260630.NewHcpOpenShiftClustersClient(fakeSubscriptionID, &azfake.TokenCredential{}, options)
			if err != nil {
				return err
			}
			_, err = CreateHCPClusterAndWait20260630(ctx, logr.Discard(), client, "rg", "cluster", sdk20260630.HcpOpenShiftCluster{Properties: &sdk20260630.HcpOpenShiftClusterProperties{Version: &sdk20260630.VersionProfile{}}}, time.Second)
			return err
		},
		"20260901": func(ctx context.Context, options *azcorearm.ClientOptions) error {
			client, err := sdk20260901.NewHcpOpenShiftClustersClient(fakeSubscriptionID, &azfake.TokenCredential{}, options)
			if err != nil {
				return err
			}
			_, err = CreateHCPClusterAndWait20260901(ctx, logr.Discard(), client, "rg", "cluster", sdk20260901.HcpOpenShiftCluster{Properties: &sdk20260901.HcpOpenShiftClusterProperties{Version: &sdk20260901.VersionProfile{}}}, time.Second)
			return err
		},
		"20261001": func(ctx context.Context, options *azcorearm.ClientOptions) error {
			client, err := sdk20261001.NewHcpOpenShiftClustersClient(fakeSubscriptionID, &azfake.TokenCredential{}, options)
			if err != nil {
				return err
			}
			_, err = CreateHCPClusterAndWait20261001(ctx, logr.Discard(), client, "rg", "cluster", sdk20261001.HcpOpenShiftCluster{}, time.Second)
			return err
		},
		"begin 20240610": func(ctx context.Context, options *azcorearm.ClientOptions) error {
			client, err := sdk20240610.NewHcpOpenShiftClustersClient(fakeSubscriptionID, &azfake.TokenCredential{}, options)
			if err != nil {
				return err
			}
			poller, err := BeginCreateHCPCluster20240610(ctx, logr.Discard(), client, "rg", "cluster", ClusterParams20240610{}, "uksouth")
			if err != nil {
				return err
			}
			_, err = poller.PollUntilDone(ctx, nil)
			return err
		},
	}
	for name, create := range creates {
		t.Run(name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				puts := 0
				start := time.Now()
				transport := &fakeTransport{do: func(req *http.Request) (*http.Response, error) {
					if req.Method == http.MethodPut {
						puts++
						if puts == 1 {
							return capacityHTTPResponse(req, http.StatusBadRequest, `{"error":{"code":"AROHCPCapacityExceeded"}}`), nil
						}
					}
					return capacityHTTPResponse(req, http.StatusOK, `{"name":"cluster","location":"uksouth","properties":{"provisioningState":"Succeeded"}}`), nil
				}}
				if err := create(context.Background(), fakeClientOptions(transport)); err != nil {
					t.Fatalf("create after capacity rejection: %v", err)
				}
				if puts != 2 || time.Since(start) < time.Minute || time.Since(start) > 91*time.Second {
					t.Fatalf("PUTs=%d elapsed=%v, want two PUTs separated by the retry interval plus jitter", puts, time.Since(start))
				}
			})
		})
	}
}

func TestCapacityWaitBoundsAndCancellation(t *testing.T) {
	for _, tc := range []struct {
		name             string
		capacityTimeout  time.Duration
		parentTimeout    time.Duration
		cancelDuringWait bool
		wantElapsed      time.Duration
		wantErr          error
	}{
		{name: "capacity exhaustion", capacityTimeout: 12 * time.Second, wantElapsed: 12 * time.Second, wantErr: context.DeadlineExceeded},
		{name: "parent deadline", capacityTimeout: time.Hour, parentTimeout: 3 * time.Second, wantElapsed: 3 * time.Second, wantErr: context.DeadlineExceeded},
		{name: "parent cancellation", capacityTimeout: time.Hour, cancelDuringWait: true, wantElapsed: 2 * time.Second, wantErr: context.Canceled},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				if tc.parentTimeout > 0 {
					var deadlineCancel context.CancelFunc
					ctx, deadlineCancel = context.WithTimeout(ctx, tc.parentTimeout)
					defer deadlineCancel()
				}
				if tc.cancelDuringWait {
					go func() { time.Sleep(2 * time.Second); cancel() }()
				}
				attempts := 0
				start := time.Now()
				_, err := waitForCapacity(ctx, newOperationBudget(0), tc.capacityTimeout, isCapacityRejection, func(context.Context) (int, error) {
					attempts++
					return 0, &azcore.ResponseError{StatusCode: http.StatusBadRequest, ErrorCode: coreapi.CloudErrorCodeCapacityExceeded}
				})
				if !errors.Is(err, tc.wantErr) || attempts != 1 || time.Since(start) != tc.wantElapsed {
					t.Fatalf("attempts=%d elapsed=%v error=%v; want one attempt, elapsed=%v, error=%v", attempts, time.Since(start), err, tc.wantElapsed, tc.wantErr)
				}
			})
		})
	}
}

func TestCapacityWaitReturnsUnrelatedErrors(t *testing.T) {
	for _, original := range []error{
		&azcore.ResponseError{StatusCode: http.StatusTooManyRequests, ErrorCode: "Throttled"},
		&azcore.ResponseError{StatusCode: http.StatusBadRequest, ErrorCode: coreapi.CloudErrorCodeCapacityHeavyUse},
		&azcore.ResponseError{StatusCode: http.StatusBadRequest, ErrorCode: coreapi.CloudErrorCodeInvalidRequestContent},
		errors.New("connection lost"),
	} {
		attempts := 0
		_, err := waitForCapacity(context.Background(), newOperationBudget(0), time.Minute, isCapacityRejection, func(context.Context) (int, error) {
			attempts++
			return 0, original
		})
		if err != original || attempts != 1 {
			t.Fatalf("attempts=%d error=%v, want original error without resubmission", attempts, err)
		}
	}
}

func TestCapacityWaitRetriesAfterUnknownOutcome(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, tracker := withRequestAttemptTracker(context.Background())
		tracker.outcomeUnknown.Store(true)
		attempts := 0
		result, err := waitForCapacity(ctx, newOperationBudget(0), 2*time.Minute, isCapacityRejection, func(context.Context) (int, error) {
			attempts++
			if attempts == 1 {
				return 0, &azcore.ResponseError{StatusCode: http.StatusBadRequest, ErrorCode: coreapi.CloudErrorCodeCapacityExceeded}
			}
			return 1, nil
		})
		if err != nil || result != 1 || attempts != 2 {
			t.Fatalf("attempts=%d result=%d error=%v, want capacity backoff to continue after an earlier unknown outcome", attempts, result, err)
		}
	})
}

func TestDeploymentCapacityRejectionClassification(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		want       bool
	}{
		{"terminal", `{"error":{"code":"DeploymentFailed","details":[{"code":"ResourceDeploymentFailure","details":[{"code":"AROHCPCapacityExceeded"}]}]}}`, true},
		{"resource terminal", `{"properties":{"error":{"code":"DeploymentFailed","details":[{"code":"AROHCPCapacityExceeded"}]}}}`, true},
		{"multiple resources", `{"error":{"code":"DeploymentFailed","details":[{"code":"MultipleErrorsOccurred","details":[{"code":"AROHCPCapacityExceeded"},{"code":"AROHCPCapacityExceeded"}]}]}}`, true},
		{"unrecognized wrapper", `{"error":{"code":"UnknownFailure","details":[{"code":"AROHCPCapacityExceeded"}]}}`, true},
		{"mixed", `{"error":{"code":"DeploymentFailed","details":[{"code":"AROHCPCapacityExceeded"},{"code":"AuthorizationFailed"}]}}`, false},
		{"mixed nested", `{"error":{"code":"DeploymentFailed","details":[{"code":"MultipleErrorsOccurred","details":[{"code":"AROHCPCapacityExceeded"},{"code":"InvalidRequestContent"}]}]}}`, false},
		{"backend capacity", `{"error":{"code":"DeploymentFailed","details":[{"code":"AROHCPCapacityHeavyUse"}]}}`, false},
		{"message only", `{"error":{"code":"DeploymentFailed","message":"AROHCPCapacityExceeded"}}`, false},
		{"malformed", `{`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := runtime.NewResponseError(capacityHTTPResponse(nil, http.StatusBadRequest, tc.body))
			if got := isDeploymentCapacityRejection(err); got != tc.want {
				t.Fatalf("classification=%v, want %v: %v", got, tc.want, err)
			}
			if got := isDeploymentCapacityRejection(fmt.Errorf("wrapped: %w", errors.Join(err, errors.New("unrelated failure")))); got {
				t.Fatal("joined unrelated error was classified as capacity-only")
			}
		})
	}
}

func TestDeploymentCapacityWait(t *testing.T) {
	for _, tc := range []struct {
		name             string
		mixed, transient bool
		wantPuts         int
		wantSuccess      bool
	}{
		{name: "terminal capacity then success", wantPuts: 2, wantSuccess: true},
		{name: "mixed terminal failure", mixed: true, wantPuts: 1},
		{name: "transient polling capacity", transient: true, wantPuts: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				puts := 0
				transport := &fakeTransport{do: func(req *http.Request) (*http.Response, error) {
					failure := `{"code":"DeploymentFailed","details":[{"code":"AROHCPCapacityExceeded"}]}`
					if tc.mixed {
						failure = `{"code":"DeploymentFailed","details":[{"code":"AROHCPCapacityExceeded"},{"code":"AuthorizationFailed"}]}`
					}
					if req.Method == http.MethodPut {
						puts++
						if puts > 1 {
							return capacityHTTPResponse(req, http.StatusOK, `{"name":"deployment","properties":{"provisioningState":"Succeeded"}}`), nil
						}
						resp := capacityHTTPResponse(req, http.StatusCreated, `{"properties":{"provisioningState":"Accepted"}}`)
						resp.Header.Set("Azure-AsyncOperation", "https://management.azure.com/operations/deployment")
						return resp, nil
					}
					if tc.transient {
						return capacityHTTPResponse(req, http.StatusBadRequest, `{"error":`+failure+`}`), nil
					}
					return capacityHTTPResponse(req, http.StatusOK, `{"status":"Failed","error":`+failure+`}`), nil
				}}
				client, err := armresources.NewDeploymentsClient(fakeSubscriptionID, &azfake.TokenCredential{}, fakeClientOptions(transport))
				if err != nil {
					t.Fatal(err)
				}
				_, err = deployWithCapacityWait(context.Background(), time.Second, "deployment", func(ctx context.Context) (*runtime.Poller[armresources.DeploymentsClientCreateOrUpdateResponse], error) {
					return client.BeginCreateOrUpdate(ctx, "rg", "deployment", armresources.Deployment{}, nil)
				}, func(context.Context) (string, error) { t.Fatal("unexpected DeploymentActive recovery"); return "", nil })
				if (err == nil) != tc.wantSuccess || puts != tc.wantPuts {
					t.Fatalf("PUTs=%d error=%v; want PUTs=%d success=%v", puts, err, tc.wantPuts, tc.wantSuccess)
				}
			})
		})
	}
}

func TestClusterCapacityDoesNotResubmitAcceptedCreate(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		puts := 0
		transport := &fakeTransport{do: func(req *http.Request) (*http.Response, error) {
			if req.Method == http.MethodPut {
				puts++
				resp := capacityHTTPResponse(req, http.StatusCreated, `{"properties":{"provisioningState":"Accepted"}}`)
				resp.Header.Set("Azure-AsyncOperation", "https://management.azure.com/operations/cluster")
				return resp, nil
			}
			return capacityHTTPResponse(req, http.StatusOK, `{"status":"Failed","error":{"code":"AROHCPCapacityExceeded"}}`), nil
		}}
		client, err := sdk20261001.NewHcpOpenShiftClustersClient(fakeSubscriptionID, &azfake.TokenCredential{}, fakeClientOptions(transport))
		if err != nil {
			t.Fatal(err)
		}
		_, err = CreateHCPClusterAndWait20261001(context.Background(), logr.Discard(), client, "rg", "cluster", sdk20261001.HcpOpenShiftCluster{}, time.Minute)
		var responseError *azcore.ResponseError
		if !errors.As(err, &responseError) || responseError.ErrorCode != coreapi.CloudErrorCodeCapacityExceeded || puts != 1 {
			t.Fatalf("PUTs=%d error=%v, want original terminal failure and no resubmission", puts, err)
		}
	})
}

func TestCapacityWaitAlreadyCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := waitForCapacity(ctx, newOperationBudget(0), time.Minute, isCapacityRejection, func(context.Context) (int, error) {
		t.Fatal("create called after parent cancellation")
		return 0, nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error=%v, want parent cancellation", err)
	}
}

func TestCapacityRetryDelay(t *testing.T) {
	for range 100 {
		if delay := capacityRetryDelay(); delay < time.Minute || delay > 90*time.Second {
			t.Fatalf("delay=%v, want the retry interval plus at most 50%% jitter", delay)
		}
	}
}

func TestCapacityWaitDoesNotResetBudget(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		attempts := 0
		start := time.Now()
		_, err := waitForCapacity(context.Background(), newOperationBudget(0), 100*time.Second, isCapacityRejection, func(context.Context) (int, error) {
			attempts++
			return 0, &azcore.ResponseError{StatusCode: http.StatusBadRequest, ErrorCode: coreapi.CloudErrorCodeCapacityExceeded}
		})
		if !errors.Is(err, context.DeadlineExceeded) || attempts != 2 || time.Since(start) != 100*time.Second {
			t.Fatalf("attempts=%d elapsed=%v error=%v; want two attempts within one 100s budget", attempts, time.Since(start), err)
		}
	})
}

func TestClusterCreateSharesOperationTimeout(t *testing.T) {
	for _, tc := range []struct {
		name         string
		quota        bool
		requestDelay time.Duration
		pollDelay    time.Duration
	}{
		{name: "submission is bounded", requestDelay: 2 * time.Second},
		{name: "submission and polling share budget", requestDelay: 700 * time.Millisecond, pollDelay: 700 * time.Millisecond},
		{name: "quota backoff alone is excluded", quota: true, requestDelay: 700 * time.Millisecond, pollDelay: 700 * time.Millisecond},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				puts := 0
				var active time.Duration
				transport := &fakeTransport{do: func(req *http.Request) (*http.Response, error) {
					if req.Method == http.MethodPut {
						puts++
						if tc.quota && puts == 1 {
							return capacityHTTPResponse(req, http.StatusBadRequest, `{"error":{"code":"AROHCPCapacityExceeded"}}`), nil
						}
					}
					delay := tc.pollDelay
					if req.Method == http.MethodPut {
						delay = tc.requestDelay
					}
					start := time.Now()
					defer func() { active += time.Since(start) }()
					select {
					case <-req.Context().Done():
						return nil, req.Context().Err()
					case <-time.After(delay):
					}
					if req.Method == http.MethodPut {
						resp := capacityHTTPResponse(req, http.StatusCreated, `{"properties":{"provisioningState":"Accepted"}}`)
						resp.Header.Set("Azure-AsyncOperation", "https://management.azure.com/operations/cluster")
						return resp, nil
					}
					return capacityHTTPResponse(req, http.StatusOK, `{"status":"Succeeded","properties":{"provisioningState":"Succeeded"}}`), nil
				}}
				client, err := sdk20261001.NewHcpOpenShiftClustersClient(fakeSubscriptionID, &azfake.TokenCredential{}, fakeClientOptions(transport))
				if err != nil {
					t.Fatal(err)
				}
				start := time.Now()
				_, err = CreateHCPClusterAndWait20261001(context.Background(), logr.Discard(), client, "rg", "cluster", sdk20261001.HcpOpenShiftCluster{}, time.Second)
				if !errors.Is(err, context.DeadlineExceeded) || active != time.Second {
					t.Fatalf("active=%v elapsed=%v error=%v; want deadline after 1s of operation work", active, time.Since(start), err)
				}
				if tc.quota && time.Since(start) < 60*time.Second {
					t.Fatalf("quota backoff consumed operation time: elapsed=%v", time.Since(start))
				}
			})
		})
	}
}

func TestDeploymentResubmissionSharesOperationTimeout(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		puts := 0
		var active time.Duration
		transport := &fakeTransport{do: func(req *http.Request) (*http.Response, error) {
			start := time.Now()
			defer func() { active += time.Since(start) }()
			if req.Method == http.MethodPut {
				puts++
			}
			select {
			case <-req.Context().Done():
				return nil, req.Context().Err()
			case <-time.After(350 * time.Millisecond):
			}
			if req.Method == http.MethodPut {
				resp := capacityHTTPResponse(req, http.StatusCreated, `{"properties":{"provisioningState":"Accepted"}}`)
				resp.Header.Set("Azure-AsyncOperation", "https://management.azure.com/operations/deployment")
				return resp, nil
			}
			if puts == 1 {
				return capacityHTTPResponse(req, http.StatusOK, `{"status":"Failed","error":{"code":"DeploymentFailed","details":[{"code":"AROHCPCapacityExceeded"}]}}`), nil
			}
			return capacityHTTPResponse(req, http.StatusOK, `{"status":"Succeeded","properties":{"provisioningState":"Succeeded"}}`), nil
		}}
		client, err := armresources.NewDeploymentsClient(fakeSubscriptionID, &azfake.TokenCredential{}, fakeClientOptions(transport))
		if err != nil {
			t.Fatal(err)
		}
		_, err = deployWithCapacityWait(context.Background(), time.Second, "deployment",
			func(ctx context.Context) (*runtime.Poller[armresources.DeploymentsClientCreateOrUpdateResponse], error) {
				return client.BeginCreateOrUpdate(ctx, "rg", "deployment", armresources.Deployment{}, nil)
			}, func(context.Context) (string, error) {
				t.Fatal("unexpected deployment recovery")
				return "", nil
			})
		if !errors.Is(err, context.DeadlineExceeded) || active != time.Second || puts != 2 {
			t.Fatalf("active=%v PUTs=%d error=%v; want one shared 1s operation budget", active, puts, err)
		}
	})
}

func TestCapacityBackoffBudgetExcludesRequests(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		start := time.Now()
		_, err := waitForCapacity(context.Background(), newOperationBudget(0), 3*time.Second, isCapacityRejection, func(ctx context.Context) (int, error) {
			select {
			case <-ctx.Done():
				return 0, ctx.Err()
			case <-time.After(4 * time.Second):
				return 0, &azcore.ResponseError{StatusCode: http.StatusBadRequest, ErrorCode: coreapi.CloudErrorCodeCapacityExceeded}
			}
		})
		if !errors.Is(err, context.DeadlineExceeded) || time.Since(start) != 7*time.Second {
			t.Fatalf("elapsed=%v error=%v; want 4s request plus 3s quota backoff", time.Since(start), err)
		}
	})
}

func TestCapacityBackoffTiming(t *testing.T) {
	for _, tt := range []struct {
		name        string
		cancelAfter time.Duration
		wantOutcome string
	}{
		{name: "retry accepted", wantOutcome: timing.OutcomeSucceeded},
		{name: "caller cancels backoff", cancelAfter: 3 * time.Second, wantOutcome: timing.OutcomeFailed},
	} {
		t.Run(tt.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				tc := &perItOrDescribeTestContext{}
				activeTestContext.Store(tc)
				defer activeTestContext.Store(nil)
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				if tt.cancelAfter > 0 {
					time.AfterFunc(tt.cancelAfter, cancel)
				}
				attempts := 0
				var requestFinished time.Time
				_, err := waitForCapacity(ctx, newOperationBudget(time.Minute), 2*time.Minute, isCapacityRejection, func(context.Context) (int, error) {
					attempts++
					time.Sleep(2 * time.Second)
					if attempts == 1 {
						requestFinished = time.Now()
						return 0, &azcore.ResponseError{StatusCode: http.StatusBadRequest, ErrorCode: coreapi.CloudErrorCodeCapacityExceeded}
					}
					return 1, nil
				})
				if (err != nil) != (tt.cancelAfter > 0) {
					t.Fatalf("unexpected result: %v", err)
				}
				if len(tc.timingMetadata.Steps) != 1 {
					t.Fatalf("expected one quota backoff span, got %+v", tc.timingMetadata.Steps)
				}
				span := tc.timingMetadata.Steps[0]
				if span.Kind != timing.KindCapacityWait || span.Outcome != tt.wantOutcome {
					t.Fatalf("unexpected span: %+v", span)
				}
				if span.StartedAt != requestFinished.Format(time.RFC3339Nano) {
					t.Fatalf("backoff included request time: %+v", span)
				}
			})
		})
	}
}
