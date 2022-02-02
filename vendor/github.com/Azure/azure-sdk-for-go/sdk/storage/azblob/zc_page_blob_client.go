// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License.

package azblob

import (
	"context"
	"io"
	"net/url"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/runtime"
)

const (
	// PageBlobPageBytes indicates the number of bytes in a page (512).
	PageBlobPageBytes = 512
)

type PageBlobClient struct {
	BlobClient
	client *pageBlobClient
}

// NewPageBlobClient creates a ServiceClient object using the specified URL, Azure AD credential, and options.
// Example of serviceURL: https://<your_storage_account>.blob.core.windows.net
func NewPageBlobClient(blobURL string, cred azcore.TokenCredential, options *ClientOptions) (PageBlobClient, error) {
	authPolicy := runtime.NewBearerTokenPolicy(cred, []string{tokenScope}, nil)
	con := newConnection(blobURL, authPolicy, options.getConnectionOptions())
	return PageBlobClient{
		client:     &pageBlobClient{con: con},
		BlobClient: BlobClient{client: &blobClient{con: con}},
	}, nil
}

// NewPageBlobClientWithNoCredential creates a ServiceClient object using the specified URL and options.
// Example of serviceURL: https://<your_storage_account>.blob.core.windows.net?<SAS token>
func NewPageBlobClientWithNoCredential(blobURL string, options *ClientOptions) (PageBlobClient, error) {
	con := newConnection(blobURL, nil, options.getConnectionOptions())
	return PageBlobClient{
		client:     &pageBlobClient{con: con},
		BlobClient: BlobClient{client: &blobClient{con: con}},
	}, nil
}

// NewPageBlobClientWithSharedKey creates a ServiceClient object using the specified URL, shared key, and options.
// Example of serviceURL: https://<your_storage_account>.blob.core.windows.net
func NewPageBlobClientWithSharedKey(blobURL string, cred *SharedKeyCredential, options *ClientOptions) (PageBlobClient, error) {
	authPolicy := newSharedKeyCredPolicy(cred)
	con := newConnection(blobURL, authPolicy, options.getConnectionOptions())
	return PageBlobClient{
		client:     &pageBlobClient{con: con},
		BlobClient: BlobClient{client: &blobClient{con: con}},
	}, nil
}

// WithSnapshot creates a new PageBlobURL object identical to the source but with the specified snapshot timestamp.
// Pass "" to remove the snapshot returning a URL to the base blob.
func (pb PageBlobClient) WithSnapshot(snapshot string) PageBlobClient {
	p := NewBlobURLParts(pb.URL())
	p.Snapshot = snapshot

	con := &connection{p.URL(), pb.client.con.p}
	return PageBlobClient{
		client:     &pageBlobClient{con: con},
		BlobClient: BlobClient{client: &blobClient{con: con}},
	}
}

// WithVersionID creates a new PageBlobURL object identical to the source but with the specified snapshot timestamp.
// Pass "" to remove the version returning a URL to the base blob.
func (pb PageBlobClient) WithVersionID(versionID string) PageBlobClient {
	p := NewBlobURLParts(pb.URL())
	p.VersionID = versionID

	con := &connection{p.URL(), pb.client.con.p}
	return PageBlobClient{
		client:     &pageBlobClient{con: con},
		BlobClient: BlobClient{client: &blobClient{con: con}},
	}
}

// Create creates a page blob of the specified length. Call PutPage to upload data to a page blob.
// For more information, see https://docs.microsoft.com/rest/api/storageservices/put-blob.
func (pb PageBlobClient) Create(ctx context.Context, size int64, options *CreatePageBlobOptions) (PageBlobCreateResponse, error) {
	creationOptions, httpHeaders, cpkInfo, cpkScope, lac, mac := options.pointers()

	resp, err := pb.client.Create(ctx, 0, size, creationOptions, httpHeaders, lac, cpkInfo, cpkScope, mac)

	return resp, handleError(err)
}

// UploadPages writes 1 or more pages to the page blob. The start offset and the stream size must be a multiple of 512 bytes.
// This method panics if the stream is not at position 0.
// Note that the http client closes the body stream after the request is sent to the service.
// For more information, see https://docs.microsoft.com/rest/api/storageservices/put-page.
func (pb PageBlobClient) UploadPages(ctx context.Context, body io.ReadSeekCloser, options *UploadPagesOptions) (PageBlobUploadPagesResponse, error) {
	count, err := validateSeekableStreamAt0AndGetCount(body)

	if err != nil {
		return PageBlobUploadPagesResponse{}, err
	}

	uploadOptions, cpkInfo, cpkScope, snac, lac, mac := options.pointers()

	resp, err := pb.client.UploadPages(ctx, count, body, uploadOptions, lac, cpkInfo, cpkScope, snac, mac)

	return resp, handleError(err)
}

// UploadPagesFromURL copies 1 or more pages from a source URL to the page blob.
// The sourceOffset specifies the start offset of source data to copy from.
// The destOffset specifies the start offset of data in page blob will be written to.
// The count must be a multiple of 512 bytes.
// For more information, see https://docs.microsoft.com/rest/api/storageservices/put-page-from-url.
func (pb PageBlobClient) UploadPagesFromURL(ctx context.Context, source string, sourceOffset, destOffset, count int64, options *UploadPagesFromURLOptions) (PageBlobUploadPagesFromURLResponse, error) {
	uploadOptions, cpkInfo, cpkScope, snac, smac, lac, mac := options.pointers()

	resp, err := pb.client.UploadPagesFromURL(ctx, source, rangeToString(sourceOffset, count), 0, rangeToString(destOffset, count), uploadOptions, cpkInfo, cpkScope, lac, snac, mac, smac)

	return resp, handleError(err)
}

// ClearPages frees the specified pages from the page blob.
// For more information, see https://docs.microsoft.com/rest/api/storageservices/put-page.
func (pb PageBlobClient) ClearPages(ctx context.Context, pageRange HttpRange, options *ClearPagesOptions) (PageBlobClearPagesResponse, error) {
	clearOptions := &PageBlobClearPagesOptions{
		Range: pageRange.pointers(),
	}

	cpkInfo, cpkScope, snac, lac, mac := options.pointers()

	resp, err := pb.client.ClearPages(ctx, 0, clearOptions, lac, cpkInfo, cpkScope, snac, mac)

	return resp, handleError(err)
}

// GetPageRanges returns the list of valid page ranges for a page blob or snapshot of a page blob.
// For more information, see https://docs.microsoft.com/rest/api/storageservices/get-page-ranges.
func (pb PageBlobClient) GetPageRanges(ctx context.Context, pageRange HttpRange, options *GetPageRangesOptions) (PageBlobGetPageRangesResponse, error) {
	snapshot, lac, mac := options.pointers()

	getRangesOptions := &PageBlobGetPageRangesOptions{
		Range:    pageRange.pointers(),
		Snapshot: snapshot,
	}

	resp, err := pb.client.GetPageRanges(ctx, getRangesOptions, lac, mac)

	return resp, handleError(err)
}

// GetManagedDiskPageRangesDiff gets the collection of page ranges that differ between a specified snapshot and this page blob representing managed disk.
// For more information, see https://docs.microsoft.com/rest/api/storageservices/get-page-ranges.
//func (pb PageBlobURL) GetManagedDiskPageRangesDiff(ctx context.Context, offset int64, count int64, prevSnapshot *string, prevSnapshotURL *string, ac BlobAccessConditions) (*PageList, error) {
//	ifModifiedSince, ifUnmodifiedSince, ifMatchETag, ifNoneMatchETag := ac.ModifiedAccessConditions.pointers()
//
//	return pb.pbClient.GetPageRangesDiff(ctx, nil, nil, prevSnapshot,
//		prevSnapshotURL, // Get managed disk diff
//		HttpRange{offset: offset, count: count}.pointers(),
//		ac.LeaseAccessConditions.pointers(),
//		ifModifiedSince, ifUnmodifiedSince, ifMatchETag, ifNoneMatchETag,
//		nil, // Blob ifTags
//		nil)
//}

// GetPageRangesDiff gets the collection of page ranges that differ between a specified snapshot and this page blob.
// For more information, see https://docs.microsoft.com/rest/api/storageservices/get-page-ranges.
func (pb PageBlobClient) GetPageRangesDiff(ctx context.Context, pageRange HttpRange, prevSnapshot string, options *GetPageRangesOptions) (PageBlobGetPageRangesDiffResponse, error) {
	snapshot, lac, mac := options.pointers()

	diffOptions := &PageBlobGetPageRangesDiffOptions{
		Prevsnapshot: &prevSnapshot,
		Range:        pageRange.pointers(),
		Snapshot:     snapshot,
	}

	resp, err := pb.client.GetPageRangesDiff(ctx, diffOptions, lac, mac)

	return resp, handleError(err)
}

// Resize resizes the page blob to the specified size (which must be a multiple of 512).
// For more information, see https://docs.microsoft.com/rest/api/storageservices/set-blob-properties.
func (pb PageBlobClient) Resize(ctx context.Context, size int64, options *ResizePageBlobOptions) (PageBlobResizeResponse, error) {
	cpkInfo, cpkScope, lac, mac := options.pointers()

	resp, err := pb.client.Resize(ctx, size, nil, lac, cpkInfo, cpkScope, mac)

	return resp, handleError(err)
}

// UpdateSequenceNumber sets the page blob's sequence number.
func (pb PageBlobClient) UpdateSequenceNumber(ctx context.Context, options *UpdateSequenceNumberPageBlob) (PageBlobUpdateSequenceNumberResponse, error) {
	updateOptions, actionType, lac, mac := options.pointers()
	resp, err := pb.client.UpdateSequenceNumber(ctx, *actionType, updateOptions, lac, mac)

	return resp, handleError(err)
}

// StartCopyIncremental begins an operation to start an incremental copy from one page blob's snapshot to this page blob.
// The snapshot is copied such that only the differential changes between the previously copied snapshot are transferred to the destination.
// The copied snapshots are complete copies of the original snapshot and can be read or copied from as usual.
// For more information, see https://docs.microsoft.com/rest/api/storageservices/incremental-copy-blob and
// https://docs.microsoft.com/en-us/azure/virtual-machines/windows/incremental-snapshots.
func (pb PageBlobClient) StartCopyIncremental(ctx context.Context, source string, prevSnapshot string, options *CopyIncrementalPageBlobOptions) (PageBlobCopyIncrementalResponse, error) {
	srcURL, _ := url.Parse(source)

	queryParams := srcURL.Query()
	queryParams.Set("snapshot", prevSnapshot)
	srcURL.RawQuery = queryParams.Encode()

	pageBlobCopyIncrementalOptions, modifiedAccessConditions := options.pointers()
	resp, err := pb.client.CopyIncremental(ctx, srcURL.String(), pageBlobCopyIncrementalOptions, modifiedAccessConditions)

	return resp, handleError(err)
}
