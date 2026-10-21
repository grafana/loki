// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License. See License.txt in the project root for license information.

package generated

import (
	"context"
	"net/http"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
)

func (client *ContainerClient) Endpoint() string {
	return client.url
}

func (client *ContainerClient) InternalClient() *azcore.Client {
	return client.internal
}

// NewContainerClient creates a new instance of ContainerClient with the specified values.
//   - endpoint - The URL of the service account, container, or blob that is the target of the desired operation.
//   - pl - the pipeline used for sending requests and handling responses.
func (client *ContainerClient) ListBlobFlatSegmentApacheArrowCreateRequest(ctx context.Context, options *ContainerClientListBlobFlatSegmentApacheArrowOptions) (*policy.Request, error) {
	return client.listBlobFlatSegmentApacheArrowCreateRequest(ctx, options)
}

func (client *ContainerClient) ListBlobFlatSegmentApacheArrowHandleResponse(resp *http.Response) (ContainerClientListBlobFlatSegmentApacheArrowResponse, error) {
	return client.listBlobFlatSegmentApacheArrowHandleResponse(resp)
}

func (client *ContainerClient) ListBlobHierarchySegmentApacheArrowCreateRequest(ctx context.Context, delimiter string, options *ContainerClientListBlobHierarchySegmentApacheArrowOptions) (*policy.Request, error) {
	return client.listBlobHierarchySegmentApacheArrowCreateRequest(ctx, delimiter, options)
}

func (client *ContainerClient) ListBlobHierarchySegmentApacheArrowHandleResponse(resp *http.Response) (ContainerClientListBlobHierarchySegmentApacheArrowResponse, error) {
	return client.listBlobHierarchySegmentApacheArrowHandleResponse(resp)
}

func NewContainerClient(endpoint string, azClient *azcore.Client) *ContainerClient {
	client := &ContainerClient{
		internal: azClient,
		url:      endpoint,
	}
	return client
}

// ListBlobFlatSegmentCreateRequest creates the ListBlobFlatSegment request.
func (client *ContainerClient) ListBlobFlatSegmentCreateRequest(ctx context.Context, options *ContainerClientListBlobFlatSegmentOptions) (*policy.Request, error) {
	return client.listBlobFlatSegmentCreateRequest(ctx, options)
}

// ListBlobFlatSegmentHandleResponse handles the ListBlobFlatSegment response.
func (client *ContainerClient) ListBlobFlatSegmentHandleResponse(resp *http.Response, successCodes ...int) (ContainerClientListBlobFlatSegmentResponse, error) {
	return client.listBlobFlatSegmentHandleResponse(resp, successCodes...)
}

// ListBlobHierarchySegmentCreateRequest creates the ListBlobHierarchySegment request.
func (client *ContainerClient) ListBlobHierarchySegmentCreateRequest(ctx context.Context, delimiter string, options *ContainerClientListBlobHierarchySegmentOptions) (*policy.Request, error) {
	return client.listBlobHierarchySegmentCreateRequest(ctx, delimiter, options)
}

// ListBlobHierarchySegmentHandleResponse handles the ListBlobHierarchySegment response.
func (client *ContainerClient) ListBlobHierarchySegmentHandleResponse(resp *http.Response, successCodes ...int) (ContainerClientListBlobHierarchySegmentResponse, error) {
	return client.listBlobHierarchySegmentHandleResponse(resp, successCodes...)
}
