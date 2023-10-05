/*
 * MinIO Go Library for Amazon S3 Compatible Cloud Storage
 * Copyright 2020 MinIO, Inc.
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
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/minio/minio-go/v7/pkg/lifecycle"
	"github.com/minio/minio-go/v7/pkg/s3utils"
)

// SetBucketLifecycle set the lifecycle on an existing bucket.
func (c *Client) SetBucketLifecycle(ctx context.Context, bucketName string, config *lifecycle.Configuration) error {
	// Input validation.
	if err := s3utils.CheckValidBucketName(bucketName); err != nil {
		return err
	}

	// If lifecycle is empty then delete it.
	if config.Empty() {
		return c.removeBucketLifecycle(ctx, bucketName)
	}

	buf, err := xml.Marshal(config)
	if err != nil {
		return err
	}

	// Save the updated lifecycle.
	return c.putBucketLifecycle(ctx, bucketName, buf)
}

// Saves a new bucket lifecycle.
func (c *Client) putBucketLifecycle(ctx context.Context, bucketName string, buf []byte) error {
	// Get resources properly escaped and lined up before
	// using them in http request.
	urlValues := make(url.Values)
	urlValues.Set("lifecycle", "")

	// Content-length is mandatory for put lifecycle request
	reqMetadata := requestMetadata{
		bucketName:       bucketName,
		queryValues:      urlValues,
		contentBody:      bytes.NewReader(buf),
		contentLength:    int64(len(buf)),
		contentMD5Base64: sumMD5Base64(buf),
	}

	// Execute PUT to upload a new bucket lifecycle.
	resp, err := c.executeMethod(ctx, http.MethodPut, reqMetadata)
	defer closeResponse(resp)
	if err != nil {
		return err
	}
	if resp != nil {
		if resp.StatusCode != http.StatusOK {
			return httpRespToErrorResponse(resp, bucketName, "")
		}
	}
	return nil
}

// Remove lifecycle from a bucket.
func (c *Client) removeBucketLifecycle(ctx context.Context, bucketName string) error {
	// Get resources properly escaped and lined up before
	// using them in http request.
	urlValues := make(url.Values)
	urlValues.Set("lifecycle", "")

	// Execute DELETE on objectName.
	resp, err := c.executeMethod(ctx, http.MethodDelete, requestMetadata{
		bucketName:       bucketName,
		queryValues:      urlValues,
		contentSHA256Hex: emptySHA256Hex,
	})
	defer closeResponse(resp)
	if err != nil {
		return err
	}
	return nil
}

// GetBucketLifecycle fetch bucket lifecycle configuration
func (c *Client) GetBucketLifecycle(ctx context.Context, bucketName string) (*lifecycle.Configuration, error) {
	lc, _, err := c.GetBucketLifecycleWithInfo(ctx, bucketName)
	return lc, err
}

// GetBucketLifecycleWithInfo fetch bucket lifecycle configuration along with when it was last updated
func (c *Client) GetBucketLifecycleWithInfo(ctx context.Context, bucketName string) (*lifecycle.Configuration, time.Time, error) {
	// Input validation.
	if err := s3utils.CheckValidBucketName(bucketName); err != nil {
		return nil, time.Time{}, err
	}

	bucketLifecycle, updatedAt, err := c.getBucketLifecycle(ctx, bucketName)
	if err != nil {
		return nil, time.Time{}, err
	}

	config := lifecycle.NewConfiguration()
	if err = xml.Unmarshal(bucketLifecycle, config); err != nil {
		return nil, time.Time{}, err
	}
	return config, updatedAt, nil
}

// Request server for current bucket lifecycle.
func (c *Client) getBucketLifecycle(ctx context.Context, bucketName string) ([]byte, time.Time, error) {
	// Get resources properly escaped and lined up before
	// using them in http request.
	urlValues := make(url.Values)
	urlValues.Set("lifecycle", "")
	urlValues.Set("withUpdatedAt", "true")

	// Execute GET on bucket to get lifecycle.
	resp, err := c.executeMethod(ctx, http.MethodGet, requestMetadata{
		bucketName:  bucketName,
		queryValues: urlValues,
	})

	defer closeResponse(resp)
	if err != nil {
		return nil, time.Time{}, err
	}

	if resp != nil {
		if resp.StatusCode != http.StatusOK {
			return nil, time.Time{}, httpRespToErrorResponse(resp, bucketName, "")
		}
	}

	lcBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, time.Time{}, err
	}

	const minIOLifecycleCfgUpdatedAt = "X-Minio-LifecycleConfig-UpdatedAt"
	var updatedAt time.Time
	if timeStr := resp.Header.Get(minIOLifecycleCfgUpdatedAt); timeStr != "" {
		updatedAt, err = time.Parse(iso8601DateFormat, timeStr)
		if err != nil {
			return nil, time.Time{}, err
		}
	}

	return lcBytes, updatedAt, nil
}
