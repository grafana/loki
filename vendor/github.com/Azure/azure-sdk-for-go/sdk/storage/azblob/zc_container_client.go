//go:build go1.18
// +build go1.18

// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License.

package azblob

import (
	"context"
	"errors"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/runtime"
)

// ContainerClient represents a URL to the Azure Storage container allowing you to manipulate its blobs.
type ContainerClient struct {
	client    *containerClient
	sharedKey *SharedKeyCredential
}

// URL returns the URL endpoint used by the ContainerClient object.
func (c *ContainerClient) URL() string {
	return c.client.endpoint
}

// NewContainerClient creates a ContainerClient object using the specified URL, Azure AD credential, and options.
func NewContainerClient(containerURL string, cred azcore.TokenCredential, options *ClientOptions) (*ContainerClient, error) {
	authPolicy := runtime.NewBearerTokenPolicy(cred, []string{tokenScope}, nil)
	conOptions := getConnectionOptions(options)
	conOptions.PerRetryPolicies = append(conOptions.PerRetryPolicies, authPolicy)
	conn := newConnection(containerURL, conOptions)

	return &ContainerClient{
		client: newContainerClient(conn.Endpoint(), conn.Pipeline()),
	}, nil
}

// NewContainerClientWithNoCredential creates a ContainerClient object using the specified URL and options.
func NewContainerClientWithNoCredential(containerURL string, options *ClientOptions) (*ContainerClient, error) {
	conOptions := getConnectionOptions(options)
	conn := newConnection(containerURL, conOptions)

	return &ContainerClient{
		client: newContainerClient(conn.Endpoint(), conn.Pipeline()),
	}, nil
}

// NewContainerClientWithSharedKey creates a ContainerClient object using the specified URL, shared key, and options.
func NewContainerClientWithSharedKey(containerURL string, cred *SharedKeyCredential, options *ClientOptions) (*ContainerClient, error) {
	authPolicy := newSharedKeyCredPolicy(cred)
	conOptions := getConnectionOptions(options)
	conOptions.PerRetryPolicies = append(conOptions.PerRetryPolicies, authPolicy)
	conn := newConnection(containerURL, conOptions)

	return &ContainerClient{
		client:    newContainerClient(conn.Endpoint(), conn.Pipeline()),
		sharedKey: cred,
	}, nil
}

// NewContainerClientFromConnectionString creates a ContainerClient object using connection string of an account
func NewContainerClientFromConnectionString(connectionString string, containerName string, options *ClientOptions) (*ContainerClient, error) {
	svcClient, err := NewServiceClientFromConnectionString(connectionString, options)
	if err != nil {
		return nil, err
	}
	return svcClient.NewContainerClient(containerName)
}

// NewBlobClient creates a new BlobClient object by concatenating blobName to the end of
// ContainerClient's URL. The new BlobClient uses the same request policy pipeline as the ContainerClient.
// To change the pipeline, create the BlobClient and then call its WithPipeline method passing in the
// desired pipeline object. Or, call this package's NewBlobClient instead of calling this object's
// NewBlobClient method.
func (c *ContainerClient) NewBlobClient(blobName string) (*BlobClient, error) {
	blobURL := appendToURLPath(c.URL(), blobName)

	return &BlobClient{
		client:    newBlobClient(blobURL, c.client.pl),
		sharedKey: c.sharedKey,
	}, nil
}

// NewAppendBlobClient creates a new AppendBlobURL object by concatenating blobName to the end of
// ContainerClient's URL. The new AppendBlobURL uses the same request policy pipeline as the ContainerClient.
// To change the pipeline, create the AppendBlobURL and then call its WithPipeline method passing in the
// desired pipeline object. Or, call this package's NewAppendBlobClient instead of calling this object's
// NewAppendBlobClient method.
func (c *ContainerClient) NewAppendBlobClient(blobName string) (*AppendBlobClient, error) {
	blobURL := appendToURLPath(c.URL(), blobName)

	return &AppendBlobClient{
		BlobClient: BlobClient{
			client:    newBlobClient(blobURL, c.client.pl),
			sharedKey: c.sharedKey,
		},
		client: newAppendBlobClient(blobURL, c.client.pl),
	}, nil
}

// NewBlockBlobClient creates a new BlockBlobClient object by concatenating blobName to the end of
// ContainerClient's URL. The new BlockBlobClient uses the same request policy pipeline as the ContainerClient.
// To change the pipeline, create the BlockBlobClient and then call its WithPipeline method passing in the
// desired pipeline object. Or, call this package's NewBlockBlobClient instead of calling this object's
// NewBlockBlobClient method.
func (c *ContainerClient) NewBlockBlobClient(blobName string) (*BlockBlobClient, error) {
	blobURL := appendToURLPath(c.URL(), blobName)

	return &BlockBlobClient{
		BlobClient: BlobClient{
			client:    newBlobClient(blobURL, c.client.pl),
			sharedKey: c.sharedKey,
		},
		client: newBlockBlobClient(blobURL, c.client.pl),
	}, nil
}

// NewPageBlobClient creates a new PageBlobURL object by concatenating blobName to the end of ContainerClient's URL. The new PageBlobURL uses the same request policy pipeline as the ContainerClient.
// To change the pipeline, create the PageBlobURL and then call its WithPipeline method passing in the
// desired pipeline object. Or, call this package's NewPageBlobClient instead of calling this object's
// NewPageBlobClient method.
func (c *ContainerClient) NewPageBlobClient(blobName string) (*PageBlobClient, error) {
	blobURL := appendToURLPath(c.URL(), blobName)

	return &PageBlobClient{
		BlobClient: BlobClient{
			client:    newBlobClient(blobURL, c.client.pl),
			sharedKey: c.sharedKey,
		},
		client: newPageBlobClient(blobURL, c.client.pl),
	}, nil
}

// Create creates a new container within a storage account. If a container with the same name already exists, the operation fails.
// For more information, see https://docs.microsoft.com/rest/api/storageservices/create-container.
func (c *ContainerClient) Create(ctx context.Context, options *ContainerCreateOptions) (ContainerCreateResponse, error) {
	basics, cpkInfo := options.format()
	resp, err := c.client.Create(ctx, basics, cpkInfo)

	return toContainerCreateResponse(resp), handleError(err)
}

// Delete marks the specified container for deletion. The container and any blobs contained within it are later deleted during garbage collection.
// For more information, see https://docs.microsoft.com/rest/api/storageservices/delete-container.
func (c *ContainerClient) Delete(ctx context.Context, o *ContainerDeleteOptions) (ContainerDeleteResponse, error) {
	basics, leaseInfo, accessConditions := o.format()
	resp, err := c.client.Delete(ctx, basics, leaseInfo, accessConditions)

	return toContainerDeleteResponse(resp), handleError(err)
}

// GetProperties returns the container's properties.
// For more information, see https://docs.microsoft.com/rest/api/storageservices/get-container-metadata.
func (c *ContainerClient) GetProperties(ctx context.Context, o *ContainerGetPropertiesOptions) (ContainerGetPropertiesResponse, error) {
	// NOTE: GetMetadata actually calls GetProperties internally because GetProperties returns the metadata AND the properties.
	// This allows us to not expose a GetProperties method at all simplifying the API.
	// The optionals are nil, like they were in track 1.5
	options, leaseAccess := o.format()
	resp, err := c.client.GetProperties(ctx, options, leaseAccess)

	return toContainerGetPropertiesResponse(resp), handleError(err)
}

// SetMetadata sets the container's metadata.
// For more information, see https://docs.microsoft.com/rest/api/storageservices/set-container-metadata.
func (c *ContainerClient) SetMetadata(ctx context.Context, o *ContainerSetMetadataOptions) (ContainerSetMetadataResponse, error) {
	metadataOptions, lac, mac := o.format()
	resp, err := c.client.SetMetadata(ctx, metadataOptions, lac, mac)

	return toContainerSetMetadataResponse(resp), handleError(err)
}

// GetAccessPolicy returns the container's access policy. The access policy indicates whether container's blobs may be accessed publicly.
// For more information, see https://docs.microsoft.com/rest/api/storageservices/get-container-acl.
func (c *ContainerClient) GetAccessPolicy(ctx context.Context, o *ContainerGetAccessPolicyOptions) (ContainerGetAccessPolicyResponse, error) {
	options, ac := o.format()
	resp, err := c.client.GetAccessPolicy(ctx, options, ac)

	return toContainerGetAccessPolicyResponse(resp), handleError(err)
}

// SetAccessPolicy sets the container's permissions. The access policy indicates whether blobs in a container may be accessed publicly.
// For more information, see https://docs.microsoft.com/rest/api/storageservices/set-container-acl.
func (c *ContainerClient) SetAccessPolicy(ctx context.Context, o *ContainerSetAccessPolicyOptions) (ContainerSetAccessPolicyResponse, error) {
	accessPolicy, mac, lac := o.format()
	resp, err := c.client.SetAccessPolicy(ctx, accessPolicy, mac, lac)

	return toContainerSetAccessPolicyResponse(resp), handleError(err)
}

// ListBlobsFlat returns a pager for blobs starting from the specified Marker. Use an empty
// Marker to start enumeration from the beginning. Blob names are returned in lexicographic order.
// For more information, see https://docs.microsoft.com/rest/api/storageservices/list-blobs.
func (c *ContainerClient) ListBlobsFlat(o *ContainerListBlobsFlatOptions) *ContainerListBlobFlatPager {
	listOptions := o.format()
	pager := c.client.ListBlobFlatSegment(listOptions)

	// override the advancer
	pager.advancer = func(ctx context.Context, response containerClientListBlobFlatSegmentResponse) (*policy.Request, error) {
		listOptions.Marker = response.NextMarker
		return c.client.listBlobFlatSegmentCreateRequest(ctx, listOptions)
	}

	return toContainerListBlobFlatSegmentPager(pager)
}

// ListBlobsHierarchy returns a channel of blobs starting from the specified Marker. Use an empty
// Marker to start enumeration from the beginning. Blob names are returned in lexicographic order.
// After getting a segment, process it, and then call ListBlobsHierarchicalSegment again (passing the the
// previously-returned Marker) to get the next segment.
// For more information, see https://docs.microsoft.com/rest/api/storageservices/list-blobs.
// AutoPagerTimeout specifies the amount of time with no read operations before the channel times out and closes. Specify no time and it will be ignored.
// AutoPagerBufferSize specifies the channel's buffer size.
// Both the blob item channel and error channel should be watched. Only one error will be released via this channel (or a nil error, to register a clean exit.)
func (c *ContainerClient) ListBlobsHierarchy(delimiter string, o *ContainerListBlobsHierarchyOptions) *ContainerListBlobHierarchyPager {
	listOptions := o.format()
	pager := c.client.ListBlobHierarchySegment(delimiter, listOptions)

	// override the advancer
	pager.advancer = func(ctx context.Context, response containerClientListBlobHierarchySegmentResponse) (*policy.Request, error) {
		listOptions.Marker = response.NextMarker
		return c.client.listBlobHierarchySegmentCreateRequest(ctx, delimiter, listOptions)
	}

	return toContainerListBlobHierarchySegmentPager(pager)
}

// GetSASURL is a convenience method for generating a SAS token for the currently pointed at container.
// It can only be used if the credential supplied during creation was a SharedKeyCredential.
func (c *ContainerClient) GetSASURL(permissions ContainerSASPermissions, start time.Time, expiry time.Time) (string, error) {
	if c.sharedKey == nil {
		return "", errors.New("SAS can only be signed with a SharedKeyCredential")
	}

	urlParts, err := NewBlobURLParts(c.URL())
	if err != nil {
		return "", err
	}

	// Containers do not have snapshots, nor versions.
	urlParts.SAS, err = BlobSASSignatureValues{
		ContainerName: urlParts.ContainerName,
		Permissions:   permissions.String(),
		StartTime:     start.UTC(),
		ExpiryTime:    expiry.UTC(),
	}.NewSASQueryParameters(c.sharedKey)

	return urlParts.URL(), err
}
