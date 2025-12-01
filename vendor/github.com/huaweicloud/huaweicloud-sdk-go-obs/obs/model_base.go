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

import (
	"encoding/xml"
	"time"
)

// Bucket defines bucket properties
type Bucket struct {
	XMLName      xml.Name  `xml:"Bucket"`
	Name         string    `xml:"Name"`
	CreationDate time.Time `xml:"CreationDate"`
	Location     string    `xml:"Location"`
	BucketType   string    `xml:"BucketType,omitempty"`
}

// Owner defines owner properties
type Owner struct {
	XMLName     xml.Name `xml:"Owner"`
	ID          string   `xml:"ID"`
	DisplayName string   `xml:"DisplayName,omitempty"`
}

// Initiator defines initiator properties
type Initiator struct {
	XMLName     xml.Name `xml:"Initiator"`
	ID          string   `xml:"ID"`
	DisplayName string   `xml:"DisplayName,omitempty"`
}

type bucketLocationObs struct {
	XMLName  xml.Name `xml:"Location"`
	Location string   `xml:",chardata"`
}

// BucketLocation defines bucket location configuration
type BucketLocation struct {
	XMLName  xml.Name `xml:"CreateBucketConfiguration"`
	Location string   `xml:"LocationConstraint,omitempty"`
}

// BucketStoragePolicy defines the bucket storage class
type BucketStoragePolicy struct {
	XMLName      xml.Name         `xml:"StoragePolicy"`
	StorageClass StorageClassType `xml:"DefaultStorageClass"`
}

type bucketStoragePolicyObs struct {
	XMLName      xml.Name `xml:"StorageClass"`
	StorageClass string   `xml:",chardata"`
}

// Content defines the object content properties
type Content struct {
	XMLName      xml.Name         `xml:"Contents"`
	Owner        Owner            `xml:"Owner"`
	ETag         string           `xml:"ETag"`
	Key          string           `xml:"Key"`
	LastModified time.Time        `xml:"LastModified"`
	Size         int64            `xml:"Size"`
	StorageClass StorageClassType `xml:"StorageClass"`
}

// Version defines the properties of versioning objects
type Version struct {
	DeleteMarker
	XMLName xml.Name `xml:"Version"`
	ETag    string   `xml:"ETag"`
	Size    int64    `xml:"Size"`
}

// DeleteMarker defines the properties of versioning delete markers
type DeleteMarker struct {
	XMLName      xml.Name         `xml:"DeleteMarker"`
	Key          string           `xml:"Key"`
	VersionId    string           `xml:"VersionId"`
	IsLatest     bool             `xml:"IsLatest"`
	LastModified time.Time        `xml:"LastModified"`
	Owner        Owner            `xml:"Owner"`
	StorageClass StorageClassType `xml:"StorageClass"`
}

// Upload defines multipart upload properties
type Upload struct {
	XMLName      xml.Name         `xml:"Upload"`
	Key          string           `xml:"Key"`
	UploadId     string           `xml:"UploadId"`
	Initiated    time.Time        `xml:"Initiated"`
	StorageClass StorageClassType `xml:"StorageClass"`
	Owner        Owner            `xml:"Owner"`
	Initiator    Initiator        `xml:"Initiator"`
}

// BucketQuota defines bucket quota configuration
type BucketQuota struct {
	XMLName xml.Name `xml:"Quota"`
	Quota   int64    `xml:"StorageQuota"`
}

// Grantee defines grantee properties
type Grantee struct {
	XMLName     xml.Name     `xml:"Grantee"`
	Type        GranteeType  `xml:"type,attr"`
	ID          string       `xml:"ID,omitempty"`
	DisplayName string       `xml:"DisplayName,omitempty"`
	URI         GroupUriType `xml:"URI,omitempty"`
}

type granteeObs struct {
	XMLName     xml.Name    `xml:"Grantee"`
	Type        GranteeType `xml:"type,attr"`
	ID          string      `xml:"ID,omitempty"`
	DisplayName string      `xml:"DisplayName,omitempty"`
	Canned      string      `xml:"Canned,omitempty"`
}

// Grant defines grant properties
type Grant struct {
	XMLName    xml.Name       `xml:"Grant"`
	Grantee    Grantee        `xml:"Grantee"`
	Permission PermissionType `xml:"Permission"`
	Delivered  bool           `xml:"Delivered"`
}

type grantObs struct {
	XMLName    xml.Name       `xml:"Grant"`
	Grantee    granteeObs     `xml:"Grantee"`
	Permission PermissionType `xml:"Permission"`
	Delivered  bool           `xml:"Delivered"`
}

// AccessControlPolicy defines access control policy properties
type AccessControlPolicy struct {
	XMLName   xml.Name `xml:"AccessControlPolicy"`
	Owner     Owner    `xml:"Owner"`
	Grants    []Grant  `xml:"AccessControlList>Grant"`
	Delivered string   `xml:"Delivered,omitempty"`
}

type accessControlPolicyObs struct {
	XMLName xml.Name   `xml:"AccessControlPolicy"`
	Owner   Owner      `xml:"Owner"`
	Grants  []grantObs `xml:"AccessControlList>Grant"`
}

// CorsRule defines the CORS rules
type CorsRule struct {
	XMLName       xml.Name `xml:"CORSRule"`
	ID            string   `xml:"ID,omitempty"`
	AllowedOrigin []string `xml:"AllowedOrigin"`
	AllowedMethod []string `xml:"AllowedMethod"`
	AllowedHeader []string `xml:"AllowedHeader,omitempty"`
	MaxAgeSeconds int      `xml:"MaxAgeSeconds"`
	ExposeHeader  []string `xml:"ExposeHeader,omitempty"`
}

// BucketCors defines the bucket CORS configuration
type BucketCors struct {
	XMLName   xml.Name   `xml:"CORSConfiguration"`
	CorsRules []CorsRule `xml:"CORSRule"`
}

// BucketVersioningConfiguration defines the versioning configuration
type BucketVersioningConfiguration struct {
	XMLName xml.Name             `xml:"VersioningConfiguration"`
	Status  VersioningStatusType `xml:"Status"`
}

// IndexDocument defines the default page configuration
type IndexDocument struct {
	Suffix string `xml:"Suffix"`
}

// ErrorDocument defines the error page configuration
type ErrorDocument struct {
	Key string `xml:"Key,omitempty"`
}

// Condition defines condition in RoutingRule
type Condition struct {
	XMLName                     xml.Name `xml:"Condition"`
	KeyPrefixEquals             string   `xml:"KeyPrefixEquals,omitempty"`
	HttpErrorCodeReturnedEquals string   `xml:"HttpErrorCodeReturnedEquals,omitempty"`
}

// Redirect defines redirect in RoutingRule
type Redirect struct {
	XMLName              xml.Name     `xml:"Redirect"`
	Protocol             ProtocolType `xml:"Protocol,omitempty"`
	HostName             string       `xml:"HostName,omitempty"`
	ReplaceKeyPrefixWith string       `xml:"ReplaceKeyPrefixWith,omitempty"`
	ReplaceKeyWith       string       `xml:"ReplaceKeyWith,omitempty"`
	HttpRedirectCode     string       `xml:"HttpRedirectCode,omitempty"`
}

// RoutingRule defines routing rules
type RoutingRule struct {
	XMLName   xml.Name  `xml:"RoutingRule"`
	Condition Condition `xml:"Condition,omitempty"`
	Redirect  Redirect  `xml:"Redirect"`
}

// RedirectAllRequestsTo defines redirect in BucketWebsiteConfiguration
type RedirectAllRequestsTo struct {
	XMLName  xml.Name     `xml:"RedirectAllRequestsTo"`
	Protocol ProtocolType `xml:"Protocol,omitempty"`
	HostName string       `xml:"HostName"`
}

// BucketWebsiteConfiguration defines the bucket website configuration
type BucketWebsiteConfiguration struct {
	XMLName               xml.Name              `xml:"WebsiteConfiguration"`
	RedirectAllRequestsTo RedirectAllRequestsTo `xml:"RedirectAllRequestsTo,omitempty"`
	IndexDocument         IndexDocument         `xml:"IndexDocument,omitempty"`
	ErrorDocument         ErrorDocument         `xml:"ErrorDocument,omitempty"`
	RoutingRules          []RoutingRule         `xml:"RoutingRules>RoutingRule,omitempty"`
}

// BucketLoggingStatus defines the bucket logging configuration
type BucketLoggingStatus struct {
	XMLName      xml.Name `xml:"BucketLoggingStatus"`
	Agency       string   `xml:"Agency,omitempty"`
	TargetBucket string   `xml:"LoggingEnabled>TargetBucket,omitempty"`
	TargetPrefix string   `xml:"LoggingEnabled>TargetPrefix,omitempty"`
	TargetGrants []Grant  `xml:"LoggingEnabled>TargetGrants>Grant,omitempty"`
}

// Transition defines transition property in LifecycleRule
type Transition struct {
	XMLName      xml.Name         `xml:"Transition" json:"-"`
	Date         time.Time        `xml:"Date,omitempty"`
	Days         int              `xml:"Days,omitempty"`
	StorageClass StorageClassType `xml:"StorageClass"`
}

// Expiration defines expiration property in LifecycleRule
type Expiration struct {
	XMLName                   xml.Name  `xml:"Expiration" json:"-"`
	Date                      time.Time `xml:"Date,omitempty"`
	Days                      int       `xml:"Days,omitempty"`
	ExpiredObjectDeleteMarker string    `xml:"ExpiredObjectDeleteMarker,omitempty"`
}

// NoncurrentVersionTransition defines noncurrentVersion transition property in LifecycleRule
type NoncurrentVersionTransition struct {
	XMLName        xml.Name         `xml:"NoncurrentVersionTransition" json:"-"`
	NoncurrentDays int              `xml:"NoncurrentDays"`
	StorageClass   StorageClassType `xml:"StorageClass"`
}

// NoncurrentVersionExpiration defines noncurrentVersion expiration property in LifecycleRule
type NoncurrentVersionExpiration struct {
	XMLName        xml.Name `xml:"NoncurrentVersionExpiration" json:"-"`
	NoncurrentDays int      `xml:"NoncurrentDays"`
}

// AbortIncompleteMultipartUpload defines abortIncomplete expiration property in LifecycleRule
type AbortIncompleteMultipartUpload struct {
	XMLName             xml.Name `xml:"AbortIncompleteMultipartUpload" json:"-"`
	DaysAfterInitiation int      `xml:"DaysAfterInitiation"`
}

// LifecycleRule defines lifecycle rule
type LifecycleRule struct {
	ID                             string                         `xml:"ID,omitempty"`
	Prefix                         string                         `xml:"Prefix"`
	Status                         RuleStatusType                 `xml:"Status"`
	Transitions                    []Transition                   `xml:"Transition,omitempty"`
	Expiration                     Expiration                     `xml:"Expiration,omitempty"`
	NoncurrentVersionTransitions   []NoncurrentVersionTransition  `xml:"NoncurrentVersionTransition,omitempty"`
	NoncurrentVersionExpiration    NoncurrentVersionExpiration    `xml:"NoncurrentVersionExpiration,omitempty"`
	AbortIncompleteMultipartUpload AbortIncompleteMultipartUpload `xml:"AbortIncompleteMultipartUpload,omitempty"`
	Filter                         LifecycleFilter                `xml:"Filter,omitempty"`
}

type LifecycleFilter struct {
	XMLName xml.Name `xml:"Filter" json:"-"`
	Prefix  string   `xml:"And>Prefix,omitempty"`
	Tags    []Tag    `xml:"And>Tag,omitempty"`
}

// BucketEncryptionConfiguration defines the bucket encryption configuration
type BucketEncryptionConfiguration struct {
	XMLName        xml.Name `xml:"ServerSideEncryptionConfiguration"`
	SSEAlgorithm   string   `xml:"Rule>ApplyServerSideEncryptionByDefault>SSEAlgorithm"`
	KMSMasterKeyID string   `xml:"Rule>ApplyServerSideEncryptionByDefault>KMSMasterKeyID,omitempty"`
	ProjectID      string   `xml:"Rule>ApplyServerSideEncryptionByDefault>ProjectID,omitempty"`
}

// Tag defines tag property in BucketTagging
type Tag struct {
	XMLName xml.Name `xml:"Tag"`
	Key     string   `xml:"Key"`
	Value   string   `xml:"Value"`
}

// BucketTagging defines the bucket tag configuration
type BucketTagging struct {
	XMLName xml.Name `xml:"Tagging"`
	Tags    []Tag    `xml:"TagSet>Tag"`
}

// FilterRule defines filter rule in TopicConfiguration
type FilterRule struct {
	XMLName xml.Name `xml:"FilterRule"`
	Name    string   `xml:"Name,omitempty"`
	Value   string   `xml:"Value,omitempty"`
}

// TopicConfiguration defines the topic configuration
type TopicConfiguration struct {
	XMLName     xml.Name     `xml:"TopicConfiguration"`
	ID          string       `xml:"Id,omitempty"`
	Topic       string       `xml:"Topic"`
	Events      []EventType  `xml:"Event"`
	FilterRules []FilterRule `xml:"Filter>Object>FilterRule"`
}

// BucketNotification defines the bucket notification configuration
type BucketNotification struct {
	XMLName             xml.Name             `xml:"NotificationConfiguration"`
	TopicConfigurations []TopicConfiguration `xml:"TopicConfiguration"`
}

type topicConfigurationS3 struct {
	XMLName     xml.Name     `xml:"TopicConfiguration"`
	ID          string       `xml:"Id,omitempty"`
	Topic       string       `xml:"Topic"`
	Events      []string     `xml:"Event"`
	FilterRules []FilterRule `xml:"Filter>S3Key>FilterRule"`
}

type bucketNotificationS3 struct {
	XMLName             xml.Name               `xml:"NotificationConfiguration"`
	TopicConfigurations []topicConfigurationS3 `xml:"TopicConfiguration"`
}

// ObjectToDelete defines the object property in DeleteObjectsInput
type ObjectToDelete struct {
	XMLName   xml.Name `xml:"Object"`
	Key       string   `xml:"Key"`
	VersionId string   `xml:"VersionId,omitempty"`
}

// Deleted defines the deleted property in DeleteObjectsOutput
type Deleted struct {
	XMLName               xml.Name `xml:"Deleted"`
	Key                   string   `xml:"Key"`
	VersionId             string   `xml:"VersionId"`
	DeleteMarker          bool     `xml:"DeleteMarker"`
	DeleteMarkerVersionId string   `xml:"DeleteMarkerVersionId"`
}

// Part defines the part properties
type Part struct {
	XMLName      xml.Name  `xml:"Part"`
	PartNumber   int       `xml:"PartNumber"`
	ETag         string    `xml:"ETag"`
	LastModified time.Time `xml:"LastModified,omitempty"`
	Size         int64     `xml:"Size,omitempty"`
}

// BucketPayer defines the request payment configuration
type BucketPayer struct {
	XMLName xml.Name  `xml:"RequestPaymentConfiguration"`
	Payer   PayerType `xml:"Payer"`
}

type PublicAccessBlockConfiguration struct {
	XMLName               xml.Name `xml:"PublicAccessBlockConfiguration"`
	BlockPublicAcls       bool     `xml:"BlockPublicAcls"`
	IgnorePublicAcls      bool     `xml:"IgnorePublicAcls"`
	BlockPublicPolicy     bool     `xml:"BlockPublicPolicy"`
	RestrictPublicBuckets bool     `xml:"RestrictPublicBuckets"`
}

type PolicyPublicStatus struct {
	XMLName  xml.Name `xml:"PolicyStatus"`
	IsPublic bool     `xml:"IsPublic"`
}

type BucketPublicStatus struct {
	XMLName  xml.Name `xml:"BucketStatus"`
	IsPublic bool     `xml:"IsPublic"`
}

// HttpHeader defines the standard metadata
type HttpHeader struct {
	CacheControl       string
	ContentDisposition string
	ContentEncoding    string
	ContentLanguage    string
	ContentType        string
	HttpExpires        string
}
