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

//ContainerLeaseClient represents lease client of container
type ContainerLeaseClient struct {
	ContainerClient
	leaseID *string
}

// NewContainerLeaseClient is constructor of ContainerLeaseClient
func (c *ContainerClient) NewContainerLeaseClient(leaseID *string) (*ContainerLeaseClient, error) {
	if leaseID == nil {
		generatedUuid, err := uuid.New()
		if err != nil {
			return nil, err
		}
		leaseID = to.Ptr(generatedUuid.String())
	}
	return &ContainerLeaseClient{
		ContainerClient: *c,
		leaseID:         leaseID,
	}, nil
}

// AcquireLease acquires a lease on the container for delete operations. The lease Duration must be between 15 to 60 seconds, or infinite (-1).
// For more information, see https://docs.microsoft.com/rest/api/storageservices/lease-container.
func (clc *ContainerLeaseClient) AcquireLease(ctx context.Context, options *ContainerAcquireLeaseOptions) (ContainerAcquireLeaseResponse, error) {
	containerAcquireLeaseOptions, modifiedAccessConditions := options.format()
	containerAcquireLeaseOptions.ProposedLeaseID = clc.leaseID

	resp, err := clc.client.AcquireLease(ctx, &containerAcquireLeaseOptions, modifiedAccessConditions)
	if err == nil && resp.LeaseID != nil {
		clc.leaseID = resp.LeaseID
	}
	return toContainerAcquireLeaseResponse(resp), handleError(err)
}

// BreakLease breaks the container's previously-acquired lease (if it exists).
// For more information, see https://docs.microsoft.com/rest/api/storageservices/lease-container.
func (clc *ContainerLeaseClient) BreakLease(ctx context.Context, options *ContainerBreakLeaseOptions) (ContainerBreakLeaseResponse, error) {
	containerBreakLeaseOptions, modifiedAccessConditions := options.format()
	resp, err := clc.client.BreakLease(ctx, containerBreakLeaseOptions, modifiedAccessConditions)
	return toContainerBreakLeaseResponse(resp), handleError(err)
}

// ChangeLease changes the container's lease ID.
// For more information, see https://docs.microsoft.com/rest/api/storageservices/lease-container.
func (clc *ContainerLeaseClient) ChangeLease(ctx context.Context, options *ContainerChangeLeaseOptions) (ContainerChangeLeaseResponse, error) {
	if clc.leaseID == nil {
		return ContainerChangeLeaseResponse{}, errors.New("leaseID cannot be nil")
	}

	proposedLeaseID, changeLeaseOptions, modifiedAccessConditions, err := options.format()
	if err != nil {
		return ContainerChangeLeaseResponse{}, err
	}

	resp, err := clc.client.ChangeLease(ctx, *clc.leaseID, *proposedLeaseID, changeLeaseOptions, modifiedAccessConditions)
	if err == nil && resp.LeaseID != nil {
		clc.leaseID = resp.LeaseID
	}
	return toContainerChangeLeaseResponse(resp), handleError(err)
}

// ReleaseLease releases the container's previously-acquired lease.
// For more information, see https://docs.microsoft.com/rest/api/storageservices/lease-container.
func (clc *ContainerLeaseClient) ReleaseLease(ctx context.Context, options *ContainerReleaseLeaseOptions) (ContainerReleaseLeaseResponse, error) {
	if clc.leaseID == nil {
		return ContainerReleaseLeaseResponse{}, errors.New("leaseID cannot be nil")
	}
	containerReleaseLeaseOptions, modifiedAccessConditions := options.format()
	resp, err := clc.client.ReleaseLease(ctx, *clc.leaseID, containerReleaseLeaseOptions, modifiedAccessConditions)

	return toContainerReleaseLeaseResponse(resp), handleError(err)
}

// RenewLease renews the container's previously-acquired lease.
// For more information, see https://docs.microsoft.com/rest/api/storageservices/lease-container.
func (clc *ContainerLeaseClient) RenewLease(ctx context.Context, options *ContainerRenewLeaseOptions) (ContainerRenewLeaseResponse, error) {
	if clc.leaseID == nil {
		return ContainerRenewLeaseResponse{}, errors.New("leaseID cannot be nil")
	}
	renewLeaseBlobOptions, modifiedAccessConditions := options.format()
	resp, err := clc.client.RenewLease(ctx, *clc.leaseID, renewLeaseBlobOptions, modifiedAccessConditions)
	if err == nil && resp.LeaseID != nil {
		clc.leaseID = resp.LeaseID
	}
	return toContainerRenewLeaseResponse(resp), handleError(err)
}
