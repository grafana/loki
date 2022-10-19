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

// LeaseBreakNaturally tells ContainerClient's or BlobClient's BreakLease method to break the lease using service semantics.
const LeaseBreakNaturally = -1

func leasePeriodPointer(period int32) *int32 {
	if period != LeaseBreakNaturally {
		return &period
	} else {
		return nil
	}
}

// ---------------------------------------------------------------------------------------------------------------------

// ContainerAcquireLeaseOptions provides set of configurations for AcquireLeaseContainer operation
type ContainerAcquireLeaseOptions struct {
	Duration                 *int32
	ModifiedAccessConditions *ModifiedAccessConditions
}

func (o *ContainerAcquireLeaseOptions) format() (containerClientAcquireLeaseOptions, *ModifiedAccessConditions) {
	if o == nil {
		return containerClientAcquireLeaseOptions{}, nil
	}
	containerAcquireLeaseOptions := containerClientAcquireLeaseOptions{
		Duration: o.Duration,
	}

	return containerAcquireLeaseOptions, o.ModifiedAccessConditions
}

// ContainerAcquireLeaseResponse contains the response from method ContainerLeaseClient.AcquireLease.
type ContainerAcquireLeaseResponse struct {
	containerClientAcquireLeaseResponse
}

func toContainerAcquireLeaseResponse(resp containerClientAcquireLeaseResponse) ContainerAcquireLeaseResponse {
	return ContainerAcquireLeaseResponse{resp}
}

// ---------------------------------------------------------------------------------------------------------------------

// ContainerBreakLeaseOptions provides set of configurations for BreakLeaseContainer operation
type ContainerBreakLeaseOptions struct {
	BreakPeriod              *int32
	ModifiedAccessConditions *ModifiedAccessConditions
}

func (o *ContainerBreakLeaseOptions) format() (*containerClientBreakLeaseOptions, *ModifiedAccessConditions) {
	if o == nil {
		return nil, nil
	}

	containerBreakLeaseOptions := &containerClientBreakLeaseOptions{
		BreakPeriod: o.BreakPeriod,
	}

	return containerBreakLeaseOptions, o.ModifiedAccessConditions
}

// ContainerBreakLeaseResponse contains the response from method ContainerLeaseClient.BreakLease.
type ContainerBreakLeaseResponse struct {
	containerClientBreakLeaseResponse
}

func toContainerBreakLeaseResponse(resp containerClientBreakLeaseResponse) ContainerBreakLeaseResponse {
	return ContainerBreakLeaseResponse{resp}
}

// ---------------------------------------------------------------------------------------------------------------------

// ContainerChangeLeaseOptions provides set of configurations for ChangeLeaseContainer operation
type ContainerChangeLeaseOptions struct {
	ProposedLeaseID          *string
	ModifiedAccessConditions *ModifiedAccessConditions
}

func (o *ContainerChangeLeaseOptions) format() (*string, *containerClientChangeLeaseOptions, *ModifiedAccessConditions, error) {
	generatedUuid, err := uuid.New()
	if err != nil {
		return nil, nil, nil, err
	}
	leaseID := to.Ptr(generatedUuid.String())
	if o == nil {
		return leaseID, nil, nil, err
	}

	if o.ProposedLeaseID == nil {
		o.ProposedLeaseID = leaseID
	}

	return o.ProposedLeaseID, nil, o.ModifiedAccessConditions, err
}

// ContainerChangeLeaseResponse contains the response from method ContainerLeaseClient.ChangeLease.
type ContainerChangeLeaseResponse struct {
	containerClientChangeLeaseResponse
}

func toContainerChangeLeaseResponse(resp containerClientChangeLeaseResponse) ContainerChangeLeaseResponse {
	return ContainerChangeLeaseResponse{resp}
}

// ---------------------------------------------------------------------------------------------------------------------

// ContainerReleaseLeaseOptions provides set of configurations for ReleaseLeaseContainer operation
type ContainerReleaseLeaseOptions struct {
	ModifiedAccessConditions *ModifiedAccessConditions
}

func (o *ContainerReleaseLeaseOptions) format() (*containerClientReleaseLeaseOptions, *ModifiedAccessConditions) {
	if o == nil {
		return nil, nil
	}

	return nil, o.ModifiedAccessConditions
}

// ContainerReleaseLeaseResponse contains the response from method ContainerLeaseClient.ReleaseLease.
type ContainerReleaseLeaseResponse struct {
	containerClientReleaseLeaseResponse
}

func toContainerReleaseLeaseResponse(resp containerClientReleaseLeaseResponse) ContainerReleaseLeaseResponse {
	return ContainerReleaseLeaseResponse{resp}
}

// ---------------------------------------------------------------------------------------------------------------------

// ContainerRenewLeaseOptions provides set of configurations for RenewLeaseContainer operation
type ContainerRenewLeaseOptions struct {
	ModifiedAccessConditions *ModifiedAccessConditions
}

func (o *ContainerRenewLeaseOptions) format() (*containerClientRenewLeaseOptions, *ModifiedAccessConditions) {
	if o == nil {
		return nil, nil
	}

	return nil, o.ModifiedAccessConditions
}

// ContainerRenewLeaseResponse contains the response from method ContainerLeaseClient.RenewLease.
type ContainerRenewLeaseResponse struct {
	containerClientRenewLeaseResponse
}

func toContainerRenewLeaseResponse(resp containerClientRenewLeaseResponse) ContainerRenewLeaseResponse {
	return ContainerRenewLeaseResponse{resp}
}

// ---------------------------------------------------------------------------------------------------------------------
