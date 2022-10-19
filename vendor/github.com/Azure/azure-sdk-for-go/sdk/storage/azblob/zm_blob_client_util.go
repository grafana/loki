//go:build go1.18
// +build go1.18

// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License.

package azblob

import (
	"context"
	"io"
	"net/http"
	"time"
)

// ---------------------------------------------------------------------------------------------------------------------

// BlobDownloadOptions provides set of configurations for Download blob operation
type BlobDownloadOptions struct {
	// When set to true and specified together with the Range, the service returns the MD5 hash for the range, as long as the
	// range is less than or equal to 4 MB in size.
	RangeGetContentMD5 *bool

	// Optional, you can specify whether a particular range of the blob is read
	Offset *int64
	Count  *int64

	BlobAccessConditions *BlobAccessConditions
	CpkInfo              *CpkInfo
	CpkScopeInfo         *CpkScopeInfo
}

func (o *BlobDownloadOptions) format() (*blobClientDownloadOptions, *LeaseAccessConditions, *CpkInfo, *ModifiedAccessConditions) {
	if o == nil {
		return nil, nil, nil, nil
	}

	offset := int64(0)
	count := int64(CountToEnd)

	if o.Offset != nil {
		offset = *o.Offset
	}

	if o.Count != nil {
		count = *o.Count
	}

	basics := blobClientDownloadOptions{
		RangeGetContentMD5: o.RangeGetContentMD5,
		Range:              (&HttpRange{Offset: offset, Count: count}).format(),
	}

	leaseAccessConditions, modifiedAccessConditions := o.BlobAccessConditions.format()
	return &basics, leaseAccessConditions, o.CpkInfo, modifiedAccessConditions
}

// BlobDownloadResponse wraps AutoRest generated BlobDownloadResponse and helps to provide info for retry.
type BlobDownloadResponse struct {
	blobClientDownloadResponse
	ctx                    context.Context
	b                      *BlobClient
	getInfo                HTTPGetterInfo
	ObjectReplicationRules []ObjectReplicationPolicy
}

// Body constructs new RetryReader stream for reading data. If a connection fails
// while reading, it will make additional requests to reestablish a connection and
// continue reading. Specifying a RetryReaderOption's with MaxRetryRequests set to 0
// (the default), returns the original response body and no retries will be performed.
// Pass in nil for options to accept the default options.
func (r *BlobDownloadResponse) Body(options *RetryReaderOptions) io.ReadCloser {
	if options == nil {
		options = &RetryReaderOptions{}
	}

	if options.MaxRetryRequests == 0 { // No additional retries
		return r.RawResponse.Body
	}
	return NewRetryReader(r.ctx, r.RawResponse, r.getInfo, *options,
		func(ctx context.Context, getInfo HTTPGetterInfo) (*http.Response, error) {
			accessConditions := &BlobAccessConditions{
				ModifiedAccessConditions: &ModifiedAccessConditions{IfMatch: &getInfo.ETag},
			}
			options := BlobDownloadOptions{
				Offset:               &getInfo.Offset,
				Count:                &getInfo.Count,
				BlobAccessConditions: accessConditions,
				CpkInfo:              options.CpkInfo,
				//CpkScopeInfo: 			  o.CpkScopeInfo,
			}
			resp, err := r.b.Download(ctx, &options)
			if err != nil {
				return nil, err
			}
			return resp.RawResponse, err
		},
	)
}

// ---------------------------------------------------------------------------------------------------------------------

// BlobDeleteOptions provides set of configurations for Delete blob operation
type BlobDeleteOptions struct {
	// Required if the blob has associated snapshots. Specify one of the following two options: include: Delete the base blob
	// and all of its snapshots. only: Delete only the blob's snapshots and not the blob itself
	DeleteSnapshots      *DeleteSnapshotsOptionType
	BlobAccessConditions *BlobAccessConditions
}

func (o *BlobDeleteOptions) format() (*blobClientDeleteOptions, *LeaseAccessConditions, *ModifiedAccessConditions) {
	if o == nil {
		return nil, nil, nil
	}

	basics := blobClientDeleteOptions{
		DeleteSnapshots: o.DeleteSnapshots,
	}

	if o.BlobAccessConditions == nil {
		return &basics, nil, nil
	}

	return &basics, o.BlobAccessConditions.LeaseAccessConditions, o.BlobAccessConditions.ModifiedAccessConditions
}

// BlobDeleteResponse contains the response from method BlobClient.Delete.
type BlobDeleteResponse struct {
	blobClientDeleteResponse
}

func toBlobDeleteResponse(resp blobClientDeleteResponse) BlobDeleteResponse {
	return BlobDeleteResponse{resp}
}

// ---------------------------------------------------------------------------------------------------------------------

// BlobUndeleteOptions provides set of configurations for Blob Undelete operation
type BlobUndeleteOptions struct {
}

func (o *BlobUndeleteOptions) format() *blobClientUndeleteOptions {
	return nil
}

// BlobUndeleteResponse contains the response from method BlobClient.Undelete.
type BlobUndeleteResponse struct {
	blobClientUndeleteResponse
}

func toBlobUndeleteResponse(resp blobClientUndeleteResponse) BlobUndeleteResponse {
	return BlobUndeleteResponse{resp}
}

// ---------------------------------------------------------------------------------------------------------------------

// BlobSetTierOptions provides set of configurations for SetTier on blob operation
type BlobSetTierOptions struct {
	// Optional: Indicates the priority with which to rehydrate an archived blob.
	RehydratePriority *RehydratePriority

	LeaseAccessConditions    *LeaseAccessConditions
	ModifiedAccessConditions *ModifiedAccessConditions
}

func (o *BlobSetTierOptions) format() (*blobClientSetTierOptions, *LeaseAccessConditions, *ModifiedAccessConditions) {
	if o == nil {
		return nil, nil, nil
	}

	basics := blobClientSetTierOptions{RehydratePriority: o.RehydratePriority}
	return &basics, o.LeaseAccessConditions, o.ModifiedAccessConditions
}

// BlobSetTierResponse contains the response from method BlobClient.SetTier.
type BlobSetTierResponse struct {
	blobClientSetTierResponse
}

func toBlobSetTierResponse(resp blobClientSetTierResponse) BlobSetTierResponse {
	return BlobSetTierResponse{resp}
}

// ---------------------------------------------------------------------------------------------------------------------

// BlobGetPropertiesOptions provides set of configurations for GetProperties blob operation
type BlobGetPropertiesOptions struct {
	BlobAccessConditions *BlobAccessConditions
	CpkInfo              *CpkInfo
}

func (o *BlobGetPropertiesOptions) format() (blobClientGetPropertiesOptions *blobClientGetPropertiesOptions,
	leaseAccessConditions *LeaseAccessConditions, cpkInfo *CpkInfo, modifiedAccessConditions *ModifiedAccessConditions) {
	if o == nil {
		return nil, nil, nil, nil
	}

	leaseAccessConditions, modifiedAccessConditions = o.BlobAccessConditions.format()
	return nil, leaseAccessConditions, o.CpkInfo, modifiedAccessConditions
}

// ObjectReplicationRules struct
type ObjectReplicationRules struct {
	RuleId string
	Status string
}

// ObjectReplicationPolicy are deserialized attributes
type ObjectReplicationPolicy struct {
	PolicyId *string
	Rules    *[]ObjectReplicationRules
}

// BlobGetPropertiesResponse reformat the GetPropertiesResponse object for easy consumption
type BlobGetPropertiesResponse struct {
	blobClientGetPropertiesResponse

	// deserialized attributes
	ObjectReplicationRules []ObjectReplicationPolicy
}

func toGetBlobPropertiesResponse(resp blobClientGetPropertiesResponse) BlobGetPropertiesResponse {
	getResp := BlobGetPropertiesResponse{
		blobClientGetPropertiesResponse: resp,
		ObjectReplicationRules:          deserializeORSPolicies(resp.ObjectReplicationRules),
	}
	return getResp
}

// ---------------------------------------------------------------------------------------------------------------------

// BlobSetHTTPHeadersOptions provides set of configurations for SetHTTPHeaders on blob operation
type BlobSetHTTPHeadersOptions struct {
	LeaseAccessConditions    *LeaseAccessConditions
	ModifiedAccessConditions *ModifiedAccessConditions
}

func (o *BlobSetHTTPHeadersOptions) format() (*blobClientSetHTTPHeadersOptions, *LeaseAccessConditions, *ModifiedAccessConditions) {
	if o == nil {
		return nil, nil, nil
	}

	return nil, o.LeaseAccessConditions, o.ModifiedAccessConditions
}

// BlobSetHTTPHeadersResponse contains the response from method BlobClient.SetHTTPHeaders.
type BlobSetHTTPHeadersResponse struct {
	blobClientSetHTTPHeadersResponse
}

func toBlobSetHTTPHeadersResponse(resp blobClientSetHTTPHeadersResponse) BlobSetHTTPHeadersResponse {
	return BlobSetHTTPHeadersResponse{resp}
}

// ---------------------------------------------------------------------------------------------------------------------

// BlobSetMetadataOptions provides set of configurations for Set Metadata on blob operation
type BlobSetMetadataOptions struct {
	LeaseAccessConditions    *LeaseAccessConditions
	CpkInfo                  *CpkInfo
	CpkScopeInfo             *CpkScopeInfo
	ModifiedAccessConditions *ModifiedAccessConditions
}

func (o *BlobSetMetadataOptions) format() (leaseAccessConditions *LeaseAccessConditions, cpkInfo *CpkInfo,
	cpkScopeInfo *CpkScopeInfo, modifiedAccessConditions *ModifiedAccessConditions) {
	if o == nil {
		return nil, nil, nil, nil
	}

	return o.LeaseAccessConditions, o.CpkInfo, o.CpkScopeInfo, o.ModifiedAccessConditions
}

// BlobSetMetadataResponse contains the response from method BlobClient.SetMetadata.
type BlobSetMetadataResponse struct {
	blobClientSetMetadataResponse
}

func toBlobSetMetadataResponse(resp blobClientSetMetadataResponse) BlobSetMetadataResponse {
	return BlobSetMetadataResponse{resp}
}

// ---------------------------------------------------------------------------------------------------------------------

// BlobCreateSnapshotOptions provides set of configurations for CreateSnapshot of blob operation
type BlobCreateSnapshotOptions struct {
	Metadata                 map[string]string
	LeaseAccessConditions    *LeaseAccessConditions
	CpkInfo                  *CpkInfo
	CpkScopeInfo             *CpkScopeInfo
	ModifiedAccessConditions *ModifiedAccessConditions
}

func (o *BlobCreateSnapshotOptions) format() (blobSetMetadataOptions *blobClientCreateSnapshotOptions, cpkInfo *CpkInfo,
	cpkScopeInfo *CpkScopeInfo, modifiedAccessConditions *ModifiedAccessConditions, leaseAccessConditions *LeaseAccessConditions) {
	if o == nil {
		return nil, nil, nil, nil, nil
	}

	basics := blobClientCreateSnapshotOptions{
		Metadata: o.Metadata,
	}

	return &basics, o.CpkInfo, o.CpkScopeInfo, o.ModifiedAccessConditions, o.LeaseAccessConditions
}

// BlobCreateSnapshotResponse contains the response from method BlobClient.CreateSnapshot
type BlobCreateSnapshotResponse struct {
	blobClientCreateSnapshotResponse
}

func toBlobCreateSnapshotResponse(resp blobClientCreateSnapshotResponse) BlobCreateSnapshotResponse {
	return BlobCreateSnapshotResponse{resp}
}

// ---------------------------------------------------------------------------------------------------------------------

// BlobStartCopyOptions provides set of configurations for StartCopyFromURL blob operation
type BlobStartCopyOptions struct {
	// Specifies the date time when the blobs immutability policy is set to expire.
	ImmutabilityPolicyExpiry *time.Time
	// Specifies the immutability policy mode to set on the blob.
	ImmutabilityPolicyMode *BlobImmutabilityPolicyMode
	// Specified if a legal hold should be set on the blob.
	LegalHold *bool
	// Optional. Used to set blob tags in various blob operations.
	TagsMap map[string]string
	// Optional. Specifies a user-defined name-value pair associated with the blob. If no name-value pairs are specified, the
	// operation will copy the metadata from the source blob or file to the destination blob. If one or more name-value pairs
	// are specified, the destination blob is created with the specified metadata, and metadata is not copied from the source
	// blob or file. Note that beginning with version 2009-09-19, metadata names must adhere to the naming rules for C# identifiers.
	// See Naming and Referencing Containers, Blobs, and Metadata for more information.
	Metadata map[string]string
	// Optional: Indicates the priority with which to rehydrate an archived blob.
	RehydratePriority *RehydratePriority
	// Overrides the sealed state of the destination blob. Service version 2019-12-12 and newer.
	SealBlob *bool
	// Optional. Indicates the tier to be set on the blob.
	Tier *AccessTier

	SourceModifiedAccessConditions *SourceModifiedAccessConditions

	ModifiedAccessConditions *ModifiedAccessConditions

	LeaseAccessConditions *LeaseAccessConditions
}

func (o *BlobStartCopyOptions) format() (blobStartCopyFromUrlOptions *blobClientStartCopyFromURLOptions,
	sourceModifiedAccessConditions *SourceModifiedAccessConditions, modifiedAccessConditions *ModifiedAccessConditions, leaseAccessConditions *LeaseAccessConditions) {
	if o == nil {
		return nil, nil, nil, nil
	}

	basics := blobClientStartCopyFromURLOptions{
		BlobTagsString:           serializeBlobTagsToStrPtr(o.TagsMap),
		Metadata:                 o.Metadata,
		RehydratePriority:        o.RehydratePriority,
		SealBlob:                 o.SealBlob,
		Tier:                     o.Tier,
		ImmutabilityPolicyExpiry: o.ImmutabilityPolicyExpiry,
		ImmutabilityPolicyMode:   o.ImmutabilityPolicyMode,
		LegalHold:                o.LegalHold,
	}

	return &basics, o.SourceModifiedAccessConditions, o.ModifiedAccessConditions, o.LeaseAccessConditions
}

// BlobStartCopyFromURLResponse contains the response from method BlobClient.StartCopyFromURL.
type BlobStartCopyFromURLResponse struct {
	blobClientStartCopyFromURLResponse
}

func toBlobStartCopyFromURLResponse(resp blobClientStartCopyFromURLResponse) BlobStartCopyFromURLResponse {
	return BlobStartCopyFromURLResponse{resp}
}

// ---------------------------------------------------------------------------------------------------------------------

// BlobAbortCopyOptions provides set of configurations for AbortCopyFromURL operation
type BlobAbortCopyOptions struct {
	LeaseAccessConditions *LeaseAccessConditions
}

func (o *BlobAbortCopyOptions) format() (blobAbortCopyFromUrlOptions *blobClientAbortCopyFromURLOptions,
	leaseAccessConditions *LeaseAccessConditions) {
	if o == nil {
		return nil, nil
	}
	return nil, o.LeaseAccessConditions
}

// BlobAbortCopyFromURLResponse contains the response from method BlobClient.AbortCopyFromURL
type BlobAbortCopyFromURLResponse struct {
	blobClientAbortCopyFromURLResponse
}

func toBlobAbortCopyFromURLResponse(resp blobClientAbortCopyFromURLResponse) BlobAbortCopyFromURLResponse {
	return BlobAbortCopyFromURLResponse{resp}
}

// ---------------------------------------------------------------------------------------------------------------------

// BlobSetTagsOptions provides set of configurations for SetTags operation
type BlobSetTagsOptions struct {
	// The version id parameter is an opaque DateTime value that, when present,
	// specifies the version of the blob to operate on. It's for service version 2019-10-10 and newer.
	VersionID *string
	// Optional header, Specifies the transactional crc64 for the body, to be validated by the service.
	TransactionalContentCRC64 []byte
	// Optional header, Specifies the transactional md5 for the body, to be validated by the service.
	TransactionalContentMD5 []byte

	TagsMap map[string]string

	ModifiedAccessConditions *ModifiedAccessConditions
	LeaseAccessConditions    *LeaseAccessConditions
}

func (o *BlobSetTagsOptions) format() (*blobClientSetTagsOptions, *ModifiedAccessConditions, *LeaseAccessConditions) {
	if o == nil {
		return nil, nil, nil
	}

	options := &blobClientSetTagsOptions{
		Tags:                      serializeBlobTags(o.TagsMap),
		TransactionalContentMD5:   o.TransactionalContentMD5,
		TransactionalContentCRC64: o.TransactionalContentCRC64,
		VersionID:                 o.VersionID,
	}

	return options, o.ModifiedAccessConditions, o.LeaseAccessConditions
}

// BlobSetTagsResponse contains the response from method BlobClient.SetTags
type BlobSetTagsResponse struct {
	blobClientSetTagsResponse
}

func toBlobSetTagsResponse(resp blobClientSetTagsResponse) BlobSetTagsResponse {
	return BlobSetTagsResponse{resp}
}

// ---------------------------------------------------------------------------------------------------------------------

// BlobGetTagsOptions provides set of configurations for GetTags operation
type BlobGetTagsOptions struct {
	// The snapshot parameter is an opaque DateTime value that, when present, specifies the blob snapshot to retrieve.
	Snapshot *string
	// The version id parameter is an opaque DateTime value that, when present, specifies the version of the blob to operate on.
	// It's for service version 2019-10-10 and newer.
	VersionID *string

	BlobAccessConditions *BlobAccessConditions
}

func (o *BlobGetTagsOptions) format() (*blobClientGetTagsOptions, *ModifiedAccessConditions, *LeaseAccessConditions) {
	if o == nil {
		return nil, nil, nil
	}

	options := &blobClientGetTagsOptions{
		Snapshot:  o.Snapshot,
		VersionID: o.VersionID,
	}

	leaseAccessConditions, modifiedAccessConditions := o.BlobAccessConditions.format()

	return options, modifiedAccessConditions, leaseAccessConditions
}

// BlobGetTagsResponse contains the response from method BlobClient.GetTags
type BlobGetTagsResponse struct {
	blobClientGetTagsResponse
}

func toBlobGetTagsResponse(resp blobClientGetTagsResponse) BlobGetTagsResponse {
	return BlobGetTagsResponse{resp}
}
