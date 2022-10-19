//go:build go1.18
// +build go1.18

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

// BlockBlobClient defines a set of operations applicable to block blobs.
type BlockBlobClient struct {
	BlobClient
	client *blockBlobClient
}

// NewBlockBlobClient creates a BlockBlobClient object using the specified URL, Azure AD credential, and options.
func NewBlockBlobClient(blobURL string, cred azcore.TokenCredential, options *ClientOptions) (*BlockBlobClient, error) {
	authPolicy := runtime.NewBearerTokenPolicy(cred, []string{tokenScope}, nil)
	conOptions := getConnectionOptions(options)
	conOptions.PerRetryPolicies = append(conOptions.PerRetryPolicies, authPolicy)
	conn := newConnection(blobURL, conOptions)

	bClient := newBlobClient(conn.Endpoint(), conn.Pipeline())
	return &BlockBlobClient{
		client: newBlockBlobClient(bClient.endpoint, bClient.pl),
		BlobClient: BlobClient{
			client: bClient,
		},
	}, nil
}

// NewBlockBlobClientWithNoCredential creates a BlockBlobClient object using the specified URL and options.
func NewBlockBlobClientWithNoCredential(blobURL string, options *ClientOptions) (*BlockBlobClient, error) {
	conOptions := getConnectionOptions(options)
	conn := newConnection(blobURL, conOptions)

	bClient := newBlobClient(conn.Endpoint(), conn.Pipeline())
	return &BlockBlobClient{
		client: newBlockBlobClient(bClient.endpoint, bClient.pl),
		BlobClient: BlobClient{
			client: bClient,
		},
	}, nil
}

// NewBlockBlobClientWithSharedKey creates a BlockBlobClient object using the specified URL, shared key, and options.
func NewBlockBlobClientWithSharedKey(blobURL string, cred *SharedKeyCredential, options *ClientOptions) (*BlockBlobClient, error) {
	authPolicy := newSharedKeyCredPolicy(cred)
	conOptions := getConnectionOptions(options)
	conOptions.PerRetryPolicies = append(conOptions.PerRetryPolicies, authPolicy)
	conn := newConnection(blobURL, conOptions)

	bClient := newBlobClient(conn.Endpoint(), conn.Pipeline())
	return &BlockBlobClient{
		client: newBlockBlobClient(bClient.endpoint, bClient.pl),
		BlobClient: BlobClient{
			client:    bClient,
			sharedKey: cred,
		},
	}, nil
}

// WithSnapshot creates a new BlockBlobClient object identical to the source but with the specified snapshot timestamp.
// Pass "" to remove the snapshot returning a URL to the base blob.
func (bb *BlockBlobClient) WithSnapshot(snapshot string) (*BlockBlobClient, error) {
	p, err := NewBlobURLParts(bb.URL())
	if err != nil {
		return nil, err
	}

	p.Snapshot = snapshot
	endpoint := p.URL()
	bClient := newBlobClient(endpoint, bb.client.pl)

	return &BlockBlobClient{
		client: newBlockBlobClient(bClient.endpoint, bClient.pl),
		BlobClient: BlobClient{
			client:    bClient,
			sharedKey: bb.sharedKey,
		},
	}, nil
}

// WithVersionID creates a new AppendBlobURL object identical to the source but with the specified version id.
// Pass "" to remove the versionID returning a URL to the base blob.
func (bb *BlockBlobClient) WithVersionID(versionID string) (*BlockBlobClient, error) {
	p, err := NewBlobURLParts(bb.URL())
	if err != nil {
		return nil, err
	}

	p.VersionID = versionID
	endpoint := p.URL()
	bClient := newBlobClient(endpoint, bb.client.pl)

	return &BlockBlobClient{
		client: newBlockBlobClient(bClient.endpoint, bClient.pl),
		BlobClient: BlobClient{
			client:    bClient,
			sharedKey: bb.sharedKey,
		},
	}, nil
}

// Upload creates a new block blob or overwrites an existing block blob.
// Updating an existing block blob overwrites any existing metadata on the blob. Partial updates are not
// supported with Upload; the content of the existing blob is overwritten with the new content. To
// perform a partial update of a block blob, use StageBlock and CommitBlockList.
// This method panics if the stream is not at position 0.
// Note that the http client closes the body stream after the request is sent to the service.
// For more information, see https://docs.microsoft.com/rest/api/storageservices/put-blob.
func (bb *BlockBlobClient) Upload(ctx context.Context, body io.ReadSeekCloser, options *BlockBlobUploadOptions) (BlockBlobUploadResponse, error) {
	count, err := validateSeekableStreamAt0AndGetCount(body)
	if err != nil {
		return BlockBlobUploadResponse{}, err
	}

	basics, httpHeaders, leaseInfo, cpkV, cpkN, accessConditions := options.format()

	resp, err := bb.client.Upload(ctx, count, body, basics, httpHeaders, leaseInfo, cpkV, cpkN, accessConditions)

	return toBlockBlobUploadResponse(resp), handleError(err)
}

// StageBlock uploads the specified block to the block blob's "staging area" to be later committed by a call to CommitBlockList.
// Note that the http client closes the body stream after the request is sent to the service.
// For more information, see https://docs.microsoft.com/rest/api/storageservices/put-block.
func (bb *BlockBlobClient) StageBlock(ctx context.Context, base64BlockID string, body io.ReadSeekCloser,
	options *BlockBlobStageBlockOptions) (BlockBlobStageBlockResponse, error) {
	count, err := validateSeekableStreamAt0AndGetCount(body)
	if err != nil {
		return BlockBlobStageBlockResponse{}, err
	}

	stageBlockOptions, leaseAccessConditions, cpkInfo, cpkScopeInfo := options.format()
	resp, err := bb.client.StageBlock(ctx, base64BlockID, count, body, stageBlockOptions, leaseAccessConditions, cpkInfo, cpkScopeInfo)

	return toBlockBlobStageBlockResponse(resp), handleError(err)
}

// StageBlockFromURL copies the specified block from a source URL to the block blob's "staging area" to be later committed by a call to CommitBlockList.
// If count is CountToEnd (0), then data is read from specified offset to the end.
// For more information, see https://docs.microsoft.com/en-us/rest/api/storageservices/put-block-from-url.
func (bb *BlockBlobClient) StageBlockFromURL(ctx context.Context, base64BlockID string, sourceURL string,
	contentLength int64, options *BlockBlobStageBlockFromURLOptions) (BlockBlobStageBlockFromURLResponse, error) {

	stageBlockFromURLOptions, cpkInfo, cpkScopeInfo, leaseAccessConditions, sourceModifiedAccessConditions := options.format()

	resp, err := bb.client.StageBlockFromURL(ctx, base64BlockID, contentLength, sourceURL, stageBlockFromURLOptions,
		cpkInfo, cpkScopeInfo, leaseAccessConditions, sourceModifiedAccessConditions)

	return toBlockBlobStageBlockFromURLResponse(resp), handleError(err)
}

// CommitBlockList writes a blob by specifying the list of block IDs that make up the blob.
// In order to be written as part of a blob, a block must have been successfully written
// to the server in a prior PutBlock operation. You can call PutBlockList to update a blob
// by uploading only those blocks that have changed, then committing the new and existing
// blocks together. Any blocks not specified in the block list and permanently deleted.
// For more information, see https://docs.microsoft.com/rest/api/storageservices/put-block-list.
func (bb *BlockBlobClient) CommitBlockList(ctx context.Context, base64BlockIDs []string, options *BlockBlobCommitBlockListOptions) (BlockBlobCommitBlockListResponse, error) {
	// this is a code smell in the generated code
	blockIds := make([]*string, len(base64BlockIDs))
	for k, v := range base64BlockIDs {
		blockIds[k] = to.Ptr(v)
	}

	blockLookupList := BlockLookupList{Latest: blockIds}
	commitOptions, headers, leaseAccess, cpkInfo, cpkScope, modifiedAccess := options.format()

	resp, err := bb.client.CommitBlockList(ctx, blockLookupList, commitOptions, headers, leaseAccess, cpkInfo, cpkScope, modifiedAccess)

	return toBlockBlobCommitBlockListResponse(resp), handleError(err)
}

// GetBlockList returns the list of blocks that have been uploaded as part of a block blob using the specified block list filter.
// For more information, see https://docs.microsoft.com/rest/api/storageservices/get-block-list.
func (bb *BlockBlobClient) GetBlockList(ctx context.Context, listType BlockListType, options *BlockBlobGetBlockListOptions) (BlockBlobGetBlockListResponse, error) {
	o, lac, mac := options.format()

	resp, err := bb.client.GetBlockList(ctx, listType, o, lac, mac)

	return toBlockBlobGetBlockListResponse(resp), handleError(err)
}

// CopyFromURL synchronously copies the data at the source URL to a block blob, with sizes up to 256 MB.
// For more information, see https://docs.microsoft.com/en-us/rest/api/storageservices/copy-blob-from-url.
func (bb *BlockBlobClient) CopyFromURL(ctx context.Context, source string, options *BlockBlobCopyFromURLOptions) (BlockBlobCopyFromURLResponse, error) {
	copyOptions, smac, mac, lac := options.format()
	resp, err := bb.BlobClient.client.CopyFromURL(ctx, source, copyOptions, smac, mac, lac)

	return toBlockBlobCopyFromURLResponse(resp), handleError(err)
}
