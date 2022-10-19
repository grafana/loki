//go:build go1.18
// +build go1.18

// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License.

package azblob

import (
	"strconv"
	"time"
)

// ---------------------------------------------------------------------------------------------------------------------

func rangeToString(offset, count int64) string {
	return "bytes=" + strconv.FormatInt(offset, 10) + "-" + strconv.FormatInt(offset+count-1, 10)
}

// ---------------------------------------------------------------------------------------------------------------------

// PageBlobCreateOptions provides set of configurations for CreatePageBlob operation
type PageBlobCreateOptions struct {
	// Set for page blobs only. The sequence number is a user-controlled value that you can use to track requests. The value of
	// the sequence number must be between 0 and 2^63 - 1.
	BlobSequenceNumber *int64
	// Optional. Used to set blob tags in various blob operations.
	BlobTagsMap map[string]string
	// Optional. Specifies a user-defined name-value pair associated with the blob. If no name-value pairs are specified, the
	// operation will copy the metadata from the source blob or file to the destination blob. If one or more name-value pairs
	// are specified, the destination blob is created with the specified metadata, and metadata is not copied from the source
	// blob or file. Note that beginning with version 2009-09-19, metadata names must adhere to the naming rules for C# identifiers.
	// See Naming and Referencing Containers, Blobs, and Metadata for more information.
	Metadata map[string]string
	// Optional. Indicates the tier to be set on the page blob.
	Tier *PremiumPageBlobAccessTier

	HTTPHeaders *BlobHTTPHeaders

	CpkInfo *CpkInfo

	CpkScopeInfo *CpkScopeInfo

	BlobAccessConditions *BlobAccessConditions
	// Specifies the date time when the blobs immutability policy is set to expire.
	ImmutabilityPolicyExpiry *time.Time
	// Specifies the immutability policy mode to set on the blob.
	ImmutabilityPolicyMode *BlobImmutabilityPolicyMode
	// Specified if a legal hold should be set on the blob.
	LegalHold *bool
}

func (o *PageBlobCreateOptions) format() (*pageBlobClientCreateOptions, *BlobHTTPHeaders, *LeaseAccessConditions, *CpkInfo, *CpkScopeInfo, *ModifiedAccessConditions) {
	if o == nil {
		return nil, nil, nil, nil, nil, nil
	}

	options := &pageBlobClientCreateOptions{
		BlobSequenceNumber: o.BlobSequenceNumber,
		BlobTagsString:     serializeBlobTagsToStrPtr(o.BlobTagsMap),
		Metadata:           o.Metadata,
		Tier:               o.Tier,
	}
	leaseAccessConditions, modifiedAccessConditions := o.BlobAccessConditions.format()
	return options, o.HTTPHeaders, leaseAccessConditions, o.CpkInfo, o.CpkScopeInfo, modifiedAccessConditions
}

// PageBlobCreateResponse contains the response from method PageBlobClient.Create.
type PageBlobCreateResponse struct {
	pageBlobClientCreateResponse
}

func toPageBlobCreateResponse(resp pageBlobClientCreateResponse) PageBlobCreateResponse {
	return PageBlobCreateResponse{resp}
}

// ---------------------------------------------------------------------------------------------------------------------

// PageBlobUploadPagesOptions provides set of configurations for UploadPages operation
type PageBlobUploadPagesOptions struct {
	// Specify the transactional crc64 for the body, to be validated by the service.
	PageRange                 *HttpRange
	TransactionalContentCRC64 []byte
	// Specify the transactional md5 for the body, to be validated by the service.
	TransactionalContentMD5 []byte

	CpkInfo                        *CpkInfo
	CpkScopeInfo                   *CpkScopeInfo
	SequenceNumberAccessConditions *SequenceNumberAccessConditions
	BlobAccessConditions           *BlobAccessConditions
}

func (o *PageBlobUploadPagesOptions) format() (*pageBlobClientUploadPagesOptions, *LeaseAccessConditions,
	*CpkInfo, *CpkScopeInfo, *SequenceNumberAccessConditions, *ModifiedAccessConditions) {
	if o == nil {
		return nil, nil, nil, nil, nil, nil
	}

	options := &pageBlobClientUploadPagesOptions{
		TransactionalContentCRC64: o.TransactionalContentCRC64,
		TransactionalContentMD5:   o.TransactionalContentMD5,
	}

	if o.PageRange != nil {
		options.Range = o.PageRange.format()
	}

	leaseAccessConditions, modifiedAccessConditions := o.BlobAccessConditions.format()
	return options, leaseAccessConditions, o.CpkInfo, o.CpkScopeInfo, o.SequenceNumberAccessConditions, modifiedAccessConditions
}

// PageBlobUploadPagesResponse contains the response from method PageBlobClient.UploadPages.
type PageBlobUploadPagesResponse struct {
	pageBlobClientUploadPagesResponse
}

func toPageBlobUploadPagesResponse(resp pageBlobClientUploadPagesResponse) PageBlobUploadPagesResponse {
	return PageBlobUploadPagesResponse{resp}
}

// ---------------------------------------------------------------------------------------------------------------------

// PageBlobUploadPagesFromURLOptions provides set of configurations for UploadPagesFromURL operation
type PageBlobUploadPagesFromURLOptions struct {
	// Only Bearer type is supported. Credentials should be a valid OAuth access token to copy source.
	CopySourceAuthorization *string
	// Specify the md5 calculated for the range of bytes that must be read from the copy source.
	SourceContentMD5 []byte
	// Specify the crc64 calculated for the range of bytes that must be read from the copy source.
	SourceContentCRC64 []byte

	CpkInfo *CpkInfo

	CpkScopeInfo *CpkScopeInfo

	SequenceNumberAccessConditions *SequenceNumberAccessConditions

	SourceModifiedAccessConditions *SourceModifiedAccessConditions

	BlobAccessConditions *BlobAccessConditions
}

func (o *PageBlobUploadPagesFromURLOptions) format() (*pageBlobClientUploadPagesFromURLOptions, *CpkInfo, *CpkScopeInfo,
	*LeaseAccessConditions, *SequenceNumberAccessConditions, *ModifiedAccessConditions, *SourceModifiedAccessConditions) {
	if o == nil {
		return nil, nil, nil, nil, nil, nil, nil
	}

	options := &pageBlobClientUploadPagesFromURLOptions{
		SourceContentMD5:        o.SourceContentMD5,
		SourceContentcrc64:      o.SourceContentCRC64,
		CopySourceAuthorization: o.CopySourceAuthorization,
	}

	leaseAccessConditions, modifiedAccessConditions := o.BlobAccessConditions.format()
	return options, o.CpkInfo, o.CpkScopeInfo, leaseAccessConditions, o.SequenceNumberAccessConditions, modifiedAccessConditions, o.SourceModifiedAccessConditions
}

// PageBlobUploadPagesFromURLResponse contains the response from method PageBlobClient.UploadPagesFromURL
type PageBlobUploadPagesFromURLResponse struct {
	pageBlobClientUploadPagesFromURLResponse
}

func toPageBlobUploadPagesFromURLResponse(resp pageBlobClientUploadPagesFromURLResponse) PageBlobUploadPagesFromURLResponse {
	return PageBlobUploadPagesFromURLResponse{resp}
}

// ---------------------------------------------------------------------------------------------------------------------

// PageBlobClearPagesOptions provides set of configurations for PageBlobClient.ClearPages operation
type PageBlobClearPagesOptions struct {
	CpkInfo                        *CpkInfo
	CpkScopeInfo                   *CpkScopeInfo
	SequenceNumberAccessConditions *SequenceNumberAccessConditions
	BlobAccessConditions           *BlobAccessConditions
}

func (o *PageBlobClearPagesOptions) format() (*LeaseAccessConditions, *CpkInfo,
	*CpkScopeInfo, *SequenceNumberAccessConditions, *ModifiedAccessConditions) {
	if o == nil {
		return nil, nil, nil, nil, nil
	}

	leaseAccessConditions, modifiedAccessConditions := o.BlobAccessConditions.format()
	return leaseAccessConditions, o.CpkInfo, o.CpkScopeInfo, o.SequenceNumberAccessConditions, modifiedAccessConditions
}

// PageBlobClearPagesResponse contains the response from method PageBlobClient.ClearPages
type PageBlobClearPagesResponse struct {
	pageBlobClientClearPagesResponse
}

func toPageBlobClearPagesResponse(resp pageBlobClientClearPagesResponse) PageBlobClearPagesResponse {
	return PageBlobClearPagesResponse{resp}
}

// ---------------------------------------------------------------------------------------------------------------------

// PageBlobGetPageRangesOptions provides set of configurations for GetPageRanges operation
type PageBlobGetPageRangesOptions struct {
	Marker *string
	// Specifies the maximum number of containers to return. If the request does not specify maxresults, or specifies a value
	// greater than 5000, the server will return up to 5000 items. Note that if the
	// listing operation crosses a partition boundary, then the service will return a continuation token for retrieving the remainder
	// of the results. For this reason, it is possible that the service will
	// return fewer results than specified by maxresults, or than the default of 5000.
	MaxResults *int32
	// Optional. This header is only supported in service versions 2019-04-19 and after and specifies the URL of a previous snapshot
	// of the target blob. The response will only contain pages that were changed
	// between the target blob and its previous snapshot.
	PrevSnapshotURL *string
	// Optional in version 2015-07-08 and newer. The prevsnapshot parameter is a DateTime value that specifies that the response
	// will contain only pages that were changed between target blob and previous
	// snapshot. Changed pages include both updated and cleared pages. The target blob may be a snapshot, as long as the snapshot
	// specified by prevsnapshot is the older of the two. Note that incremental
	// snapshots are currently supported only for blobs created on or after January 1, 2016.
	PrevSnapshot *string
	// Optional, you can specify whether a particular range of the blob is read
	PageRange *HttpRange
	// The snapshot parameter is an opaque DateTime value that, when present, specifies the blob snapshot to retrieve. For more
	// information on working with blob snapshots, see Creating a Snapshot of a Blob.
	// [https://docs.microsoft.com/en-us/rest/api/storageservices/fileservices/creating-a-snapshot-of-a-blob]
	Snapshot *string

	BlobAccessConditions *BlobAccessConditions
}

func (o *PageBlobGetPageRangesOptions) format() (*pageBlobClientGetPageRangesOptions, *LeaseAccessConditions, *ModifiedAccessConditions) {
	if o == nil {
		return nil, nil, nil
	}

	leaseAccessConditions, modifiedAccessConditions := o.BlobAccessConditions.format()
	return &pageBlobClientGetPageRangesOptions{
		Marker:     o.Marker,
		Maxresults: o.MaxResults,
		Range:      o.PageRange.format(),
		Snapshot:   o.Snapshot,
	}, leaseAccessConditions, modifiedAccessConditions
}

// PageBlobGetPageRangesPager provides operations for iterating over paged responses
type PageBlobGetPageRangesPager struct {
	*pageBlobClientGetPageRangesPager
}

func toPageBlobGetPageRangesPager(resp *pageBlobClientGetPageRangesPager) *PageBlobGetPageRangesPager {
	return &PageBlobGetPageRangesPager{resp}
}

// ---------------------------------------------------------------------------------------------------------------------

// PageBlobGetPageRangesDiffOptions provides set of configurations for PageBlobClient.GetPageRangesDiff operation
type PageBlobGetPageRangesDiffOptions struct {
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
	// Optional. This header is only supported in service versions 2019-04-19 and after and specifies the URL of a previous snapshot
	// of the target blob. The response will only contain pages that were changed
	// between the target blob and its previous snapshot.
	PrevSnapshotURL *string
	// Optional in version 2015-07-08 and newer. The prevsnapshot parameter is a DateTime value that specifies that the response
	// will contain only pages that were changed between target blob and previous
	// snapshot. Changed pages include both updated and cleared pages. The target blob may be a snapshot, as long as the snapshot
	// specified by prevsnapshot is the older of the two. Note that incremental
	// snapshots are currently supported only for blobs created on or after January 1, 2016.
	PrevSnapshot *string
	// Optional, you can specify whether a particular range of the blob is read
	PageRange *HttpRange

	// The snapshot parameter is an opaque DateTime value that, when present, specifies the blob snapshot to retrieve. For more
	// information on working with blob snapshots, see Creating a Snapshot of a Blob.
	// [https://docs.microsoft.com/en-us/rest/api/storageservices/fileservices/creating-a-snapshot-of-a-blob]
	Snapshot *string

	BlobAccessConditions *BlobAccessConditions
}

func (o *PageBlobGetPageRangesDiffOptions) format() (*pageBlobClientGetPageRangesDiffOptions, *LeaseAccessConditions, *ModifiedAccessConditions) {
	if o == nil {
		return nil, nil, nil
	}

	leaseAccessConditions, modifiedAccessConditions := o.BlobAccessConditions.format()
	return &pageBlobClientGetPageRangesDiffOptions{
		Marker:          o.Marker,
		Maxresults:      o.MaxResults,
		PrevSnapshotURL: o.PrevSnapshotURL,
		Prevsnapshot:    o.PrevSnapshot,
		Range:           o.PageRange.format(),
		Snapshot:        o.Snapshot,
	}, leaseAccessConditions, modifiedAccessConditions

}

// PageBlobGetPageRangesDiffPager provides operations for iterating over paged responses
type PageBlobGetPageRangesDiffPager struct {
	*pageBlobClientGetPageRangesDiffPager
}

func toPageBlobGetPageRangesDiffPager(resp *pageBlobClientGetPageRangesDiffPager) *PageBlobGetPageRangesDiffPager {
	return &PageBlobGetPageRangesDiffPager{resp}
}

// ---------------------------------------------------------------------------------------------------------------------

// PageBlobResizeOptions provides set of configurations for PageBlobClient.Resize operation
type PageBlobResizeOptions struct {
	CpkInfo              *CpkInfo
	CpkScopeInfo         *CpkScopeInfo
	BlobAccessConditions *BlobAccessConditions
}

func (o *PageBlobResizeOptions) format() (*pageBlobClientResizeOptions, *LeaseAccessConditions, *CpkInfo, *CpkScopeInfo, *ModifiedAccessConditions) {
	if o == nil {
		return nil, nil, nil, nil, nil
	}

	leaseAccessConditions, modifiedAccessConditions := o.BlobAccessConditions.format()
	return nil, leaseAccessConditions, o.CpkInfo, o.CpkScopeInfo, modifiedAccessConditions
}

// PageBlobResizeResponse contains the response from method PageBlobClient.Resize
type PageBlobResizeResponse struct {
	pageBlobClientResizeResponse
}

func toPageBlobResizeResponse(resp pageBlobClientResizeResponse) PageBlobResizeResponse {
	return PageBlobResizeResponse{resp}
}

// ---------------------------------------------------------------------------------------------------------------------

// PageBlobUpdateSequenceNumberOptions provides set of configurations for PageBlobClient.UpdateSequenceNumber operation
type PageBlobUpdateSequenceNumberOptions struct {
	ActionType *SequenceNumberActionType

	BlobSequenceNumber *int64

	BlobAccessConditions *BlobAccessConditions
}

func (o *PageBlobUpdateSequenceNumberOptions) format() (*SequenceNumberActionType, *pageBlobClientUpdateSequenceNumberOptions, *LeaseAccessConditions, *ModifiedAccessConditions) {
	if o == nil {
		return nil, nil, nil, nil
	}

	options := &pageBlobClientUpdateSequenceNumberOptions{
		BlobSequenceNumber: o.BlobSequenceNumber,
	}

	if *o.ActionType == SequenceNumberActionTypeIncrement {
		options.BlobSequenceNumber = nil
	}

	leaseAccessConditions, modifiedAccessConditions := o.BlobAccessConditions.format()
	return o.ActionType, options, leaseAccessConditions, modifiedAccessConditions
}

// PageBlobUpdateSequenceNumberResponse contains the response from method PageBlobClient.UpdateSequenceNumber
type PageBlobUpdateSequenceNumberResponse struct {
	pageBlobClientUpdateSequenceNumberResponse
}

func toPageBlobUpdateSequenceNumberResponse(resp pageBlobClientUpdateSequenceNumberResponse) PageBlobUpdateSequenceNumberResponse {
	return PageBlobUpdateSequenceNumberResponse{resp}
}

// ---------------------------------------------------------------------------------------------------------------------

// PageBlobCopyIncrementalOptions provides set of configurations for PageBlobClient.StartCopyIncremental operation
type PageBlobCopyIncrementalOptions struct {
	ModifiedAccessConditions *ModifiedAccessConditions
}

func (o *PageBlobCopyIncrementalOptions) format() (*pageBlobClientCopyIncrementalOptions, *ModifiedAccessConditions) {
	if o == nil {
		return nil, nil
	}

	return nil, o.ModifiedAccessConditions
}

// PageBlobCopyIncrementalResponse contains the response from method PageBlobClient.StartCopyIncremental
type PageBlobCopyIncrementalResponse struct {
	pageBlobClientCopyIncrementalResponse
}

func toPageBlobCopyIncrementalResponse(resp pageBlobClientCopyIncrementalResponse) PageBlobCopyIncrementalResponse {
	return PageBlobCopyIncrementalResponse{resp}
}

// ---------------------------------------------------------------------------------------------------------------------
