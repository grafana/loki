//go:build go1.18
// +build go1.18

// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License.

package azblob

import "time"

// ---------------------------------------------------------------------------------------------------------------------

// AppendBlobCreateOptions provides set of configurations for Create Append Blob operation
type AppendBlobCreateOptions struct {
	// Specifies the date time when the blobs immutability policy is set to expire.
	ImmutabilityPolicyExpiry *time.Time
	// Specifies the immutability policy mode to set on the blob.
	ImmutabilityPolicyMode *BlobImmutabilityPolicyMode
	// Specified if a legal hold should be set on the blob.
	LegalHold *bool

	BlobAccessConditions *BlobAccessConditions

	HTTPHeaders *BlobHTTPHeaders

	CpkInfo *CpkInfo

	CpkScopeInfo *CpkScopeInfo
	// Optional. Used to set blob tags in various blob operations.
	TagsMap map[string]string
	// Optional. Specifies a user-defined name-value pair associated with the blob. If no name-value pairs are specified, the
	// operation will copy the metadata from the source blob or file to the destination blob. If one or more name-value pairs
	// are specified, the destination blob is created with the specified metadata, and metadata is not copied from the source
	// blob or file. Note that beginning with version 2009-09-19, metadata names must adhere to the naming rules for C# identifiers.
	// See Naming and Referencing Containers, Blobs, and Metadata for more information.
	Metadata map[string]string
}

func (o *AppendBlobCreateOptions) format() (*appendBlobClientCreateOptions, *BlobHTTPHeaders, *LeaseAccessConditions,
	*CpkInfo, *CpkScopeInfo, *ModifiedAccessConditions) {

	if o == nil {
		return nil, nil, nil, nil, nil, nil
	}

	options := appendBlobClientCreateOptions{
		BlobTagsString:           serializeBlobTagsToStrPtr(o.TagsMap),
		Metadata:                 o.Metadata,
		ImmutabilityPolicyExpiry: o.ImmutabilityPolicyExpiry,
		ImmutabilityPolicyMode:   o.ImmutabilityPolicyMode,
		LegalHold:                o.LegalHold,
	}

	leaseAccessConditions, modifiedAccessConditions := o.BlobAccessConditions.format()
	return &options, o.HTTPHeaders, leaseAccessConditions, o.CpkInfo, o.CpkScopeInfo, modifiedAccessConditions
}

// AppendBlobCreateResponse contains the response from method AppendBlobClient.Create.
type AppendBlobCreateResponse struct {
	appendBlobClientCreateResponse
}

func toAppendBlobCreateResponse(resp appendBlobClientCreateResponse) AppendBlobCreateResponse {
	return AppendBlobCreateResponse{resp}
}

// ---------------------------------------------------------------------------------------------------------------------

// AppendBlobAppendBlockOptions provides set of configurations for AppendBlock operation
type AppendBlobAppendBlockOptions struct {
	// Specify the transactional crc64 for the body, to be validated by the service.
	TransactionalContentCRC64 []byte
	// Specify the transactional md5 for the body, to be validated by the service.
	TransactionalContentMD5 []byte

	AppendPositionAccessConditions *AppendPositionAccessConditions

	CpkInfo *CpkInfo

	CpkScopeInfo *CpkScopeInfo

	BlobAccessConditions *BlobAccessConditions
}

func (o *AppendBlobAppendBlockOptions) format() (*appendBlobClientAppendBlockOptions, *AppendPositionAccessConditions, *CpkInfo, *CpkScopeInfo, *ModifiedAccessConditions, *LeaseAccessConditions) {
	if o == nil {
		return nil, nil, nil, nil, nil, nil
	}

	options := &appendBlobClientAppendBlockOptions{
		TransactionalContentCRC64: o.TransactionalContentCRC64,
		TransactionalContentMD5:   o.TransactionalContentMD5,
	}
	leaseAccessConditions, modifiedAccessConditions := o.BlobAccessConditions.format()
	return options, o.AppendPositionAccessConditions, o.CpkInfo, o.CpkScopeInfo, modifiedAccessConditions, leaseAccessConditions
}

// AppendBlobAppendBlockResponse contains the response from method AppendBlobClient.AppendBlock.
type AppendBlobAppendBlockResponse struct {
	appendBlobClientAppendBlockResponse
}

func toAppendBlobAppendBlockResponse(resp appendBlobClientAppendBlockResponse) AppendBlobAppendBlockResponse {
	return AppendBlobAppendBlockResponse{resp}
}

// ---------------------------------------------------------------------------------------------------------------------

// AppendBlobAppendBlockFromURLOptions provides set of configurations for AppendBlockFromURL operation
type AppendBlobAppendBlockFromURLOptions struct {
	// Specify the md5 calculated for the range of bytes that must be read from the copy source.
	SourceContentMD5 []byte
	// Specify the crc64 calculated for the range of bytes that must be read from the copy source.
	SourceContentCRC64 []byte
	// Specify the transactional md5 for the body, to be validated by the service.
	TransactionalContentMD5 []byte

	AppendPositionAccessConditions *AppendPositionAccessConditions

	CpkInfo *CpkInfo

	CpkScopeInfo *CpkScopeInfo

	SourceModifiedAccessConditions *SourceModifiedAccessConditions

	BlobAccessConditions *BlobAccessConditions
	// Optional, you can specify whether a particular range of the blob is read
	Offset *int64

	Count *int64
}

func (o *AppendBlobAppendBlockFromURLOptions) format() (*appendBlobClientAppendBlockFromURLOptions, *CpkInfo, *CpkScopeInfo, *LeaseAccessConditions, *AppendPositionAccessConditions, *ModifiedAccessConditions, *SourceModifiedAccessConditions) {
	if o == nil {
		return nil, nil, nil, nil, nil, nil, nil
	}

	options := &appendBlobClientAppendBlockFromURLOptions{
		SourceRange:             getSourceRange(o.Offset, o.Count),
		SourceContentMD5:        o.SourceContentMD5,
		SourceContentcrc64:      o.SourceContentCRC64,
		TransactionalContentMD5: o.TransactionalContentMD5,
	}

	leaseAccessConditions, modifiedAccessConditions := o.BlobAccessConditions.format()
	return options, o.CpkInfo, o.CpkScopeInfo, leaseAccessConditions, o.AppendPositionAccessConditions, modifiedAccessConditions, o.SourceModifiedAccessConditions
}

// AppendBlobAppendBlockFromURLResponse contains the response from method AppendBlobClient.AppendBlockFromURL.
type AppendBlobAppendBlockFromURLResponse struct {
	appendBlobClientAppendBlockFromURLResponse
}

func toAppendBlobAppendBlockFromURLResponse(resp appendBlobClientAppendBlockFromURLResponse) AppendBlobAppendBlockFromURLResponse {
	return AppendBlobAppendBlockFromURLResponse{resp}
}

// ---------------------------------------------------------------------------------------------------------------------

// AppendBlobSealOptions provides set of configurations for SealAppendBlob operation
type AppendBlobSealOptions struct {
	BlobAccessConditions           *BlobAccessConditions
	AppendPositionAccessConditions *AppendPositionAccessConditions
}

func (o *AppendBlobSealOptions) format() (leaseAccessConditions *LeaseAccessConditions,
	modifiedAccessConditions *ModifiedAccessConditions, appendPositionAccessConditions *AppendPositionAccessConditions) {
	if o == nil {
		return nil, nil, nil
	}

	return
}

// AppendBlobSealResponse contains the response from method AppendBlobClient.Seal.
type AppendBlobSealResponse struct {
	appendBlobClientSealResponse
}

func toAppendBlobSealResponse(resp appendBlobClientSealResponse) AppendBlobSealResponse {
	return AppendBlobSealResponse{resp}
}

// ---------------------------------------------------------------------------------------------------------------------
