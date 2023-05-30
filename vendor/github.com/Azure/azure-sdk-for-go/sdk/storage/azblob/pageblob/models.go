//go:build go1.18
// +build go1.18

// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License. See License.txt in the project root for license information.

package pageblob

import (
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/blob"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/internal/shared"

	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/internal/exported"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/internal/generated"
)

// Type Declarations ---------------------------------------------------------------------

// PageList - the list of pages
type PageList = generated.PageList

// ClearRange defines a range of pages.
type ClearRange = generated.ClearRange

// PageRange defines a range of pages.
type PageRange = generated.PageRange

// SequenceNumberAccessConditions contains a group of parameters for the Client.UploadPages method.
type SequenceNumberAccessConditions = generated.SequenceNumberAccessConditions

// Request Model Declaration -------------------------------------------------------------------------------------------

// CreateOptions contains the optional parameters for the Client.Create method.
type CreateOptions struct {
	// Set for page blobs only. The sequence number is a user-controlled value that you can use to track requests. The value of
	// the sequence number must be between 0 and 2^63 - 1.
	SequenceNumber *int64

	// Optional. Used to set blob tags in various blob operations.
	Tags map[string]string

	// Optional. Specifies a user-defined name-value pair associated with the blob. If no name-value pairs are specified, the
	// operation will copy the metadata from the source blob or file to the destination blob. If one or more name-value pairs
	// are specified, the destination blob is created with the specified metadata, and metadata is not copied from the source
	// blob or file. Note that beginning with version 2009-09-19, metadata names must adhere to the naming rules for C# identifiers.
	// See Naming and Referencing Containers, Blobs, and Metadata for more information.
	Metadata map[string]string

	// Optional. Indicates the tier to be set on the page blob.
	Tier *PremiumPageBlobAccessTier

	HTTPHeaders *blob.HTTPHeaders

	CpkInfo *blob.CpkInfo

	CpkScopeInfo *blob.CpkScopeInfo

	AccessConditions *blob.AccessConditions
	// Specifies the date time when the blobs immutability policy is set to expire.
	ImmutabilityPolicyExpiry *time.Time
	// Specifies the immutability policy mode to set on the blob.
	ImmutabilityPolicyMode *blob.ImmutabilityPolicyMode
	// Specified if a legal hold should be set on the blob.
	LegalHold *bool
}

func (o *CreateOptions) format() (*generated.PageBlobClientCreateOptions, *generated.BlobHTTPHeaders,
	*generated.LeaseAccessConditions, *generated.CpkInfo, *generated.CpkScopeInfo, *generated.ModifiedAccessConditions) {
	if o == nil {
		return nil, nil, nil, nil, nil, nil
	}

	options := &generated.PageBlobClientCreateOptions{
		BlobSequenceNumber: o.SequenceNumber,
		BlobTagsString:     shared.SerializeBlobTagsToStrPtr(o.Tags),
		Metadata:           o.Metadata,
		Tier:               o.Tier,
	}
	leaseAccessConditions, modifiedAccessConditions := exported.FormatBlobAccessConditions(o.AccessConditions)
	return options, o.HTTPHeaders, leaseAccessConditions, o.CpkInfo, o.CpkScopeInfo, modifiedAccessConditions
}

// ---------------------------------------------------------------------------------------------------------------------

// UploadPagesOptions contains the optional parameters for the Client.UploadPages method.
type UploadPagesOptions struct {
	// Range specifies a range of bytes.  The default value is all bytes.
	Range blob.HTTPRange

	TransactionalContentCRC64 []byte
	// Specify the transactional md5 for the body, to be validated by the service.
	TransactionalContentMD5 []byte

	CpkInfo                        *blob.CpkInfo
	CpkScopeInfo                   *blob.CpkScopeInfo
	SequenceNumberAccessConditions *SequenceNumberAccessConditions
	AccessConditions               *blob.AccessConditions
}

func (o *UploadPagesOptions) format() (*generated.PageBlobClientUploadPagesOptions, *generated.LeaseAccessConditions,
	*generated.CpkInfo, *generated.CpkScopeInfo, *generated.SequenceNumberAccessConditions, *generated.ModifiedAccessConditions) {
	if o == nil {
		return nil, nil, nil, nil, nil, nil
	}

	options := &generated.PageBlobClientUploadPagesOptions{
		TransactionalContentCRC64: o.TransactionalContentCRC64,
		TransactionalContentMD5:   o.TransactionalContentMD5,
		Range:                     exported.FormatHTTPRange(o.Range),
	}

	leaseAccessConditions, modifiedAccessConditions := exported.FormatBlobAccessConditions(o.AccessConditions)
	return options, leaseAccessConditions, o.CpkInfo, o.CpkScopeInfo, o.SequenceNumberAccessConditions, modifiedAccessConditions
}

// ---------------------------------------------------------------------------------------------------------------------

// UploadPagesFromURLOptions contains the optional parameters for the Client.UploadPagesFromURL method.
type UploadPagesFromURLOptions struct {
	// Only Bearer type is supported. Credentials should be a valid OAuth access token to copy source.
	CopySourceAuthorization *string
	// Specify the md5 calculated for the range of bytes that must be read from the copy source.
	SourceContentMD5 []byte
	// Specify the crc64 calculated for the range of bytes that must be read from the copy source.
	SourceContentCRC64 []byte

	CpkInfo *blob.CpkInfo

	CpkScopeInfo *blob.CpkScopeInfo

	SequenceNumberAccessConditions *SequenceNumberAccessConditions

	SourceModifiedAccessConditions *blob.SourceModifiedAccessConditions

	AccessConditions *blob.AccessConditions
}

func (o *UploadPagesFromURLOptions) format() (*generated.PageBlobClientUploadPagesFromURLOptions, *generated.CpkInfo, *generated.CpkScopeInfo,
	*generated.LeaseAccessConditions, *generated.SequenceNumberAccessConditions, *generated.ModifiedAccessConditions, *generated.SourceModifiedAccessConditions) {
	if o == nil {
		return nil, nil, nil, nil, nil, nil, nil
	}

	options := &generated.PageBlobClientUploadPagesFromURLOptions{
		SourceContentMD5:        o.SourceContentMD5,
		SourceContentcrc64:      o.SourceContentCRC64,
		CopySourceAuthorization: o.CopySourceAuthorization,
	}

	leaseAccessConditions, modifiedAccessConditions := exported.FormatBlobAccessConditions(o.AccessConditions)
	return options, o.CpkInfo, o.CpkScopeInfo, leaseAccessConditions, o.SequenceNumberAccessConditions, modifiedAccessConditions, o.SourceModifiedAccessConditions
}

// ---------------------------------------------------------------------------------------------------------------------

// ClearPagesOptions contains the optional parameters for the Client.ClearPages operation
type ClearPagesOptions struct {
	CpkInfo                        *blob.CpkInfo
	CpkScopeInfo                   *blob.CpkScopeInfo
	SequenceNumberAccessConditions *SequenceNumberAccessConditions
	AccessConditions               *blob.AccessConditions
}

func (o *ClearPagesOptions) format() (*generated.LeaseAccessConditions, *generated.CpkInfo,
	*generated.CpkScopeInfo, *generated.SequenceNumberAccessConditions, *generated.ModifiedAccessConditions) {
	if o == nil {
		return nil, nil, nil, nil, nil
	}

	leaseAccessConditions, modifiedAccessConditions := exported.FormatBlobAccessConditions(o.AccessConditions)
	return leaseAccessConditions, o.CpkInfo, o.CpkScopeInfo, o.SequenceNumberAccessConditions, modifiedAccessConditions
}

// ---------------------------------------------------------------------------------------------------------------------

// GetPageRangesOptions contains the optional parameters for the Client.NewGetPageRangesPager method.
type GetPageRangesOptions struct {
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
	// Range specifies a range of bytes.  The default value is all bytes.
	Range blob.HTTPRange
	// The snapshot parameter is an opaque DateTime value that, when present, specifies the blob snapshot to retrieve. For more
	// information on working with blob snapshots, see Creating a Snapshot of a Blob.
	// [https://docs.microsoft.com/en-us/rest/api/storageservices/fileservices/creating-a-snapshot-of-a-blob]
	Snapshot *string

	AccessConditions *blob.AccessConditions
}

func (o *GetPageRangesOptions) format() (*generated.PageBlobClientGetPageRangesOptions, *generated.LeaseAccessConditions, *generated.ModifiedAccessConditions) {
	if o == nil {
		return nil, nil, nil
	}

	leaseAccessConditions, modifiedAccessConditions := exported.FormatBlobAccessConditions(o.AccessConditions)
	return &generated.PageBlobClientGetPageRangesOptions{
		Marker:     o.Marker,
		Maxresults: o.MaxResults,
		Range:      exported.FormatHTTPRange(o.Range),
		Snapshot:   o.Snapshot,
	}, leaseAccessConditions, modifiedAccessConditions
}

// ---------------------------------------------------------------------------------------------------------------------

// GetPageRangesDiffOptions contains the optional parameters for the Client.NewGetPageRangesDiffPager method.
type GetPageRangesDiffOptions struct {
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
	// Range specifies a range of bytes.  The default value is all bytes.
	Range blob.HTTPRange

	// The snapshot parameter is an opaque DateTime value that, when present, specifies the blob snapshot to retrieve. For more
	// information on working with blob snapshots, see Creating a Snapshot of a Blob.
	// [https://docs.microsoft.com/en-us/rest/api/storageservices/fileservices/creating-a-snapshot-of-a-blob]
	Snapshot *string

	AccessConditions *blob.AccessConditions
}

func (o *GetPageRangesDiffOptions) format() (*generated.PageBlobClientGetPageRangesDiffOptions, *generated.LeaseAccessConditions, *generated.ModifiedAccessConditions) {
	if o == nil {
		return nil, nil, nil
	}

	leaseAccessConditions, modifiedAccessConditions := exported.FormatBlobAccessConditions(o.AccessConditions)
	return &generated.PageBlobClientGetPageRangesDiffOptions{
		Marker:          o.Marker,
		Maxresults:      o.MaxResults,
		PrevSnapshotURL: o.PrevSnapshotURL,
		Prevsnapshot:    o.PrevSnapshot,
		Range:           exported.FormatHTTPRange(o.Range),
		Snapshot:        o.Snapshot,
	}, leaseAccessConditions, modifiedAccessConditions

}

// ---------------------------------------------------------------------------------------------------------------------

// ResizeOptions contains the optional parameters for the Client.Resize method.
type ResizeOptions struct {
	CpkInfo          *blob.CpkInfo
	CpkScopeInfo     *blob.CpkScopeInfo
	AccessConditions *blob.AccessConditions
}

func (o *ResizeOptions) format() (*generated.PageBlobClientResizeOptions, *generated.LeaseAccessConditions,
	*generated.CpkInfo, *generated.CpkScopeInfo, *generated.ModifiedAccessConditions) {
	if o == nil {
		return nil, nil, nil, nil, nil
	}

	leaseAccessConditions, modifiedAccessConditions := exported.FormatBlobAccessConditions(o.AccessConditions)
	return nil, leaseAccessConditions, o.CpkInfo, o.CpkScopeInfo, modifiedAccessConditions
}

// ---------------------------------------------------------------------------------------------------------------------

// UpdateSequenceNumberOptions contains the optional parameters for the Client.UpdateSequenceNumber method.
type UpdateSequenceNumberOptions struct {
	ActionType *SequenceNumberActionType

	SequenceNumber *int64

	AccessConditions *blob.AccessConditions
}

func (o *UpdateSequenceNumberOptions) format() (*generated.SequenceNumberActionType, *generated.PageBlobClientUpdateSequenceNumberOptions,
	*generated.LeaseAccessConditions, *generated.ModifiedAccessConditions) {
	if o == nil {
		return nil, nil, nil, nil
	}

	options := &generated.PageBlobClientUpdateSequenceNumberOptions{
		BlobSequenceNumber: o.SequenceNumber,
	}

	if *o.ActionType == SequenceNumberActionTypeIncrement {
		options.BlobSequenceNumber = nil
	}

	leaseAccessConditions, modifiedAccessConditions := exported.FormatBlobAccessConditions(o.AccessConditions)
	return o.ActionType, options, leaseAccessConditions, modifiedAccessConditions
}

// ---------------------------------------------------------------------------------------------------------------------

// CopyIncrementalOptions contains the optional parameters for the Client.StartCopyIncremental method.
type CopyIncrementalOptions struct {
	ModifiedAccessConditions *blob.ModifiedAccessConditions
}

func (o *CopyIncrementalOptions) format() (*generated.PageBlobClientCopyIncrementalOptions, *generated.ModifiedAccessConditions) {
	if o == nil {
		return nil, nil
	}

	return nil, o.ModifiedAccessConditions
}

// ---------------------------------------------------------------------------------------------------------------------
