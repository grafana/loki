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
)

// AppendBlobClient represents a client to an Azure Storage append blob;
type AppendBlobClient struct {
	BlobClient
	client *appendBlobClient
}

// NewAppendBlobClient creates an AppendBlobClient with the specified URL, Azure AD credential, and options.
func NewAppendBlobClient(blobURL string, cred azcore.TokenCredential, options *ClientOptions) (*AppendBlobClient, error) {
	authPolicy := runtime.NewBearerTokenPolicy(cred, []string{tokenScope}, nil)
	conOptions := getConnectionOptions(options)
	conOptions.PerRetryPolicies = append(conOptions.PerRetryPolicies, authPolicy)
	conn := newConnection(blobURL, conOptions)

	return &AppendBlobClient{
		client: newAppendBlobClient(conn.Endpoint(), conn.Pipeline()),
		BlobClient: BlobClient{
			client: newBlobClient(conn.Endpoint(), conn.Pipeline()),
		},
	}, nil
}

// NewAppendBlobClientWithNoCredential creates an AppendBlobClient with the specified URL and options.
func NewAppendBlobClientWithNoCredential(blobURL string, options *ClientOptions) (*AppendBlobClient, error) {
	conOptions := getConnectionOptions(options)
	conn := newConnection(blobURL, conOptions)

	return &AppendBlobClient{
		client: newAppendBlobClient(conn.Endpoint(), conn.Pipeline()),
		BlobClient: BlobClient{
			client: newBlobClient(conn.Endpoint(), conn.Pipeline()),
		},
	}, nil
}

// NewAppendBlobClientWithSharedKey creates an AppendBlobClient with the specified URL, shared key, and options.
func NewAppendBlobClientWithSharedKey(blobURL string, cred *SharedKeyCredential, options *ClientOptions) (*AppendBlobClient, error) {
	authPolicy := newSharedKeyCredPolicy(cred)
	conOptions := getConnectionOptions(options)
	conOptions.PerRetryPolicies = append(conOptions.PerRetryPolicies, authPolicy)
	conn := newConnection(blobURL, conOptions)

	return &AppendBlobClient{
		client: newAppendBlobClient(conn.Endpoint(), conn.Pipeline()),
		BlobClient: BlobClient{
			client:    newBlobClient(conn.Endpoint(), conn.Pipeline()),
			sharedKey: cred,
		},
	}, nil
}

// WithSnapshot creates a new AppendBlobURL object identical to the source but with the specified snapshot timestamp.
// Pass "" to remove the snapshot returning a URL to the base blob.
func (ab *AppendBlobClient) WithSnapshot(snapshot string) (*AppendBlobClient, error) {
	p, err := NewBlobURLParts(ab.URL())
	if err != nil {
		return nil, err
	}

	p.Snapshot = snapshot
	endpoint := p.URL()
	pipeline := ab.client.pl

	return &AppendBlobClient{
		client: newAppendBlobClient(endpoint, pipeline),
		BlobClient: BlobClient{
			client:    newBlobClient(endpoint, pipeline),
			sharedKey: ab.sharedKey,
		},
	}, nil
}

// WithVersionID creates a new AppendBlobURL object identical to the source but with the specified version id.
// Pass "" to remove the versionID returning a URL to the base blob.
func (ab *AppendBlobClient) WithVersionID(versionID string) (*AppendBlobClient, error) {
	p, err := NewBlobURLParts(ab.URL())
	if err != nil {
		return nil, err
	}

	p.VersionID = versionID
	endpoint := p.URL()
	pipeline := ab.client.pl

	return &AppendBlobClient{
		client: newAppendBlobClient(endpoint, pipeline),
		BlobClient: BlobClient{
			client:    newBlobClient(endpoint, pipeline),
			sharedKey: ab.sharedKey,
		},
	}, nil
}

// Create creates a 0-size append blob. Call AppendBlock to append data to an append blob.
// For more information, see https://docs.microsoft.com/rest/api/storageservices/put-blob.
func (ab *AppendBlobClient) Create(ctx context.Context, options *AppendBlobCreateOptions) (AppendBlobCreateResponse, error) {
	appendBlobAppendBlockOptions, blobHttpHeaders, leaseAccessConditions, cpkInfo, cpkScopeInfo, modifiedAccessConditions := options.format()

	resp, err := ab.client.Create(ctx, 0, appendBlobAppendBlockOptions, blobHttpHeaders,
		leaseAccessConditions, cpkInfo, cpkScopeInfo, modifiedAccessConditions)

	return toAppendBlobCreateResponse(resp), handleError(err)
}

// AppendBlock writes a stream to a new block of data to the end of the existing append blob.
// This method panics if the stream is not at position 0.
// Note that the http client closes the body stream after the request is sent to the service.
// For more information, see https://docs.microsoft.com/rest/api/storageservices/append-block.
func (ab *AppendBlobClient) AppendBlock(ctx context.Context, body io.ReadSeekCloser, options *AppendBlobAppendBlockOptions) (AppendBlobAppendBlockResponse, error) {
	count, err := validateSeekableStreamAt0AndGetCount(body)
	if err != nil {
		return AppendBlobAppendBlockResponse{}, nil
	}

	appendOptions, appendPositionAccessConditions, cpkInfo, cpkScope, modifiedAccessConditions, leaseAccessConditions := options.format()

	resp, err := ab.client.AppendBlock(ctx, count, body, appendOptions, leaseAccessConditions, appendPositionAccessConditions, cpkInfo, cpkScope, modifiedAccessConditions)

	return toAppendBlobAppendBlockResponse(resp), handleError(err)
}

// AppendBlockFromURL copies a new block of data from source URL to the end of the existing append blob.
// For more information, see https://docs.microsoft.com/rest/api/storageservices/append-block-from-url.
func (ab *AppendBlobClient) AppendBlockFromURL(ctx context.Context, source string, o *AppendBlobAppendBlockFromURLOptions) (AppendBlobAppendBlockFromURLResponse, error) {
	appendBlockFromURLOptions, cpkInfo, cpkScopeInfo, leaseAccessConditions, appendPositionAccessConditions, modifiedAccessConditions, sourceModifiedAccessConditions := o.format()

	// content length should be 0 on * from URL. always. It's a 400 if it isn't.
	resp, err := ab.client.AppendBlockFromURL(ctx, source, 0, appendBlockFromURLOptions, cpkInfo, cpkScopeInfo,
		leaseAccessConditions, appendPositionAccessConditions, modifiedAccessConditions, sourceModifiedAccessConditions)
	return toAppendBlobAppendBlockFromURLResponse(resp), handleError(err)
}

// SealAppendBlob - The purpose of Append Blob Seal is to allow users and applications to seal append blobs, marking them as read only.
// https://docs.microsoft.com/en-us/rest/api/storageservices/append-blob-seal
func (ab *AppendBlobClient) SealAppendBlob(ctx context.Context, options *AppendBlobSealOptions) (AppendBlobSealResponse, error) {
	leaseAccessConditions, modifiedAccessConditions, positionAccessConditions := options.format()
	resp, err := ab.client.Seal(ctx, nil, leaseAccessConditions, modifiedAccessConditions, positionAccessConditions)
	return toAppendBlobSealResponse(resp), handleError(err)
}
