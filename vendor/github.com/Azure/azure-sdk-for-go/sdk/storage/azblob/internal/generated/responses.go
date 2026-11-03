// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License. See License.txt in the project root for license information.

package generated

import (
	"io"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
)

// BlobClientDownloadResponse contains the response from method BlobClient.Download.
type BlobClientDownloadResponse struct {
	// Indicates that the service supports requests for partial blob content.
	AcceptRanges *string

	// The access tier of the blob.
	AccessTier *string

	// The time the tier was changed on the blob. This is only returned if the tier on the blob was ever set.
	AccessTierChangeTime *time.Time

	// The number of committed blocks present in the blob. This header is returned only for append blobs.
	BlobCommittedBlockCount *int32

	// If the blob has a MD5 hash, and if request contains range header (Range or x-ms-range), this response header is returned
	// with the value of the whole blob's MD5 value. This value may or may not be equal to the value returned in Content-MD5 header,
	// with the latter calculated from the requested range
	BlobContentMD5 []byte

	// The current sequence number for a page blob. This header is not returned for block blobs or append blobs.
	BlobSequenceNumber *int64

	// The type of the blob.
	BlobType *BlobType

	// Body contains the streaming response.
	Body io.ReadCloser

	// This header is returned if it was previously specified for the blob.
	CacheControl *string

	// An opaque, globally-unique, client-generated string identifier for the request.
	ClientRequestID *string

	// This response header is returned so that the client can check for the integrity of the copied content.
	ContentCRC64 []byte

	// This header returns the value that was specified for the 'x-ms-blob-content-disposition' header. The Content-Disposition
	// response header field conveys additional information about how to process the response payload, and also can be used to
	// attach additional metadata. For example, if set to attachment, it indicates that the user-agent should not display the
	// response, but instead show a Save As dialog with a filename other than the blob name specified.
	ContentDisposition *string

	// This header returns the value that was specified for the Content-Encoding request header
	ContentEncoding *string

	// This header returns the value that was specified for the Content-Language request header.
	ContentLanguage *string

	// The number of bytes present in the response body.
	ContentLength *int64

	// If the blob has an MD5 hash and this operation is to read the full blob, this response header is returned so that the client
	// can check for message content integrity.
	ContentMD5 []byte

	// Indicates the range of bytes returned in the event that the client requested a subset of the blob by setting the 'Range'
	// request header.
	ContentRange *string

	// The media type of the body of the response.
	ContentType *string

	// Conclusion time of the last attempted Copy Blob operation where this blob was the destination blob. This value can specify
	// the time of a completed, aborted, or failed copy attempt. This header does not appear if a copy is pending, if this blob
	// has never been the destination in a Copy Blob operation, or if this blob has been modified after a concluded Copy Blob
	// operation using Set Blob Properties, Put Blob, or Put Block List.
	CopyCompletionTime *time.Time

	// String identifier for this copy operation. Use with Get Blob Properties to check the status of this copy operation, or
	// pass to Abort Copy Blob to abort a pending copy.
	CopyID *string

	// Contains the number of bytes copied and the total bytes in the source in the last attempted Copy Blob operation where this
	// blob was the destination blob. Can show between 0 and Content-Length bytes copied. This header does not appear if this
	// blob has never been the destination in a Copy Blob operation, or if this blob has been modified after a concluded Copy
	// Blob operation using Set Blob Properties, Put Blob, or Put Block List
	CopyProgress *string

	// URL up to 2 KB in length that specifies the source blob or file used in the last attempted Copy Blob operation where this
	// blob was the destination blob. This header does not appear if this blob has never been the destination in a Copy Blob operation,
	// or if this blob has been modified after a concluded Copy Blob operation using Set Blob Properties, Put Blob, or Put Block
	// List.
	CopySource *string

	// State of the copy operation identified by x-ms-copy-id.
	CopyStatus *CopyStatusType

	// Only appears when x-ms-copy-status is failed or pending. Describes the cause of the last fatal or non-fatal copy operation
	// failure. This header does not appear if this blob has never been the destination in a Copy Blob operation, or if this blob
	// has been modified after a concluded Copy Blob operation using Set Blob Properties, Put Blob, or Put Block List
	CopyStatusDescription *string

	// Returns the date and time the blob was created.
	CreationTime *time.Time

	// UTC date/time value generated by the service that indicates the time at which the response was initiated
	Date *time.Time

	// ErrorCode contains the information returned from the x-ms-error-code header response.
	ErrorCode *string

	// The ETag contains a value that you can use to perform operations conditionally.
	ETag *azcore.ETag

	// The SHA-256 hash of the encryption key used to encrypt the blob. This header is only returned when the blob was encrypted
	// with a customer-provided key.
	EncryptionKeySHA256 *string

	// If the blob has a MD5 hash, and if request contains range header (Range or x-ms-range), this response header is returned
	// with the value of the whole blob's MD5 value. This value may or may not be equal to the value returned in Content-MD5 header,
	// with the latter calculated from the requested range
	EncryptionScope *string

	// UTC date/time value generated by the service that indicates the time at which the blob immutability policy will expire.
	ImmutabilityPolicyExpiresOn *time.Time

	// Indicates the immutability policy mode of the blob.
	ImmutabilityPolicyMode *ImmutabilityPolicyMode

	// The value of this header indicates whether version of this blob is a current version, see also x-ms-version-id header.
	IsCurrentVersion *bool

	// If this blob has been sealed
	IsSealed *bool

	// The value of this header is set to true if the contents of the request are successfully encrypted using the specified algorithm,
	// and false otherwise.
	IsServerEncrypted *bool

	// UTC date/time value generated by the service that indicates the time at which the blob was last read or written to
	LastAccessed *time.Time

	// The date/time that the container was last modified.
	LastModified *time.Time

	// Specifies the duration of the lease, in seconds, or negative one (-1) for a lease that never expires. A non-infinite lease
	// can be between 15 and 60 seconds. A lease duration cannot be changed using renew or change.
	LeaseDuration *LeaseDurationType

	// Lease state of the blob.
	LeaseState *LeaseStateType

	// The lease status of the blob.
	LeaseStatus *LeaseStatusType

	// Indicates whether the blob has a legal hold.
	LegalHold *bool

	// The metadata headers.
	Metadata map[string]*string

	// Optional. Only valid when Object Replication is enabled for the storage container and on the destination blob of the replication.
	ObjectReplicationPolicyID *string

	// Optional. Only valid when Object Replication is enabled for the storage container and on the source blob of the replication.
	// When retrieving this header, it will return the header with the policy id and rule id (e.g. x-ms-or-policyid_ruleid), and
	// the value will be the status of the replication (e.g. complete, failed).
	ObjectReplicationRules map[string]*string

	// An opaque, globally-unique, server-generated string identifier for the request.
	RequestID *string

	// Indicates the response body contains a structured message and specifies the message schema version and properties.
	StructuredBodyType *string

	// The length of the blob/file content inside the message body when the response body is returned as a structured message.
	// Will always be smaller than Content-Length.
	StructuredContentLength *int64

	// The number of tags associated with the blob
	TagCount *int64

	// Specifies the version of the operation to use for this request.
	Version *string

	// A DateTime value returned by the service that uniquely identifies the blob. The value of this header indicates the blob
	// version, and may be used in subsequent requests to access this version of the blob.
	VersionID *string
}
