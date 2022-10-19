//go:build go1.18
// +build go1.18

// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License.

package azblob

// ---------------------------------------------------------------------------------------------------------------------

// ServiceGetAccountInfoOptions provides set of options for ServiceClient.GetAccountInfo
type ServiceGetAccountInfoOptions struct {
	// placeholder for future options
}

func (o *ServiceGetAccountInfoOptions) format() *serviceClientGetAccountInfoOptions {
	return nil
}

// ServiceGetAccountInfoResponse contains the response from ServiceClient.GetAccountInfo
type ServiceGetAccountInfoResponse struct {
	serviceClientGetAccountInfoResponse
}

func toServiceGetAccountInfoResponse(resp serviceClientGetAccountInfoResponse) ServiceGetAccountInfoResponse {
	return ServiceGetAccountInfoResponse{resp}
}

// ---------------------------------------------------------------------------------------------------------------------

// ListContainersDetail indicates what additional information the service should return with each container.
type ListContainersDetail struct {
	// Tells the service whether to return metadata for each container.
	Metadata bool

	// Tells the service whether to return soft-deleted containers.
	Deleted bool
}

// string produces the `Include` query parameter's value.
func (o *ListContainersDetail) format() []ListContainersIncludeType {
	if !o.Metadata && !o.Deleted {
		return nil
	}

	items := make([]ListContainersIncludeType, 0, 2)
	// NOTE: Multiple strings MUST be appended in alphabetic order or signing the string for authentication fails!
	if o.Deleted {
		items = append(items, ListContainersIncludeTypeDeleted)
	}
	if o.Metadata {
		items = append(items, ListContainersIncludeTypeMetadata)
	}
	return items
}

// ListContainersOptions provides set of configurations for ListContainers operation
type ListContainersOptions struct {
	Include ListContainersDetail

	// A string value that identifies the portion of the list of containers to be returned with the next listing operation. The
	// operation returns the NextMarker value within the response body if the listing operation did not return all containers
	// remaining to be listed with the current page. The NextMarker value can be used as the value for the marker parameter in
	// a subsequent call to request the next page of list items. The marker value is opaque to the client.
	Marker *string

	// Specifies the maximum number of containers to return. If the request does not specify max results, or specifies a value
	// greater than 5000, the server will return up to 5000 items. Note that if the listing operation crosses a partition boundary,
	// then the service will return a continuation token for retrieving the remainder of the results. For this reason, it is possible
	// that the service will return fewer results than specified by max results, or than the default of 5000.
	MaxResults *int32

	// Filters the results to return only containers whose name begins with the specified prefix.
	Prefix *string
}

func (o *ListContainersOptions) format() *serviceClientListContainersSegmentOptions {
	if o == nil {
		return nil
	}

	return &serviceClientListContainersSegmentOptions{
		Include:    o.Include.format(),
		Marker:     o.Marker,
		Maxresults: o.MaxResults,
		Prefix:     o.Prefix,
	}
}

// ServiceListContainersSegmentPager provides operations for iterating over paged responses.
type ServiceListContainersSegmentPager struct {
	serviceClientListContainersSegmentPager
}

func toServiceListContainersSegmentPager(resp serviceClientListContainersSegmentPager) *ServiceListContainersSegmentPager {
	return &ServiceListContainersSegmentPager{resp}
}

// ---------------------------------------------------------------------------------------------------------------------

// ServiceGetPropertiesOptions provides set of options for ServiceClient.GetProperties
type ServiceGetPropertiesOptions struct {
	// placeholder for future options
}

func (o *ServiceGetPropertiesOptions) format() *serviceClientGetPropertiesOptions {
	return nil
}

// ServiceGetPropertiesResponse contains the response from ServiceClient.GetProperties
type ServiceGetPropertiesResponse struct {
	serviceClientGetPropertiesResponse
}

func toServiceGetPropertiesResponse(resp serviceClientGetPropertiesResponse) ServiceGetPropertiesResponse {
	return ServiceGetPropertiesResponse{resp}
}

// ---------------------------------------------------------------------------------------------------------------------

// ServiceSetPropertiesOptions provides set of options for ServiceClient.SetProperties
type ServiceSetPropertiesOptions struct {
	// The set of CORS rules.
	Cors []*CorsRule

	// The default version to use for requests to the Blob service if an incoming request's version is not specified. Possible
	// values include version 2008-10-27 and all more recent versions
	DefaultServiceVersion *string

	// the retention policy which determines how long the associated data should persist
	DeleteRetentionPolicy *RetentionPolicy

	// a summary of request statistics grouped by API in hour or minute aggregates for blobs
	HourMetrics *Metrics

	// Azure Analytics Logging settings.
	Logging *Logging

	// a summary of request statistics grouped by API in hour or minute aggregates for blobs
	MinuteMetrics *Metrics

	// The properties that enable an account to host a static website
	StaticWebsite *StaticWebsite
}

func (o *ServiceSetPropertiesOptions) format() (StorageServiceProperties, *serviceClientSetPropertiesOptions) {
	if o == nil {
		return StorageServiceProperties{}, nil
	}

	return StorageServiceProperties{
		Cors:                  o.Cors,
		DefaultServiceVersion: o.DefaultServiceVersion,
		DeleteRetentionPolicy: o.DeleteRetentionPolicy,
		HourMetrics:           o.HourMetrics,
		Logging:               o.Logging,
		MinuteMetrics:         o.MinuteMetrics,
		StaticWebsite:         o.StaticWebsite,
	}, nil
}

// ServiceSetPropertiesResponse contains the response from ServiceClient.SetProperties
type ServiceSetPropertiesResponse struct {
	serviceClientSetPropertiesResponse
}

func toServiceSetPropertiesResponse(resp serviceClientSetPropertiesResponse) ServiceSetPropertiesResponse {
	return ServiceSetPropertiesResponse{resp}
}

// ---------------------------------------------------------------------------------------------------------------------

// ServiceGetStatisticsOptions provides set of options for ServiceClient.GetStatistics
type ServiceGetStatisticsOptions struct {
	// placeholder for future options
}

func (o *ServiceGetStatisticsOptions) format() *serviceClientGetStatisticsOptions {
	return nil
}

// ServiceGetStatisticsResponse contains the response from ServiceClient.GetStatistics.
type ServiceGetStatisticsResponse struct {
	serviceClientGetStatisticsResponse
}

func toServiceGetStatisticsResponse(resp serviceClientGetStatisticsResponse) ServiceGetStatisticsResponse {
	return ServiceGetStatisticsResponse{resp}
}

// ---------------------------------------------------------------------------------------------------------------------

// ServiceFilterBlobsOptions provides set of configurations for ServiceClient.FindBlobsByTags
type ServiceFilterBlobsOptions struct {
	// A string value that identifies the portion of the list of containers to be returned with the next listing operation. The operation returns the NextMarker
	// value within the response body if the listing operation did not return all containers remaining to be listed with the current page. The NextMarker value
	// can be used as the value for the marker parameter in a subsequent call to request the next page of list items. The marker value is opaque to the client.
	Marker *string
	// Specifies the maximum number of containers to return. If the request does not specify maxresults, or specifies a value greater than 5000, the server
	// will return up to 5000 items. Note that if the listing operation crosses a partition boundary, then the service will return a continuation token for
	// retrieving the remainder of the results. For this reason, it is possible that the service will return fewer results than specified by maxresults, or
	// than the default of 5000.
	MaxResults *int32
	// Filters the results to return only to return only blobs whose tags match the specified expression.
	Where *string
}

func (o *ServiceFilterBlobsOptions) pointer() *serviceClientFilterBlobsOptions {
	if o == nil {
		return nil
	}
	return &serviceClientFilterBlobsOptions{
		Marker:     o.Marker,
		Maxresults: o.MaxResults,
		Where:      o.Where,
	}
}

// ServiceFilterBlobsResponse contains the response from ServiceClient.FindBlobsByTags
type ServiceFilterBlobsResponse struct {
	serviceClientFilterBlobsResponse
}

func toServiceFilterBlobsResponse(resp serviceClientFilterBlobsResponse) ServiceFilterBlobsResponse {
	return ServiceFilterBlobsResponse{resp}
}
