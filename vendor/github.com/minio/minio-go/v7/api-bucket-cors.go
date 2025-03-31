/*
 * MinIO Go Library for Amazon S3 Compatible Cloud Storage
 * Copyright 2024 MinIO, Inc.
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
	"net/http"
	"net/url"

	"github.com/minio/minio-go/v7/pkg/cors"
	"github.com/minio/minio-go/v7/pkg/s3utils"
)

// SetBucketCors sets the cors configuration for the bucket
func (c *Client) SetBucketCors(ctx context.Context, bucketName string, corsConfig *cors.Config) error {
	if err := s3utils.CheckValidBucketName(bucketName); err != nil {
		return err
	}

	if corsConfig == nil {
		return c.removeBucketCors(ctx, bucketName)
	}

	return c.putBucketCors(ctx, bucketName, corsConfig)
}

func (c *Client) putBucketCors(ctx context.Context, bucketName string, corsConfig *cors.Config) error {
	urlValues := make(url.Values)
	urlValues.Set("cors", "")

	corsStr, err := corsConfig.ToXML()
	if err != nil {
		return err
	}

	reqMetadata := requestMetadata{
		bucketName:       bucketName,
		queryValues:      urlValues,
		contentBody:      bytes.NewReader(corsStr),
		contentLength:    int64(len(corsStr)),
		contentMD5Base64: sumMD5Base64([]byte(corsStr)),
	}

	resp, err := c.executeMethod(ctx, http.MethodPut, reqMetadata)
	defer closeResponse(resp)
	if err != nil {
		return err
	}
	if resp != nil {
		if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
			return httpRespToErrorResponse(resp, bucketName, "")
		}
	}
	return nil
}

func (c *Client) removeBucketCors(ctx context.Context, bucketName string) error {
	urlValues := make(url.Values)
	urlValues.Set("cors", "")

	resp, err := c.executeMethod(ctx, http.MethodDelete, requestMetadata{
		bucketName:       bucketName,
		queryValues:      urlValues,
		contentSHA256Hex: emptySHA256Hex,
	})
	defer closeResponse(resp)
	if err != nil {
		return err
	}

	if resp.StatusCode != http.StatusNoContent {
		return httpRespToErrorResponse(resp, bucketName, "")
	}

	return nil
}

// GetBucketCors returns the current cors
func (c *Client) GetBucketCors(ctx context.Context, bucketName string) (*cors.Config, error) {
	if err := s3utils.CheckValidBucketName(bucketName); err != nil {
		return nil, err
	}
	bucketCors, err := c.getBucketCors(ctx, bucketName)
	if err != nil {
		errResponse := ToErrorResponse(err)
		if errResponse.Code == "NoSuchCORSConfiguration" {
			return nil, nil
		}
		return nil, err
	}
	return bucketCors, nil
}

func (c *Client) getBucketCors(ctx context.Context, bucketName string) (*cors.Config, error) {
	urlValues := make(url.Values)
	urlValues.Set("cors", "")

	resp, err := c.executeMethod(ctx, http.MethodGet, requestMetadata{
		bucketName:       bucketName,
		queryValues:      urlValues,
		contentSHA256Hex: emptySHA256Hex, // TODO: needed? copied over from other example, but not spec'd in API.
	})

	defer closeResponse(resp)
	if err != nil {
		return nil, err
	}

	if resp != nil {
		if resp.StatusCode != http.StatusOK {
			return nil, httpRespToErrorResponse(resp, bucketName, "")
		}
	}

	corsConfig, err := cors.ParseBucketCorsConfig(resp.Body)
	if err != nil {
		return nil, err
	}

	return corsConfig, nil
}
