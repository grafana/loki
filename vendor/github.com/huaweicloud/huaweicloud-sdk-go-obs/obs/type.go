// Copyright 2019 Huawei Technologies Co.,Ltd.
// Licensed under the Apache License, Version 2.0 (the "License"); you may not use
// this file except in compliance with the License.  You may obtain a copy of the
// License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software distributed
// under the License is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR
// CONDITIONS OF ANY KIND, either express or implied.  See the License for the
// specific language governing permissions and limitations under the License.

package obs

// SignatureType defines type of signature
type SignatureType string

const (
	// SignatureV2 signature type v2
	SignatureV2 SignatureType = "v2"
	// SignatureV4 signature type v4
	SignatureV4 SignatureType = "v4"
	// SignatureObs signature type OBS
	SignatureObs SignatureType = "OBS"
)

// HttpMethodType defines http method type
type HttpMethodType string

const (
	HttpMethodGet     HttpMethodType = HTTP_GET
	HttpMethodPut     HttpMethodType = HTTP_PUT
	HttpMethodPost    HttpMethodType = HTTP_POST
	HttpMethodDelete  HttpMethodType = HTTP_DELETE
	HttpMethodHead    HttpMethodType = HTTP_HEAD
	HttpMethodOptions HttpMethodType = HTTP_OPTIONS
)

// SubResourceType defines the subResource value
type SubResourceType string

const (
	// SubResourceStoragePolicy subResource value: storagePolicy
	SubResourceStoragePolicy SubResourceType = "storagePolicy"

	// SubResourceStorageClass subResource value: storageClass
	SubResourceStorageClass SubResourceType = "storageClass"

	// SubResourceQuota subResource value: quota
	SubResourceQuota SubResourceType = "quota"

	// SubResourceStorageInfo subResource value: storageinfo
	SubResourceStorageInfo SubResourceType = "storageinfo"

	// SubResourceLocation subResource value: location
	SubResourceLocation SubResourceType = "location"

	// SubResourceAcl subResource value: acl
	SubResourceAcl SubResourceType = "acl"

	// SubResourcePolicy subResource value: policy
	SubResourcePolicy SubResourceType = "policy"

	// SubResourceCors subResource value: cors
	SubResourceCors SubResourceType = "cors"

	// SubResourceVersioning subResource value: versioning
	SubResourceVersioning SubResourceType = "versioning"

	// SubResourceWebsite subResource value: website
	SubResourceWebsite SubResourceType = "website"

	// SubResourceLogging subResource value: logging
	SubResourceLogging SubResourceType = "logging"

	// SubResourceLifecycle subResource value: lifecycle
	SubResourceLifecycle SubResourceType = "lifecycle"

	// SubResourceNotification subResource value: notification
	SubResourceNotification SubResourceType = "notification"

	// SubResourceEncryption subResource value: encryption
	SubResourceEncryption SubResourceType = "encryption"

	// SubResourceTagging subResource value: tagging
	SubResourceTagging SubResourceType = "tagging"

	// SubResourceDelete subResource value: delete
	SubResourceDelete SubResourceType = "delete"

	// SubResourceVersions subResource value: versions
	SubResourceVersions SubResourceType = "versions"

	// SubResourceUploads subResource value: uploads
	SubResourceUploads SubResourceType = "uploads"

	// SubResourceRestore subResource value: restore
	SubResourceRestore SubResourceType = "restore"

	// SubResourceMetadata subResource value: metadata
	SubResourceMetadata SubResourceType = "metadata"

	// SubResourceRequestPayment subResource value: requestPayment
	SubResourceRequestPayment SubResourceType = "requestPayment"

	// SubResourceAppend subResource value: append
	SubResourceAppend SubResourceType = "append"

	// SubResourceModify subResource value: modify
	SubResourceModify SubResourceType = "modify"

	// SubResourceRename subResource value: rename
	SubResourceRename SubResourceType = "rename"

	// SubResourceCustomDomain subResource value: customdomain
	SubResourceCustomDomain SubResourceType = "customdomain"

	// SubResourceMirrorBackToSource subResource value: mirrorBackToSource
	SubResourceMirrorBackToSource SubResourceType = "mirrorBackToSource"

	// SubResourceMirrorBackToSource subResource value: mirrorBackToSource
	SubResourceAccesslabel SubResourceType = "x-obs-accesslabel"

	// SubResourceMirrorBackToSource subResource value: publicAccessBlock
	SubResourcePublicAccessBlock SubResourceType = "publicAccessBlock"

	// SubResourcePublicBucketStatus subResource value: bucketStatus
	SubResourceBucketPublicStatus SubResourceType = "bucketStatus"

	// SubResourcePublicPolicyStatus subResource value: policyStatus
	SubResourceBucketPolicyPublicStatus SubResourceType = "policyStatus"
)

// objectKeyType defines the objectKey value
type objectKeyType string

const (
	// objectKeyExtensionPolicy objectKey value: v1/extension_policy
	objectKeyExtensionPolicy objectKeyType = "v1/extension_policy"

	// objectKeyAsyncFetchJob objectKey value: v1/async-fetch/jobs
	objectKeyAsyncFetchJob objectKeyType = "v1/async-fetch/jobs"
)

// AclType defines bucket/object acl type
type AclType string

const (
	AclPrivate                 AclType = "private"
	AclPublicRead              AclType = "public-read"
	AclPublicReadWrite         AclType = "public-read-write"
	AclAuthenticatedRead       AclType = "authenticated-read"
	AclBucketOwnerRead         AclType = "bucket-owner-read"
	AclBucketOwnerFullControl  AclType = "bucket-owner-full-control"
	AclLogDeliveryWrite        AclType = "log-delivery-write"
	AclPublicReadDelivery      AclType = "public-read-delivered"
	AclPublicReadWriteDelivery AclType = "public-read-write-delivered"
)

// StorageClassType defines bucket storage class
type StorageClassType string

const (
	//StorageClassStandard storage class: STANDARD
	StorageClassStandard StorageClassType = "STANDARD"

	//StorageClassWarm storage class: WARM
	StorageClassWarm StorageClassType = "WARM"

	//StorageClassCold storage class: COLD
	StorageClassCold StorageClassType = "COLD"

	//StorageClassDeepArchive storage class: DEEP_ARCHIVE
	StorageClassDeepArchive StorageClassType = "DEEP_ARCHIVE"

	//StorageClassIntelligentTiering storage class: INTELLIGENT_TIERING
	StorageClassIntelligentTiering StorageClassType = "INTELLIGENT_TIERING"

	storageClassStandardIA StorageClassType = "STANDARD_IA"
	storageClassGlacier    StorageClassType = "GLACIER"
)

// PermissionType defines permission type
type PermissionType string

const (
	// PermissionRead permission type: READ
	PermissionRead PermissionType = "READ"

	// PermissionWrite permission type: WRITE
	PermissionWrite PermissionType = "WRITE"

	// PermissionReadAcp permission type: READ_ACP
	PermissionReadAcp PermissionType = "READ_ACP"

	// PermissionWriteAcp permission type: WRITE_ACP
	PermissionWriteAcp PermissionType = "WRITE_ACP"

	// PermissionFullControl permission type: FULL_CONTROL
	PermissionFullControl PermissionType = "FULL_CONTROL"
)

// GranteeType defines grantee type
type GranteeType string

const (
	// GranteeGroup grantee type: Group
	GranteeGroup GranteeType = "Group"

	// GranteeUser grantee type: CanonicalUser
	GranteeUser GranteeType = "CanonicalUser"
)

// GroupUriType defines grantee uri type
type GroupUriType string

const (
	// GroupAllUsers grantee uri type: AllUsers
	GroupAllUsers GroupUriType = "AllUsers"

	// GroupAuthenticatedUsers grantee uri type: AuthenticatedUsers
	GroupAuthenticatedUsers GroupUriType = "AuthenticatedUsers"

	// GroupLogDelivery grantee uri type: LogDelivery
	GroupLogDelivery GroupUriType = "LogDelivery"
)

// VersioningStatusType defines bucket version status
type VersioningStatusType string

const (
	// VersioningStatusEnabled version status: Enabled
	VersioningStatusEnabled VersioningStatusType = "Enabled"

	// VersioningStatusSuspended version status: Suspended
	VersioningStatusSuspended VersioningStatusType = "Suspended"
)

// ProtocolType defines protocol type
type ProtocolType string

const (
	// ProtocolHttp prorocol type: http
	ProtocolHttp ProtocolType = "http"

	// ProtocolHttps prorocol type: https
	ProtocolHttps ProtocolType = "https"
)

// RuleStatusType defines lifeCycle rule status
type RuleStatusType string

const (
	// RuleStatusEnabled rule status: Enabled
	RuleStatusEnabled RuleStatusType = "Enabled"

	// RuleStatusDisabled rule status: Disabled
	RuleStatusDisabled RuleStatusType = "Disabled"
)

// RestoreTierType defines restore options
type RestoreTierType string

const (
	// RestoreTierExpedited restore options: Expedited
	RestoreTierExpedited RestoreTierType = "Expedited"

	// RestoreTierStandard restore options: Standard
	RestoreTierStandard RestoreTierType = "Standard"

	// RestoreTierBulk restore options: Bulk
	RestoreTierBulk RestoreTierType = "Bulk"
)

// MetadataDirectiveType defines metadata operation indicator
type MetadataDirectiveType string

const (
	// CopyMetadata metadata operation: COPY
	CopyMetadata MetadataDirectiveType = "COPY"

	// ReplaceNew metadata operation: REPLACE_NEW
	ReplaceNew MetadataDirectiveType = "REPLACE_NEW"

	// ReplaceMetadata metadata operation: REPLACE
	ReplaceMetadata MetadataDirectiveType = "REPLACE"
)

// EventType defines bucket notification type of events
type EventType string

const (
	// ObjectCreatedAll type of events: ObjectCreated:*
	ObjectCreatedAll EventType = "ObjectCreated:*"

	// ObjectCreatedPut type of events: ObjectCreated:Put
	ObjectCreatedPut EventType = "ObjectCreated:Put"

	// ObjectCreatedPost type of events: ObjectCreated:Post
	ObjectCreatedPost EventType = "ObjectCreated:Post"

	// ObjectCreatedCopy type of events: ObjectCreated:Copy
	ObjectCreatedCopy EventType = "ObjectCreated:Copy"

	// ObjectCreatedCompleteMultipartUpload type of events: ObjectCreated:CompleteMultipartUpload
	ObjectCreatedCompleteMultipartUpload EventType = "ObjectCreated:CompleteMultipartUpload"

	// ObjectRemovedAll type of events: ObjectRemoved:*
	ObjectRemovedAll EventType = "ObjectRemoved:*"

	// ObjectRemovedDelete type of events: ObjectRemoved:Delete
	ObjectRemovedDelete EventType = "ObjectRemoved:Delete"

	// ObjectRemovedDeleteMarkerCreated type of events: ObjectRemoved:DeleteMarkerCreated
	ObjectRemovedDeleteMarkerCreated EventType = "ObjectRemoved:DeleteMarkerCreated"
)

// PayerType defines type of payer
type PayerType string

const (
	// BucketOwnerPayer type of payer: BucketOwner
	BucketOwnerPayer PayerType = "BucketOwner"

	// RequesterPayer type of payer: Requester
	RequesterPayer PayerType = "Requester"

	// Requester header for requester-Pays
	Requester PayerType = "requester"
)

// FetchPolicyStatusType defines type of fetch policy status
type FetchPolicyStatusType string

const (
	// FetchStatusOpen type of status: open
	FetchStatusOpen FetchPolicyStatusType = "open"

	// FetchStatusClosed type of status: closed
	FetchStatusClosed FetchPolicyStatusType = "closed"
)

// AvailableZoneType defines type of az redundancy
type AvailableZoneType string

const (
	AvailableZoneMultiAz AvailableZoneType = "3az"
)

// FSStatusType defines type of file system status
type FSStatusType string

const (
	FSStatusEnabled  FSStatusType = "Enabled"
	FSStatusDisabled FSStatusType = "Disabled"
)

// BucketType defines type of bucket
type BucketType string

const (
	OBJECT BucketType = "OBJECT"
	POSIX  BucketType = "POSIX"
)

// RedundancyType defines type of redundancyType
type BucketRedundancyType string

const (
	BucketRedundancyClassic BucketRedundancyType = "CLASSIC"
	BucketRedundancyFusion  BucketRedundancyType = "FUSION"
)
