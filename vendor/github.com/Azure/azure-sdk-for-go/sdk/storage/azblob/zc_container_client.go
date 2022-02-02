// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License.

package azblob

import (
	"context"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/runtime"
)

// A ContainerClient represents a URL to the Azure Storage container allowing you to manipulate its blobs.
type ContainerClient struct {
	client    *containerClient
	sharedKey *SharedKeyCredential
}

// URL returns the URL endpoint used by the ContainerClient object.
func (c ContainerClient) URL() string {
	return c.client.con.u
}

// NewContainerClient creates a ContainerClient object using the specified URL, Azure AD credential, and options.
func NewContainerClient(containerURL string, cred azcore.TokenCredential, options *ClientOptions) (ContainerClient, error) {
	authPolicy := runtime.NewBearerTokenPolicy(cred, []string{tokenScope}, nil)
	return ContainerClient{client: &containerClient{
		con: newConnection(containerURL, authPolicy, options.getConnectionOptions()),
	}}, nil
}

// NewContainerClientWithNoCredential creates a ContainerClient object using the specified URL and options.
func NewContainerClientWithNoCredential(containerURL string, options *ClientOptions) (ContainerClient, error) {
	return ContainerClient{client: &containerClient{
		con: newConnection(containerURL, nil, options.getConnectionOptions()),
	}}, nil
}

// NewContainerClientWithSharedKey creates a ContainerClient object using the specified URL, shared key, and options.
func NewContainerClientWithSharedKey(containerURL string, cred *SharedKeyCredential, options *ClientOptions) (ContainerClient, error) {
	authPolicy := newSharedKeyCredPolicy(cred)
	return ContainerClient{client: &containerClient{
		con: newConnection(containerURL, authPolicy, options.getConnectionOptions()),
	}, sharedKey: cred}, nil
}

// NewContainerClientFromConnectionString creates a ContainerClient object using connection string of an account
func NewContainerClientFromConnectionString(connectionString string, containerName string, options *ClientOptions) (ContainerClient, error) {
	svcClient, err := NewServiceClientFromConnectionString(connectionString, options)
	if err != nil {
		return ContainerClient{}, err
	}
	return svcClient.NewContainerClient(containerName), nil
}

// NewBlobClient creates a new BlobClient object by concatenating blobName to the end of
// ContainerClient's URL. The new BlobClient uses the same request policy pipeline as the ContainerClient.
// To change the pipeline, create the BlobClient and then call its WithPipeline method passing in the
// desired pipeline object. Or, call this package's NewBlobClient instead of calling this object's
// NewBlobClient method.
func (c ContainerClient) NewBlobClient(blobName string) BlobClient {
	blobURL := appendToURLPath(c.URL(), blobName)
	newCon := &connection{u: blobURL, p: c.client.con.p}

	return BlobClient{
		client: &blobClient{newCon, nil},
	}
}

// NewAppendBlobClient creates a new AppendBlobURL object by concatenating blobName to the end of
// ContainerClient's URL. The new AppendBlobURL uses the same request policy pipeline as the ContainerClient.
// To change the pipeline, create the AppendBlobURL and then call its WithPipeline method passing in the
// desired pipeline object. Or, call this package's NewAppendBlobClient instead of calling this object's
// NewAppendBlobClient method.
func (c ContainerClient) NewAppendBlobClient(blobName string) AppendBlobClient {
	blobURL := appendToURLPath(c.URL(), blobName)
	newCon := &connection{blobURL, c.client.con.p}

	return AppendBlobClient{
		client:     &appendBlobClient{newCon},
		BlobClient: BlobClient{client: &blobClient{con: newCon}},
	}
}

// NewBlockBlobClient creates a new BlockBlobClient object by concatenating blobName to the end of
// ContainerClient's URL. The new BlockBlobClient uses the same request policy pipeline as the ContainerClient.
// To change the pipeline, create the BlockBlobClient and then call its WithPipeline method passing in the
// desired pipeline object. Or, call this package's NewBlockBlobClient instead of calling this object's
// NewBlockBlobClient method.
func (c ContainerClient) NewBlockBlobClient(blobName string) BlockBlobClient {
	blobURL := appendToURLPath(c.URL(), blobName)
	newCon := &connection{blobURL, c.client.con.p}

	return BlockBlobClient{
		client:     &blockBlobClient{newCon},
		BlobClient: BlobClient{client: &blobClient{con: newCon}},
	}
}

// NewPageBlobClient creates a new PageBlobURL object by concatenating blobName to the end of ContainerClient's URL. The new PageBlobURL uses the same request policy pipeline as the ContainerClient.
// To change the pipeline, create the PageBlobURL and then call its WithPipeline method passing in the
// desired pipeline object. Or, call this package's NewPageBlobClient instead of calling this object's
// NewPageBlobClient method.
func (c ContainerClient) NewPageBlobClient(blobName string) PageBlobClient {
	blobURL := appendToURLPath(c.URL(), blobName)
	newCon := &connection{blobURL, c.client.con.p}

	return PageBlobClient{
		client:     &pageBlobClient{newCon},
		BlobClient: BlobClient{client: &blobClient{con: newCon}},
	}
}

// Create creates a new container within a storage account. If a container with the same name already exists, the operation fails.
// For more information, see https://docs.microsoft.com/rest/api/storageservices/create-container.
func (c ContainerClient) Create(ctx context.Context, options *CreateContainerOptions) (ContainerCreateResponse, error) {
	basics, cpkInfo := options.pointers()
	resp, err := c.client.Create(ctx, basics, cpkInfo)

	return resp, handleError(err)
}

// Delete marks the specified container for deletion. The container and any blobs contained within it are later deleted during garbage collection.
// For more information, see https://docs.microsoft.com/rest/api/storageservices/delete-container.
func (c ContainerClient) Delete(ctx context.Context, options *DeleteContainerOptions) (ContainerDeleteResponse, error) {
	basics, leaseInfo, accessConditions := options.pointers()
	resp, err := c.client.Delete(ctx, basics, leaseInfo, accessConditions)

	return resp, handleError(err)
}

// GetProperties returns the container's properties.
// For more information, see https://docs.microsoft.com/rest/api/storageservices/get-container-metadata.
func (c ContainerClient) GetProperties(ctx context.Context, gpo *GetPropertiesOptionsContainer) (ContainerGetPropertiesResponse, error) {
	// NOTE: GetMetadata actually calls GetProperties internally because GetProperties returns the metadata AND the properties.
	// This allows us to not expose a GetProperties method at all simplifying the API.
	// The optionals are nil, like they were in track 1.5
	options, leaseAccess := gpo.pointers()

	resp, err := c.client.GetProperties(ctx, options, leaseAccess)

	return resp, handleError(err)
}

// SetMetadata sets the container's metadata.
// For more information, see https://docs.microsoft.com/rest/api/storageservices/set-container-metadata.
func (c ContainerClient) SetMetadata(ctx context.Context, options *SetMetadataContainerOptions) (ContainerSetMetadataResponse, error) {
	metadataOptions, lac, mac := options.pointers()

	resp, err := c.client.SetMetadata(ctx, metadataOptions, lac, mac)

	return resp, handleError(err)
}

// GetAccessPolicy returns the container's access policy. The access policy indicates whether container's blobs may be accessed publicly.
// For more information, see https://docs.microsoft.com/rest/api/storageservices/get-container-acl.
func (c ContainerClient) GetAccessPolicy(ctx context.Context, options *GetAccessPolicyOptions) (ContainerGetAccessPolicyResponse, error) {
	o, ac := options.pointers()

	resp, err := c.client.GetAccessPolicy(ctx, o, ac)

	return resp, handleError(err)
}

// SetAccessPolicy sets the container's permissions. The access policy indicates whether blobs in a container may be accessed publicly.
// For more information, see https://docs.microsoft.com/rest/api/storageservices/set-container-acl.
func (c ContainerClient) SetAccessPolicy(ctx context.Context, options *SetAccessPolicyOptions) (ContainerSetAccessPolicyResponse, error) {
	accessPolicy, mac, lac := options.pointers()

	resp, err := c.client.SetAccessPolicy(ctx, &accessPolicy, mac, lac)

	return resp, handleError(err)
}

// ListBlobsFlat returns a pager for blobs starting from the specified Marker. Use an empty
// Marker to start enumeration from the beginning. Blob names are returned in lexicographic order.
// For more information, see https://docs.microsoft.com/rest/api/storageservices/list-blobs.
func (c ContainerClient) ListBlobsFlat(listOptions *ContainerListBlobFlatSegmentOptions) *ContainerListBlobFlatSegmentPager {
	pager := c.client.ListBlobFlatSegment(listOptions)
	// override the generated pager to insert our handleError(error)
	if pager.Err() != nil {
		return pager
	}

	// override the advancer
	pager.advancer = func(ctx context.Context, response ContainerListBlobFlatSegmentResponse) (*policy.Request, error) {
		return c.client.listBlobFlatSegmentCreateRequest(ctx, &ContainerListBlobFlatSegmentOptions{
			Marker: response.NextMarker,
		})
	}

	// TODO: Come Here
	//pager.err = func(response *azcore.Response) error {
	//	return handleError(c.client.listBlobFlatSegmentHandleError(response))
	//}

	return pager
}

// ListBlobsHierarchy returns a channel of blobs starting from the specified Marker. Use an empty
// Marker to start enumeration from the beginning. Blob names are returned in lexicographic order.
// After getting a segment, process it, and then call ListBlobsHierarchicalSegment again (passing the the
// previously-returned Marker) to get the next segment.
// For more information, see https://docs.microsoft.com/rest/api/storageservices/list-blobs.
// AutoPagerTimeout specifies the amount of time with no read operations before the channel times out and closes. Specify no time and it will be ignored.
// AutoPagerBufferSize specifies the channel's buffer size.
// Both the blob item channel and error channel should be watched. Only one error will be released via this channel (or a nil error, to register a clean exit.)
func (c ContainerClient) ListBlobsHierarchy(delimiter string, listOptions *ContainerListBlobHierarchySegmentOptions) *ContainerListBlobHierarchySegmentPager {
	pager := c.client.ListBlobHierarchySegment(delimiter, listOptions)
	// override the generated pager to insert our handleError(error)
	if pager.Err() != nil {
		return pager
	}

	// override the advancer
	pager.advancer = func(ctx context.Context, response ContainerListBlobHierarchySegmentResponse) (*policy.Request, error) {
		return c.client.listBlobHierarchySegmentCreateRequest(ctx, delimiter, &ContainerListBlobHierarchySegmentOptions{
			Marker: response.NextMarker,
		})
	}

	// todo: come here
	//p.errorer = func(response *azcore.Response) error {
	//	return handleError(c.client.listBlobHierarchySegmentHandleError(response))
	//}

	return pager
}

// GetSASToken is a convenience method for generating a SAS token for the currently pointed at container.
// It can only be used if the credential supplied during creation was a SharedKeyCredential.
func (c ContainerClient) GetSASToken(permissions BlobSASPermissions, start time.Time, expiry time.Time) (SASQueryParameters, error) {
	urlParts := NewBlobURLParts(c.URL())

	// Containers do not have snapshots, nor versions.

	return BlobSASSignatureValues{
		ContainerName: urlParts.ContainerName,

		Permissions: permissions.String(),

		StartTime:  start.UTC(),
		ExpiryTime: expiry.UTC(),
	}.NewSASQueryParameters(c.sharedKey)
}
