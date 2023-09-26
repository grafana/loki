//go:build go1.18
// +build go1.18

// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License. See License.txt in the project root for license information.

package container

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/runtime"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/appendblob"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/blob"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/blockblob"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/internal/base"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/internal/exported"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/internal/generated"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/internal/shared"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/pageblob"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/sas"
)

// ClientOptions contains the optional parameters when creating a Client.
type ClientOptions struct {
	azcore.ClientOptions
}

// Client represents a URL to the Azure Storage container allowing you to manipulate its blobs.
type Client base.Client[generated.ContainerClient]

// NewClient creates an instance of Client with the specified values.
//   - containerURL - the URL of the container e.g. https://<account>.blob.core.windows.net/container
//   - cred - an Azure AD credential, typically obtained via the azidentity module
//   - options - client options; pass nil to accept the default values
func NewClient(containerURL string, cred azcore.TokenCredential, options *ClientOptions) (*Client, error) {
	authPolicy := runtime.NewBearerTokenPolicy(cred, []string{shared.TokenScope}, nil)
	conOptions := shared.GetClientOptions(options)
	conOptions.PerRetryPolicies = append(conOptions.PerRetryPolicies, authPolicy)
	pl := runtime.NewPipeline(exported.ModuleName, exported.ModuleVersion, runtime.PipelineOptions{}, &conOptions.ClientOptions)

	return (*Client)(base.NewContainerClient(containerURL, pl, nil)), nil
}

// NewClientWithNoCredential creates an instance of Client with the specified values.
// This is used to anonymously access a container or with a shared access signature (SAS) token.
//   - containerURL - the URL of the container e.g. https://<account>.blob.core.windows.net/container?<sas token>
//   - options - client options; pass nil to accept the default values
func NewClientWithNoCredential(containerURL string, options *ClientOptions) (*Client, error) {
	conOptions := shared.GetClientOptions(options)
	pl := runtime.NewPipeline(exported.ModuleName, exported.ModuleVersion, runtime.PipelineOptions{}, &conOptions.ClientOptions)

	return (*Client)(base.NewContainerClient(containerURL, pl, nil)), nil
}

// NewClientWithSharedKeyCredential creates an instance of Client with the specified values.
//   - containerURL - the URL of the container e.g. https://<account>.blob.core.windows.net/container
//   - cred - a SharedKeyCredential created with the matching container's storage account and access key
//   - options - client options; pass nil to accept the default values
func NewClientWithSharedKeyCredential(containerURL string, cred *SharedKeyCredential, options *ClientOptions) (*Client, error) {
	authPolicy := exported.NewSharedKeyCredPolicy(cred)
	conOptions := shared.GetClientOptions(options)
	conOptions.PerRetryPolicies = append(conOptions.PerRetryPolicies, authPolicy)
	pl := runtime.NewPipeline(exported.ModuleName, exported.ModuleVersion, runtime.PipelineOptions{}, &conOptions.ClientOptions)

	return (*Client)(base.NewContainerClient(containerURL, pl, cred)), nil
}

// NewClientFromConnectionString creates an instance of Client with the specified values.
//   - connectionString - a connection string for the desired storage account
//   - containerName - the name of the container within the storage account
//   - options - client options; pass nil to accept the default values
func NewClientFromConnectionString(connectionString string, containerName string, options *ClientOptions) (*Client, error) {
	parsed, err := shared.ParseConnectionString(connectionString)
	if err != nil {
		return nil, err
	}
	parsed.ServiceURL = runtime.JoinPaths(parsed.ServiceURL, containerName)

	if parsed.AccountKey != "" && parsed.AccountName != "" {
		credential, err := exported.NewSharedKeyCredential(parsed.AccountName, parsed.AccountKey)
		if err != nil {
			return nil, err
		}
		return NewClientWithSharedKeyCredential(parsed.ServiceURL, credential, options)
	}

	return NewClientWithNoCredential(parsed.ServiceURL, options)
}

func (c *Client) generated() *generated.ContainerClient {
	return base.InnerClient((*base.Client[generated.ContainerClient])(c))
}

func (c *Client) sharedKey() *SharedKeyCredential {
	return base.SharedKey((*base.Client[generated.ContainerClient])(c))
}

// URL returns the URL endpoint used by the Client object.
func (c *Client) URL() string {
	return c.generated().Endpoint()
}

// NewBlobClient creates a new BlobClient object by concatenating blobName to the end of
// Client's URL. The new BlobClient uses the same request policy pipeline as the Client.
// To change the pipeline, create the BlobClient and then call its WithPipeline method passing in the
// desired pipeline object. Or, call this package's NewBlobClient instead of calling this object's
// NewBlobClient method.
func (c *Client) NewBlobClient(blobName string) *blob.Client {
	blobURL := runtime.JoinPaths(c.URL(), blobName)
	return (*blob.Client)(base.NewBlobClient(blobURL, c.generated().Pipeline(), c.sharedKey()))
}

// NewAppendBlobClient creates a new AppendBlobURL object by concatenating blobName to the end of
// Client's URL. The new AppendBlobURL uses the same request policy pipeline as the Client.
// To change the pipeline, create the AppendBlobURL and then call its WithPipeline method passing in the
// desired pipeline object. Or, call this package's NewAppendBlobClient instead of calling this object's
// NewAppendBlobClient method.
func (c *Client) NewAppendBlobClient(blobName string) *appendblob.Client {
	blobURL := runtime.JoinPaths(c.URL(), blobName)
	return (*appendblob.Client)(base.NewAppendBlobClient(blobURL, c.generated().Pipeline(), c.sharedKey()))
}

// NewBlockBlobClient creates a new BlockBlobClient object by concatenating blobName to the end of
// Client's URL. The new BlockBlobClient uses the same request policy pipeline as the Client.
// To change the pipeline, create the BlockBlobClient and then call its WithPipeline method passing in the
// desired pipeline object. Or, call this package's NewBlockBlobClient instead of calling this object's
// NewBlockBlobClient method.
func (c *Client) NewBlockBlobClient(blobName string) *blockblob.Client {
	blobURL := runtime.JoinPaths(c.URL(), blobName)
	return (*blockblob.Client)(base.NewBlockBlobClient(blobURL, c.generated().Pipeline(), c.sharedKey()))
}

// NewPageBlobClient creates a new PageBlobURL object by concatenating blobName to the end of Client's URL. The new PageBlobURL uses the same request policy pipeline as the Client.
// To change the pipeline, create the PageBlobURL and then call its WithPipeline method passing in the
// desired pipeline object. Or, call this package's NewPageBlobClient instead of calling this object's
// NewPageBlobClient method.
func (c *Client) NewPageBlobClient(blobName string) *pageblob.Client {
	blobURL := runtime.JoinPaths(c.URL(), blobName)
	return (*pageblob.Client)(base.NewPageBlobClient(blobURL, c.generated().Pipeline(), c.sharedKey()))
}

// Create creates a new container within a storage account. If a container with the same name already exists, the operation fails.
// For more information, see https://docs.microsoft.com/rest/api/storageservices/create-container.
func (c *Client) Create(ctx context.Context, options *CreateOptions) (CreateResponse, error) {
	var opts *generated.ContainerClientCreateOptions
	var cpkScopes *generated.ContainerCpkScopeInfo
	if options != nil {
		opts = &generated.ContainerClientCreateOptions{
			Access:   options.Access,
			Metadata: options.Metadata,
		}
		cpkScopes = options.CpkScopeInfo
	}
	resp, err := c.generated().Create(ctx, opts, cpkScopes)

	return resp, err
}

// Delete marks the specified container for deletion. The container and any blobs contained within it are later deleted during garbage collection.
// For more information, see https://docs.microsoft.com/rest/api/storageservices/delete-container.
func (c *Client) Delete(ctx context.Context, options *DeleteOptions) (DeleteResponse, error) {
	opts, leaseAccessConditions, modifiedAccessConditions := options.format()
	resp, err := c.generated().Delete(ctx, opts, leaseAccessConditions, modifiedAccessConditions)

	return resp, err
}

// Restore operation restore the contents and properties of a soft deleted container to a specified container.
// For more information, see https://docs.microsoft.com/en-us/rest/api/storageservices/restore-container.
func (c *Client) Restore(ctx context.Context, deletedContainerVersion string, options *RestoreOptions) (RestoreResponse, error) {
	urlParts, err := blob.ParseURL(c.URL())
	if err != nil {
		return RestoreResponse{}, err
	}

	opts := &generated.ContainerClientRestoreOptions{
		DeletedContainerName:    &urlParts.ContainerName,
		DeletedContainerVersion: &deletedContainerVersion,
	}
	resp, err := c.generated().Restore(ctx, opts)

	return resp, err
}

// GetProperties returns the container's properties.
// For more information, see https://docs.microsoft.com/rest/api/storageservices/get-container-metadata.
func (c *Client) GetProperties(ctx context.Context, o *GetPropertiesOptions) (GetPropertiesResponse, error) {
	// NOTE: GetMetadata actually calls GetProperties internally because GetProperties returns the metadata AND the properties.
	// This allows us to not expose a GetProperties method at all simplifying the API.
	// The optionals are nil, like they were in track 1.5
	opts, leaseAccessConditions := o.format()

	resp, err := c.generated().GetProperties(ctx, opts, leaseAccessConditions)
	return resp, err
}

// SetMetadata sets the container's metadata.
// For more information, see https://docs.microsoft.com/rest/api/storageservices/set-container-metadata.
func (c *Client) SetMetadata(ctx context.Context, o *SetMetadataOptions) (SetMetadataResponse, error) {
	metadataOptions, lac, mac := o.format()
	resp, err := c.generated().SetMetadata(ctx, metadataOptions, lac, mac)

	return resp, err
}

// GetAccessPolicy returns the container's access policy. The access policy indicates whether container's blobs may be accessed publicly.
// For more information, see https://docs.microsoft.com/rest/api/storageservices/get-container-acl.
func (c *Client) GetAccessPolicy(ctx context.Context, o *GetAccessPolicyOptions) (GetAccessPolicyResponse, error) {
	options, ac := o.format()
	resp, err := c.generated().GetAccessPolicy(ctx, options, ac)
	return resp, err
}

// SetAccessPolicy sets the container's permissions. The access policy indicates whether blobs in a container may be accessed publicly.
// For more information, see https://docs.microsoft.com/rest/api/storageservices/set-container-acl.
func (c *Client) SetAccessPolicy(ctx context.Context, containerACL []*SignedIdentifier, o *SetAccessPolicyOptions) (SetAccessPolicyResponse, error) {
	accessPolicy, mac, lac := o.format()
	resp, err := c.generated().SetAccessPolicy(ctx, containerACL, accessPolicy, mac, lac)
	return resp, err
}

// NewListBlobsFlatPager returns a pager for blobs starting from the specified Marker. Use an empty
// Marker to start enumeration from the beginning. Blob names are returned in lexicographic order.
// For more information, see https://docs.microsoft.com/rest/api/storageservices/list-blobs.
func (c *Client) NewListBlobsFlatPager(o *ListBlobsFlatOptions) *runtime.Pager[ListBlobsFlatResponse] {
	listOptions := generated.ContainerClientListBlobFlatSegmentOptions{}
	if o != nil {
		listOptions.Include = o.Include.format()
		listOptions.Marker = o.Marker
		listOptions.Maxresults = o.MaxResults
		listOptions.Prefix = o.Prefix
	}
	return runtime.NewPager(runtime.PagingHandler[ListBlobsFlatResponse]{
		More: func(page ListBlobsFlatResponse) bool {
			return page.NextMarker != nil && len(*page.NextMarker) > 0
		},
		Fetcher: func(ctx context.Context, page *ListBlobsFlatResponse) (ListBlobsFlatResponse, error) {
			var req *policy.Request
			var err error
			if page == nil {
				req, err = c.generated().ListBlobFlatSegmentCreateRequest(ctx, &listOptions)
			} else {
				listOptions.Marker = page.NextMarker
				req, err = c.generated().ListBlobFlatSegmentCreateRequest(ctx, &listOptions)
			}
			if err != nil {
				return ListBlobsFlatResponse{}, err
			}
			resp, err := c.generated().Pipeline().Do(req)
			if err != nil {
				return ListBlobsFlatResponse{}, err
			}
			if !runtime.HasStatusCode(resp, http.StatusOK) {
				// TOOD: storage error?
				return ListBlobsFlatResponse{}, runtime.NewResponseError(resp)
			}
			return c.generated().ListBlobFlatSegmentHandleResponse(resp)
		},
	})
}

// NewListBlobsHierarchyPager returns a channel of blobs starting from the specified Marker. Use an empty
// Marker to start enumeration from the beginning. Blob names are returned in lexicographic order.
// After getting a segment, process it, and then call ListBlobsHierarchicalSegment again (passing the the
// previously-returned Marker) to get the next segment.
// For more information, see https://docs.microsoft.com/rest/api/storageservices/list-blobs.
// AutoPagerTimeout specifies the amount of time with no read operations before the channel times out and closes. Specify no time and it will be ignored.
// AutoPagerBufferSize specifies the channel's buffer size.
// Both the blob item channel and error channel should be watched. Only one error will be released via this channel (or a nil error, to register a clean exit.)
func (c *Client) NewListBlobsHierarchyPager(delimiter string, o *ListBlobsHierarchyOptions) *runtime.Pager[ListBlobsHierarchyResponse] {
	listOptions := o.format()
	return runtime.NewPager(runtime.PagingHandler[ListBlobsHierarchyResponse]{
		More: func(page ListBlobsHierarchyResponse) bool {
			return page.NextMarker != nil && len(*page.NextMarker) > 0
		},
		Fetcher: func(ctx context.Context, page *ListBlobsHierarchyResponse) (ListBlobsHierarchyResponse, error) {
			var req *policy.Request
			var err error
			if page == nil {
				req, err = c.generated().ListBlobHierarchySegmentCreateRequest(ctx, delimiter, &listOptions)
			} else {
				listOptions.Marker = page.NextMarker
				req, err = c.generated().ListBlobHierarchySegmentCreateRequest(ctx, delimiter, &listOptions)
			}
			if err != nil {
				return ListBlobsHierarchyResponse{}, err
			}
			resp, err := c.generated().Pipeline().Do(req)
			if err != nil {
				return ListBlobsHierarchyResponse{}, err
			}
			if !runtime.HasStatusCode(resp, http.StatusOK) {
				return ListBlobsHierarchyResponse{}, runtime.NewResponseError(resp)
			}
			return c.generated().ListBlobHierarchySegmentHandleResponse(resp)
		},
	})
}

// GetSASURL is a convenience method for generating a SAS token for the currently pointed at container.
// It can only be used if the credential supplied during creation was a SharedKeyCredential.
func (c *Client) GetSASURL(permissions sas.ContainerPermissions, start time.Time, expiry time.Time) (string, error) {
	if c.sharedKey() == nil {
		return "", errors.New("SAS can only be signed with a SharedKeyCredential")
	}

	urlParts, err := blob.ParseURL(c.URL())
	if err != nil {
		return "", err
	}

	// Containers do not have snapshots, nor versions.
	qps, err := sas.BlobSignatureValues{
		Version:       sas.Version,
		Protocol:      sas.ProtocolHTTPS,
		ContainerName: urlParts.ContainerName,
		Permissions:   permissions.String(),
		StartTime:     start.UTC(),
		ExpiryTime:    expiry.UTC(),
	}.SignWithSharedKey(c.sharedKey())
	if err != nil {
		return "", err
	}

	endpoint := c.URL() + "?" + qps.Encode()

	return endpoint, nil
}
