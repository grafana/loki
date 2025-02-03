/*
 * MinIO Go Library for Amazon S3 Compatible Cloud Storage
 * Copyright 2015-2024 MinIO, Inc.
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
	"io"
	"net/http"

	"github.com/goccy/go-json"
	"github.com/minio/minio-go/v7/pkg/s3utils"
)

// PromptObject performs language model inference with the prompt and referenced object as context.
// Inference is performed using a Lambda handler that can process the prompt and object.
// Currently, this functionality is limited to certain MinIO servers.
func (c *Client) PromptObject(ctx context.Context, bucketName, objectName, prompt string, opts PromptObjectOptions) (io.ReadCloser, error) {
	// Input validation.
	if err := s3utils.CheckValidBucketName(bucketName); err != nil {
		return nil, ErrorResponse{
			StatusCode: http.StatusBadRequest,
			Code:       "InvalidBucketName",
			Message:    err.Error(),
		}
	}
	if err := s3utils.CheckValidObjectName(objectName); err != nil {
		return nil, ErrorResponse{
			StatusCode: http.StatusBadRequest,
			Code:       "XMinioInvalidObjectName",
			Message:    err.Error(),
		}
	}

	opts.AddLambdaArnToReqParams(opts.LambdaArn)
	opts.SetHeader("Content-Type", "application/json")
	opts.AddPromptArg("prompt", prompt)
	promptReqBytes, err := json.Marshal(opts.PromptArgs)
	if err != nil {
		return nil, err
	}

	// Execute POST on bucket/object.
	resp, err := c.executeMethod(ctx, http.MethodPost, requestMetadata{
		bucketName:       bucketName,
		objectName:       objectName,
		queryValues:      opts.toQueryValues(),
		customHeader:     opts.Header(),
		contentSHA256Hex: sum256Hex(promptReqBytes),
		contentBody:      bytes.NewReader(promptReqBytes),
		contentLength:    int64(len(promptReqBytes)),
	})
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		defer closeResponse(resp)
		return nil, httpRespToErrorResponse(resp, bucketName, objectName)
	}

	return resp.Body, nil
}
