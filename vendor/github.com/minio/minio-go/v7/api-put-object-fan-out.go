/*
 * MinIO Go Library for Amazon S3 Compatible Cloud Storage
 * Copyright 2023 MinIO, Inc.
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
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/minio/minio-go/v7/pkg/encrypt"
)

// PutObjectFanOutEntry is per object entry fan-out metadata
type PutObjectFanOutEntry struct {
	Key                string            `json:"key"`
	UserMetadata       map[string]string `json:"metadata,omitempty"`
	UserTags           map[string]string `json:"tags,omitempty"`
	ContentType        string            `json:"contentType,omitempty"`
	ContentEncoding    string            `json:"contentEncoding,omitempty"`
	ContentDisposition string            `json:"contentDisposition,omitempty"`
	ContentLanguage    string            `json:"contentLanguage,omitempty"`
	CacheControl       string            `json:"cacheControl,omitempty"`
	Retention          RetentionMode     `json:"retention,omitempty"`
	RetainUntilDate    *time.Time        `json:"retainUntil,omitempty"`
}

// PutObjectFanOutRequest this is the request structure sent
// to the server to fan-out the stream to multiple objects.
type PutObjectFanOutRequest struct {
	Entries  []PutObjectFanOutEntry
	Checksum Checksum
	SSE      encrypt.ServerSide
}

// PutObjectFanOutResponse this is the response structure sent
// by the server upon success or failure for each object
// fan-out keys. Additionally, this response carries ETag,
// VersionID and LastModified for each object fan-out.
type PutObjectFanOutResponse struct {
	Key          string     `json:"key"`
	ETag         string     `json:"etag,omitempty"`
	VersionID    string     `json:"versionId,omitempty"`
	LastModified *time.Time `json:"lastModified,omitempty"`
	Error        error      `json:"error,omitempty"`
}

// PutObjectFanOut - is a variant of PutObject instead of writing a single object from a single
// stream multiple objects are written, defined via a list of PutObjectFanOutRequests. Each entry
// in PutObjectFanOutRequest carries an object keyname and its relevant metadata if any. `Key` is
// mandatory, rest of the other options in PutObjectFanOutRequest are optional.
func (c *Client) PutObjectFanOut(ctx context.Context, bucket string, fanOutData io.Reader, fanOutReq PutObjectFanOutRequest) ([]PutObjectFanOutResponse, error) {
	if len(fanOutReq.Entries) == 0 {
		return nil, errInvalidArgument("fan out requests cannot be empty")
	}

	policy := NewPostPolicy()
	policy.SetBucket(bucket)
	policy.SetKey(strconv.FormatInt(time.Now().UnixNano(), 16))

	// Expires in 15 minutes.
	policy.SetExpires(time.Now().UTC().Add(15 * time.Minute))

	// Set encryption headers if any.
	policy.SetEncryption(fanOutReq.SSE)

	// Set checksum headers if any.
	policy.SetChecksum(fanOutReq.Checksum)

	url, formData, err := c.PresignedPostPolicy(ctx, policy)
	if err != nil {
		return nil, err
	}

	r, w := io.Pipe()

	req, err := http.NewRequest(http.MethodPost, url.String(), r)
	if err != nil {
		w.Close()
		return nil, err
	}

	var b strings.Builder
	enc := json.NewEncoder(&b)
	for _, req := range fanOutReq.Entries {
		if req.Key == "" {
			w.Close()
			return nil, errors.New("PutObjectFanOutRequest.Key is mandatory and cannot be empty")
		}
		if err = enc.Encode(&req); err != nil {
			w.Close()
			return nil, err
		}
	}

	mwriter := multipart.NewWriter(w)
	req.Header.Add("Content-Type", mwriter.FormDataContentType())

	go func() {
		defer w.Close()
		defer mwriter.Close()

		for k, v := range formData {
			if err := mwriter.WriteField(k, v); err != nil {
				return
			}
		}

		if err := mwriter.WriteField("x-minio-fanout-list", b.String()); err != nil {
			return
		}

		mw, err := mwriter.CreateFormFile("file", "fanout-content")
		if err != nil {
			return
		}

		if _, err = io.Copy(mw, fanOutData); err != nil {
			return
		}
	}()

	resp, err := c.do(req)
	if err != nil {
		return nil, err
	}
	defer closeResponse(resp)

	if resp.StatusCode != http.StatusOK {
		return nil, httpRespToErrorResponse(resp, bucket, "fanout-content")
	}

	dec := json.NewDecoder(resp.Body)
	fanOutResp := make([]PutObjectFanOutResponse, 0, len(fanOutReq.Entries))
	for dec.More() {
		var m PutObjectFanOutResponse
		if err = dec.Decode(&m); err != nil {
			return nil, err
		}
		fanOutResp = append(fanOutResp, m)
	}

	return fanOutResp, nil
}
