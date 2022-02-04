// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License.

package azblob

import (
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/to"
	"github.com/Azure/azure-sdk-for-go/sdk/internal/uuid"
)

type AcquireLeaseBlobOptions struct {
	// Specifies the Duration of the lease, in seconds, or negative one (-1) for a lease that never expires. A non-infinite lease
	// can be between 15 and 60 seconds. A lease Duration cannot be changed using renew or change.
	Duration *int32

	ModifiedAccessConditions *ModifiedAccessConditions
}

func (o *AcquireLeaseBlobOptions) pointers() (*BlobAcquireLeaseOptions, *ModifiedAccessConditions) {
	if o == nil {
		return nil, nil
	}
	return &BlobAcquireLeaseOptions{
		Duration: o.Duration,
	}, o.ModifiedAccessConditions
}

type BreakLeaseBlobOptions struct {
	// For a break operation, proposed Duration the lease should continue before it is broken, in seconds, between 0 and 60. This
	// break period is only used if it is shorter than the time remaining on the lease. If longer, the time remaining on the lease
	// is used. A new lease will not be available before the break period has expired, but the lease may be held for longer than
	// the break period. If this header does not appear with a break operation, a fixed-Duration lease breaks after the remaining
	// lease period elapses, and an infinite lease breaks immediately.
	BreakPeriod              *int32
	ModifiedAccessConditions *ModifiedAccessConditions
}

func (o *BreakLeaseBlobOptions) pointers() (*BlobBreakLeaseOptions, *ModifiedAccessConditions) {
	if o == nil {
		return nil, nil
	}

	if o.BreakPeriod != nil {
		period := leasePeriodPointer(*o.BreakPeriod)
		return &BlobBreakLeaseOptions{
			BreakPeriod: period,
		}, o.ModifiedAccessConditions
	}

	return nil, o.ModifiedAccessConditions
}

type ChangeLeaseBlobOptions struct {
	ProposedLeaseID          *string
	ModifiedAccessConditions *ModifiedAccessConditions
}

func (o *ChangeLeaseBlobOptions) pointers() (proposedLeaseI *string, modifiedAccessConditions *ModifiedAccessConditions, err error) {
	generatedUuid, err := uuid.New()
	if err != nil {
		return nil, nil, err
	}
	leaseID := to.StringPtr(generatedUuid.String())
	if o == nil {
		return leaseID, nil, nil
	}

	if o.ProposedLeaseID == nil {
		o.ProposedLeaseID = leaseID
	}

	return o.ProposedLeaseID, o.ModifiedAccessConditions, nil
}

type ReleaseLeaseBlobOptions struct {
	ModifiedAccessConditions *ModifiedAccessConditions
}

func (o *ReleaseLeaseBlobOptions) pointers() (blobReleaseLeaseOptions *BlobReleaseLeaseOptions, modifiedAccessConditions *ModifiedAccessConditions) {
	if o == nil {
		return nil, nil
	}

	return nil, o.ModifiedAccessConditions
}

type RenewLeaseBlobOptions struct {
	ModifiedAccessConditions *ModifiedAccessConditions
}

func (o *RenewLeaseBlobOptions) pointers() (blobRenewLeaseOptions *BlobRenewLeaseOptions, modifiedAccessConditions *ModifiedAccessConditions) {
	if o == nil {
		return nil, nil
	}

	return nil, o.ModifiedAccessConditions
}

type AcquireLeaseContainerOptions struct {
	Duration                 *int32
	ModifiedAccessConditions *ModifiedAccessConditions
}

func (o *AcquireLeaseContainerOptions) pointers() (*ContainerAcquireLeaseOptions, *ModifiedAccessConditions) {
	if o == nil {
		return nil, nil
	}
	containerAcquireLeaseOptions := &ContainerAcquireLeaseOptions{
		Duration: o.Duration,
	}

	return containerAcquireLeaseOptions, o.ModifiedAccessConditions
}

type BreakLeaseContainerOptions struct {
	BreakPeriod              *int32
	ModifiedAccessConditions *ModifiedAccessConditions
}

func (o *BreakLeaseContainerOptions) pointers() (*ContainerBreakLeaseOptions, *ModifiedAccessConditions) {
	if o == nil {
		return nil, nil
	}

	containerBreakLeaseOptions := &ContainerBreakLeaseOptions{
		BreakPeriod: o.BreakPeriod,
	}

	return containerBreakLeaseOptions, o.ModifiedAccessConditions
}

type ChangeLeaseContainerOptions struct {
	ProposedLeaseID          *string
	ModifiedAccessConditions *ModifiedAccessConditions
}

func (o *ChangeLeaseContainerOptions) pointers() (proposedLeaseID *string, modifiedAccessConditions *ModifiedAccessConditions, err error) {
	generatedUuid, err := uuid.New()
	if err != nil {
		return nil, nil, err
	}
	leaseID := to.StringPtr(generatedUuid.String())
	if o == nil {
		return leaseID, nil, err
	}

	if o.ProposedLeaseID == nil {
		o.ProposedLeaseID = leaseID
	}

	return o.ProposedLeaseID, o.ModifiedAccessConditions, err

}

type RenewLeaseContainerOptions struct {
	ModifiedAccessConditions *ModifiedAccessConditions
}

func (o *RenewLeaseContainerOptions) pointers() (containerRenewLeaseOptions *ContainerRenewLeaseOptions,
	modifiedAccessConditions *ModifiedAccessConditions) {
	if o == nil {
		return nil, nil
	}

	return nil, o.ModifiedAccessConditions
}

type ReleaseLeaseContainerOptions struct {
	ModifiedAccessConditions *ModifiedAccessConditions
}

func (o *ReleaseLeaseContainerOptions) pointers() (containerReleaseLeaseOptions *ContainerReleaseLeaseOptions,
	modifiedAccessConditions *ModifiedAccessConditions) {
	if o == nil {
		return nil, nil
	}

	return nil, o.ModifiedAccessConditions
}

// LeaseBreakNaturally tells ContainerClient's or BlobClient's BreakLease method to break the lease using service semantics.
const LeaseBreakNaturally = -1

func leasePeriodPointer(period int32) *int32 {
	if period != LeaseBreakNaturally {
		return &period
	} else {
		return nil
	}
}
