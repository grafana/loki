//go:build go1.18
// +build go1.18

// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License.

package azblob

import "time"

// ---------------------------------------------------------------------------------------------------------------------

// BlockBlobUploadOptions provides set of configurations for UploadBlockBlob operation
type BlockBlobUploadOptions struct {
	// Optional. Used to set blob tags in various blob operations.
	TagsMap map[string]string

	// Optional. Specifies a user-defined name-value pair associated with the blob.
	Metadata map[string]string

	// Optional. Indicates the tier to be set on the blob.
	Tier *AccessTier

	// Specify the transactional md5 for the body, to be validated by the service.
	TransactionalContentMD5 []byte

	HTTPHeaders          *BlobHTTPHeaders
	CpkInfo              *CpkInfo
	CpkScopeInfo         *CpkScopeInfo
	BlobAccessConditions *BlobAccessConditions
}

func (o *BlockBlobUploadOptions) format() (*blockBlobClientUploadOptions, *BlobHTTPHeaders, *LeaseAccessConditions,
	*CpkInfo, *CpkScopeInfo, *ModifiedAccessConditions) {
	if o == nil {
		return nil, nil, nil, nil, nil, nil
	}

	basics := blockBlobClientUploadOptions{
		BlobTagsString:          serializeBlobTagsToStrPtr(o.TagsMap),
		Metadata:                o.Metadata,
		Tier:                    o.Tier,
		TransactionalContentMD5: o.TransactionalContentMD5,
	}

	leaseAccessConditions, modifiedAccessConditions := o.BlobAccessConditions.format()
	return &basics, o.HTTPHeaders, leaseAccessConditions, o.CpkInfo, o.CpkScopeInfo, modifiedAccessConditions
}

// BlockBlobUploadResponse contains the response from method BlockBlobClient.Upload.
type BlockBlobUploadResponse struct {
	blockBlobClientUploadResponse
}

func toBlockBlobUploadResponse(resp blockBlobClientUploadResponse) BlockBlobUploadResponse {
	return BlockBlobUploadResponse{resp}
}

// ---------------------------------------------------------------------------------------------------------------------

// BlockBlobStageBlockOptions provides set of configurations for StageBlock operation
type BlockBlobStageBlockOptions struct {
	CpkInfo *CpkInfo

	CpkScopeInfo *CpkScopeInfo

	LeaseAccessConditions *LeaseAccessConditions
	// Specify the transactional crc64 for the body, to be validated by the service.
	TransactionalContentCRC64 []byte
	// Specify the transactional md5 for the body, to be validated by the service.
	TransactionalContentMD5 []byte
}

func (o *BlockBlobStageBlockOptions) format() (*blockBlobClientStageBlockOptions, *LeaseAccessConditions, *CpkInfo, *CpkScopeInfo) {
	if o == nil {
		return nil, nil, nil, nil
	}

	return &blockBlobClientStageBlockOptions{
		TransactionalContentCRC64: o.TransactionalContentCRC64,
		TransactionalContentMD5:   o.TransactionalContentMD5,
	}, o.LeaseAccessConditions, o.CpkInfo, o.CpkScopeInfo
}

// BlockBlobStageBlockResponse contains the response from method BlockBlobClient.StageBlock.
type BlockBlobStageBlockResponse struct {
	blockBlobClientStageBlockResponse
}

func toBlockBlobStageBlockResponse(resp blockBlobClientStageBlockResponse) BlockBlobStageBlockResponse {
	return BlockBlobStageBlockResponse{resp}
}

// ---------------------------------------------------------------------------------------------------------------------

// BlockBlobStageBlockFromURLOptions provides set of configurations for StageBlockFromURL operation
type BlockBlobStageBlockFromURLOptions struct {
	// Only Bearer type is supported. Credentials should be a valid OAuth access token to copy source.
	CopySourceAuthorization *string

	LeaseAccessConditions *LeaseAccessConditions

	SourceModifiedAccessConditions *SourceModifiedAccessConditions
	// Specify the md5 calculated for the range of bytes that must be read from the copy source.
	SourceContentMD5 []byte
	// Specify the crc64 calculated for the range of bytes that must be read from the copy source.
	SourceContentCRC64 []byte

	Offset *int64

	Count *int64

	CpkInfo *CpkInfo

	CpkScopeInfo *CpkScopeInfo
}

func (o *BlockBlobStageBlockFromURLOptions) format() (*blockBlobClientStageBlockFromURLOptions, *CpkInfo, *CpkScopeInfo, *LeaseAccessConditions, *SourceModifiedAccessConditions) {
	if o == nil {
		return nil, nil, nil, nil, nil
	}

	options := &blockBlobClientStageBlockFromURLOptions{
		CopySourceAuthorization: o.CopySourceAuthorization,
		SourceContentMD5:        o.SourceContentMD5,
		SourceContentcrc64:      o.SourceContentCRC64,
		SourceRange:             getSourceRange(o.Offset, o.Count),
	}

	return options, o.CpkInfo, o.CpkScopeInfo, o.LeaseAccessConditions, o.SourceModifiedAccessConditions
}

// BlockBlobStageBlockFromURLResponse contains the response from method BlockBlobClient.StageBlockFromURL.
type BlockBlobStageBlockFromURLResponse struct {
	blockBlobClientStageBlockFromURLResponse
}

func toBlockBlobStageBlockFromURLResponse(resp blockBlobClientStageBlockFromURLResponse) BlockBlobStageBlockFromURLResponse {
	return BlockBlobStageBlockFromURLResponse{resp}
}

// ---------------------------------------------------------------------------------------------------------------------

// BlockBlobCommitBlockListOptions provides set of configurations for CommitBlockList operation
type BlockBlobCommitBlockListOptions struct {
	BlobTagsMap               map[string]string
	Metadata                  map[string]string
	RequestID                 *string
	Tier                      *AccessTier
	Timeout                   *int32
	TransactionalContentCRC64 []byte
	TransactionalContentMD5   []byte
	BlobHTTPHeaders           *BlobHTTPHeaders
	CpkInfo                   *CpkInfo
	CpkScopeInfo              *CpkScopeInfo
	BlobAccessConditions      *BlobAccessConditions
}

func (o *BlockBlobCommitBlockListOptions) format() (*blockBlobClientCommitBlockListOptions, *BlobHTTPHeaders, *LeaseAccessConditions, *CpkInfo, *CpkScopeInfo, *ModifiedAccessConditions) {
	if o == nil {
		return nil, nil, nil, nil, nil, nil
	}

	options := &blockBlobClientCommitBlockListOptions{
		BlobTagsString:            serializeBlobTagsToStrPtr(o.BlobTagsMap),
		Metadata:                  o.Metadata,
		RequestID:                 o.RequestID,
		Tier:                      o.Tier,
		Timeout:                   o.Timeout,
		TransactionalContentCRC64: o.TransactionalContentCRC64,
		TransactionalContentMD5:   o.TransactionalContentMD5,
	}
	leaseAccessConditions, modifiedAccessConditions := o.BlobAccessConditions.format()
	return options, o.BlobHTTPHeaders, leaseAccessConditions, o.CpkInfo, o.CpkScopeInfo, modifiedAccessConditions
}

// BlockBlobCommitBlockListResponse contains the response from method BlockBlobClient.CommitBlockList.
type BlockBlobCommitBlockListResponse struct {
	blockBlobClientCommitBlockListResponse
}

func toBlockBlobCommitBlockListResponse(resp blockBlobClientCommitBlockListResponse) BlockBlobCommitBlockListResponse {
	return BlockBlobCommitBlockListResponse{resp}
}

// ---------------------------------------------------------------------------------------------------------------------

// BlockBlobGetBlockListOptions provides set of configurations for GetBlockList operation
type BlockBlobGetBlockListOptions struct {
	Snapshot             *string
	BlobAccessConditions *BlobAccessConditions
}

func (o *BlockBlobGetBlockListOptions) format() (*blockBlobClientGetBlockListOptions, *LeaseAccessConditions, *ModifiedAccessConditions) {
	if o == nil {
		return nil, nil, nil
	}

	leaseAccessConditions, modifiedAccessConditions := o.BlobAccessConditions.format()
	return &blockBlobClientGetBlockListOptions{Snapshot: o.Snapshot}, leaseAccessConditions, modifiedAccessConditions
}

// BlockBlobGetBlockListResponse contains the response from method BlockBlobClient.GetBlockList.
type BlockBlobGetBlockListResponse struct {
	blockBlobClientGetBlockListResponse
}

func toBlockBlobGetBlockListResponse(resp blockBlobClientGetBlockListResponse) BlockBlobGetBlockListResponse {
	return BlockBlobGetBlockListResponse{resp}
}

// ---------------------------------------------------------------------------------------------------------------------

// BlockBlobCopyFromURLOptions provides set of configurations for CopyBlockBlobFromURL operation
type BlockBlobCopyFromURLOptions struct {
	// Optional. Used to set blob tags in various blob operations.
	BlobTagsMap map[string]string
	// Only Bearer type is supported. Credentials should be a valid OAuth access token to copy source.
	CopySourceAuthorization *string
	// Specifies the date time when the blobs immutability policy is set to expire.
	ImmutabilityPolicyExpiry *time.Time
	// Specifies the immutability policy mode to set on the blob.
	ImmutabilityPolicyMode *BlobImmutabilityPolicyMode
	// Specified if a legal hold should be set on the blob.
	LegalHold *bool
	// Optional. Specifies a user-defined name-value pair associated with the blob. If no name-value pairs are specified, the
	// operation will copy the metadata from the source blob or file to the destination
	// blob. If one or more name-value pairs are specified, the destination blob is created with the specified metadata, and metadata
	// is not copied from the source blob or file. Note that beginning with
	// version 2009-09-19, metadata names must adhere to the naming rules for C# identifiers. See Naming and Referencing Containers,
	// Blobs, and Metadata for more information.
	Metadata map[string]string
	// Specify the md5 calculated for the range of bytes that must be read from the copy source.
	SourceContentMD5 []byte
	// Optional. Indicates the tier to be set on the blob.
	Tier *AccessTier

	SourceModifiedAccessConditions *SourceModifiedAccessConditions

	BlobAccessConditions *BlobAccessConditions
}

func (o *BlockBlobCopyFromURLOptions) format() (*blobClientCopyFromURLOptions, *SourceModifiedAccessConditions, *ModifiedAccessConditions, *LeaseAccessConditions) {
	if o == nil {
		return nil, nil, nil, nil
	}

	options := &blobClientCopyFromURLOptions{
		BlobTagsString:           serializeBlobTagsToStrPtr(o.BlobTagsMap),
		CopySourceAuthorization:  o.CopySourceAuthorization,
		ImmutabilityPolicyExpiry: o.ImmutabilityPolicyExpiry,
		ImmutabilityPolicyMode:   o.ImmutabilityPolicyMode,
		LegalHold:                o.LegalHold,
		Metadata:                 o.Metadata,
		SourceContentMD5:         o.SourceContentMD5,
		Tier:                     o.Tier,
	}

	leaseAccessConditions, modifiedAccessConditions := o.BlobAccessConditions.format()
	return options, o.SourceModifiedAccessConditions, modifiedAccessConditions, leaseAccessConditions
}

// BlockBlobCopyFromURLResponse contains the response from method BlockBlobClient.CopyFromURL.
type BlockBlobCopyFromURLResponse struct {
	blobClientCopyFromURLResponse
}

func toBlockBlobCopyFromURLResponse(resp blobClientCopyFromURLResponse) BlockBlobCopyFromURLResponse {
	return BlockBlobCopyFromURLResponse{resp}
}

// ---------------------------------------------------------------------------------------------------------------------
