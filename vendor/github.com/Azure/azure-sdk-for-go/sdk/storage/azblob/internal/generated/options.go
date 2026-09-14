// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License. See License.txt in the project root for license information.

package generated

import (
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
)

// AppendPositionAccessConditions contains a group of parameters for the AppendBlobClient.AppendBlock method.
type AppendPositionAccessConditions struct {
	// Optional conditional header, used only for the Append Block operation. A number indicating the byte offset to compare.
	// Append Block will succeed only if the append position is equal to this number. If
	// it is not, the request will fail with the AppendPositionConditionNotMet error (HTTP status code 412 - Precondition Failed).
	AppendPosition *int64

	// Optional conditional header. The max length in bytes permitted for the append blob. If the Append Block operation would
	// cause the blob to exceed that limit or if the blob size is already greater than
	// the value specified in this header, the request will fail with MaxBlobSizeConditionNotMet error (HTTP status code 412 -
	// Precondition Failed).
	MaxSize *int64
}

// BlobHTTPHeaders contains a group of parameters for the BlobClient.SetHTTPHeaders method.
type BlobHTTPHeaders struct {
	// Optional. Sets the blob's cache control. If specified, this property is stored with the blob and returned with a read request.
	BlobCacheControl *string

	// Optional. Sets the blob's Content-Disposition header.
	BlobContentDisposition *string

	// Optional. Sets the blob's content encoding. If specified, this property is stored with the blob and returned with a read
	// request.
	BlobContentEncoding *string

	// Optional. Set the blob's content language. If specified, this property is stored with the blob and returned with a read
	// request.
	BlobContentLanguage *string

	// Optional. An MD5 hash of the blob content. Note that this hash is not validated, as the hashes for the individual blocks
	// were validated when each was uploaded.
	BlobContentMD5 []byte

	// Optional. Sets the blob's content type. If specified, this property is stored with the blob and returned with a read request.
	BlobContentType *string
}

// BlobModifiedAccessConditions contains a group of parameters for the BlobClient.GetTags method.
type BlobModifiedAccessConditions struct {
	// Specify an ETag value to operate only on blobs with a matching value.
	IfMatch *azcore.ETag

	// Specify this header value to operate only on a blob if it has been modified since the specified date/time.
	IfModifiedSince *time.Time

	// Specify an ETag value to operate only on blobs without a matching value.
	IfNoneMatch *azcore.ETag

	// Specify this header value to operate only on a blob if it has not been modified since the specified date/time.
	IfUnmodifiedSince *time.Time
}

// ContainerCPKScopeInfo contains a group of parameters for the ContainerClient.Create method.
type ContainerCPKScopeInfo struct {
	// Optional. Version 2019-07-07 and later. Specifies the default encryption scope to set on the container and use for all
	// future writes.
	DefaultEncryptionScope *string

	// Optional. Version 2019-07-07 and newer. If true, prevents any request from specifying a different encryption scope than
	// the scope set on the container.
	PreventEncryptionScopeOverride *bool
}

// CPKInfo contains a group of parameters for the BlobClient.Download method.
type CPKInfo struct {
	// The algorithm used to produce the encryption key hash. Currently, the only accepted value is "AES256". Must be provided
	// if the x-ms-encryption-key header is provided.
	EncryptionAlgorithm *EncryptionAlgorithmType

	// Optional. Specifies the encryption key to use to encrypt the data provided in the request. If not specified, encryption
	// is performed with the root account encryption key. For more information, see
	// Encryption at Rest for Azure Storage Services.
	EncryptionKey *string

	// The SHA-256 hash of the provided encryption key. Must be provided if the x-ms-encryption-key header is provided.
	EncryptionKeySHA256 *string
}

// CPKScopeInfo contains a group of parameters for the BlobClient.SetMetadata method.
type CPKScopeInfo struct {
	// Optional. Version 2019-07-07 and later. Specifies the name of the encryption scope to use to encrypt the data provided
	// in the request. If not specified, encryption is performed with the default
	// account encryption scope. For more information, see Encryption at Rest for Azure Storage Services.
	EncryptionScope *string
}

// LeaseAccessConditions contains a group of parameters for the ContainerClient.GetProperties method.
type LeaseAccessConditions struct {
	// If specified, the operation only succeeds if the resource's lease is active and matches this ID.
	LeaseID *string
}

// ModifiedAccessConditions contains a group of parameters for the ContainerClient.Delete method.
type ModifiedAccessConditions struct {
	// Specify an ETag value to operate only on blobs with a matching value.
	IfMatch *azcore.ETag

	// Specify this header value to operate only on a blob if it has been modified since the specified date/time.
	IfModifiedSince *time.Time

	// Specify an ETag value to operate only on blobs without a matching value.
	IfNoneMatch *azcore.ETag

	// Specify a SQL where clause on blob tags to operate only on blobs with a matching value.
	IfTags *string

	// Specify this header value to operate only on a blob if it has not been modified since the specified date/time.
	IfUnmodifiedSince *time.Time
}

// SequenceNumberAccessConditions contains a group of parameters for the PageBlobClient.UploadPages method.
type SequenceNumberAccessConditions struct {
	// Specify this header value to operate only on a blob if it has the specified sequence number.
	IfSequenceNumberEqualTo *int64

	// Specify this header value to operate only on a blob if it has a sequence number less than the specified.
	IfSequenceNumberLessThan *int64

	// Specify this header value to operate only on a blob if it has a sequence number less than or equal to the specified.
	IfSequenceNumberLessThanOrEqualTo *int64
}

// SourceCPKInfo contains a group of parameters for the PageBlobClient.UploadPagesFromURL method.
type SourceCPKInfo struct {
	// The algorithm used to produce the source encryption key hash. Currently, the only accepted value is "AES256". Must be provided
	// if the x-ms-source-encryption-key is provided.
	SourceEncryptionAlgorithm *EncryptionAlgorithmType

	// Optional. Specifies the source encryption key to use to encrypt the source data provided in the request.
	SourceEncryptionKey *string

	// The SHA-256 hash of the provided source encryption key. Must be provided if the x-ms-source-encryption-key header is provided.
	SourceEncryptionKeySHA256 *string
}

// SourceModifiedAccessConditions contains a group of parameters for the BlobClient.StartCopyFromURL method.
type SourceModifiedAccessConditions struct {
	// Specify an ETag value to operate only on blobs with a matching value.
	SourceIfMatch *azcore.ETag

	// Specify this header value to operate only on a blob if it has been modified since the specified date/time.
	SourceIfModifiedSince *time.Time

	// Specify an ETag value to operate only on blobs without a matching value.
	SourceIfNoneMatch *azcore.ETag

	// Specify a SQL where clause on blob tags to operate only on blobs with a matching value.
	SourceIfTags *string

	// Specify this header value to operate only on a blob if it has not been modified since the specified date/time.
	SourceIfUnmodifiedSince *time.Time
}
