// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License.

package azblob

import (
	"context"
	"io"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/runtime"
)

type AppendBlobClient struct {
	BlobClient
	client *appendBlobClient
}

// NewAppendBlobClient creates an AppendBlobClient with the specified URL, Azure AD credential, and options.
func NewAppendBlobClient(blobURL string, cred azcore.TokenCredential, options *ClientOptions) (AppendBlobClient, error) {
	authPolicy := runtime.NewBearerTokenPolicy(cred, []string{tokenScope}, nil)
	con := newConnection(blobURL, authPolicy, options.getConnectionOptions())
	return AppendBlobClient{
		client:     &appendBlobClient{con: con},
		BlobClient: BlobClient{client: &blobClient{con: con}},
	}, nil
}

// NewAppendBlobClientWithNoCredential creates an AppendBlobClient with the specified URL and options.
func NewAppendBlobClientWithNoCredential(blobURL string, options *ClientOptions) (AppendBlobClient, error) {
	con := newConnection(blobURL, nil, options.getConnectionOptions())
	return AppendBlobClient{
		client:     &appendBlobClient{con: con},
		BlobClient: BlobClient{client: &blobClient{con: con}},
	}, nil
}

// NewAppendBlobClientWithSharedKey creates an AppendBlobClient with the specified URL, shared key, and options.
func NewAppendBlobClientWithSharedKey(blobURL string, cred *SharedKeyCredential, options *ClientOptions) (AppendBlobClient, error) {
	authPolicy := newSharedKeyCredPolicy(cred)
	con := newConnection(blobURL, authPolicy, options.getConnectionOptions())
	return AppendBlobClient{
		client:     &appendBlobClient{con: con},
		BlobClient: BlobClient{client: &blobClient{con: con}},
	}, nil
}

// WithSnapshot creates a new AppendBlobURL object identical to the source but with the specified snapshot timestamp.
// Pass "" to remove the snapshot returning a URL to the base blob.
func (ab AppendBlobClient) WithSnapshot(snapshot string) AppendBlobClient {
	p := NewBlobURLParts(ab.URL())
	p.Snapshot = snapshot
	con := &connection{u: p.URL(), p: ab.client.con.p}

	return AppendBlobClient{
		client:     &appendBlobClient{con: con},
		BlobClient: BlobClient{client: &blobClient{con: con}},
	}
}

// WithVersionID creates a new AppendBlobURL object identical to the source but with the specified version id.
// Pass "" to remove the versionID returning a URL to the base blob.
func (ab AppendBlobClient) WithVersionID(versionID string) AppendBlobClient {
	p := NewBlobURLParts(ab.URL())
	p.VersionID = versionID
	con := &connection{u: p.URL(), p: ab.client.con.p}

	return AppendBlobClient{
		client:     &appendBlobClient{con: con},
		BlobClient: BlobClient{client: &blobClient{con: con}},
	}
}

// Create creates a 0-size append blob. Call AppendBlock to append data to an append blob.
// For more information, see https://docs.microsoft.com/rest/api/storageservices/put-blob.
func (ab AppendBlobClient) Create(ctx context.Context, options *CreateAppendBlobOptions) (AppendBlobCreateResponse, error) {
	appendBlobAppendBlockOptions, blobHttpHeaders, leaseAccessConditions, cpkInfo, cpkScopeInfo, modifiedAccessConditions := options.pointers()
	resp, err := ab.client.Create(ctx, 0, appendBlobAppendBlockOptions, blobHttpHeaders, leaseAccessConditions, cpkInfo, cpkScopeInfo, modifiedAccessConditions)

	return resp, handleError(err)
}

// AppendBlock writes a stream to a new block of data to the end of the existing append blob.
// This method panics if the stream is not at position 0.
// Note that the http client closes the body stream after the request is sent to the service.
// For more information, see https://docs.microsoft.com/rest/api/storageservices/append-block.
func (ab AppendBlobClient) AppendBlock(ctx context.Context, body io.ReadSeekCloser, options *AppendBlockOptions) (AppendBlobAppendBlockResponse, error) {
	count, err := validateSeekableStreamAt0AndGetCount(body)
	if err != nil {
		return AppendBlobAppendBlockResponse{}, nil
	}

	appendOptions, aac, cpkinfo, cpkscope, mac, lac := options.pointers()

	resp, err := ab.client.AppendBlock(ctx, count, body, appendOptions, lac, aac, cpkinfo, cpkscope, mac)

	return resp, handleError(err)
}

// AppendBlockFromURL copies a new block of data from source URL to the end of the existing append blob.
// For more information, see https://docs.microsoft.com/rest/api/storageservices/append-block-from-url.
func (ab AppendBlobClient) AppendBlockFromURL(ctx context.Context, source string, options *AppendBlockURLOptions) (AppendBlobAppendBlockFromURLResponse, error) {
	appendOptions, aac, cpkinfo, cpkscope, mac, lac, smac := options.pointers()

	// content length should be 0 on * from URL. always. It's a 400 if it isn't.
	resp, err := ab.client.AppendBlockFromURL(ctx, source, 0, appendOptions, cpkinfo, cpkscope, lac, aac, mac, smac)

	return resp, handleError(err)
}

// SealAppendBlob - The purpose of Append Blob Seal is to allow users and applications to seal append blobs, marking them as read only.
// https://docs.microsoft.com/en-us/rest/api/storageservices/append-blob-seal
func (ab AppendBlobClient) SealAppendBlob(ctx context.Context, options *SealAppendBlobOptions) (AppendBlobSealResponse, error) {
	leaseAccessConditions, modifiedAccessConditions, positionAccessConditions := options.pointers()
	resp, err := ab.client.Seal(ctx, nil, leaseAccessConditions, modifiedAccessConditions, positionAccessConditions)
	return resp, handleError(err)
}
