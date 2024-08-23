/*
 * MinIO Go Library for Amazon S3 Compatible Cloud Storage
 * Copyright 2020 MinIO, Inc.
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package minio

import (
	"context"
	"encoding/xml"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/minio/minio-go/v7/pkg/encrypt"
	"github.com/minio/minio-go/v7/pkg/s3utils"
)

// ObjectAttributesOptions are options used for the GetObjectAttributes API
//
// - MaxParts
// How many parts the caller wants to be returned (default: 1000)
//
// - VersionID
// The object version you want to attributes for
//
// - PartNumberMarker
// the listing will start AFTER the part matching PartNumberMarker
//
// - ServerSideEncryption
// The server-side encryption algorithm used when storing this object in Minio
type ObjectAttributesOptions struct {
	MaxParts             int
	VersionID            string
	PartNumberMarker     int
	ServerSideEncryption encrypt.ServerSide
}

// ObjectAttributes is the response object returned by the GetObjectAttributes API
//
// - VersionID
// The object version
//
// - LastModified
// The last time the object was modified
//
// - ObjectAttributesResponse
// Contains more information about the object
type ObjectAttributes struct {
	VersionID    string
	LastModified time.Time
	ObjectAttributesResponse
}

// ObjectAttributesResponse contains details returned by the GetObjectAttributes API
//
// Noteworthy fields:
//
// - ObjectParts.PartsCount
// Contains the total part count for the object (not the current response)
//
// - ObjectParts.PartNumberMarker
// Pagination of parts will begin at (but not include) PartNumberMarker
//
// - ObjectParts.NextPartNumberMarket
// The next PartNumberMarker to be used in order to continue pagination
//
// - ObjectParts.IsTruncated
// Indicates if the last part is included in the request (does not check if parts are missing from the start of the list, ONLY the end)
//
// - ObjectParts.MaxParts
// Reflects the MaxParts used by the caller or the default MaxParts value of the API
type ObjectAttributesResponse struct {
	ETag         string `xml:",omitempty"`
	StorageClass string
	ObjectSize   int
	Checksum     struct {
		ChecksumCRC32  string `xml:",omitempty"`
		ChecksumCRC32C string `xml:",omitempty"`
		ChecksumSHA1   string `xml:",omitempty"`
		ChecksumSHA256 string `xml:",omitempty"`
	}
	ObjectParts struct {
		PartsCount           int
		PartNumberMarker     int
		NextPartNumberMarker int
		MaxParts             int
		IsTruncated          bool
		Parts                []*ObjectAttributePart `xml:"Part"`
	}
}

// ObjectAttributePart is used by ObjectAttributesResponse to describe an object part
type ObjectAttributePart struct {
	ChecksumCRC32  string `xml:",omitempty"`
	ChecksumCRC32C string `xml:",omitempty"`
	ChecksumSHA1   string `xml:",omitempty"`
	ChecksumSHA256 string `xml:",omitempty"`
	PartNumber     int
	Size           int
}

func (o *ObjectAttributes) parseResponse(resp *http.Response) (err error) {
	mod, err := parseRFC7231Time(resp.Header.Get("Last-Modified"))
	if err != nil {
		return err
	}
	o.LastModified = mod
	o.VersionID = resp.Header.Get(amzVersionID)

	response := new(ObjectAttributesResponse)
	if err := xml.NewDecoder(resp.Body).Decode(response); err != nil {
		return err
	}
	o.ObjectAttributesResponse = *response

	return
}

// GetObjectAttributes API combines HeadObject and ListParts.
// More details on usage can be found in the documentation for ObjectAttributesOptions{}
func (c *Client) GetObjectAttributes(ctx context.Context, bucketName, objectName string, opts ObjectAttributesOptions) (*ObjectAttributes, error) {
	if err := s3utils.CheckValidBucketName(bucketName); err != nil {
		return nil, err
	}

	if err := s3utils.CheckValidObjectName(objectName); err != nil {
		return nil, err
	}

	urlValues := make(url.Values)
	urlValues.Add("attributes", "")
	if opts.VersionID != "" {
		urlValues.Add("versionId", opts.VersionID)
	}

	headers := make(http.Header)
	headers.Set(amzObjectAttributes, GetObjectAttributesTags)

	if opts.PartNumberMarker > 0 {
		headers.Set(amzPartNumberMarker, strconv.Itoa(opts.PartNumberMarker))
	}

	if opts.MaxParts > 0 {
		headers.Set(amzMaxParts, strconv.Itoa(opts.MaxParts))
	} else {
		headers.Set(amzMaxParts, strconv.Itoa(GetObjectAttributesMaxParts))
	}

	if opts.ServerSideEncryption != nil {
		opts.ServerSideEncryption.Marshal(headers)
	}

	resp, err := c.executeMethod(ctx, http.MethodGet, requestMetadata{
		bucketName:       bucketName,
		objectName:       objectName,
		queryValues:      urlValues,
		contentSHA256Hex: emptySHA256Hex,
		customHeader:     headers,
	})
	if err != nil {
		return nil, err
	}

	defer closeResponse(resp)

	hasEtag := resp.Header.Get(ETag)
	if hasEtag != "" {
		return nil, errors.New("getObjectAttributes is not supported by the current endpoint version")
	}

	if resp.StatusCode != http.StatusOK {
		ER := new(ErrorResponse)
		if err := xml.NewDecoder(resp.Body).Decode(ER); err != nil {
			return nil, err
		}

		return nil, *ER
	}

	OA := new(ObjectAttributes)
	err = OA.parseResponse(resp)
	if err != nil {
		return nil, err
	}

	return OA, nil
}
