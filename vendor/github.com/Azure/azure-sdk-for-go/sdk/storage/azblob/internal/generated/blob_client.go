// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License. See License.txt in the project root for license information.

package generated

import (
	"context"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
)

func (client *BlobClient) Endpoint() string {
	return client.url
}

func (client *BlobClient) InternalClient() *azcore.Client {
	return client.internal
}

func (client *BlobClient) DeleteCreateRequest(ctx context.Context, options *BlobClientDeleteOptions) (*policy.Request, error) {
	return client.deleteCreateRequest(ctx, options)
}

func (client *BlobClient) SetTierCreateRequest(ctx context.Context, tier AccessTier, options *BlobClientSetTierOptions) (*policy.Request, error) {
	return client.setTierCreateRequest(ctx, tier, options)
}

// NewBlobClient creates a new instance of BlobClient with the specified values.
//   - endpoint - The URL of the service account, container, or blob that is the target of the desired operation.
//   - azClient - azcore.Client is a basic HTTP client. It consists of a pipeline and tracing provider.
func NewBlobClient(endpoint string, azClient *azcore.Client) *BlobClient {
	client := &BlobClient{
		internal: azClient,
		url:      endpoint,
	}
	return client
}
