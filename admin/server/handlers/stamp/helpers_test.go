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

package stamp

import (
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"

	"github.com/Azure/ARO-HCP/internal/api/arm"
)

func TestHandleETagConflict(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name           string
		err            error
		expectConflict bool
	}{
		{
			name:           "precondition failed returns 409",
			err:            &azcore.ResponseError{StatusCode: http.StatusPreconditionFailed},
			expectConflict: true,
		},
		{
			name:           "other error passes through",
			err:            fmt.Errorf("some other error"),
			expectConflict: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := handleETagConflict(tt.err)
			require.Error(t, result)

			if tt.expectConflict {
				var cloudErr *arm.CloudError
				require.True(t, errors.As(result, &cloudErr))
				require.Equal(t, http.StatusConflict, cloudErr.StatusCode)
			} else {
				require.ErrorIs(t, result, tt.err)
			}
		})
	}
}
