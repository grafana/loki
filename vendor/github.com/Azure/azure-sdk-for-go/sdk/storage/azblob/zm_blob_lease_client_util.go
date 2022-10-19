//go:build go1.18
// +build go1.18

// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License.

package azblob

import (
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/to"
	"github.com/Azure/azure-sdk-for-go/sdk/internal/uuid"
)

// ---------------------------------------------------------------------------------------------------------------------

// BlobAcquireLeaseOptions provides set of configurations for AcquireLeaseBlob operation
type BlobAcquireLeaseOptions struct {
	// Specifies the Duration of the lease, in seconds, or negative one (-1) for a lease that never expires. A non-infinite lease
	// can be between 15 and 60 seconds. A lease Duration cannot be changed using renew or change.
	Duration *int32

	ModifiedAccessConditions *ModifiedAccessConditions
}

func (o *BlobAcquireLeaseOptions) format() (blobClientAcquireLeaseOptions, *ModifiedAccessConditions) {
	if o == nil {
		return blobClientAcquireLeaseOptions{}, nil
	}
	return blobClientAcquireLeaseOptions{
		Duration: o.Duration,
	}, o.ModifiedAccessConditions
}

// BlobAcquireLeaseResponse contains the response from method BlobLeaseClient.AcquireLease.
type BlobAcquireLeaseResponse struct {
	blobClientAcquireLeaseResponse
}

func toBlobAcquireLeaseResponse(resp blobClientAcquireLeaseResponse) BlobAcquireLeaseResponse {
	return BlobAcquireLeaseResponse{resp}
}

// ---------------------------------------------------------------------------------------------------------------------

// BlobBreakLeaseOptions provides set of configurations for BreakLeaseBlob operation
type BlobBreakLeaseOptions struct {
	// For a break operation, proposed Duration the lease should continue before it is broken, in seconds, between 0 and 60. This
	// break period is only used if it is shorter than the time remaining on the lease. If longer, the time remaining on the lease
	// is used. A new lease will not be available before the break period has expired, but the lease may be held for longer than
	// the break period. If this header does not appear with a break operation, a fixed-Duration lease breaks after the remaining
	// lease period elapses, and an infinite lease breaks immediately.
	BreakPeriod              *int32
	ModifiedAccessConditions *ModifiedAccessConditions
}

func (o *BlobBreakLeaseOptions) format() (*blobClientBreakLeaseOptions, *ModifiedAccessConditions) {
	if o == nil {
		return nil, nil
	}

	if o.BreakPeriod != nil {
		period := leasePeriodPointer(*o.BreakPeriod)
		return &blobClientBreakLeaseOptions{
			BreakPeriod: period,
		}, o.ModifiedAccessConditions
	}

	return nil, o.ModifiedAccessConditions
}

// BlobBreakLeaseResponse contains the response from method BlobLeaseClient.BreakLease.
type BlobBreakLeaseResponse struct {
	blobClientBreakLeaseResponse
}

func toBlobBreakLeaseResponse(resp blobClientBreakLeaseResponse) BlobBreakLeaseResponse {
	return BlobBreakLeaseResponse{resp}
}

// ---------------------------------------------------------------------------------------------------------------------

// BlobChangeLeaseOptions provides set of configurations for ChangeLeaseBlob operation
type BlobChangeLeaseOptions struct {
	ProposedLeaseID          *string
	ModifiedAccessConditions *ModifiedAccessConditions
}

func (o *BlobChangeLeaseOptions) format() (*string, *blobClientChangeLeaseOptions, *ModifiedAccessConditions, error) {
	generatedUuid, err := uuid.New()
	if err != nil {
		return nil, nil, nil, err
	}
	leaseID := to.Ptr(generatedUuid.String())
	if o == nil {
		return leaseID, nil, nil, nil
	}

	if o.ProposedLeaseID == nil {
		o.ProposedLeaseID = leaseID
	}

	return o.ProposedLeaseID, nil, o.ModifiedAccessConditions, nil
}

// BlobChangeLeaseResponse contains the response from method BlobLeaseClient.ChangeLease
type BlobChangeLeaseResponse struct {
	blobClientChangeLeaseResponse
}

func toBlobChangeLeaseResponse(resp blobClientChangeLeaseResponse) BlobChangeLeaseResponse {
	return BlobChangeLeaseResponse{resp}
}

// ---------------------------------------------------------------------------------------------------------------------

// BlobRenewLeaseOptions provides set of configurations for RenewLeaseBlob operation
type BlobRenewLeaseOptions struct {
	ModifiedAccessConditions *ModifiedAccessConditions
}

func (o *BlobRenewLeaseOptions) format() (*blobClientRenewLeaseOptions, *ModifiedAccessConditions) {
	if o == nil {
		return nil, nil
	}

	return nil, o.ModifiedAccessConditions
}

// BlobRenewLeaseResponse contains the response from method BlobClient.RenewLease.
type BlobRenewLeaseResponse struct {
	blobClientRenewLeaseResponse
}

func toBlobRenewLeaseResponse(resp blobClientRenewLeaseResponse) BlobRenewLeaseResponse {
	return BlobRenewLeaseResponse{resp}
}

// ---------------------------------------------------------------------------------------------------------------------

// ReleaseLeaseBlobOptions provides set of configurations for ReleaseLeaseBlob operation
type ReleaseLeaseBlobOptions struct {
	ModifiedAccessConditions *ModifiedAccessConditions
}

func (o *ReleaseLeaseBlobOptions) format() (*blobClientReleaseLeaseOptions, *ModifiedAccessConditions) {
	if o == nil {
		return nil, nil
	}

	return nil, o.ModifiedAccessConditions
}

// BlobReleaseLeaseResponse contains the response from method BlobClient.ReleaseLease.
type BlobReleaseLeaseResponse struct {
	blobClientReleaseLeaseResponse
}

func toBlobReleaseLeaseResponse(resp blobClientReleaseLeaseResponse) BlobReleaseLeaseResponse {
	return BlobReleaseLeaseResponse{resp}
}
