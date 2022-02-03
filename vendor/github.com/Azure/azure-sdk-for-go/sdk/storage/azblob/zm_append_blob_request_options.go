// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License.

package azblob

type CreateAppendBlobOptions struct {
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
	// Provides a client-generated, opaque value with a 1 KB character limit that is recorded in the analytics logs when storage analytics logging is enabled.
	RequestID *string

	Timeout *int32
}

func (o *CreateAppendBlobOptions) pointers() (*AppendBlobCreateOptions, *BlobHTTPHeaders, *LeaseAccessConditions, *CpkInfo, *CpkScopeInfo, *ModifiedAccessConditions) {
	if o == nil {
		return nil, nil, nil, nil, nil, nil
	}

	options := AppendBlobCreateOptions{
		BlobTagsString: serializeBlobTagsToStrPtr(o.TagsMap),
		Metadata:       o.Metadata,
		RequestID:      o.RequestID,
		Timeout:        o.Timeout,
	}

	leaseAccessConditions, modifiedAccessConditions := o.BlobAccessConditions.pointers()
	return &options, o.HTTPHeaders, leaseAccessConditions, o.CpkInfo, o.CpkScopeInfo, modifiedAccessConditions
}

type AppendBlockOptions struct {
	// Specify the transactional crc64 for the body, to be validated by the service.
	TransactionalContentCRC64 []byte
	// Specify the transactional md5 for the body, to be validated by the service.
	TransactionalContentMD5 []byte

	AppendPositionAccessConditions *AppendPositionAccessConditions
	CpkInfo                        *CpkInfo
	CpkScopeInfo                   *CpkScopeInfo
	BlobAccessConditions           *BlobAccessConditions
}

func (o *AppendBlockOptions) pointers() (*AppendBlobAppendBlockOptions, *AppendPositionAccessConditions, *CpkInfo, *CpkScopeInfo, *ModifiedAccessConditions, *LeaseAccessConditions) {
	if o == nil {
		return nil, nil, nil, nil, nil, nil
	}

	options := &AppendBlobAppendBlockOptions{
		TransactionalContentCRC64: o.TransactionalContentCRC64,
		TransactionalContentMD5:   o.TransactionalContentMD5,
	}
	leaseAccessConditions, modifiedAccessConditions := o.BlobAccessConditions.pointers()
	return options, o.AppendPositionAccessConditions, o.CpkInfo, o.CpkScopeInfo, modifiedAccessConditions, leaseAccessConditions
}

type AppendBlockURLOptions struct {
	// Specify the md5 calculated for the range of bytes that must be read from the copy source.
	SourceContentMD5 []byte
	// Specify the crc64 calculated for the range of bytes that must be read from the copy source.
	SourceContentCRC64 []byte
	// Specify the transactional md5 for the body, to be validated by the service.
	TransactionalContentMD5 []byte

	AppendPositionAccessConditions *AppendPositionAccessConditions
	CpkInfo                        *CpkInfo
	CpkScopeInfo                   *CpkScopeInfo
	SourceModifiedAccessConditions *SourceModifiedAccessConditions
	BlobAccessConditions           *BlobAccessConditions
	// Optional, you can specify whether a particular range of the blob is read
	Offset *int64
	Count  *int64
}

func (o *AppendBlockURLOptions) pointers() (*AppendBlobAppendBlockFromURLOptions, *AppendPositionAccessConditions, *CpkInfo, *CpkScopeInfo, *ModifiedAccessConditions, *LeaseAccessConditions, *SourceModifiedAccessConditions) {
	if o == nil {
		return nil, nil, nil, nil, nil, nil, nil
	}

	options := &AppendBlobAppendBlockFromURLOptions{
		SourceRange:             getSourceRange(o.Offset, o.Count),
		SourceContentMD5:        o.SourceContentMD5,
		SourceContentcrc64:      o.SourceContentCRC64,
		TransactionalContentMD5: o.TransactionalContentMD5,
	}

	leaseAccessConditions, modifiedAccessConditions := o.BlobAccessConditions.pointers()
	return options, o.AppendPositionAccessConditions, o.CpkInfo, o.CpkScopeInfo, modifiedAccessConditions,
		leaseAccessConditions, o.SourceModifiedAccessConditions
}

type SealAppendBlobOptions struct {
	BlobAccessConditions           *BlobAccessConditions
	AppendPositionAccessConditions *AppendPositionAccessConditions
}

func (o *SealAppendBlobOptions) pointers() (leaseAccessConditions *LeaseAccessConditions,
	modifiedAccessConditions *ModifiedAccessConditions, appendPositionAccessConditions *AppendPositionAccessConditions) {
	if o == nil {
		return nil, nil, nil
	}

	return
}
