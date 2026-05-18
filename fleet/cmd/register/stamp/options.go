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

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/cloud"
	"github.com/spf13/cobra"

	"github.com/Azure/ARO-HCP/internal/api/fleet"
	"github.com/Azure/ARO-HCP/internal/azsdk"
	"github.com/Azure/ARO-HCP/internal/database"
)

type RawStampOptions struct {
	CosmosURL       string
	CosmosName      string
	StampIdentifier string
	AutoApprove     bool
}

func DefaultStampOptions() *RawStampOptions {
	return &RawStampOptions{}
}

func BindStampOptions(opts *RawStampOptions, cmd *cobra.Command) error {
	cmd.Flags().StringVar(&opts.CosmosURL, "cosmos-url", opts.CosmosURL, "CosmosDB endpoint URL")
	cmd.Flags().StringVar(&opts.CosmosName, "cosmos-name", opts.CosmosName, "CosmosDB database name")
	cmd.Flags().StringVar(&opts.StampIdentifier, "stamp-identifier", opts.StampIdentifier, "stamp identifier")
	cmd.Flags().BoolVar(&opts.AutoApprove, "auto-approve", opts.AutoApprove, "automatically approve the stamp during registration")
	for _, flag := range []string{"cosmos-url", "cosmos-name", "stamp-identifier"} {
		if err := cmd.MarkFlagRequired(flag); err != nil {
			return err
		}
	}
	return nil
}

type validatedStampOptions struct {
	*RawStampOptions
}

type ValidatedStampOptions struct {
	*validatedStampOptions
}

func (o *RawStampOptions) Validate(ctx context.Context) (*ValidatedStampOptions, error) {
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

	return &ValidatedStampOptions{
		validatedStampOptions: &validatedStampOptions{
			RawStampOptions: o,
		},
	}, nil
}

type stampOptions struct {
	fleetDBClient   database.FleetDBClient
	stampIdentifier string
	autoApprove     bool
}

type StampOptions struct {
	*stampOptions
}

func (o *ValidatedStampOptions) Complete(ctx context.Context) (*StampOptions, error) {
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

	return &StampOptions{
		stampOptions: &stampOptions{
			fleetDBClient:   fleetDBClient,
			stampIdentifier: o.StampIdentifier,
			autoApprove:     o.AutoApprove,
		},
	}, nil
}
