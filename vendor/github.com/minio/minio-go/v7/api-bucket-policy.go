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
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/minio/minio-go/v7/pkg/s3utils"
)

// SetBucketPolicy sets the access permissions policy on an existing bucket.
// The policy should be a valid JSON string that conforms to the IAM policy format.
// If policy is an empty string, the existing bucket policy will be removed.
//
// Parameters:
//   - ctx: Context for request cancellation and timeout
//   - bucketName: Name of the bucket
//   - policy: JSON policy string (empty string to remove existing policy)
//
// Returns an error if the operation fails.
func (c *Client) SetBucketPolicy(ctx context.Context, bucketName, policy string) error {
	// Input validation.
	if err := s3utils.CheckValidBucketName(bucketName); err != nil {
		return err
	}

	// If policy is empty then delete the bucket policy.
	if policy == "" {
		return c.removeBucketPolicy(ctx, bucketName)
	}

	// Save the updated policies.
	return c.putBucketPolicy(ctx, bucketName, policy)
}

// Saves a new bucket policy.
func (c *Client) putBucketPolicy(ctx context.Context, bucketName, policy string) error {
	// Get resources properly escaped and lined up before
	// using them in http request.
	urlValues := make(url.Values)
	urlValues.Set("policy", "")

	reqMetadata := requestMetadata{
		bucketName:    bucketName,
		queryValues:   urlValues,
		contentBody:   strings.NewReader(policy),
		contentLength: int64(len(policy)),
	}

	// Execute PUT to upload a new bucket policy.
	resp, err := c.executeMethod(ctx, http.MethodPut, reqMetadata)
	defer closeResponse(resp)
	if err != nil {
		return err
	}
	if resp != nil {
		if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
			return httpRespToErrorResponse(resp, bucketName, "")
		}
	}
	return nil
}

// Removes all policies on a bucket.
func (c *Client) removeBucketPolicy(ctx context.Context, bucketName string) error {
	// Get resources properly escaped and lined up before
	// using them in http request.
	urlValues := make(url.Values)
	urlValues.Set("policy", "")

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

	if resp.StatusCode != http.StatusNoContent {
		return httpRespToErrorResponse(resp, bucketName, "")
	}

	return nil
}

// GetBucketPolicy retrieves the access permissions policy for the bucket.
// If no bucket policy exists, returns an empty string with no error.
//
// Parameters:
//   - ctx: Context for request cancellation and timeout
//   - bucketName: Name of the bucket
//
// Returns the policy as a JSON string or an error if the operation fails.
func (c *Client) GetBucketPolicy(ctx context.Context, bucketName string) (string, error) {
	// Input validation.
	if err := s3utils.CheckValidBucketName(bucketName); err != nil {
		return "", err
	}
	bucketPolicy, err := c.getBucketPolicy(ctx, bucketName)
	if err != nil {
		errResponse := ToErrorResponse(err)
		if errResponse.Code == NoSuchBucketPolicy {
			return "", nil
		}
		return "", err
	}
	return bucketPolicy, nil
}

// Request server for current bucket policy.
func (c *Client) getBucketPolicy(ctx context.Context, bucketName string) (string, error) {
	// Get resources properly escaped and lined up before
	// using them in http request.
	urlValues := make(url.Values)
	urlValues.Set("policy", "")

	// Execute GET on bucket to list objects.
	resp, err := c.executeMethod(ctx, http.MethodGet, requestMetadata{
		bucketName:       bucketName,
		queryValues:      urlValues,
		contentSHA256Hex: emptySHA256Hex,
	})

	defer closeResponse(resp)
	if err != nil {
		return "", err
	}

	if resp != nil {
		if resp.StatusCode != http.StatusOK {
			return "", httpRespToErrorResponse(resp, bucketName, "")
		}
	}

	bucketPolicyBuf, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	policy := string(bucketPolicyBuf)
	return policy, err
}
