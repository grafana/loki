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
	"io"
	"time"
)

// ListObjsInput defines parameters for listing objects
type ListObjsInput struct {
	Prefix        string
	MaxKeys       int
	Delimiter     string
	Origin        string
	RequestHeader string
	EncodingType  string
}

// ListObjectsInput is the input parameter of ListObjects function
type ListObjectsInput struct {
	ListObjsInput
	Bucket string
	Marker string
}

type ListPosixObjectsInput struct {
	ListObjectsInput
}

// ListObjectsOutput is the result of ListObjects function
type ListObjectsOutput struct {
	BaseModel
	XMLName        xml.Name  `xml:"ListBucketResult"`
	Delimiter      string    `xml:"Delimiter"`
	IsTruncated    bool      `xml:"IsTruncated"`
	Marker         string    `xml:"Marker"`
	NextMarker     string    `xml:"NextMarker"`
	MaxKeys        int       `xml:"MaxKeys"`
	Name           string    `xml:"Name"`
	Prefix         string    `xml:"Prefix"`
	Contents       []Content `xml:"Contents"`
	CommonPrefixes []string  `xml:"CommonPrefixes>Prefix"`
	Location       string    `xml:"-"`
	EncodingType   string    `xml:"EncodingType,omitempty"`
}

type ListPosixObjectsOutput struct {
	ListObjectsOutput
	CommonPrefixes []CommonPrefix `xml:"CommonPrefixes"`
}

type CommonPrefix struct {
	XMLName      xml.Name  `xml:"CommonPrefixes"`
	Prefix       string    `xml:"Prefix"`
	MTime        string    `xml:"MTime"`
	Mode         string    `xml:"Mode"`
	InodeNo      string    `xml:"InodeNo"`
	LastModified time.Time `xml:"LastModified"`
}

// ListVersionsInput is the input parameter of ListVersions function
type ListVersionsInput struct {
	ListObjsInput
	Bucket          string
	KeyMarker       string
	VersionIdMarker string
}

// ListVersionsOutput is the result of ListVersions function
type ListVersionsOutput struct {
	BaseModel
	XMLName             xml.Name       `xml:"ListVersionsResult"`
	Delimiter           string         `xml:"Delimiter"`
	IsTruncated         bool           `xml:"IsTruncated"`
	KeyMarker           string         `xml:"KeyMarker"`
	NextKeyMarker       string         `xml:"NextKeyMarker"`
	VersionIdMarker     string         `xml:"VersionIdMarker"`
	NextVersionIdMarker string         `xml:"NextVersionIdMarker"`
	MaxKeys             int            `xml:"MaxKeys"`
	Name                string         `xml:"Name"`
	Prefix              string         `xml:"Prefix"`
	Versions            []Version      `xml:"Version"`
	DeleteMarkers       []DeleteMarker `xml:"DeleteMarker"`
	CommonPrefixes      []string       `xml:"CommonPrefixes>Prefix"`
	Location            string         `xml:"-"`
	EncodingType        string         `xml:"EncodingType,omitempty"`
}

// DeleteObjectInput is the input parameter of DeleteObject function
type DeleteObjectInput struct {
	Bucket    string
	Key       string
	VersionId string
}

// DeleteObjectOutput is the result of DeleteObject function
type DeleteObjectOutput struct {
	BaseModel
	VersionId    string
	DeleteMarker bool
}

// DeleteObjectsInput is the input parameter of DeleteObjects function
type DeleteObjectsInput struct {
	Bucket       string           `xml:"-"`
	XMLName      xml.Name         `xml:"Delete"`
	Quiet        bool             `xml:"Quiet,omitempty"`
	Objects      []ObjectToDelete `xml:"Object"`
	EncodingType string           `xml:"EncodingType"`
}

// DeleteObjectsOutput is the result of DeleteObjects function
type DeleteObjectsOutput struct {
	BaseModel
	XMLName      xml.Name  `xml:"DeleteResult"`
	Deleteds     []Deleted `xml:"Deleted"`
	Errors       []Error   `xml:"Error"`
	EncodingType string    `xml:"EncodingType,omitempty"`
}

// SetObjectAclInput is the input parameter of SetObjectAcl function
type SetObjectAclInput struct {
	Bucket    string  `xml:"-"`
	Key       string  `xml:"-"`
	VersionId string  `xml:"-"`
	ACL       AclType `xml:"-"`
	AccessControlPolicy
}

// GetObjectAclInput is the input parameter of GetObjectAcl function
type GetObjectAclInput struct {
	Bucket    string
	Key       string
	VersionId string
}

// GetObjectAclOutput is the result of GetObjectAcl function
type GetObjectAclOutput struct {
	BaseModel
	VersionId string
	AccessControlPolicy
}

// RestoreObjectInput is the input parameter of RestoreObject function
type RestoreObjectInput struct {
	Bucket    string          `xml:"-"`
	Key       string          `xml:"-"`
	VersionId string          `xml:"-"`
	XMLName   xml.Name        `xml:"RestoreRequest"`
	Days      int             `xml:"Days"`
	Tier      RestoreTierType `xml:"GlacierJobParameters>Tier,omitempty"`
}

// GetObjectMetadataInput is the input parameter of GetObjectMetadata function
type GetObjectMetadataInput struct {
	Bucket        string
	Key           string
	VersionId     string
	Origin        string
	RequestHeader string
	SseHeader     ISseHeader
}

// GetObjectMetadataOutput is the result of GetObjectMetadata function
type GetObjectMetadataOutput struct {
	BaseModel
	HttpHeader
	VersionId               string
	WebsiteRedirectLocation string
	Expiration              string
	Restore                 string
	ObjectType              string
	NextAppendPosition      string
	StorageClass            StorageClassType
	ContentLength           int64
	ETag                    string
	AllowOrigin             string
	AllowHeader             string
	AllowMethod             string
	ExposeHeader            string
	MaxAgeSeconds           int
	LastModified            time.Time
	SseHeader               ISseHeader
	Metadata                map[string]string
}

type GetAttributeInput struct {
	GetObjectMetadataInput
	RequestPayer string
}

type GetAttributeOutput struct {
	GetObjectMetadataOutput
	Mode int
}

// GetObjectInput is the input parameter of GetObject function
type GetObjectInput struct {
	GetObjectMetadataInput
	IfMatch                    string
	IfNoneMatch                string
	AcceptEncoding             string
	IfUnmodifiedSince          time.Time
	IfModifiedSince            time.Time
	RangeStart                 int64
	RangeEnd                   int64
	Range                      string
	ImageProcess               string
	ResponseCacheControl       string
	ResponseContentDisposition string
	ResponseContentEncoding    string
	ResponseContentLanguage    string
	ResponseContentType        string
	ResponseExpires            string
}

// GetObjectOutput is the result of GetObject function
type GetObjectOutput struct {
	GetObjectMetadataOutput
	DeleteMarker bool
	Expires      string
	Body         io.ReadCloser
}

// ObjectOperationInput defines the object operation properties
type ObjectOperationInput struct {
	Bucket                  string
	Key                     string
	ACL                     AclType
	GrantReadId             string
	GrantReadAcpId          string
	GrantWriteAcpId         string
	GrantFullControlId      string
	StorageClass            StorageClassType
	WebsiteRedirectLocation string
	Expires                 int64
	SseHeader               ISseHeader
	Metadata                map[string]string
}

// PutObjectBasicInput defines the basic object operation properties
type PutObjectBasicInput struct {
	ObjectOperationInput
	HttpHeader
	ContentMD5    string
	ContentSHA256 string
	ContentLength int64
}

// PutObjectInput is the input parameter of PutObject function
type PutObjectInput struct {
	PutObjectBasicInput
	Body io.Reader
}

type NewFolderInput struct {
	ObjectOperationInput
	RequestPayer string
}

type NewFolderOutput struct {
	PutObjectOutput
}

// PutFileInput is the input parameter of PutFile function
type PutFileInput struct {
	PutObjectBasicInput
	SourceFile string
}

// PutObjectOutput is the result of PutObject function
type PutObjectOutput struct {
	BaseModel
	VersionId    string
	SseHeader    ISseHeader
	StorageClass StorageClassType
	ETag         string
	ObjectUrl    string
	CallbackBody
}

// CopyObjectInput is the input parameter of CopyObject function
type CopyObjectInput struct {
	ObjectOperationInput
	CopySourceBucket            string
	CopySourceKey               string
	CopySourceVersionId         string
	CopySourceIfMatch           string
	CopySourceIfNoneMatch       string
	CopySourceIfUnmodifiedSince time.Time
	CopySourceIfModifiedSince   time.Time
	SourceSseHeader             ISseHeader
	Expires                     string
	MetadataDirective           MetadataDirectiveType
	SuccessActionRedirect       string
	HttpHeader
}

// CopyObjectOutput is the result of CopyObject function
type CopyObjectOutput struct {
	BaseModel
	CopySourceVersionId string     `xml:"-"`
	VersionId           string     `xml:"-"`
	SseHeader           ISseHeader `xml:"-"`
	XMLName             xml.Name   `xml:"CopyObjectResult"`
	LastModified        time.Time  `xml:"LastModified"`
	ETag                string     `xml:"ETag"`
}

// UploadFileInput is the input parameter of UploadFile function
type UploadFileInput struct {
	ObjectOperationInput
	UploadFile       string
	PartSize         int64
	TaskNum          int
	EnableCheckpoint bool
	CheckpointFile   string
	EncodingType     string
	HttpHeader
}

// DownloadFileInput is the input parameter of DownloadFile function
type DownloadFileInput struct {
	GetObjectMetadataInput
	IfMatch           string
	IfNoneMatch       string
	IfModifiedSince   time.Time
	IfUnmodifiedSince time.Time
	DownloadFile      string
	PartSize          int64
	TaskNum           int
	EnableCheckpoint  bool
	CheckpointFile    string
}

type AppendObjectInput struct {
	PutObjectBasicInput
	Body     io.Reader
	Position int64
}

type AppendObjectOutput struct {
	BaseModel
	VersionId          string
	SseHeader          ISseHeader
	NextAppendPosition int64
	ETag               string
}

type ModifyObjectInput struct {
	Bucket        string
	Key           string
	Position      int64
	Body          io.Reader
	ContentLength int64
}

type ModifyObjectOutput struct {
	BaseModel
	ETag string
}

// HeadObjectInput is the input parameter of HeadObject function
type HeadObjectInput struct {
	Bucket    string
	Key       string
	VersionId string
}

type RenameFileInput struct {
	Bucket       string
	Key          string
	NewObjectKey string
	RequestPayer string
}

type RenameFileOutput struct {
	BaseModel
}

type RenameFolderInput struct {
	Bucket       string
	Key          string
	NewObjectKey string
	RequestPayer string
}

type RenameFolderOutput struct {
	BaseModel
}

// SetObjectMetadataInput is the input parameter of SetObjectMetadata function
type SetObjectMetadataInput struct {
	Bucket                  string
	Key                     string
	VersionId               string
	MetadataDirective       MetadataDirectiveType
	Expires                 string
	WebsiteRedirectLocation string
	StorageClass            StorageClassType
	Metadata                map[string]string
	HttpHeader
}

// SetObjectMetadataOutput is the result of SetObjectMetadata function
type SetObjectMetadataOutput struct {
	BaseModel
	MetadataDirective MetadataDirectiveType
	HttpHeader
	Expires                 string
	WebsiteRedirectLocation string
	StorageClass            StorageClassType
	Metadata                map[string]string
}

type CallbackInput struct {
	CallbackUrl      string `json:"callbackUrl"`
	CallbackHost     string `json:"callbackHost,omitempty"`
	CallbackBody     string `json:"callbackBody"`
	CallbackBodyType string `json:"callbackBodyType,omitempty"`
}
