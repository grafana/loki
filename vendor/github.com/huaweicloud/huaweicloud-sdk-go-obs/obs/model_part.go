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

// ListMultipartUploadsInput is the input parameter of ListMultipartUploads function
type ListMultipartUploadsInput struct {
	Bucket         string
	Prefix         string
	MaxUploads     int
	Delimiter      string
	KeyMarker      string
	UploadIdMarker string
	EncodingType   string
}

// ListMultipartUploadsOutput is the result of ListMultipartUploads function
type ListMultipartUploadsOutput struct {
	BaseModel
	XMLName            xml.Name `xml:"ListMultipartUploadsResult"`
	Bucket             string   `xml:"Bucket"`
	KeyMarker          string   `xml:"KeyMarker"`
	NextKeyMarker      string   `xml:"NextKeyMarker"`
	UploadIdMarker     string   `xml:"UploadIdMarker"`
	NextUploadIdMarker string   `xml:"NextUploadIdMarker"`
	Delimiter          string   `xml:"Delimiter"`
	IsTruncated        bool     `xml:"IsTruncated"`
	MaxUploads         int      `xml:"MaxUploads"`
	Prefix             string   `xml:"Prefix"`
	Uploads            []Upload `xml:"Upload"`
	CommonPrefixes     []string `xml:"CommonPrefixes>Prefix"`
	EncodingType       string   `xml:"EncodingType,omitempty"`
}

// AbortMultipartUploadInput is the input parameter of AbortMultipartUpload function
type AbortMultipartUploadInput struct {
	Bucket   string
	Key      string
	UploadId string
}

// InitiateMultipartUploadInput is the input parameter of InitiateMultipartUpload function
type InitiateMultipartUploadInput struct {
	ObjectOperationInput
	HttpHeader
	EncodingType string
}

// InitiateMultipartUploadOutput is the result of InitiateMultipartUpload function
type InitiateMultipartUploadOutput struct {
	BaseModel
	XMLName      xml.Name `xml:"InitiateMultipartUploadResult"`
	Bucket       string   `xml:"Bucket"`
	Key          string   `xml:"Key"`
	UploadId     string   `xml:"UploadId"`
	SseHeader    ISseHeader
	EncodingType string `xml:"EncodingType,omitempty"`
}

// UploadPartInput is the input parameter of UploadPart function
type UploadPartInput struct {
	Bucket        string
	Key           string
	PartNumber    int
	UploadId      string
	ContentMD5    string
	ContentSHA256 string
	SseHeader     ISseHeader
	Body          io.Reader
	SourceFile    string
	Offset        int64
	PartSize      int64
}

// UploadPartOutput is the result of UploadPart function
type UploadPartOutput struct {
	BaseModel
	PartNumber int
	ETag       string
	SseHeader  ISseHeader
}

// CompleteMultipartUploadInput is the input parameter of CompleteMultipartUpload function
type CompleteMultipartUploadInput struct {
	Bucket       string   `xml:"-"`
	Key          string   `xml:"-"`
	UploadId     string   `xml:"-"`
	XMLName      xml.Name `xml:"CompleteMultipartUpload"`
	Parts        []Part   `xml:"Part"`
	EncodingType string   `xml:"-"`
}

// CompleteMultipartUploadOutput is the result of CompleteMultipartUpload function
type CompleteMultipartUploadOutput struct {
	BaseModel
	VersionId    string     `xml:"-"`
	SseHeader    ISseHeader `xml:"-"`
	XMLName      xml.Name   `xml:"CompleteMultipartUploadResult"`
	Location     string     `xml:"Location"`
	Bucket       string     `xml:"Bucket"`
	Key          string     `xml:"Key"`
	ETag         string     `xml:"ETag"`
	EncodingType string     `xml:"EncodingType,omitempty"`
	CallbackBody
}

// ListPartsInput is the input parameter of ListParts function
type ListPartsInput struct {
	Bucket           string
	Key              string
	UploadId         string
	MaxParts         int
	PartNumberMarker int
	EncodingType     string
}

// ListPartsOutput is the result of ListParts function
type ListPartsOutput struct {
	BaseModel
	XMLName              xml.Name         `xml:"ListPartsResult"`
	Bucket               string           `xml:"Bucket"`
	Key                  string           `xml:"Key"`
	UploadId             string           `xml:"UploadId"`
	PartNumberMarker     int              `xml:"PartNumberMarker"`
	NextPartNumberMarker int              `xml:"NextPartNumberMarker"`
	MaxParts             int              `xml:"MaxParts"`
	IsTruncated          bool             `xml:"IsTruncated"`
	StorageClass         StorageClassType `xml:"StorageClass"`
	Initiator            Initiator        `xml:"Initiator"`
	Owner                Owner            `xml:"Owner"`
	Parts                []Part           `xml:"Part"`
	EncodingType         string           `xml:"EncodingType,omitempty"`
}

// CopyPartInput is the input parameter of CopyPart function
type CopyPartInput struct {
	Bucket               string
	Key                  string
	UploadId             string
	PartNumber           int
	CopySourceBucket     string
	CopySourceKey        string
	CopySourceVersionId  string
	CopySourceRangeStart int64
	CopySourceRangeEnd   int64
	CopySourceRange      string
	SseHeader            ISseHeader
	SourceSseHeader      ISseHeader
}

// CopyPartOutput is the result of CopyPart function
type CopyPartOutput struct {
	BaseModel
	XMLName      xml.Name   `xml:"CopyPartResult"`
	PartNumber   int        `xml:"-"`
	ETag         string     `xml:"ETag"`
	LastModified time.Time  `xml:"LastModified"`
	SseHeader    ISseHeader `xml:"-"`
}
