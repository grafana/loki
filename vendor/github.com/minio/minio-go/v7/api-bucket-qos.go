/*
 * MinIO Go Library for Amazon S3 Compatible Cloud Storage
 * Copyright 2025 MinIO, Inc.
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
	"net/http"
	"net/url"
	"strings"

	"github.com/minio/minio-go/v7/pkg/s3utils"
	"gopkg.in/yaml.v3"
)

// QOSConfigVersionCurrent is the current version of the QoS configuration.
const QOSConfigVersionCurrent = "v1"

// QOSConfig represents the QoS configuration for a bucket.
type QOSConfig struct {
	Version string    `yaml:"version"`
	Rules   []QOSRule `yaml:"rules"`
}

// QOSRule represents a single QoS rule.
type QOSRule struct {
	ID           string `yaml:"id"`
	Label        string `yaml:"label,omitempty"`
	Priority     int    `yaml:"priority"`
	ObjectPrefix string `yaml:"objectPrefix"`
	API          string `yaml:"api"`
	Rate         int64  `yaml:"rate"`
	Burst        int64  `yaml:"burst"` // not required for concurrency limit
	Limit        string `yaml:"limit"` // "concurrency" or "rps"
}

// NewQOSConfig creates a new empty QoS configuration.
func NewQOSConfig() *QOSConfig {
	return &QOSConfig{
		Version: "v1",
		Rules:   []QOSRule{},
	}
}

// GetBucketQOS retrieves the Quality of Service (QoS) configuration for the bucket.
//
// Parameters:
//   - ctx: Context for request cancellation and timeout
//   - bucket: Name of the bucket
//
// Returns the QoS configuration or an error if the operation fails.
func (c *Client) GetBucketQOS(ctx context.Context, bucket string) (*QOSConfig, error) {
	var qosCfg QOSConfig
	// Input validation.
	if err := s3utils.CheckValidBucketName(bucket); err != nil {
		return nil, err
	}
	urlValues := make(url.Values)
	urlValues.Set("qos", "")
	// Execute GET on bucket to  fetch qos.
	resp, err := c.executeMethod(ctx, http.MethodGet, requestMetadata{
		bucketName:       bucket,
		queryValues:      urlValues,
		contentSHA256Hex: emptySHA256Hex,
	})
	defer closeResponse(resp)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, httpRespToErrorResponse(resp, bucket, "")
	}
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if err = yaml.Unmarshal(b, &qosCfg); err != nil {
		return nil, err
	}

	return &qosCfg, nil
}

// SetBucketQOS sets the Quality of Service (QoS) configuration for a bucket.
//
// Parameters:
//   - ctx: Context for request cancellation and timeout
//   - bucket: Name of the bucket
//   - qosCfg: QoS configuration to apply
//
// Returns an error if the operation fails.
func (c *Client) SetBucketQOS(ctx context.Context, bucket string, qosCfg *QOSConfig) error {
	// Input validation.
	if err := s3utils.CheckValidBucketName(bucket); err != nil {
		return err
	}

	data, err := yaml.Marshal(qosCfg)
	if err != nil {
		return err
	}
	// Get resources properly escaped and lined up before
	// using them in http request.
	urlValues := make(url.Values)
	urlValues.Set("qos", "")

	reqMetadata := requestMetadata{
		bucketName:    bucket,
		queryValues:   urlValues,
		contentBody:   strings.NewReader(string(data)),
		contentLength: int64(len(data)),
	}

	// Execute PUT to upload a new bucket QoS configuration.
	resp, err := c.executeMethod(ctx, http.MethodPut, reqMetadata)
	defer closeResponse(resp)
	if err != nil {
		return err
	}
	if resp != nil {
		if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
			return httpRespToErrorResponse(resp, bucket, "")
		}
	}
	return nil
}

// CounterMetric returns stats for a counter
type CounterMetric struct {
	Last1m  uint64 `json:"last1m"`
	Last1hr uint64 `json:"last1hr"`
	Total   uint64 `json:"total"`
}

// QOSMetric - metric for a qos rule per bucket
type QOSMetric struct {
	APIName            string        `json:"apiName"`
	Rule               QOSRule       `json:"rule"`
	Totals             CounterMetric `json:"totals"`
	Throttled          CounterMetric `json:"throttleCount"`
	ExceededRateLimit  CounterMetric `json:"exceededRateLimitCount"`
	ClientDisconnCount CounterMetric `json:"clientDisconnectCount"`
	ReqTimeoutCount    CounterMetric `json:"reqTimeoutCount"`
}

// QOSNodeStats represents stats for a bucket on a single node
type QOSNodeStats struct {
	Stats    []QOSMetric `json:"stats"`
	NodeName string      `json:"node"`
}

// GetBucketQOSMetrics retrieves Quality of Service (QoS) metrics for a bucket.
//
// Parameters:
//   - ctx: Context for request cancellation and timeout
//   - bucketName: Name of the bucket
//   - nodeName: Name of the node (empty string for all nodes)
//
// Returns QoS metrics per node or an error if the operation fails.
func (c *Client) GetBucketQOSMetrics(ctx context.Context, bucketName, nodeName string) (qs []QOSNodeStats, err error) {
	// Input validation.
	if err := s3utils.CheckValidBucketName(bucketName); err != nil {
		return qs, err
	}
	// Get resources properly escaped and lined up before
	// using them in http request.
	urlValues := make(url.Values)
	urlValues.Set("qos-metrics", "")
	if nodeName != "" {
		urlValues.Set("node", nodeName)
	}
	// Execute GET on bucket to get qos metrics.
	resp, err := c.executeMethod(ctx, http.MethodGet, requestMetadata{
		bucketName:  bucketName,
		queryValues: urlValues,
	})

	defer closeResponse(resp)
	if err != nil {
		return qs, err
	}

	if resp.StatusCode != http.StatusOK {
		return qs, httpRespToErrorResponse(resp, bucketName, "")
	}
	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return qs, err
	}

	if err := json.Unmarshal(respBytes, &qs); err != nil {
		return qs, err
	}
	return qs, nil
}
