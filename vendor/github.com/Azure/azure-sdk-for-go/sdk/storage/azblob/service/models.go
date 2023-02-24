//go:build go1.18
// +build go1.18

// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License. See License.txt in the project root for license information.

package service

import (
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/container"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/internal/exported"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/internal/generated"
)

// SharedKeyCredential contains an account's name and its primary or secondary key.
type SharedKeyCredential = exported.SharedKeyCredential

// NewSharedKeyCredential creates an immutable SharedKeyCredential containing the
// storage account's name and either its primary or secondary key.
func NewSharedKeyCredential(accountName, accountKey string) (*SharedKeyCredential, error) {
	return exported.NewSharedKeyCredential(accountName, accountKey)
}

// UserDelegationCredential contains an account's name and its user delegation key.
type UserDelegationCredential = exported.UserDelegationCredential

// UserDelegationKey contains UserDelegationKey.
type UserDelegationKey = generated.UserDelegationKey

// KeyInfo contains KeyInfo struct.
type KeyInfo = generated.KeyInfo

// GetUserDelegationCredentialOptions contains optional parameters for Service.GetUserDelegationKey method
type GetUserDelegationCredentialOptions struct {
	// placeholder for future options
}

func (o *GetUserDelegationCredentialOptions) format() *generated.ServiceClientGetUserDelegationKeyOptions {
	return nil
}

// AccessConditions identifies container-specific access conditions which you optionally set.
type AccessConditions = exported.ContainerAccessConditions

// CpkInfo contains a group of parameters for the BlobClient.Download method.
type CpkInfo = generated.CpkInfo

// CpkScopeInfo contains a group of parameters for the BlobClient.SetMetadata method.
type CpkScopeInfo = generated.CpkScopeInfo

// CreateContainerOptions contains the optional parameters for the container.Client.Create method.
type CreateContainerOptions = container.CreateOptions

// DeleteContainerOptions contains the optional parameters for the container.Client.Delete method.
type DeleteContainerOptions = container.DeleteOptions

// RestoreContainerOptions contains the optional parameters for the container.Client.Restore method.
type RestoreContainerOptions = container.RestoreOptions

// CorsRule - CORS is an HTTP feature that enables a web application running under one domain to access resources in another
// domain. Web browsers implement a security restriction known as same-origin policy that
// prevents a web page from calling APIs in a different domain; CORS provides a secure way to allow one domain (the origin
// domain) to call APIs in another domain
type CorsRule = generated.CorsRule

// RetentionPolicy - the retention policy which determines how long the associated data should persist
type RetentionPolicy = generated.RetentionPolicy

// Metrics - a summary of request statistics grouped by API in hour or minute aggregates for blobs
type Metrics = generated.Metrics

// Logging - Azure Analytics Logging settings.
type Logging = generated.Logging

// StaticWebsite - The properties that enable an account to host a static website
type StaticWebsite = generated.StaticWebsite

// StorageServiceProperties - Storage Service Properties.
type StorageServiceProperties = generated.StorageServiceProperties

// StorageServiceStats - Stats for the storage service.
type StorageServiceStats = generated.StorageServiceStats

// ---------------------------------------------------------------------------------------------------------------------

// GetAccountInfoOptions provides set of options for Client.GetAccountInfo
type GetAccountInfoOptions struct {
	// placeholder for future options
}

func (o *GetAccountInfoOptions) format() *generated.ServiceClientGetAccountInfoOptions {
	return nil
}

// ---------------------------------------------------------------------------------------------------------------------

// GetPropertiesOptions contains the optional parameters for the Client.GetProperties method.
type GetPropertiesOptions struct {
	// placeholder for future options
}

func (o *GetPropertiesOptions) format() *generated.ServiceClientGetPropertiesOptions {
	return nil
}

// ---------------------------------------------------------------------------------------------------------------------

// ListContainersOptions provides set of configurations for ListContainers operation
type ListContainersOptions struct {
	Include ListContainersInclude

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

// ListContainersInclude indicates what additional information the service should return with each container.
type ListContainersInclude struct {
	// Tells the service whether to return metadata for each container.
	Metadata bool

	// Tells the service whether to return soft-deleted containers.
	Deleted bool
}

// ---------------------------------------------------------------------------------------------------------------------

// SetPropertiesOptions provides set of options for Client.SetProperties
type SetPropertiesOptions struct {
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

func (o *SetPropertiesOptions) format() (generated.StorageServiceProperties, *generated.ServiceClientSetPropertiesOptions) {
	if o == nil {
		return generated.StorageServiceProperties{}, nil
	}

	return generated.StorageServiceProperties{
		Cors:                  o.Cors,
		DefaultServiceVersion: o.DefaultServiceVersion,
		DeleteRetentionPolicy: o.DeleteRetentionPolicy,
		HourMetrics:           o.HourMetrics,
		Logging:               o.Logging,
		MinuteMetrics:         o.MinuteMetrics,
		StaticWebsite:         o.StaticWebsite,
	}, nil
}

// ---------------------------------------------------------------------------------------------------------------------

// GetStatisticsOptions provides set of options for Client.GetStatistics
type GetStatisticsOptions struct {
	// placeholder for future options
}

func (o *GetStatisticsOptions) format() *generated.ServiceClientGetStatisticsOptions {
	return nil
}

// ---------------------------------------------------------------------------------------------------------------------

// FilterBlobsOptions provides set of options for Client.FindBlobsByTags
type FilterBlobsOptions struct {
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
	// Filters the results to return only to return only blobs whose tags match the specified expression.
	Where *string
}

func (o *FilterBlobsOptions) format() *generated.ServiceClientFilterBlobsOptions {
	if o == nil {
		return nil
	}
	return &generated.ServiceClientFilterBlobsOptions{
		Marker:     o.Marker,
		Maxresults: o.MaxResults,
		Where:      o.Where,
	}
}
