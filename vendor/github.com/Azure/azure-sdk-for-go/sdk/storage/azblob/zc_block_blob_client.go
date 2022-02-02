// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License.

package azblob

import (
	"context"
	"io"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/runtime"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/to"
)

const (
	// BlockBlobMaxUploadBlobBytes indicates the maximum number of bytes that can be sent in a call to Upload.
	BlockBlobMaxUploadBlobBytes = 256 * 1024 * 1024 // 256MB

	// BlockBlobMaxStageBlockBytes indicates the maximum number of bytes that can be sent in a call to StageBlock.
	BlockBlobMaxStageBlockBytes = 4000 * 1024 * 1024 // 4GB

	// BlockBlobMaxBlocks indicates the maximum number of blocks allowed in a block blob.
	BlockBlobMaxBlocks = 50000
)

// BlockBlobClient defines a set of operations applicable to block blobs.
type BlockBlobClient struct {
	BlobClient
	client *blockBlobClient
}

// NewBlockBlobClient creates a BlockBlobClient object using the specified URL, Azure AD credential, and options.
func NewBlockBlobClient(blobURL string, cred azcore.TokenCredential, options *ClientOptions) (BlockBlobClient, error) {
	authPolicy := runtime.NewBearerTokenPolicy(cred, []string{tokenScope}, nil)
	con := newConnection(blobURL, authPolicy, options.getConnectionOptions())
	return BlockBlobClient{
		client:     &blockBlobClient{con: con},
		BlobClient: BlobClient{client: &blobClient{con: con}},
	}, nil
}

// NewBlockBlobClientWithNoCredential creates a BlockBlobClient object using the specified URL and options.
func NewBlockBlobClientWithNoCredential(blobURL string, options *ClientOptions) (BlockBlobClient, error) {
	con := newConnection(blobURL, nil, options.getConnectionOptions())
	return BlockBlobClient{
		client:     &blockBlobClient{con: con},
		BlobClient: BlobClient{client: &blobClient{con: con}},
	}, nil
}

// NewBlockBlobClientWithSharedKey creates a BlockBlobClient object using the specified URL, shared key, and options.
func NewBlockBlobClientWithSharedKey(blobURL string, cred *SharedKeyCredential, options *ClientOptions) (BlockBlobClient, error) {
	authPolicy := newSharedKeyCredPolicy(cred)
	con := newConnection(blobURL, authPolicy, options.getConnectionOptions())
	return BlockBlobClient{
		client:     &blockBlobClient{con: con},
		BlobClient: BlobClient{client: &blobClient{con: con}},
	}, nil
}

// WithSnapshot creates a new BlockBlobClient object identical to the source but with the specified snapshot timestamp.
// Pass "" to remove the snapshot returning a URL to the base blob.
func (bb BlockBlobClient) WithSnapshot(snapshot string) BlockBlobClient {
	p := NewBlobURLParts(bb.URL())
	p.Snapshot = snapshot
	con := &connection{u: p.URL(), p: bb.client.con.p}
	return BlockBlobClient{
		client: &blockBlobClient{
			con: con,
		},
		BlobClient: BlobClient{client: &blobClient{con: con}},
	}
}

// WithVersionID creates a new AppendBlobURL object identical to the source but with the specified version id.
// Pass "" to remove the versionID returning a URL to the base blob.
func (bb BlockBlobClient) WithVersionID(versionID string) BlockBlobClient {
	p := NewBlobURLParts(bb.URL())
	p.VersionID = versionID
	con := &connection{u: p.URL(), p: bb.client.con.p}
	return BlockBlobClient{
		client:     &blockBlobClient{con: con},
		BlobClient: BlobClient{client: &blobClient{con: con}},
	}
}

// Upload creates a new block blob or overwrites an existing block blob.
// Updating an existing block blob overwrites any existing metadata on the blob. Partial updates are not
// supported with Upload; the content of the existing blob is overwritten with the new content. To
// perform a partial update of a block blob, use StageBlock and CommitBlockList.
// This method panics if the stream is not at position 0.
// Note that the http client closes the body stream after the request is sent to the service.
// For more information, see https://docs.microsoft.com/rest/api/storageservices/put-blob.
func (bb BlockBlobClient) Upload(ctx context.Context, body io.ReadSeekCloser, options *UploadBlockBlobOptions) (BlockBlobUploadResponse, error) {
	count, err := validateSeekableStreamAt0AndGetCount(body)
	if err != nil {
		return BlockBlobUploadResponse{}, err
	}

	basics, httpHeaders, leaseInfo, cpkV, cpkN, accessConditions := options.pointers()

	resp, err := bb.client.Upload(ctx, count, body, basics, httpHeaders, leaseInfo, cpkV, cpkN, accessConditions)

	return resp, handleError(err)
}

// StageBlock uploads the specified block to the block blob's "staging area" to be later committed by a call to CommitBlockList.
// Note that the http client closes the body stream after the request is sent to the service.
// For more information, see https://docs.microsoft.com/rest/api/storageservices/put-block.
func (bb BlockBlobClient) StageBlock(ctx context.Context, base64BlockID string, body io.ReadSeekCloser, options *StageBlockOptions) (BlockBlobStageBlockResponse, error) {
	count, err := validateSeekableStreamAt0AndGetCount(body)
	if err != nil {
		return BlockBlobStageBlockResponse{}, err
	}

	ac, stageBlockOptions, cpkInfo, cpkScopeInfo := options.pointers()
	resp, err := bb.client.StageBlock(ctx, base64BlockID, count, body, stageBlockOptions, ac, cpkInfo, cpkScopeInfo)

	return resp, handleError(err)
}

// StageBlockFromURL copies the specified block from a source URL to the block blob's "staging area" to be later committed by a call to CommitBlockList.
// If count is CountToEnd (0), then data is read from specified offset to the end.
// For more information, see https://docs.microsoft.com/en-us/rest/api/storageservices/put-block-from-url.
func (bb BlockBlobClient) StageBlockFromURL(ctx context.Context, base64BlockID string, sourceURL string, contentLength int64, options *StageBlockFromURLOptions) (BlockBlobStageBlockFromURLResponse, error) {
	ac, smac, stageOptions, cpkInfo, cpkScope := options.pointers()

	resp, err := bb.client.StageBlockFromURL(ctx, base64BlockID, contentLength, sourceURL, stageOptions, cpkInfo, cpkScope, ac, smac)

	return resp, handleError(err)
}

// CommitBlockList writes a blob by specifying the list of block IDs that make up the blob.
// In order to be written as part of a blob, a block must have been successfully written
// to the server in a prior PutBlock operation. You can call PutBlockList to update a blob
// by uploading only those blocks that have changed, then committing the new and existing
// blocks together. Any blocks not specified in the block list and permanently deleted.
// For more information, see https://docs.microsoft.com/rest/api/storageservices/put-block-list.
func (bb BlockBlobClient) CommitBlockList(ctx context.Context, base64BlockIDs []string, options *CommitBlockListOptions) (BlockBlobCommitBlockListResponse, error) {
	commitOptions, headers, cpkInfo, cpkScope, modifiedAccess, leaseAccess := options.pointers()

	// this is a code smell in the generated code
	blockIds := make([]*string, len(base64BlockIDs))
	for k, v := range base64BlockIDs {
		blockIds[k] = to.StringPtr(v)
	}

	resp, err := bb.client.CommitBlockList(ctx, BlockLookupList{
		Latest: blockIds,
	}, commitOptions, headers, leaseAccess, cpkInfo, cpkScope, modifiedAccess)

	return resp, handleError(err)
}

// GetBlockList returns the list of blocks that have been uploaded as part of a block blob using the specified block list filter.
// For more information, see https://docs.microsoft.com/rest/api/storageservices/get-block-list.
func (bb BlockBlobClient) GetBlockList(ctx context.Context, listType BlockListType, options *GetBlockListOptions) (BlockBlobGetBlockListResponse, error) {
	o, mac, lac := options.pointers()

	resp, err := bb.client.GetBlockList(ctx, listType, o, lac, mac)

	return resp, handleError(err)
}

// CopyFromURL synchronously copies the data at the source URL to a block blob, with sizes up to 256 MB.
// For more information, see https://docs.microsoft.com/en-us/rest/api/storageservices/copy-blob-from-url.
func (bb BlockBlobClient) CopyFromURL(ctx context.Context, source string, options *CopyBlockBlobFromURLOptions) (BlobCopyFromURLResponse, error) {
	copyOptions, smac, mac, lac := options.pointers()

	bClient := blobClient{
		con: bb.client.con,
	}

	resp, err := bClient.CopyFromURL(ctx, source, copyOptions, smac, mac, lac)

	return resp, handleError(err)
}
