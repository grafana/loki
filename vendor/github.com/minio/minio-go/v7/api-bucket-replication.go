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
	"encoding/json"
	"encoding/xml"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/google/uuid"
	"github.com/minio/minio-go/v7/pkg/replication"
	"github.com/minio/minio-go/v7/pkg/s3utils"
)

// RemoveBucketReplication removes the replication configuration from an existing bucket.
//
// Parameters:
//   - ctx: Context for request cancellation and timeout
//   - bucketName: Name of the bucket
//
// Returns an error if the operation fails.
func (c *Client) RemoveBucketReplication(ctx context.Context, bucketName string) error {
	return c.removeBucketReplication(ctx, bucketName)
}

// SetBucketReplication sets the replication configuration on an existing bucket.
// If the provided configuration is empty, this method removes the existing replication configuration.
//
// Parameters:
//   - ctx: Context for request cancellation and timeout
//   - bucketName: Name of the bucket
//   - cfg: Replication configuration to apply
//
// Returns an error if the operation fails.
func (c *Client) SetBucketReplication(ctx context.Context, bucketName string, cfg replication.Config) error {
	// Input validation.
	if err := s3utils.CheckValidBucketName(bucketName); err != nil {
		return err
	}

	// If replication is empty then delete it.
	if cfg.Empty() {
		return c.removeBucketReplication(ctx, bucketName)
	}
	// Save the updated replication.
	return c.putBucketReplication(ctx, bucketName, cfg)
}

// Saves a new bucket replication.
func (c *Client) putBucketReplication(ctx context.Context, bucketName string, cfg replication.Config) error {
	// Get resources properly escaped and lined up before
	// using them in http request.
	urlValues := make(url.Values)
	urlValues.Set("replication", "")
	replication, err := xml.Marshal(cfg)
	if err != nil {
		return err
	}

	reqMetadata := requestMetadata{
		bucketName:       bucketName,
		queryValues:      urlValues,
		contentBody:      bytes.NewReader(replication),
		contentLength:    int64(len(replication)),
		contentMD5Base64: sumMD5Base64(replication),
	}

	// Execute PUT to upload a new bucket replication config.
	resp, err := c.executeMethod(ctx, http.MethodPut, reqMetadata)
	defer closeResponse(resp)
	if err != nil {
		return err
	}

	if resp.StatusCode != http.StatusOK {
		return httpRespToErrorResponse(resp, bucketName, "")
	}

	return nil
}

// Remove replication from a bucket.
func (c *Client) removeBucketReplication(ctx context.Context, bucketName string) error {
	// Get resources properly escaped and lined up before
	// using them in http request.
	urlValues := make(url.Values)
	urlValues.Set("replication", "")

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
	if resp.StatusCode != http.StatusOK {
		return httpRespToErrorResponse(resp, bucketName, "")
	}
	return nil
}

// GetBucketReplication retrieves the bucket replication configuration.
// If no replication configuration is found, returns an empty config with nil error.
//
// Parameters:
//   - ctx: Context for request cancellation and timeout
//   - bucketName: Name of the bucket
//
// Returns the replication configuration or an error if the operation fails.
func (c *Client) GetBucketReplication(ctx context.Context, bucketName string) (cfg replication.Config, err error) {
	// Input validation.
	if err := s3utils.CheckValidBucketName(bucketName); err != nil {
		return cfg, err
	}
	bucketReplicationCfg, err := c.getBucketReplication(ctx, bucketName)
	if err != nil {
		errResponse := ToErrorResponse(err)
		if errResponse.Code == "ReplicationConfigurationNotFoundError" {
			return cfg, nil
		}
		return cfg, err
	}
	return bucketReplicationCfg, nil
}

// Request server for current bucket replication config.
func (c *Client) getBucketReplication(ctx context.Context, bucketName string) (cfg replication.Config, err error) {
	// Get resources properly escaped and lined up before
	// using them in http request.
	urlValues := make(url.Values)
	urlValues.Set("replication", "")

	// Execute GET on bucket to get replication config.
	resp, err := c.executeMethod(ctx, http.MethodGet, requestMetadata{
		bucketName:  bucketName,
		queryValues: urlValues,
	})

	defer closeResponse(resp)
	if err != nil {
		return cfg, err
	}

	if resp.StatusCode != http.StatusOK {
		return cfg, httpRespToErrorResponse(resp, bucketName, "")
	}

	if err = xmlDecoder(resp.Body, &cfg); err != nil {
		return cfg, err
	}

	return cfg, nil
}

// GetBucketReplicationMetrics retrieves bucket replication status metrics.
//
// Parameters:
//   - ctx: Context for request cancellation and timeout
//   - bucketName: Name of the bucket
//
// Returns the replication metrics or an error if the operation fails.
func (c *Client) GetBucketReplicationMetrics(ctx context.Context, bucketName string) (s replication.Metrics, err error) {
	// Input validation.
	if err := s3utils.CheckValidBucketName(bucketName); err != nil {
		return s, err
	}
	// Get resources properly escaped and lined up before
	// using them in http request.
	urlValues := make(url.Values)
	urlValues.Set("replication-metrics", "")

	// Execute GET on bucket to get replication config.
	resp, err := c.executeMethod(ctx, http.MethodGet, requestMetadata{
		bucketName:  bucketName,
		queryValues: urlValues,
	})

	defer closeResponse(resp)
	if err != nil {
		return s, err
	}

	if resp.StatusCode != http.StatusOK {
		return s, httpRespToErrorResponse(resp, bucketName, "")
	}
	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return s, err
	}

	if err := json.Unmarshal(respBytes, &s); err != nil {
		return s, err
	}
	return s, nil
}

// mustGetUUID - get a random UUID.
func mustGetUUID() string {
	u, err := uuid.NewRandom()
	if err != nil {
		return ""
	}
	return u.String()
}

// ResetBucketReplication initiates replication of previously replicated objects.
// This requires ExistingObjectReplication to be enabled in the replication configuration.
//
// Parameters:
//   - ctx: Context for request cancellation and timeout
//   - bucketName: Name of the bucket
//   - olderThan: Only replicate objects older than this duration (0 for all objects)
//
// Returns a reset ID that can be used to track the operation, or an error if the operation fails.
func (c *Client) ResetBucketReplication(ctx context.Context, bucketName string, olderThan time.Duration) (rID string, err error) {
	rID = mustGetUUID()
	_, err = c.resetBucketReplicationOnTarget(ctx, bucketName, olderThan, "", rID)
	if err != nil {
		return rID, err
	}
	return rID, nil
}

// ResetBucketReplicationOnTarget initiates replication of previously replicated objects to a specific target.
// This requires ExistingObjectReplication to be enabled in the replication configuration.
//
// Parameters:
//   - ctx: Context for request cancellation and timeout
//   - bucketName: Name of the bucket
//   - olderThan: Only replicate objects older than this duration (0 for all objects)
//   - tgtArn: ARN of the target to reset replication for
//
// Returns resync target information or an error if the operation fails.
func (c *Client) ResetBucketReplicationOnTarget(ctx context.Context, bucketName string, olderThan time.Duration, tgtArn string) (replication.ResyncTargetsInfo, error) {
	return c.resetBucketReplicationOnTarget(ctx, bucketName, olderThan, tgtArn, mustGetUUID())
}

// ResetBucketReplication kicks off replication of previously replicated objects if ExistingObjectReplication
// is enabled in the replication config
func (c *Client) resetBucketReplicationOnTarget(ctx context.Context, bucketName string, olderThan time.Duration, tgtArn, resetID string) (rinfo replication.ResyncTargetsInfo, err error) {
	// Input validation.
	if err = s3utils.CheckValidBucketName(bucketName); err != nil {
		return rinfo, err
	}
	// Get resources properly escaped and lined up before
	// using them in http request.
	urlValues := make(url.Values)
	urlValues.Set("replication-reset", "")
	if olderThan > 0 {
		urlValues.Set("older-than", olderThan.String())
	}
	if tgtArn != "" {
		urlValues.Set("arn", tgtArn)
	}
	urlValues.Set("reset-id", resetID)
	// Execute GET on bucket to get replication config.
	resp, err := c.executeMethod(ctx, http.MethodPut, requestMetadata{
		bucketName:  bucketName,
		queryValues: urlValues,
	})

	defer closeResponse(resp)
	if err != nil {
		return rinfo, err
	}

	if resp.StatusCode != http.StatusOK {
		return rinfo, httpRespToErrorResponse(resp, bucketName, "")
	}

	if err = json.NewDecoder(resp.Body).Decode(&rinfo); err != nil {
		return rinfo, err
	}
	return rinfo, nil
}

// GetBucketReplicationResyncStatus retrieves the status of a replication resync operation.
//
// Parameters:
//   - ctx: Context for request cancellation and timeout
//   - bucketName: Name of the bucket
//   - arn: ARN of the replication target (empty string for all targets)
//
// Returns resync status information or an error if the operation fails.
func (c *Client) GetBucketReplicationResyncStatus(ctx context.Context, bucketName, arn string) (rinfo replication.ResyncTargetsInfo, err error) {
	// Input validation.
	if err := s3utils.CheckValidBucketName(bucketName); err != nil {
		return rinfo, err
	}
	// Get resources properly escaped and lined up before
	// using them in http request.
	urlValues := make(url.Values)
	urlValues.Set("replication-reset-status", "")
	if arn != "" {
		urlValues.Set("arn", arn)
	}
	// Execute GET on bucket to get replication config.
	resp, err := c.executeMethod(ctx, http.MethodGet, requestMetadata{
		bucketName:  bucketName,
		queryValues: urlValues,
	})

	defer closeResponse(resp)
	if err != nil {
		return rinfo, err
	}

	if resp.StatusCode != http.StatusOK {
		return rinfo, httpRespToErrorResponse(resp, bucketName, "")
	}

	if err = json.NewDecoder(resp.Body).Decode(&rinfo); err != nil {
		return rinfo, err
	}
	return rinfo, nil
}

// CancelBucketReplicationResync cancels an in-progress replication resync operation.
//
// Parameters:
//   - ctx: Context for request cancellation and timeout
//   - bucketName: Name of the bucket
//   - tgtArn: ARN of the replication target (empty string for all targets)
//
// Returns the ID of the canceled resync operation or an error if the operation fails.
func (c *Client) CancelBucketReplicationResync(ctx context.Context, bucketName string, tgtArn string) (id string, err error) {
	// Input validation.
	if err = s3utils.CheckValidBucketName(bucketName); err != nil {
		return id, err
	}
	// Get resources properly escaped and lined up before
	// using them in http request.
	urlValues := make(url.Values)
	urlValues.Set("replication-reset-cancel", "")
	if tgtArn != "" {
		urlValues.Set("arn", tgtArn)
	}
	// Execute GET on bucket to get replication config.
	resp, err := c.executeMethod(ctx, http.MethodPut, requestMetadata{
		bucketName:  bucketName,
		queryValues: urlValues,
	})

	defer closeResponse(resp)
	if err != nil {
		return id, err
	}

	if resp.StatusCode != http.StatusOK {
		return id, httpRespToErrorResponse(resp, bucketName, "")
	}
	strBuf, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	id = string(strBuf)
	return id, nil
}

// GetBucketReplicationMetricsV2 retrieves bucket replication status metrics using the V2 API.
//
// Parameters:
//   - ctx: Context for request cancellation and timeout
//   - bucketName: Name of the bucket
//
// Returns the V2 replication metrics or an error if the operation fails.
func (c *Client) GetBucketReplicationMetricsV2(ctx context.Context, bucketName string) (s replication.MetricsV2, err error) {
	// Input validation.
	if err := s3utils.CheckValidBucketName(bucketName); err != nil {
		return s, err
	}
	// Get resources properly escaped and lined up before
	// using them in http request.
	urlValues := make(url.Values)
	urlValues.Set("replication-metrics", "2")

	// Execute GET on bucket to get replication metrics.
	resp, err := c.executeMethod(ctx, http.MethodGet, requestMetadata{
		bucketName:  bucketName,
		queryValues: urlValues,
	})

	defer closeResponse(resp)
	if err != nil {
		return s, err
	}

	if resp.StatusCode != http.StatusOK {
		return s, httpRespToErrorResponse(resp, bucketName, "")
	}
	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return s, err
	}

	if err := json.Unmarshal(respBytes, &s); err != nil {
		return s, err
	}
	return s, nil
}

// CheckBucketReplication validates whether replication is properly configured for a bucket.
//
// Parameters:
//   - ctx: Context for request cancellation and timeout
//   - bucketName: Name of the bucket
//
// Returns nil if replication is valid, or an error describing the validation failure.
func (c *Client) CheckBucketReplication(ctx context.Context, bucketName string) (err error) {
	// Input validation.
	if err := s3utils.CheckValidBucketName(bucketName); err != nil {
		return err
	}
	// Get resources properly escaped and lined up before
	// using them in http request.
	urlValues := make(url.Values)
	urlValues.Set("replication-check", "")

	// Execute GET on bucket to get replication config.
	resp, err := c.executeMethod(ctx, http.MethodGet, requestMetadata{
		bucketName:  bucketName,
		queryValues: urlValues,
	})

	defer closeResponse(resp)
	if err != nil {
		return err
	}

	if resp.StatusCode != http.StatusOK {
		return httpRespToErrorResponse(resp, bucketName, "")
	}
	return nil
}
