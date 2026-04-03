/*
 * MinIO Go Library for Amazon S3 Compatible Cloud Storage
 * Copyright 2025-2026 MinIO, Inc.
 *
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
	"bytes"
	"context"
	"encoding/xml"
	"net/http"
	"net/url"

	"github.com/minio/minio-go/v7/pkg/s3utils"
)

// updateObjectEncryptionSSEKMS represents the SSE-KMS element in the request body.
type updateObjectEncryptionSSEKMS struct {
	BucketKeyEnabled bool   `xml:"BucketKeyEnabled,omitempty"`
	KMSKeyArn        string `xml:"KMSKeyArn"`
}

// updateObjectEncryptionRequest represents the XML request body for UpdateObjectEncryption.
type updateObjectEncryptionRequest struct {
	XMLName xml.Name                      `xml:"ObjectEncryption"`
	XMLNS   string                        `xml:"xmlns,attr"`
	SSEKMS  *updateObjectEncryptionSSEKMS `xml:"SSE-KMS"`
}

// UpdateObjectEncryptionOptions holds options for the UpdateObjectEncryption call.
type UpdateObjectEncryptionOptions struct {
	// KMSKeyArn is the KMS key name or ARN to encrypt the object with.
	KMSKeyArn string

	// BucketKeyEnabled enables S3 Bucket Key for KMS encryption.
	BucketKeyEnabled bool

	// VersionID targets a specific object version.
	VersionID string
}

// UpdateObjectEncryption changes the encryption configuration of an existing object in-place.
// The object must already be encrypted with SSE-S3 or SSE-KMS. SSE-C objects are not supported.
// This operation rotates the data encryption key envelope without re-reading/re-writing object data.
//
// Parameters:
//   - ctx: Context for request cancellation and timeout
//   - bucketName: Name of the bucket
//   - objectName: Name of the object
//   - opts: Options including KMSKeyArn (required), optional BucketKeyEnabled, and optional VersionID
//
// Returns an error if the operation fails.
func (c *Client) UpdateObjectEncryption(ctx context.Context, bucketName, objectName string, opts UpdateObjectEncryptionOptions) error {
	// Input validation.
	if err := s3utils.CheckValidBucketName(bucketName); err != nil {
		return err
	}

	if err := s3utils.CheckValidObjectName(objectName); err != nil {
		return err
	}

	if opts.KMSKeyArn == "" {
		return errInvalidArgument("KMSKeyArn is required for UpdateObjectEncryption.")
	}

	// Get resources properly escaped and lined up before
	// using them in http request.
	urlValues := make(url.Values)
	urlValues.Set("encryption", "")

	if opts.VersionID != "" {
		urlValues.Set("versionId", opts.VersionID)
	}

	reqBody := updateObjectEncryptionRequest{
		XMLNS: "http://s3.amazonaws.com/doc/2006-03-01/",
		SSEKMS: &updateObjectEncryptionSSEKMS{
			BucketKeyEnabled: opts.BucketKeyEnabled,
			KMSKeyArn:        opts.KMSKeyArn,
		},
	}

	bodyData, err := xml.Marshal(reqBody)
	if err != nil {
		return err
	}

	reqMetadata := requestMetadata{
		bucketName:       bucketName,
		objectName:       objectName,
		queryValues:      urlValues,
		contentBody:      bytes.NewReader(bodyData),
		contentLength:    int64(len(bodyData)),
		contentMD5Base64: sumMD5Base64(bodyData),
		contentSHA256Hex: sum256Hex(bodyData),
	}

	// Execute PUT Object Encryption.
	resp, err := c.executeMethod(ctx, http.MethodPut, reqMetadata)
	defer closeResponse(resp)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return httpRespToErrorResponse(resp, bucketName, objectName)
	}
	return nil
}
