/*
 * Copyright 2017 Baidu, Inc.
 *
 * Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file
 * except in compliance with the License. You may obtain a copy of the License at
 *
 * http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software distributed under the
 * License is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND,
 * either express or implied. See the License for the specific language governing permissions
 * and limitations under the License.
 */

// model.go - definitions of the request arguments and results data structure model

package api

import (
	"io"
)

type OwnerType struct {
	Id          string `json:"id"`
	DisplayName string `json:"displayName"`
}

type BucketSummaryType struct {
	Name         string `json:"name"`
	Location     string `json:"location"`
	CreationDate string `json:"creationDate"`
}

// ListBucketsResult defines the result structure of ListBuckets api.
type ListBucketsResult struct {
	Owner   OwnerType           `json:"owner"`
	Buckets []BucketSummaryType `json:"buckets"`
}

// ListObjectsArgs defines the optional arguments for ListObjects api.
type ListObjectsArgs struct {
	Delimiter       string `json:"delimiter"`
	Marker          string `json:"marker"`
	MaxKeys         int    `json:"maxKeys"`
	Prefix          string `json:"prefix"`
	VersionIdMarker string `json:"versionIdMarker,omitempty"`
}

type ObjectSummaryType struct {
	Key          string    `json:"key"`
	LastModified string    `json:"lastModified"`
	ETag         string    `json:"eTag"`
	Size         int       `json:"size"`
	StorageClass string    `json:"storageClass"`
	Owner        OwnerType `json:"owner"`
	VersionId    string    `json:"versionId,omitempty"`
	IsLatest     int       `json:"isLatest,omitempty"`
}

type PrefixType struct {
	Prefix string `json:"prefix"`
}

type PutBucketArgs struct {
	TagList         string `json:"-"`
	EnableMultiAz   bool   `json:"enableMultiAz"`
	LccLocation     string `json:"lccLocation,omitempty"`
	EnableDedicated bool   `json:"enableDedicated,omitempty"`
}

// ListObjectsResult defines the result structure of ListObjects api.
type ListObjectsResult struct {
	Name                string              `json:"name"`
	Prefix              string              `json:"prefix"`
	Delimiter           string              `json:"delimiter"`
	Marker              string              `json:"marker"`
	NextMarker          string              `json:"nextMarker,omitempty"`
	NextVersionidMarker string              `json:"nextVersionidMarker,omitempty"`
	MaxKeys             int                 `json:"maxKeys"`
	IsTruncated         bool                `json:"isTruncated"`
	Contents            []ObjectSummaryType `json:"contents"`
	CommonPrefixes      []PrefixType        `json:"commonPrefixes"`
}

type LocationType struct {
	LocationConstraint string `json:"locationConstraint"`
}

// AclOwnerType defines the owner struct in ACL setting
type AclOwnerType struct {
	Id string `json:"id"`
}

// GranteeType defines the grantee struct in ACL setting
type GranteeType struct {
	Id string `json:"id"`
}

type AclRefererType struct {
	StringLike   []string `json:"stringLike"`
	StringEquals []string `json:"stringEquals"`
}

type AclCondType struct {
	IpAddress []string       `json:"ipAddress"`
	Referer   AclRefererType `json:"referer"`
	VpcId     []string       `json:"vpcId"`
}

// GrantType defines the grant struct in ACL setting
type GrantType struct {
	Grantee     []GranteeType `json:"grantee"`
	Permission  []string      `json:"permission"`
	Resource    []string      `json:"resource,omitempty"`
	NotResource []string      `json:"notResource,omitempty"`
	Condition   AclCondType   `json:"condition,omitempty"`
	Effect      string        `json:"effect,omitempty"`
}

// PutBucketAclArgs defines the input args structure for putting bucket acl.
type PutBucketAclArgs struct {
	AccessControlList []GrantType `json:"accessControlList"`
}

// GetBucketAclResult defines the result structure of getting bucket acl.
type GetBucketAclResult struct {
	AccessControlList []GrantType  `json:"accessControlList"`
	Owner             AclOwnerType `json:"owner"`
}

// PutBucketLoggingArgs defines the input args structure for putting bucket logging.
type PutBucketLoggingArgs struct {
	TargetBucket string `json:"targetBucket"`
	TargetPrefix string `json:"targetPrefix"`
}

// GetBucketLoggingResult defines the result structure for getting bucket logging.
type GetBucketLoggingResult struct {
	Status       string `json:"status"`
	TargetBucket string `json:"targetBucket,omitempty"`
	TargetPrefix string `json:"targetPrefix,omitempty"`
}

// LifecycleConditionTimeType defines the structure of time condition
type LifecycleConditionTimeType struct {
	DateGreaterThan string `json:"dateGreaterThan"`
}

// LifecycleConditionType defines the structure of condition
type LifecycleConditionType struct {
	Time LifecycleConditionTimeType `json:"time"`
}

// LifecycleActionType defines the structure of lifecycle action
type LifecycleActionType struct {
	Name         string `json:"name"`
	StorageClass string `json:"storageClass,omitempty"`
}

// LifecycleRuleType defines the structure of a single lifecycle rule
type LifecycleRuleType struct {
	Id        string                 `json:"id"`
	Status    string                 `json:"status"`
	Resource  []string               `json:"resource"`
	Condition LifecycleConditionType `json:"condition"`
	Action    LifecycleActionType    `json:"action"`
}

// GetBucketLifecycleResult defines the lifecycle argument structure for putting
type PutBucketLifecycleArgs struct {
	Rule []LifecycleRuleType `json:"rule"`
}

// GetBucketLifecycleResult defines the lifecycle result structure for getting
type GetBucketLifecycleResult struct {
	Rule []LifecycleRuleType `json:"rule"`
}

type StorageClassType struct {
	StorageClass string `json:"storageClass"`
}

// BucketReplicationDescriptor defines the description data structure
type BucketReplicationDescriptor struct {
	Bucket       string `json:"bucket,omitempty"`
	StorageClass string `json:"storageClass,omitempty"`
	Prefix       string `json:"prefix,omitempty"`
}

// BucketReplicationType defines the data structure for Put and Get of bucket replication
type BucketReplicationType struct {
	Id                 string                       `json:"id"`
	Status             string                       `json:"status"`
	Resource           []string                     `json:"resource"`
	NotIncludeResource []string                     `json:"notIncludeResource,omitempty"`
	ReplicateDeletes   string                       `json:"replicateDeletes"`
	Destination        *BucketReplicationDescriptor `json:"destination,omitempty"`
	ReplicateHistory   *BucketReplicationDescriptor `json:"replicateHistory,omitempty"`
	CreateTime         int64                        `json:"createTime"`
	DestRegion         string                       `json:"destRegion"`
}

type PutBucketReplicationArgs BucketReplicationType
type GetBucketReplicationResult BucketReplicationType

// ListBucketReplicationResult defines output result for replication conf list
type ListBucketReplicationResult struct {
	Rules []BucketReplicationType `json:"rules"`
}

// GetBucketReplicationProgressResult defines output result for replication process
type GetBucketReplicationProgressResult struct {
	Status                    string  `json:"status"`
	HistoryReplicationPercent float64 `json:"historyReplicationPercent"`
	LatestReplicationTime     string  `json:"latestReplicationTime"`
}

// BucketEncryptionType defines the data structure for Put and Get of bucket encryption
type BucketEncryptionType struct {
	EncryptionAlgorithm string `json:"encryptionAlgorithm"`
}

// BucketStaticWebsiteType defines the data structure for Put and Get of bucket static website
type BucketStaticWebsiteType struct {
	Index    string `json:"index"`
	NotFound string `json:"notFound"`
}

type PutBucketStaticWebsiteArgs BucketStaticWebsiteType
type GetBucketStaticWebsiteResult BucketStaticWebsiteType

type BucketCORSType struct {
	AllowedOrigins       []string `json:"allowedOrigins"`
	AllowedMethods       []string `json:"allowedMethods"`
	AllowedHeaders       []string `json:"allowedHeaders,omitempty"`
	AllowedExposeHeaders []string `json:"allowedExposeHeaders,omitempty"`
	MaxAgeSeconds        int64    `json:"maxAgeSeconds,omitempty"`
}

// PutBucketCorsArgs defines the request argument for bucket CORS setting
type PutBucketCorsArgs struct {
	CorsConfiguration []BucketCORSType `json:"corsConfiguration"`
}

// GetBucketCorsResult defines the data structure of getting bucket CORS result
type GetBucketCorsResult struct {
	CorsConfiguration []BucketCORSType `json:"corsConfiguration"`
}

// CopyrightProtectionType defines the data structure for Put and Get copyright protection API
type CopyrightProtectionType struct {
	Resource []string `json:"resource"`
}

// ObjectAclType defines the data structure for Put and Get object acl API
type ObjectAclType struct {
	AccessControlList []GrantType `json:"accessControlList"`
}

type PutObjectAclArgs ObjectAclType
type GetObjectAclResult ObjectAclType

// PutObjectArgs defines the optional args structure for the put object api.
type PutObjectArgs struct {
	CacheControl       string
	ContentDisposition string
	ContentMD5         string
	ContentType        string
	ContentLength      int64
	Expires            string
	UserMeta           map[string]string
	ContentSha256      string
	ContentCrc32       string
	StorageClass       string
	Process            string
	CannedAcl          string
	ObjectTagging      string
	TrafficLimit       int64
}

// CopyObjectArgs defines the optional args structure for the copy object api.
type CopyObjectArgs struct {
	ObjectMeta
	MetadataDirective string
	IfMatch           string
	IfNoneMatch       string
	IfModifiedSince   string
	IfUnmodifiedSince string
	TrafficLimit      int64
	CannedAcl         string
	TaggingDirective  string
	ObjectTagging     string
}

type MultiCopyObjectArgs struct {
	StorageClass     string
	ObjectTagging    string
	TaggingDirective string
}

type CallbackResult struct {
	Result string `json:"result"`
}

type PutObjectResult struct {
	Callback CallbackResult `json:"callback"`
}

// CopyObjectResult defines the result json structure for the copy object api.
type CopyObjectResult struct {
	LastModified string `json:"lastModified"`
	ETag         string `json:"eTag"`
	VersionId    string `json:"versionId"`
}

type ObjectMeta struct {
	CacheControl       string
	ContentDisposition string
	ContentEncoding    string
	ContentLength      int64
	ContentRange       string
	ContentType        string
	ContentMD5         string
	ContentSha256      string
	ContentCrc32       string
	Expires            string
	LastModified       string
	ETag               string
	UserMeta           map[string]string
	StorageClass       string
	NextAppendOffset   string
	ObjectType         string
	BceRestore         string
	BceObjectType      string
	VersionId          string
}

// GetObjectResult defines the result data of the get object api.
type GetObjectResult struct {
	ObjectMeta
	ContentLanguage string
	Body            io.ReadCloser
}

// GetObjectMetaResult defines the result data of the get object meta api.
type GetObjectMetaResult struct {
	ObjectMeta
}

// SelectObjectResult defines the result data of the select object api.
type SelectObjectResult struct {
	Body io.ReadCloser
}

// selectObject request args
type SelectObjectArgs struct {
	SelectType    string               `json:"-"`
	SelectRequest *SelectObjectRequest `json:"selectRequest"`
}

type SelectObjectRequest struct {
	Expression          string                `json:"expression"`
	ExpressionType      string                `json:"expressionType"` // SQL
	InputSerialization  *SelectObjectInput    `json:"inputSerialization"`
	OutputSerialization *SelectObjectOutput   `json:"outputSerialization"`
	RequestProgress     *SelectObjectProgress `json:"requestProgress"`
}

type SelectObjectInput struct {
	CompressionType string            `json:"compressionType"`
	CsvParams       map[string]string `json:"csv"`
	JsonParams      map[string]string `json:"json"`
}
type SelectObjectOutput struct {
	OutputHeader bool              `json:"outputHeader"`
	CsvParams    map[string]string `json:"csv"`
	JsonParams   map[string]string `json:"json"`
}
type SelectObjectProgress struct {
	Enabled bool `json:"enabled"`
}

type Prelude struct {
	TotalLen   uint32
	HeadersLen uint32
}

// selectObject response msg
type CommonMessage struct {
	Prelude
	Headers map[string]string // message-type/content-type……
	Crc32   uint32            // crc32 of RecordsMessage
}
type RecordsMessage struct {
	CommonMessage
	Records []string // csv/json seleted data, one or more records
}
type ContinuationMessage struct {
	CommonMessage
	BytesScanned  uint64
	BytesReturned uint64
}
type EndMessage struct {
	CommonMessage
}

// FetchObjectArgs defines the optional arguments structure for the fetch object api.
type FetchObjectArgs struct {
	FetchMode            string
	StorageClass         string
	FetchCallBackAddress string
}

// FetchObjectResult defines the result json structure for the fetch object api.
type FetchObjectResult struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	RequestId string `json:"requestId"`
	JobId     string `json:"jobId"`
}

// AppendObjectArgs defines the optional arguments structure for appending object.
type AppendObjectArgs struct {
	Offset             int64
	CacheControl       string
	ContentDisposition string
	ContentMD5         string
	ContentType        string
	Expires            string
	UserMeta           map[string]string
	ContentSha256      string
	ContentCrc32       string
	StorageClass       string
	TrafficLimit       int64
}

// AppendObjectResult defines the result data structure for appending object.
type AppendObjectResult struct {
	ContentMD5       string
	NextAppendOffset int64
	ContentCrc32     string
	ETag             string
}

// DeleteObjectArgs defines the input args structure for a single object.
type DeleteObjectArgs struct {
	Key string `json:"key"`
}

// DeleteMultipleObjectsResult defines the input args structure for deleting multiple objects.
type DeleteMultipleObjectsArgs struct {
	Objects []DeleteObjectArgs `json:"objects"`
}

// DeleteObjectResult defines the result structure for deleting a single object.
type DeleteObjectResult struct {
	Key     string `json:"key"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

// DeleteMultipleObjectsResult defines the result structure for deleting multiple objects.
type DeleteMultipleObjectsResult struct {
	Errors []DeleteObjectResult `json:"errors"`
}

// InitiateMultipartUploadArgs defines the input arguments to initiate a multipart upload.
type InitiateMultipartUploadArgs struct {
	CacheControl       string
	ContentDisposition string
	Expires            string
	StorageClass       string
	ObjectTagging      string
	TaggingDirective   string
}

// InitiateMultipartUploadResult defines the result structure to initiate a multipart upload.
type InitiateMultipartUploadResult struct {
	Bucket   string `json:"bucket"`
	Key      string `json:"key"`
	UploadId string `json:"uploadId"`
}

// UploadPartArgs defines the optinoal argumets for uploading part.
type UploadPartArgs struct {
	ContentMD5    string
	ContentSha256 string
	ContentCrc32  string
	TrafficLimit  int64
}

// UploadPartCopyArgs defines the optional arguments of UploadPartCopy.
type UploadPartCopyArgs struct {
	SourceRange       string
	IfMatch           string
	IfNoneMatch       string
	IfModifiedSince   string
	IfUnmodifiedSince string
	TrafficLimit      int64
}

type PutSymlinkArgs struct {
	ForbidOverwrite string
	StorageClass    string
	UserMeta        map[string]string
	SymlinkBucket   string
}

// UploadInfoType defines an uploaded part info structure.
type UploadInfoType struct {
	PartNumber int    `json:"partNumber"`
	ETag       string `json:"eTag"`
}

// CompleteMultipartUploadArgs defines the input arguments structure of CompleteMultipartUpload.
type CompleteMultipartUploadArgs struct {
	Parts        []UploadInfoType  `json:"parts"`
	UserMeta     map[string]string `json:"-"`
	Process      string            `json:"-"`
	ContentCrc32 string            `json:"-"`
}

// CompleteMultipartUploadResult defines the result structure of CompleteMultipartUpload.
type CompleteMultipartUploadResult struct {
	Location     string `json:"location"`
	Bucket       string `json:"bucket"`
	Key          string `json:"key"`
	ETag         string `json:"eTag"`
	ContentCrc32 string `json:"-"`
}

// ListPartsArgs defines the input optional arguments of listing parts information.
type ListPartsArgs struct {
	MaxParts         int
	PartNumberMarker string
}

type ListPartType struct {
	PartNumber   int    `json:"partNumber"`
	LastModified string `json:"lastModified"`
	ETag         string `json:"eTag"`
	Size         int    `json:"size"`
}

// ListPartsResult defines the parts info result from ListParts.
type ListPartsResult struct {
	Bucket               string         `json:"bucket"`
	Key                  string         `json:"key"`
	UploadId             string         `json:"uploadId"`
	Initiated            string         `json:"initiated"`
	Owner                OwnerType      `json:"owner"`
	StorageClass         string         `json:"storageClass"`
	PartNumberMarker     int            `json:"partNumberMarker"`
	NextPartNumberMarker int            `json:"nextPartNumberMarker"`
	MaxParts             int            `json:"maxParts"`
	IsTruncated          bool           `json:"isTruncated"`
	Parts                []ListPartType `json:"parts"`
}

// ListMultipartUploadsArgs defines the optional arguments for ListMultipartUploads.
type ListMultipartUploadsArgs struct {
	Delimiter  string
	KeyMarker  string
	MaxUploads int
	Prefix     string
}

type ListMultipartUploadsType struct {
	Key          string    `json:"key"`
	UploadId     string    `json:"uploadId"`
	Owner        OwnerType `json:"owner"`
	Initiated    string    `json:"initiated"`
	StorageClass string    `json:"storageClass,omitempty"`
}

// ListMultipartUploadsResult defines the multipart uploads result structure.
type ListMultipartUploadsResult struct {
	Bucket         string                     `json:"bucket"`
	CommonPrefixes []PrefixType               `json:"commonPrefixes"`
	Delimiter      string                     `json:"delimiter"`
	Prefix         string                     `json:"prefix"`
	IsTruncated    bool                       `json:"isTruncated"`
	KeyMarker      string                     `json:"keyMarker"`
	MaxUploads     int                        `json:"maxUploads"`
	NextKeyMarker  string                     `json:"nextKeyMarker"`
	Uploads        []ListMultipartUploadsType `json:"uploads"`
}

type ArchiveRestoreArgs struct {
	RestoreTier string
	RestoreDays int
}

type GetBucketTrashResult struct {
	TrashDir string `json:"trashDir"`
}

type PutBucketTrashReq struct {
	TrashDir string `json:"trashDir"`
}

type PutBucketNotificationReq struct {
	Notifications []PutBucketNotificationSt `json:"notifications"`
}

type PutBucketNotificationSt struct {
	Id        string                        `json:"id"`
	Name      string                        `json:"name"`
	AppId     string                        `json:"appId"`
	Status    string                        `json:"status"`
	Resources []string                      `json:"resources"`
	Events    []string                      `json:"events"`
	Apps      []PutBucketNotificationAppsSt `json:"apps"`
}

type PutBucketNotificationAppsSt struct {
	Id       string `json:"id"`
	EventUrl string `json:"eventUrl"`
	XVars    string `json:"xVars"`
}

type MirrorConfigurationRule struct {
	Prefix          string       `json:"prefix,omitempty"`
	SourceUrl       string       `json:"sourceUrl"`
	PassQueryString bool         `json:"passQuerystring"`
	Mode            string       `json:"mode"`
	StorageClass    string       `json:"storageClass"`
	PassHeaders     []string     `json:"passHeaders"`
	IgnoreHeaders   []string     `json:"ignoreHeaders"`
	CustomHeaders   []HeaderPair `json:"customHeaders"`
	BackSourceUrl   string       `json:"backSourceUrl"`
	Resource        string       `json:"resource"`
	Suffix          string       `json:"suffix"`
	FixedKey        string       `json:"fixedKey"`
	PrefixReplace   string       `json:"prefixReplace"`
	Version         string       `json:"version"`
}

type HeaderPair struct {
	HeaderName  string `json:"headerName"`
	HeaderValue string `json:"headerValue"`
}

type PutBucketMirrorArgs struct {
	BucketMirroringConfiguration []MirrorConfigurationRule `json:"bucketMirroringConfiguration"`
}

type PutBucketTagArgs struct {
	Tags []Tag `json:"tags"`
}

type Tag struct {
	TagKey   string `json:"tagKey"`
	TagValue string `json:"tagValue"`
}

type GetBucketTagResult struct {
	Tags []BucketTag `json:"tag"`
}

type BucketTag struct {
	TagKey   string `json:"tag_key"`
	TagValue string `json:"tag_value"`
}

type BosContext struct {
	Bucket          string
	PathStyleEnable bool
}

type PutObjectTagArgs struct {
	ObjectTags []ObjectTags `json:"tagSet"`
}

type ObjectTags struct {
	TagInfo []ObjectTag `json:"tagInfo"`
}

type ObjectTag struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type BosShareLinkArgs struct {
	Bucket          string `json:"bucket"`
	Endpoint        string `json:"endpoint"`
	Prefix          string `json:"prefix"`
	ShareCode       string `json:"shareCode"`
	DurationSeconds int64  `json:"durationSeconds"`
}

type BosShareResBody struct {
	ShareUrl       string `json:"shareUrl"`
	LinkExpireTime int64  `json:"linkExpireTime"`
	ShareCode      string `json:"shareCode"`
}

type BucketVersioningArgs struct {
	Status string `json:"status"`
}