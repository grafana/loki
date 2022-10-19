//go:build go1.18
// +build go1.18

// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License.

package azblob

import (
	"context"
	"errors"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/to"
	"github.com/Azure/azure-sdk-for-go/sdk/internal/uuid"
)

// BlobLeaseClient represents lease client on blob
type BlobLeaseClient struct {
	BlobClient
	leaseID *string
}

// NewBlobLeaseClient is constructor for BlobLeaseClient
func (b *BlobClient) NewBlobLeaseClient(leaseID *string) (*BlobLeaseClient, error) {
	if leaseID == nil {
		generatedUuid, err := uuid.New()
		if err != nil {
			return nil, err
		}
		leaseID = to.Ptr(generatedUuid.String())
	}
	return &BlobLeaseClient{
		BlobClient: *b,
		leaseID:    leaseID,
	}, nil
}

// AcquireLease acquires a lease on the blob for write and delete operations.
//The lease Duration must be between 15 and 60 seconds, or infinite (-1).
// For more information, see https://docs.microsoft.com/rest/api/storageservices/lease-blob.
func (blc *BlobLeaseClient) AcquireLease(ctx context.Context, options *BlobAcquireLeaseOptions) (BlobAcquireLeaseResponse, error) {
	blobAcquireLeaseOptions, modifiedAccessConditions := options.format()
	blobAcquireLeaseOptions.ProposedLeaseID = blc.leaseID

	resp, err := blc.client.AcquireLease(ctx, &blobAcquireLeaseOptions, modifiedAccessConditions)
	return toBlobAcquireLeaseResponse(resp), handleError(err)
}

// BreakLease breaks the blob's previously-acquired lease (if it exists). Pass the LeaseBreakDefault (-1)
// constant to break a fixed-Duration lease when it expires or an infinite lease immediately.
// For more information, see https://docs.microsoft.com/rest/api/storageservices/lease-blob.
func (blc *BlobLeaseClient) BreakLease(ctx context.Context, options *BlobBreakLeaseOptions) (BlobBreakLeaseResponse, error) {
	blobBreakLeaseOptions, modifiedAccessConditions := options.format()
	resp, err := blc.client.BreakLease(ctx, blobBreakLeaseOptions, modifiedAccessConditions)
	return toBlobBreakLeaseResponse(resp), handleError(err)
}

// ChangeLease changes the blob's lease ID.
// For more information, see https://docs.microsoft.com/rest/api/storageservices/lease-blob.
func (blc *BlobLeaseClient) ChangeLease(ctx context.Context, options *BlobChangeLeaseOptions) (BlobChangeLeaseResponse, error) {
	if blc.leaseID == nil {
		return BlobChangeLeaseResponse{}, errors.New("leaseID cannot be nil")
	}
	proposedLeaseID, changeLeaseOptions, modifiedAccessConditions, err := options.format()
	if err != nil {
		return BlobChangeLeaseResponse{}, err
	}
	resp, err := blc.client.ChangeLease(ctx, *blc.leaseID, *proposedLeaseID, changeLeaseOptions, modifiedAccessConditions)

	// If lease has been changed successfully, set the leaseID in client
	if err == nil {
		blc.leaseID = proposedLeaseID
	}

	return toBlobChangeLeaseResponse(resp), handleError(err)
}

// RenewLease renews the blob's previously-acquired lease.
// For more information, see https://docs.microsoft.com/rest/api/storageservices/lease-blob.
func (blc *BlobLeaseClient) RenewLease(ctx context.Context, options *BlobRenewLeaseOptions) (BlobRenewLeaseResponse, error) {
	if blc.leaseID == nil {
		return BlobRenewLeaseResponse{}, errors.New("leaseID cannot be nil")
	}
	renewLeaseBlobOptions, modifiedAccessConditions := options.format()
	resp, err := blc.client.RenewLease(ctx, *blc.leaseID, renewLeaseBlobOptions, modifiedAccessConditions)
	return toBlobRenewLeaseResponse(resp), handleError(err)
}

// ReleaseLease releases the blob's previously-acquired lease.
// For more information, see https://docs.microsoft.com/rest/api/storageservices/lease-blob.
func (blc *BlobLeaseClient) ReleaseLease(ctx context.Context, options *ReleaseLeaseBlobOptions) (BlobReleaseLeaseResponse, error) {
	if blc.leaseID == nil {
		return BlobReleaseLeaseResponse{}, errors.New("leaseID cannot be nil")
	}
	renewLeaseBlobOptions, modifiedAccessConditions := options.format()
	resp, err := blc.client.ReleaseLease(ctx, *blc.leaseID, renewLeaseBlobOptions, modifiedAccessConditions)
	return toBlobReleaseLeaseResponse(resp), handleError(err)
}
