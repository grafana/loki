// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License. See License.txt in the project root for license information.

package generated

import (
	"context"
	"net/http"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
)

func (client *PageBlobClient) Endpoint() string {
	return client.url
}

func (client *PageBlobClient) InternalClient() *azcore.Client {
	return client.internal
}

// NewPageBlobClient creates a new instance of PageBlobClient with the specified values.
//   - endpoint - The URL of the service account, container, or blob that is the target of the desired operation.
//   - azClient - azcore.Client is a basic HTTP client. It consists of a pipeline and tracing provider.
func NewPageBlobClient(endpoint string, azClient *azcore.Client) *PageBlobClient {
	client := &PageBlobClient{
		internal: azClient,
		url:      endpoint,
	}
	return client
}

// GetPageRangesCreateRequest creates the GetPageRanges request.
func (client *PageBlobClient) GetPageRangesCreateRequest(ctx context.Context, options *PageBlobClientGetPageRangesOptions) (*policy.Request, error) {
	return client.getPageRangesCreateRequest(ctx, options)
}

// GetPageRangesHandleResponse handles the GetPageRanges response.
func (client *PageBlobClient) GetPageRangesHandleResponse(resp *http.Response, successCodes ...int) (PageBlobClientGetPageRangesResponse, error) {
	return client.getPageRangesHandleResponse(resp, successCodes...)
}

// GetPageRangesDiffCreateRequest creates the GetPageRangesDiff request.
func (client *PageBlobClient) GetPageRangesDiffCreateRequest(ctx context.Context, options *PageBlobClientGetPageRangesDiffOptions) (*policy.Request, error) {
	return client.getPageRangesDiffCreateRequest(ctx, options)
}

// GetPageRangesDiffHandleResponse handles the GetPageRangesDiff response.
func (client *PageBlobClient) GetPageRangesDiffHandleResponse(resp *http.Response, successCodes ...int) (PageBlobClientGetPageRangesDiffResponse, error) {
	return client.getPageRangesDiffHandleResponse(resp, successCodes...)
}
