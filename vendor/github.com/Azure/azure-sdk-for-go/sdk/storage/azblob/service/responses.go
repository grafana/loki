//go:build go1.18
// +build go1.18

// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License. See License.txt in the project root for license information.

package service

import (
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/internal/generated"
)

// CreateContainerResponse contains the response from method container.Client.Create.
type CreateContainerResponse = generated.ContainerClientCreateResponse

// DeleteContainerResponse contains the response from method container.Client.Delete
type DeleteContainerResponse = generated.ContainerClientDeleteResponse

// RestoreContainerResponse contains the response from method container.Client.Restore
type RestoreContainerResponse = generated.ContainerClientRestoreResponse

// GetAccountInfoResponse contains the response from method Client.GetAccountInfo.
type GetAccountInfoResponse = generated.ServiceClientGetAccountInfoResponse

// ListContainersResponse contains the response from method Client.ListContainersSegment.
type ListContainersResponse = generated.ServiceClientListContainersSegmentResponse

// GetPropertiesResponse contains the response from method Client.GetProperties.
type GetPropertiesResponse = generated.ServiceClientGetPropertiesResponse

// SetPropertiesResponse contains the response from method Client.SetProperties.
type SetPropertiesResponse = generated.ServiceClientSetPropertiesResponse

// GetStatisticsResponse contains the response from method Client.GetStatistics.
type GetStatisticsResponse = generated.ServiceClientGetStatisticsResponse

// FilterBlobsResponse contains the response from method Client.FilterBlobs.
type FilterBlobsResponse = generated.ServiceClientFilterBlobsResponse

// GetUserDelegationKeyResponse contains the response from method ServiceClient.GetUserDelegationKey.
type GetUserDelegationKeyResponse = generated.ServiceClientGetUserDelegationKeyResponse
