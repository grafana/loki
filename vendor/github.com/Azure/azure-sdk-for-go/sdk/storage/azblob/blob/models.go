//go:build go1.18
// +build go1.18

// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License. See License.txt in the project root for license information.

package blob

import (
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/internal/exported"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/internal/generated"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/internal/shared"
)

// SharedKeyCredential contains an account's name and its primary or secondary key.
type SharedKeyCredential = exported.SharedKeyCredential

// NewSharedKeyCredential creates an immutable SharedKeyCredential containing the
// storage account's name and either its primary or secondary key.
func NewSharedKeyCredential(accountName, accountKey string) (*SharedKeyCredential, error) {
	return exported.NewSharedKeyCredential(accountName, accountKey)
}

// Type Declarations ---------------------------------------------------------------------

// AccessConditions identifies blob-specific access conditions which you optionally set.
type AccessConditions = exported.BlobAccessConditions

// LeaseAccessConditions contains optional parameters to access leased entity.
type LeaseAccessConditions = exported.LeaseAccessConditions

// ModifiedAccessConditions contains a group of parameters for specifying access conditions.
type ModifiedAccessConditions = exported.ModifiedAccessConditions

// CpkInfo contains a group of parameters for client provided encryption key.
type CpkInfo = generated.CpkInfo

// CpkScopeInfo contains a group of parameters for client provided encryption scope.
type CpkScopeInfo = generated.CpkScopeInfo

// HTTPHeaders contains a group of parameters for the BlobClient.SetHTTPHeaders method.
type HTTPHeaders = generated.BlobHTTPHeaders

// SourceModifiedAccessConditions contains a group of parameters for the BlobClient.StartCopyFromURL method.
type SourceModifiedAccessConditions = generated.SourceModifiedAccessConditions

// Tags represent map of blob index tags
type Tags = generated.BlobTag

// HTTPRange defines a range of bytes within an HTTP resource, starting at offset and
// ending at offset+count. A zero-value HTTPRange indicates the entire resource. An HTTPRange
// which has an offset but no zero value count indicates from the offset to the resource's end.
type HTTPRange = exported.HTTPRange

// Request Model Declaration -------------------------------------------------------------------------------------------

// DownloadStreamOptions contains the optional parameters for the Client.Download method.
type DownloadStreamOptions struct {
	// When set to true and specified together with the Range, the service returns the MD5 hash for the range, as long as the
	// range is less than or equal to 4 MB in size.
	RangeGetContentMD5 *bool

	// Range specifies a range of bytes.  The default value is all bytes.
	Range HTTPRange

	AccessConditions *AccessConditions
	CpkInfo          *CpkInfo
	CpkScopeInfo     *CpkScopeInfo
}

func (o *DownloadStreamOptions) format() (*generated.BlobClientDownloadOptions, *generated.LeaseAccessConditions, *generated.CpkInfo, *generated.ModifiedAccessConditions) {
	if o == nil {
		return nil, nil, nil, nil
	}

	basics := generated.BlobClientDownloadOptions{
		RangeGetContentMD5: o.RangeGetContentMD5,
		Range:              exported.FormatHTTPRange(o.Range),
	}

	leaseAccessConditions, modifiedAccessConditions := exported.FormatBlobAccessConditions(o.AccessConditions)
	return &basics, leaseAccessConditions, o.CpkInfo, modifiedAccessConditions
}

// ---------------------------------------------------------------------------------------------------------------------

// downloadOptions contains common options used by the DownloadBuffer and DownloadFile functions.
type downloadOptions struct {
	// Range specifies a range of bytes.  The default value is all bytes.
	Range HTTPRange

	// BlockSize specifies the block size to use for each parallel download; the default size is DefaultDownloadBlockSize.
	BlockSize int64

	// Progress is a function that is invoked periodically as bytes are received.
	Progress func(bytesTransferred int64)

	// BlobAccessConditions indicates the access conditions used when making HTTP GET requests against the blob.
	AccessConditions *AccessConditions

	// ClientProvidedKeyOptions indicates the client provided key by name and/or by value to encrypt/decrypt data.
	CpkInfo      *CpkInfo
	CpkScopeInfo *CpkScopeInfo

	// Concurrency indicates the maximum number of blocks to download in parallel (0=default)
	Concurrency uint16

	// RetryReaderOptionsPerBlock is used when downloading each block.
	RetryReaderOptionsPerBlock RetryReaderOptions
}

func (o *downloadOptions) getBlobPropertiesOptions() *GetPropertiesOptions {
	if o == nil {
		return nil
	}
	return &GetPropertiesOptions{
		AccessConditions: o.AccessConditions,
		CpkInfo:          o.CpkInfo,
	}
}

func (o *downloadOptions) getDownloadBlobOptions(rnge HTTPRange, rangeGetContentMD5 *bool) *DownloadStreamOptions {
	if o == nil {
		return nil
	}
	return &DownloadStreamOptions{
		AccessConditions:   o.AccessConditions,
		CpkInfo:            o.CpkInfo,
		CpkScopeInfo:       o.CpkScopeInfo,
		Range:              rnge,
		RangeGetContentMD5: rangeGetContentMD5,
	}
}

// DownloadBufferOptions contains the optional parameters for the DownloadBuffer method.
type DownloadBufferOptions struct {
	// Range specifies a range of bytes.  The default value is all bytes.
	Range HTTPRange

	// BlockSize specifies the block size to use for each parallel download; the default size is DefaultDownloadBlockSize.
	BlockSize int64

	// Progress is a function that is invoked periodically as bytes are received.
	Progress func(bytesTransferred int64)

	// BlobAccessConditions indicates the access conditions used when making HTTP GET requests against the blob.
	AccessConditions *AccessConditions

	// CpkInfo contains a group of parameters for client provided encryption key.
	CpkInfo *CpkInfo

	// CpkScopeInfo contains a group of parameters for client provided encryption scope.
	CpkScopeInfo *CpkScopeInfo

	// Concurrency indicates the maximum number of blocks to download in parallel (0=default)
	Concurrency uint16

	// RetryReaderOptionsPerBlock is used when downloading each block.
	RetryReaderOptionsPerBlock RetryReaderOptions
}

// DownloadFileOptions contains the optional parameters for the DownloadFile method.
type DownloadFileOptions struct {
	// Range specifies a range of bytes.  The default value is all bytes.
	Range HTTPRange

	// BlockSize specifies the block size to use for each parallel download; the default size is DefaultDownloadBlockSize.
	BlockSize int64

	// Progress is a function that is invoked periodically as bytes are received.
	Progress func(bytesTransferred int64)

	// BlobAccessConditions indicates the access conditions used when making HTTP GET requests against the blob.
	AccessConditions *AccessConditions

	// ClientProvidedKeyOptions indicates the client provided key by name and/or by value to encrypt/decrypt data.
	CpkInfo      *CpkInfo
	CpkScopeInfo *CpkScopeInfo

	// Concurrency indicates the maximum number of blocks to download in parallel.  The default value is 5.
	Concurrency uint16

	// RetryReaderOptionsPerBlock is used when downloading each block.
	RetryReaderOptionsPerBlock RetryReaderOptions
}

// ---------------------------------------------------------------------------------------------------------------------

// DeleteOptions contains the optional parameters for the Client.Delete method.
type DeleteOptions struct {
	// Required if the blob has associated snapshots. Specify one of the following two options: include: Delete the base blob
	// and all of its snapshots. only: Delete only the blob's snapshots and not the blob itself
	DeleteSnapshots  *DeleteSnapshotsOptionType
	AccessConditions *AccessConditions
}

func (o *DeleteOptions) format() (*generated.BlobClientDeleteOptions, *generated.LeaseAccessConditions, *generated.ModifiedAccessConditions) {
	if o == nil {
		return nil, nil, nil
	}

	basics := generated.BlobClientDeleteOptions{
		DeleteSnapshots: o.DeleteSnapshots,
	}

	if o.AccessConditions == nil {
		return &basics, nil, nil
	}

	return &basics, o.AccessConditions.LeaseAccessConditions, o.AccessConditions.ModifiedAccessConditions
}

// ---------------------------------------------------------------------------------------------------------------------

// UndeleteOptions contains the optional parameters for the Client.Undelete method.
type UndeleteOptions struct {
	// placeholder for future options
}

func (o *UndeleteOptions) format() *generated.BlobClientUndeleteOptions {
	return nil
}

// ---------------------------------------------------------------------------------------------------------------------

// SetTierOptions contains the optional parameters for the Client.SetTier method.
type SetTierOptions struct {
	// Optional: Indicates the priority with which to rehydrate an archived blob.
	RehydratePriority *RehydratePriority

	AccessConditions *AccessConditions
}

func (o *SetTierOptions) format() (*generated.BlobClientSetTierOptions, *generated.LeaseAccessConditions, *generated.ModifiedAccessConditions) {
	if o == nil {
		return nil, nil, nil
	}

	leaseAccessConditions, modifiedAccessConditions := exported.FormatBlobAccessConditions(o.AccessConditions)
	return &generated.BlobClientSetTierOptions{RehydratePriority: o.RehydratePriority}, leaseAccessConditions, modifiedAccessConditions
}

// ---------------------------------------------------------------------------------------------------------------------

// GetPropertiesOptions contains the optional parameters for the Client.GetProperties method
type GetPropertiesOptions struct {
	AccessConditions *AccessConditions
	CpkInfo          *CpkInfo
}

func (o *GetPropertiesOptions) format() (*generated.BlobClientGetPropertiesOptions,
	*generated.LeaseAccessConditions, *generated.CpkInfo, *generated.ModifiedAccessConditions) {
	if o == nil {
		return nil, nil, nil, nil
	}

	leaseAccessConditions, modifiedAccessConditions := exported.FormatBlobAccessConditions(o.AccessConditions)
	return nil, leaseAccessConditions, o.CpkInfo, modifiedAccessConditions
}

// ---------------------------------------------------------------------------------------------------------------------

// SetHTTPHeadersOptions contains the optional parameters for the Client.SetHTTPHeaders method.
type SetHTTPHeadersOptions struct {
	AccessConditions *AccessConditions
}

func (o *SetHTTPHeadersOptions) format() (*generated.BlobClientSetHTTPHeadersOptions, *generated.LeaseAccessConditions, *generated.ModifiedAccessConditions) {
	if o == nil {
		return nil, nil, nil
	}

	leaseAccessConditions, modifiedAccessConditions := exported.FormatBlobAccessConditions(o.AccessConditions)
	return nil, leaseAccessConditions, modifiedAccessConditions
}

// ---------------------------------------------------------------------------------------------------------------------

// SetMetadataOptions provides set of configurations for Set Metadata on blob operation
type SetMetadataOptions struct {
	AccessConditions *AccessConditions
	CpkInfo          *CpkInfo
	CpkScopeInfo     *CpkScopeInfo
}

func (o *SetMetadataOptions) format() (*generated.LeaseAccessConditions, *CpkInfo,
	*CpkScopeInfo, *ModifiedAccessConditions) {
	if o == nil {
		return nil, nil, nil, nil
	}

	leaseAccessConditions, modifiedAccessConditions := exported.FormatBlobAccessConditions(o.AccessConditions)
	return leaseAccessConditions, o.CpkInfo, o.CpkScopeInfo, modifiedAccessConditions
}

// ---------------------------------------------------------------------------------------------------------------------

// CreateSnapshotOptions contains the optional parameters for the Client.CreateSnapshot method.
type CreateSnapshotOptions struct {
	Metadata         map[string]string
	AccessConditions *AccessConditions
	CpkInfo          *CpkInfo
	CpkScopeInfo     *CpkScopeInfo
}

func (o *CreateSnapshotOptions) format() (*generated.BlobClientCreateSnapshotOptions, *generated.CpkInfo,
	*generated.CpkScopeInfo, *generated.ModifiedAccessConditions, *generated.LeaseAccessConditions) {
	if o == nil {
		return nil, nil, nil, nil, nil
	}

	leaseAccessConditions, modifiedAccessConditions := exported.FormatBlobAccessConditions(o.AccessConditions)

	return &generated.BlobClientCreateSnapshotOptions{
		Metadata: o.Metadata,
	}, o.CpkInfo, o.CpkScopeInfo, modifiedAccessConditions, leaseAccessConditions
}

// ---------------------------------------------------------------------------------------------------------------------

// StartCopyFromURLOptions contains the optional parameters for the Client.StartCopyFromURL method.
type StartCopyFromURLOptions struct {
	// Specifies the date time when the blobs immutability policy is set to expire.
	ImmutabilityPolicyExpiry *time.Time
	// Specifies the immutability policy mode to set on the blob.
	ImmutabilityPolicyMode *ImmutabilityPolicySetting
	// Specified if a legal hold should be set on the blob.
	LegalHold *bool
	// Optional. Used to set blob tags in various blob operations.
	BlobTags map[string]string
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

	AccessConditions *AccessConditions
}

func (o *StartCopyFromURLOptions) format() (*generated.BlobClientStartCopyFromURLOptions,
	*generated.SourceModifiedAccessConditions, *generated.ModifiedAccessConditions, *generated.LeaseAccessConditions) {
	if o == nil {
		return nil, nil, nil, nil
	}

	basics := generated.BlobClientStartCopyFromURLOptions{
		BlobTagsString:           shared.SerializeBlobTagsToStrPtr(o.BlobTags),
		Metadata:                 o.Metadata,
		RehydratePriority:        o.RehydratePriority,
		SealBlob:                 o.SealBlob,
		Tier:                     o.Tier,
		ImmutabilityPolicyExpiry: o.ImmutabilityPolicyExpiry,
		ImmutabilityPolicyMode:   o.ImmutabilityPolicyMode,
		LegalHold:                o.LegalHold,
	}

	leaseAccessConditions, modifiedAccessConditions := exported.FormatBlobAccessConditions(o.AccessConditions)
	return &basics, o.SourceModifiedAccessConditions, modifiedAccessConditions, leaseAccessConditions
}

// ---------------------------------------------------------------------------------------------------------------------

// AbortCopyFromURLOptions contains the optional parameters for the Client.AbortCopyFromURL method.
type AbortCopyFromURLOptions struct {
	LeaseAccessConditions *LeaseAccessConditions
}

func (o *AbortCopyFromURLOptions) format() (*generated.BlobClientAbortCopyFromURLOptions, *generated.LeaseAccessConditions) {
	if o == nil {
		return nil, nil
	}
	return nil, o.LeaseAccessConditions
}

// ---------------------------------------------------------------------------------------------------------------------

// SetTagsOptions contains the optional parameters for the Client.SetTags method.
type SetTagsOptions struct {
	// The version id parameter is an opaque DateTime value that, when present,
	// specifies the version of the blob to operate on. It's for service version 2019-10-10 and newer.
	VersionID *string
	// Optional header, Specifies the transactional crc64 for the body, to be validated by the service.
	TransactionalContentCRC64 []byte
	// Optional header, Specifies the transactional md5 for the body, to be validated by the service.
	TransactionalContentMD5 []byte

	AccessConditions *AccessConditions
}

func (o *SetTagsOptions) format() (*generated.BlobClientSetTagsOptions, *ModifiedAccessConditions, *generated.LeaseAccessConditions) {
	if o == nil {
		return nil, nil, nil
	}

	options := &generated.BlobClientSetTagsOptions{
		TransactionalContentMD5:   o.TransactionalContentMD5,
		TransactionalContentCRC64: o.TransactionalContentCRC64,
		VersionID:                 o.VersionID,
	}

	leaseAccessConditions, modifiedAccessConditions := exported.FormatBlobAccessConditions(o.AccessConditions)
	return options, modifiedAccessConditions, leaseAccessConditions
}

// ---------------------------------------------------------------------------------------------------------------------

// GetTagsOptions contains the optional parameters for the Client.GetTags method.
type GetTagsOptions struct {
	// The snapshot parameter is an opaque DateTime value that, when present, specifies the blob snapshot to retrieve.
	Snapshot *string
	// The version id parameter is an opaque DateTime value that, when present, specifies the version of the blob to operate on.
	// It's for service version 2019-10-10 and newer.
	VersionID *string

	BlobAccessConditions *AccessConditions
}

func (o *GetTagsOptions) format() (*generated.BlobClientGetTagsOptions, *generated.ModifiedAccessConditions, *generated.LeaseAccessConditions) {
	if o == nil {
		return nil, nil, nil
	}

	options := &generated.BlobClientGetTagsOptions{
		Snapshot:  o.Snapshot,
		VersionID: o.VersionID,
	}

	leaseAccessConditions, modifiedAccessConditions := exported.FormatBlobAccessConditions(o.BlobAccessConditions)
	return options, modifiedAccessConditions, leaseAccessConditions
}

// ---------------------------------------------------------------------------------------------------------------------

// CopyFromURLOptions contains the optional parameters for the Client.CopyFromURL method.
type CopyFromURLOptions struct {
	// Optional. Used to set blob tags in various blob operations.
	BlobTags map[string]string
	// Only Bearer type is supported. Credentials should be a valid OAuth access token to copy source.
	CopySourceAuthorization *string
	// Specifies the date time when the blobs immutability policy is set to expire.
	ImmutabilityPolicyExpiry *time.Time
	// Specifies the immutability policy mode to set on the blob.
	ImmutabilityPolicyMode *ImmutabilityPolicySetting
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

	BlobAccessConditions *AccessConditions
}

func (o *CopyFromURLOptions) format() (*generated.BlobClientCopyFromURLOptions, *generated.SourceModifiedAccessConditions, *generated.ModifiedAccessConditions, *generated.LeaseAccessConditions) {
	if o == nil {
		return nil, nil, nil, nil
	}

	options := &generated.BlobClientCopyFromURLOptions{
		BlobTagsString:           shared.SerializeBlobTagsToStrPtr(o.BlobTags),
		CopySourceAuthorization:  o.CopySourceAuthorization,
		ImmutabilityPolicyExpiry: o.ImmutabilityPolicyExpiry,
		ImmutabilityPolicyMode:   o.ImmutabilityPolicyMode,
		LegalHold:                o.LegalHold,
		Metadata:                 o.Metadata,
		SourceContentMD5:         o.SourceContentMD5,
		Tier:                     o.Tier,
	}

	leaseAccessConditions, modifiedAccessConditions := exported.FormatBlobAccessConditions(o.BlobAccessConditions)
	return options, o.SourceModifiedAccessConditions, modifiedAccessConditions, leaseAccessConditions
}
