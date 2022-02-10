// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License.

package azblob

import (
	"strconv"
)

func rangeToString(offset, count int64) string {
	return "bytes=" + strconv.FormatInt(offset, 10) + "-" + strconv.FormatInt(offset+count-1, 10)
}

type CreatePageBlobOptions struct {
	// Set for page blobs only. The sequence number is a user-controlled value that you can use to track requests. The value of
	// the sequence number must be between 0 and 2^63 - 1.
	BlobSequenceNumber *int64
	// Optional. Used to set blob tags in various blob operations.
	TagsMap map[string]string
	// Optional. Specifies a user-defined name-value pair associated with the blob. If no name-value pairs are specified, the
	// operation will copy the metadata from the source blob or file to the destination blob. If one or more name-value pairs
	// are specified, the destination blob is created with the specified metadata, and metadata is not copied from the source
	// blob or file. Note that beginning with version 2009-09-19, metadata names must adhere to the naming rules for C# identifiers.
	// See Naming and Referencing Containers, Blobs, and Metadata for more information.
	Metadata map[string]string
	// Optional. Indicates the tier to be set on the page blob.
	Tier *PremiumPageBlobAccessTier

	HTTPHeaders          *BlobHTTPHeaders
	CpkInfo              *CpkInfo
	CpkScopeInfo         *CpkScopeInfo
	BlobAccessConditions *BlobAccessConditions
}

func (o *CreatePageBlobOptions) pointers() (*PageBlobCreateOptions, *BlobHTTPHeaders, *CpkInfo, *CpkScopeInfo, *LeaseAccessConditions, *ModifiedAccessConditions) {
	if o == nil {
		return nil, nil, nil, nil, nil, nil
	}

	options := &PageBlobCreateOptions{
		BlobSequenceNumber: o.BlobSequenceNumber,
		BlobTagsString:     serializeBlobTagsToStrPtr(o.TagsMap),
		Metadata:           o.Metadata,
		Tier:               o.Tier,
	}
	leaseAccessConditions, modifiedAccessConditions := o.BlobAccessConditions.pointers()
	return options, o.HTTPHeaders, o.CpkInfo, o.CpkScopeInfo, leaseAccessConditions, modifiedAccessConditions
}

type UploadPagesOptions struct {
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

func (o *UploadPagesOptions) pointers() (*PageBlobUploadPagesOptions, *CpkInfo, *CpkScopeInfo, *SequenceNumberAccessConditions, *LeaseAccessConditions, *ModifiedAccessConditions) {
	if o == nil {
		return nil, nil, nil, nil, nil, nil
	}

	options := &PageBlobUploadPagesOptions{
		TransactionalContentCRC64: o.TransactionalContentCRC64,
		TransactionalContentMD5:   o.TransactionalContentMD5,
	}

	if o.PageRange != nil {
		options.Range = o.PageRange.pointers()
	}

	leaseAccessConditions, modifiedAccessConditions := o.BlobAccessConditions.pointers()
	return options, o.CpkInfo, o.CpkScopeInfo, o.SequenceNumberAccessConditions, leaseAccessConditions, modifiedAccessConditions
}

type UploadPagesFromURLOptions struct {
	// Specify the md5 calculated for the range of bytes that must be read from the copy source.
	SourceContentMD5 []byte
	// Specify the crc64 calculated for the range of bytes that must be read from the copy source.
	SourceContentcrc64 []byte

	CpkInfo                        *CpkInfo
	CpkScopeInfo                   *CpkScopeInfo
	SequenceNumberAccessConditions *SequenceNumberAccessConditions
	SourceModifiedAccessConditions *SourceModifiedAccessConditions
	BlobAccessConditions           *BlobAccessConditions
}

func (o *UploadPagesFromURLOptions) pointers() (*PageBlobUploadPagesFromURLOptions, *CpkInfo, *CpkScopeInfo, *SequenceNumberAccessConditions, *SourceModifiedAccessConditions, *LeaseAccessConditions, *ModifiedAccessConditions) {
	if o == nil {
		return nil, nil, nil, nil, nil, nil, nil
	}

	options := &PageBlobUploadPagesFromURLOptions{
		SourceContentMD5:   o.SourceContentMD5,
		SourceContentcrc64: o.SourceContentcrc64,
	}

	leaseAccessConditions, modifiedAccessConditions := o.BlobAccessConditions.pointers()
	return options, o.CpkInfo, o.CpkScopeInfo, o.SequenceNumberAccessConditions, o.SourceModifiedAccessConditions, leaseAccessConditions, modifiedAccessConditions
}

type ClearPagesOptions struct {
	CpkInfo                        *CpkInfo
	CpkScopeInfo                   *CpkScopeInfo
	SequenceNumberAccessConditions *SequenceNumberAccessConditions
	BlobAccessConditions           *BlobAccessConditions
}

func (o *ClearPagesOptions) pointers() (*CpkInfo, *CpkScopeInfo, *SequenceNumberAccessConditions, *LeaseAccessConditions, *ModifiedAccessConditions) {
	if o == nil {
		return nil, nil, nil, nil, nil
	}

	leaseAccessConditions, modifiedAccessConditions := o.BlobAccessConditions.pointers()
	return o.CpkInfo, o.CpkScopeInfo, o.SequenceNumberAccessConditions, leaseAccessConditions, modifiedAccessConditions
}

type GetPageRangesOptions struct {
	Snapshot *string

	BlobAccessConditions *BlobAccessConditions
}

func (o *GetPageRangesOptions) pointers() (*string, *LeaseAccessConditions, *ModifiedAccessConditions) {
	if o == nil {
		return nil, nil, nil
	}

	leaseAccessConditions, modifiedAccessConditions := o.BlobAccessConditions.pointers()
	return o.Snapshot, leaseAccessConditions, modifiedAccessConditions
}

type ResizePageBlobOptions struct {
	CpkInfo              *CpkInfo
	CpkScopeInfo         *CpkScopeInfo
	BlobAccessConditions *BlobAccessConditions
}

func (o *ResizePageBlobOptions) pointers() (*CpkInfo, *CpkScopeInfo, *LeaseAccessConditions, *ModifiedAccessConditions) {
	if o == nil {
		return nil, nil, nil, nil
	}

	leaseAccessConditions, modifiedAccessConditions := o.BlobAccessConditions.pointers()
	return o.CpkInfo, o.CpkScopeInfo, leaseAccessConditions, modifiedAccessConditions
}

type UpdateSequenceNumberPageBlob struct {
	ActionType         *SequenceNumberActionType
	BlobSequenceNumber *int64

	BlobAccessConditions *BlobAccessConditions
}

func (o *UpdateSequenceNumberPageBlob) pointers() (*PageBlobUpdateSequenceNumberOptions, *SequenceNumberActionType, *LeaseAccessConditions, *ModifiedAccessConditions) {
	if o == nil {
		return nil, nil, nil, nil
	}

	options := &PageBlobUpdateSequenceNumberOptions{
		BlobSequenceNumber: o.BlobSequenceNumber,
	}

	if *o.ActionType == SequenceNumberActionTypeIncrement {
		options.BlobSequenceNumber = nil
	}

	leaseAccessConditions, modifiedAccessConditions := o.BlobAccessConditions.pointers()
	return options, o.ActionType, leaseAccessConditions, modifiedAccessConditions
}

type CopyIncrementalPageBlobOptions struct {
	ModifiedAccessConditions *ModifiedAccessConditions

	RequestID *string

	Timeout *int32
}

func (o *CopyIncrementalPageBlobOptions) pointers() (*PageBlobCopyIncrementalOptions, *ModifiedAccessConditions) {
	if o == nil {
		return nil, nil
	}

	options := PageBlobCopyIncrementalOptions{
		RequestID: o.RequestID,
		Timeout:   o.Timeout,
	}

	return &options, o.ModifiedAccessConditions
}
