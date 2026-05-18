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
	"context"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	"github.com/spf13/cobra"

	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/Azure/ARO-HCP/internal/api"
	"github.com/Azure/ARO-HCP/internal/api/fleet"
	"github.com/Azure/ARO-HCP/internal/database"
)

func NewStampCommand() (*cobra.Command, error) {
	opts := DefaultStampOptions()
	cmd := &cobra.Command{
		Use:   "stamp",
		Short: "Register or update a stamp in CosmosDB",
		RunE: func(cmd *cobra.Command, args []string) error {
			return run(cmd.Context(), opts)
		},
	}
	if err := BindStampOptions(opts, cmd); err != nil {
		return nil, err
	}
	return cmd, nil
}

func run(ctx context.Context, rawOpts *RawStampOptions) error {
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

	stampResourceID, _ := fleet.ToStampResourceID(completed.stampIdentifier)
	newStamp := &fleet.Stamp{
		CosmosMetadata: api.CosmosMetadata{ResourceID: stampResourceID},
		ResourceID:     stampResourceID,
	}

	if completed.autoApprove {
		apimeta.SetStatusCondition(&newStamp.Status.Conditions, metav1.Condition{
			Type:               string(fleet.StampConditionApproved),
			Status:             metav1.ConditionTrue,
			Reason:             string(fleet.StampConditionReasonAutoApproved),
			Message:            "Auto-approved during registration",
			LastTransitionTime: metav1.NewTime(time.Now()),
		})
	}

	existing, err := stampsCRUD.Get(ctx, completed.stampIdentifier)
	if err != nil {
		if !database.IsNotFoundError(err) {
			return fmt.Errorf("failed to get stamp: %w", err)
		}

		logger.Info("Creating stamp", "stampIdentifier", completed.stampIdentifier, "autoApprove", completed.autoApprove)
		if _, err := stampsCRUD.Create(ctx, newStamp, nil); err != nil {
			return fmt.Errorf("failed to create stamp: %w", err)
		}
		logger.Info("Stamp created", "stampIdentifier", completed.stampIdentifier)
		return nil
	}

	logger.Info("Updating existing stamp", "stampIdentifier", completed.stampIdentifier, "autoApprove", completed.autoApprove)
	updated := existing.DeepCopy()
	updated.Spec = newStamp.Spec

	if completed.autoApprove {
		apimeta.SetStatusCondition(&updated.Status.Conditions, metav1.Condition{
			Type:               string(fleet.StampConditionApproved),
			Status:             metav1.ConditionTrue,
			Reason:             string(fleet.StampConditionReasonAutoApproved),
			Message:            "Auto-approved during registration",
			LastTransitionTime: metav1.NewTime(time.Now()),
		})
	}

	if _, err := stampsCRUD.Replace(ctx, updated, existing, nil); err != nil {
		return fmt.Errorf("failed to update stamp: %w", err)
	}
	logger.Info("Stamp updated", "stampIdentifier", completed.stampIdentifier)
	return nil
}
