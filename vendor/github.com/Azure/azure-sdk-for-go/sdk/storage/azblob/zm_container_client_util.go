//go:build go1.18
// +build go1.18

// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License.

package azblob

// ---------------------------------------------------------------------------------------------------------------------

// ContainerCreateOptions provides set of configurations for CreateContainer operation
type ContainerCreateOptions struct {
	// Specifies whether data in the container may be accessed publicly and the level of access
	Access *PublicAccessType

	// Optional. Specifies a user-defined name-value pair associated with the blob.
	Metadata map[string]string

	// Optional. Specifies the encryption scope settings to set on the container.
	CpkScope *ContainerCpkScopeInfo
}

func (o *ContainerCreateOptions) format() (*containerClientCreateOptions, *ContainerCpkScopeInfo) {
	if o == nil {
		return nil, nil
	}

	basicOptions := containerClientCreateOptions{
		Access:   o.Access,
		Metadata: o.Metadata,
	}

	return &basicOptions, o.CpkScope
}

// ContainerCreateResponse is wrapper around containerClientCreateResponse
type ContainerCreateResponse struct {
	containerClientCreateResponse
}

func toContainerCreateResponse(resp containerClientCreateResponse) ContainerCreateResponse {
	return ContainerCreateResponse{resp}
}

// ---------------------------------------------------------------------------------------------------------------------

// ContainerDeleteOptions provides set of configurations for DeleteContainer operation
type ContainerDeleteOptions struct {
	LeaseAccessConditions    *LeaseAccessConditions
	ModifiedAccessConditions *ModifiedAccessConditions
}

func (o *ContainerDeleteOptions) format() (*containerClientDeleteOptions, *LeaseAccessConditions, *ModifiedAccessConditions) {
	if o == nil {
		return nil, nil, nil
	}

	return nil, o.LeaseAccessConditions, o.ModifiedAccessConditions
}

// ContainerDeleteResponse contains the response from method ContainerClient.Delete.
type ContainerDeleteResponse struct {
	containerClientDeleteResponse
}

func toContainerDeleteResponse(resp containerClientDeleteResponse) ContainerDeleteResponse {
	return ContainerDeleteResponse{resp}
}

// ---------------------------------------------------------------------------------------------------------------------

// ContainerGetPropertiesOptions provides set of configurations for GetPropertiesContainer operation
type ContainerGetPropertiesOptions struct {
	LeaseAccessConditions *LeaseAccessConditions
}

func (o *ContainerGetPropertiesOptions) format() (*containerClientGetPropertiesOptions, *LeaseAccessConditions) {
	if o == nil {
		return nil, nil
	}

	return nil, o.LeaseAccessConditions
}

// ContainerGetPropertiesResponse contains the response from method ContainerClient.GetProperties
type ContainerGetPropertiesResponse struct {
	containerClientGetPropertiesResponse
}

func toContainerGetPropertiesResponse(resp containerClientGetPropertiesResponse) ContainerGetPropertiesResponse {
	return ContainerGetPropertiesResponse{resp}
}

// ---------------------------------------------------------------------------------------------------------------------

// ContainerSetMetadataOptions provides set of configurations for SetMetadataContainer operation
type ContainerSetMetadataOptions struct {
	Metadata                 map[string]string
	LeaseAccessConditions    *LeaseAccessConditions
	ModifiedAccessConditions *ModifiedAccessConditions
}

func (o *ContainerSetMetadataOptions) format() (*containerClientSetMetadataOptions, *LeaseAccessConditions, *ModifiedAccessConditions) {
	if o == nil {
		return nil, nil, nil
	}

	return &containerClientSetMetadataOptions{Metadata: o.Metadata}, o.LeaseAccessConditions, o.ModifiedAccessConditions
}

// ContainerSetMetadataResponse contains the response from method containerClient.SetMetadata
type ContainerSetMetadataResponse struct {
	containerClientSetMetadataResponse
}

func toContainerSetMetadataResponse(resp containerClientSetMetadataResponse) ContainerSetMetadataResponse {
	return ContainerSetMetadataResponse{resp}
}

// ---------------------------------------------------------------------------------------------------------------------

// ContainerGetAccessPolicyOptions provides set of configurations for GetAccessPolicy operation
type ContainerGetAccessPolicyOptions struct {
	LeaseAccessConditions *LeaseAccessConditions
}

func (o *ContainerGetAccessPolicyOptions) format() (*containerClientGetAccessPolicyOptions, *LeaseAccessConditions) {
	if o == nil {
		return nil, nil
	}

	return nil, o.LeaseAccessConditions
}

// ContainerGetAccessPolicyResponse contains the response from method ContainerClient.GetAccessPolicy.
type ContainerGetAccessPolicyResponse struct {
	containerClientGetAccessPolicyResponse
}

func toContainerGetAccessPolicyResponse(resp containerClientGetAccessPolicyResponse) ContainerGetAccessPolicyResponse {
	return ContainerGetAccessPolicyResponse{resp}
}

// ---------------------------------------------------------------------------------------------------------------------

// ContainerSetAccessPolicyOptions provides set of configurations for ContainerClient.SetAccessPolicy operation
type ContainerSetAccessPolicyOptions struct {
	AccessConditions *ContainerAccessConditions
	// Specifies whether data in the container may be accessed publicly and the level of access
	Access *PublicAccessType
	// the acls for the container
	ContainerACL []*SignedIdentifier
	// Provides a client-generated, opaque value with a 1 KB character limit that is recorded in the analytics logs when storage
	// analytics logging is enabled.
	RequestID *string
	// The timeout parameter is expressed in seconds. For more information, see Setting Timeouts for Blob Service Operations.
	// [https://docs.microsoft.com/en-us/rest/api/storageservices/fileservices/setting-timeouts-for-blob-service-operations]
	Timeout *int32
}

func (o *ContainerSetAccessPolicyOptions) format() (*containerClientSetAccessPolicyOptions, *LeaseAccessConditions, *ModifiedAccessConditions) {
	if o == nil {
		return nil, nil, nil
	}
	mac, lac := o.AccessConditions.format()
	return &containerClientSetAccessPolicyOptions{
		Access:       o.Access,
		ContainerACL: o.ContainerACL,
		RequestID:    o.RequestID,
	}, lac, mac
}

// ContainerSetAccessPolicyResponse contains the response from method ContainerClient.SetAccessPolicy
type ContainerSetAccessPolicyResponse struct {
	containerClientSetAccessPolicyResponse
}

func toContainerSetAccessPolicyResponse(resp containerClientSetAccessPolicyResponse) ContainerSetAccessPolicyResponse {
	return ContainerSetAccessPolicyResponse{resp}
}

// ---------------------------------------------------------------------------------------------------------------------

// ContainerListBlobsFlatOptions provides set of configurations for SetAccessPolicy operation
type ContainerListBlobsFlatOptions struct {
	// Include this parameter to specify one or more datasets to include in the response.
	Include []ListBlobsIncludeItem
	// A string value that identifies the portion of the list of containers to be returned with the next listing operation. The
	// operation returns the NextMarker value within the response body if the listing
	// operation did not return all containers remaining to be listed with the current page. The NextMarker value can be used
	// as the value for the marker parameter in a subsequent call to request the next
	// page of list items. The marker value is opaque to the client.
	Marker *string
	// Specifies the maximum number of containers to return. If the request does not specify maxresults, or specifies a value
	// greater than 5000, the server will return up to 5000 items. Note that if the
	// listing operation crosses a partition boundary, then the service will return a continuation token for retrieving the remainder
	// of the results. For this reason, it is possible that the service will
	// return fewer results than specified by maxresults, or than the default of 5000.
	MaxResults *int32
	// Filters the results to return only containers whose name begins with the specified prefix.
	Prefix *string
}

func (o *ContainerListBlobsFlatOptions) format() *containerClientListBlobFlatSegmentOptions {
	if o == nil {
		return nil
	}

	return &containerClientListBlobFlatSegmentOptions{
		Include:    o.Include,
		Marker:     o.Marker,
		Maxresults: o.MaxResults,
		Prefix:     o.Prefix,
	}
}

// ContainerListBlobFlatPager provides operations for iterating over paged responses
type ContainerListBlobFlatPager struct {
	*containerClientListBlobFlatSegmentPager
}

func toContainerListBlobFlatSegmentPager(resp *containerClientListBlobFlatSegmentPager) *ContainerListBlobFlatPager {
	return &ContainerListBlobFlatPager{resp}
}

// ---------------------------------------------------------------------------------------------------------------------

//ContainerListBlobsHierarchyOptions provides set of configurations for ContainerClient.ListBlobsHierarchy
type ContainerListBlobsHierarchyOptions struct {
	// Include this parameter to specify one or more datasets to include in the response.
	Include []ListBlobsIncludeItem
	// A string value that identifies the portion of the list of containers to be returned with the next listing operation. The
	// operation returns the NextMarker value within the response body if the listing
	// operation did not return all containers remaining to be listed with the current page. The NextMarker value can be used
	// as the value for the marker parameter in a subsequent call to request the next
	// page of list items. The marker value is opaque to the client.
	Marker *string
	// Specifies the maximum number of containers to return. If the request does not specify maxresults, or specifies a value
	// greater than 5000, the server will return up to 5000 items. Note that if the
	// listing operation crosses a partition boundary, then the service will return a continuation token for retrieving the remainder
	// of the results. For this reason, it is possible that the service will
	// return fewer results than specified by maxresults, or than the default of 5000.
	MaxResults *int32
	// Filters the results to return only containers whose name begins with the specified prefix.
	Prefix *string
}

func (o *ContainerListBlobsHierarchyOptions) format() *containerClientListBlobHierarchySegmentOptions {
	if o == nil {
		return nil
	}

	return &containerClientListBlobHierarchySegmentOptions{
		Include:    o.Include,
		Marker:     o.Marker,
		Maxresults: o.MaxResults,
		Prefix:     o.Prefix,
	}
}

// ContainerListBlobHierarchyPager provides operations for iterating over paged responses.
type ContainerListBlobHierarchyPager struct {
	containerClientListBlobHierarchySegmentPager
}

func toContainerListBlobHierarchySegmentPager(resp *containerClientListBlobHierarchySegmentPager) *ContainerListBlobHierarchyPager {
	if resp == nil {
		return nil
	}
	return &ContainerListBlobHierarchyPager{*resp}
}
