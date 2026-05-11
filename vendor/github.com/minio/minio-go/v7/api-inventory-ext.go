/*
 * MinIO Go Library for Amazon S3 Compatible Cloud Storage
 * Copyright 2015-2025 MinIO, Inc.
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
	"context"
	"encoding/json"
	"io"
	"iter"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/minio/minio-go/v7/pkg/s3utils"
)

// This file contains the inventory API extension for MinIO server. It is not
// compatible with AWS S3.

func makeInventoryReqMetadata(bucket string, urlParams ...string) requestMetadata {
	urlValues := make(url.Values)
	urlValues.Set("minio-inventory", "")

	// If an odd number of parameters is given, we skip the last pair to avoid
	// an out of bounds access.
	for i := 0; i+1 < len(urlParams); i += 2 {
		urlValues.Set(urlParams[i], urlParams[i+1])
	}

	return requestMetadata{
		bucketName:  bucket,
		queryValues: urlValues,
	}
}

// GenerateInventoryConfigYAML generates a YAML template for an inventory configuration.
// This is a MinIO-specific API and is not compatible with AWS S3.
//
// Parameters:
//   - ctx: Context for request cancellation and timeout
//   - bucket: Name of the bucket
//   - id: Unique identifier for the inventory configuration
//
// Returns a YAML template string that can be customized and used with PutBucketInventoryConfiguration.
func (c *Client) GenerateInventoryConfigYAML(ctx context.Context, bucket, id string) (string, error) {
	if err := s3utils.CheckValidBucketName(bucket); err != nil {
		return "", err
	}
	if id == "" {
		return "", errInvalidArgument("inventory ID cannot be empty")
	}
	reqMeta := makeInventoryReqMetadata(bucket, "generate", "", "id", id)
	resp, err := c.executeMethod(ctx, http.MethodGet, reqMeta)
	defer closeResponse(resp)
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", httpRespToErrorResponse(resp, bucket, "")
	}
	buf := new(strings.Builder)
	_, err = io.Copy(buf, resp.Body)
	return buf.String(), err
}

// inventoryPutConfigOpts is a placeholder for future options that may be added.
type inventoryPutConfigOpts struct{}

// InventoryPutConfigOption is to allow for functional options for
// PutBucketInventoryConfiguration. It may be used in the future to customize
// the PutBucketInventoryConfiguration request, but currently does not do
// anything.
type InventoryPutConfigOption func(*inventoryPutConfigOpts)

// PutBucketInventoryConfiguration creates or updates an inventory configuration for a bucket.
// This is a MinIO-specific API and is not compatible with AWS S3.
//
// Parameters:
//   - ctx: Context for request cancellation and timeout
//   - bucket: Name of the bucket
//   - id: Unique identifier for the inventory configuration
//   - yamlDef: YAML definition of the inventory configuration
//
// Returns an error if the operation fails, or if bucket name, id, or yamlDef is empty.
func (c *Client) PutBucketInventoryConfiguration(ctx context.Context, bucket string, id string, yamlDef string, _ ...InventoryPutConfigOption) error {
	if err := s3utils.CheckValidBucketName(bucket); err != nil {
		return err
	}
	if id == "" {
		return errInvalidArgument("inventory ID cannot be empty")
	}
	if yamlDef == "" {
		return errInvalidArgument("YAML definition cannot be empty")
	}
	reqMeta := makeInventoryReqMetadata(bucket, "id", id)
	reqMeta.contentBody = strings.NewReader(yamlDef)
	reqMeta.contentLength = int64(len(yamlDef))
	reqMeta.contentMD5Base64 = sumMD5Base64([]byte(yamlDef))

	resp, err := c.executeMethod(ctx, http.MethodPut, reqMeta)
	defer closeResponse(resp)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return httpRespToErrorResponse(resp, bucket, "")
	}
	return nil
}

// GetBucketInventoryConfiguration retrieves the inventory configuration for a bucket.
// This is a MinIO-specific API and is not compatible with AWS S3.
//
// Parameters:
//   - ctx: Context for request cancellation and timeout
//   - bucket: Name of the bucket
//   - id: Unique identifier for the inventory configuration
//
// Returns the inventory configuration or an error if the operation fails or if the configuration doesn't exist.
func (c *Client) GetBucketInventoryConfiguration(ctx context.Context, bucket, id string) (*InventoryConfiguration, error) {
	if err := s3utils.CheckValidBucketName(bucket); err != nil {
		return nil, err
	}
	if id == "" {
		return nil, errInvalidArgument("inventory ID cannot be empty")
	}
	reqMeta := makeInventoryReqMetadata(bucket, "id", id)
	resp, err := c.executeMethod(ctx, http.MethodGet, reqMeta)
	defer closeResponse(resp)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, httpRespToErrorResponse(resp, bucket, "")
	}
	decoder := json.NewDecoder(resp.Body)
	var ic InventoryConfiguration
	err = decoder.Decode(&ic)
	if err != nil {
		return nil, err
	}
	return &ic, nil
}

// DeleteBucketInventoryConfiguration deletes an inventory configuration from a bucket.
// This is a MinIO-specific API and is not compatible with AWS S3.
//
// Parameters:
//   - ctx: Context for request cancellation and timeout
//   - bucket: Name of the bucket
//   - id: Unique identifier for the inventory configuration to delete
//
// Returns an error if the operation fails or if the configuration doesn't exist.
func (c *Client) DeleteBucketInventoryConfiguration(ctx context.Context, bucket, id string) error {
	if err := s3utils.CheckValidBucketName(bucket); err != nil {
		return err
	}
	if id == "" {
		return errInvalidArgument("inventory ID cannot be empty")
	}
	reqMeta := makeInventoryReqMetadata(bucket, "id", id)
	resp, err := c.executeMethod(ctx, http.MethodDelete, reqMeta)
	defer closeResponse(resp)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return httpRespToErrorResponse(resp, bucket, "")
	}
	return nil
}

// InventoryConfiguration represents the inventory configuration
type InventoryConfiguration struct {
	Bucket  string `json:"bucket"`
	ID      string `json:"id"`
	User    string `json:"user"`
	YamlDef string `json:"yamlDef,omitempty"`
}

// InventoryListResult represents the result of listing inventory
// configurations.
type InventoryListResult struct {
	Items                 []InventoryConfiguration `json:"items"`
	NextContinuationToken string                   `json:"nextContinuationToken,omitempty"`
}

// ListBucketInventoryConfigurations lists up to 100 inventory configurations for a bucket.
// This is a MinIO-specific API and is not compatible with AWS S3.
//
// Parameters:
//   - ctx: Context for request cancellation and timeout
//   - bucket: Name of the bucket
//   - continuationToken: Token for pagination (empty string for first request)
//
// Returns a list result with configurations and a continuation token for the next page, or an error.
func (c *Client) ListBucketInventoryConfigurations(ctx context.Context, bucket, continuationToken string) (lr *InventoryListResult, err error) {
	if err := s3utils.CheckValidBucketName(bucket); err != nil {
		return nil, err
	}
	reqMeta := makeInventoryReqMetadata(bucket, "continuation-token", continuationToken)
	resp, err := c.executeMethod(ctx, http.MethodGet, reqMeta)
	defer closeResponse(resp)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, httpRespToErrorResponse(resp, bucket, "")
	}

	decoder := json.NewDecoder(resp.Body)
	err = decoder.Decode(&lr)
	if err != nil {
		return nil, err
	}
	return lr, nil
}

// ListBucketInventoryConfigurationsIterator returns an iterator that lists all inventory configurations
// for a bucket. This is a MinIO-specific API and is not compatible with AWS S3.
//
// Parameters:
//   - ctx: Context for request cancellation and timeout
//   - bucket: Name of the bucket
//
// Returns an iterator that yields InventoryConfiguration values and errors. The iterator automatically
// handles pagination and fetches all configurations.
func (c *Client) ListBucketInventoryConfigurationsIterator(ctx context.Context, bucket string) iter.Seq2[InventoryConfiguration, error] {
	return func(yield func(InventoryConfiguration, error) bool) {
		if err := s3utils.CheckValidBucketName(bucket); err != nil {
			yield(InventoryConfiguration{}, err)
			return
		}
		var continuationToken string
		for {
			listResult, err := c.ListBucketInventoryConfigurations(ctx, bucket, continuationToken)
			if err != nil {
				yield(InventoryConfiguration{}, err)
				return
			}

			for _, item := range listResult.Items {
				if !yield(item, nil) {
					return
				}
			}

			if listResult.NextContinuationToken == "" {
				return
			}
			continuationToken = listResult.NextContinuationToken
		}
	}
}

// InventoryJobStatus represents the status of an inventory job.
type InventoryJobStatus struct {
	Bucket            string    `json:"bucket"`
	ID                string    `json:"id"`
	User              string    `json:"user"`
	AccessKey         string    `json:"accessKey"`
	Schedule          string    `json:"schedule"`
	State             string    `json:"state"`
	NextScheduledTime time.Time `json:"nextScheduledTime,omitempty"`
	StartTime         time.Time `json:"startTime,omitempty"`
	EndTime           time.Time `json:"endTime,omitempty"`
	LastUpdate        time.Time `json:"lastUpdate,omitempty"`
	Scanned           string    `json:"scanned,omitempty"`
	Matched           string    `json:"matched,omitempty"`
	ScannedCount      uint64    `json:"scannedCount,omitempty"`
	MatchedCount      uint64    `json:"matchedCount,omitempty"`
	RecordsWritten    uint64    `json:"recordsWritten,omitempty"`
	OutputFilesCount  uint64    `json:"outputFilesCount,omitempty"`
	ExecutionTime     string    `json:"executionTime,omitempty"`
	NumStarts         uint64    `json:"numStarts,omitempty"`
	NumErrors         uint64    `json:"numErrors,omitempty"`
	NumLockLosses     uint64    `json:"numLockLosses,omitempty"`
	ManifestPath      string    `json:"manifestPath,omitempty"`
	RetryAttempts     uint64    `json:"retryAttempts,omitempty"`
	LastFailTime      time.Time `json:"lastFailTime,omitempty"`
	LastFailErrors    []string  `json:"lastFailErrors,omitempty"`
}

// GetBucketInventoryJobStatus retrieves the status of an inventory job for a bucket.
// This is a MinIO-specific API and is not compatible with AWS S3.
//
// Parameters:
//   - ctx: Context for request cancellation and timeout
//   - bucket: Name of the bucket
//   - id: Unique identifier for the inventory job
//
// Returns the inventory job status including execution state, progress, and error information, or an error if the operation fails.
func (c *Client) GetBucketInventoryJobStatus(ctx context.Context, bucket, id string) (*InventoryJobStatus, error) {
	if err := s3utils.CheckValidBucketName(bucket); err != nil {
		return nil, err
	}
	if id == "" {
		return nil, errInvalidArgument("inventory ID cannot be empty")
	}
	reqMeta := makeInventoryReqMetadata(bucket, "id", id, "status", "")
	resp, err := c.executeMethod(ctx, http.MethodGet, reqMeta)
	defer closeResponse(resp)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, httpRespToErrorResponse(resp, bucket, "")
	}
	decoder := json.NewDecoder(resp.Body)
	var jStatus InventoryJobStatus
	err = decoder.Decode(&jStatus)
	if err != nil {
		return nil, err
	}
	return &jStatus, nil
}
