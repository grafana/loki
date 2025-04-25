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
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"

	"github.com/minio/minio-go/v7/pkg/s3utils"
)

// AppendObjectOptions https://docs.aws.amazon.com/AmazonS3/latest/userguide/directory-buckets-objects-append.html
type AppendObjectOptions struct {
	// Provide a progress reader to indicate the current append() progress.
	Progress io.Reader
	// ChunkSize indicates the maximum append() size,
	// it is useful when you want to control how much data
	// per append() you are interested in sending to server
	// while keeping the input io.Reader of a longer length.
	ChunkSize uint64
	// Aggressively disable sha256 payload, it is automatically
	// turned-off for TLS supporting endpoints, useful in benchmarks
	// where you are interested in the peak() numbers.
	DisableContentSha256 bool

	customHeaders http.Header
	checksumType  ChecksumType
}

// Header returns the custom header for AppendObject API
func (opts AppendObjectOptions) Header() (header http.Header) {
	header = make(http.Header)
	for k, v := range opts.customHeaders {
		header[k] = v
	}
	return header
}

func (opts *AppendObjectOptions) setWriteOffset(offset int64) {
	if len(opts.customHeaders) == 0 {
		opts.customHeaders = make(http.Header)
	}
	opts.customHeaders["x-amz-write-offset-bytes"] = []string{strconv.FormatInt(offset, 10)}
}

func (opts *AppendObjectOptions) setChecksumParams(info ObjectInfo) {
	if len(opts.customHeaders) == 0 {
		opts.customHeaders = make(http.Header)
	}
	fullObject := info.ChecksumMode == ChecksumFullObjectMode.String()
	switch {
	case info.ChecksumCRC32 != "":
		if fullObject {
			opts.checksumType = ChecksumFullObjectCRC32
		}
	case info.ChecksumCRC32C != "":
		if fullObject {
			opts.checksumType = ChecksumFullObjectCRC32C
		}
	case info.ChecksumCRC64NVME != "":
		// CRC64NVME only has a full object variant
		// so it does not carry any special full object
		// modifier
		opts.checksumType = ChecksumCRC64NVME
	}
}

func (opts AppendObjectOptions) validate(c *Client) (err error) {
	if opts.ChunkSize > maxPartSize {
		return errInvalidArgument("Append chunkSize cannot be larger than max part size allowed")
	}
	switch {
	case !c.trailingHeaderSupport:
		return errInvalidArgument("AppendObject() requires Client with TrailingHeaders enabled")
	case c.overrideSignerType.IsV2():
		return errInvalidArgument("AppendObject() cannot be used with v2 signatures")
	case s3utils.IsGoogleEndpoint(*c.endpointURL):
		return errInvalidArgument("AppendObject() cannot be used with GCS endpoints")
	}

	return nil
}

// appendObjectDo - executes the append object http operation.
// NOTE: You must have WRITE permissions on a bucket to add an object to it.
func (c *Client) appendObjectDo(ctx context.Context, bucketName, objectName string, reader io.Reader, size int64, opts AppendObjectOptions) (UploadInfo, error) {
	// Input validation.
	if err := s3utils.CheckValidBucketName(bucketName); err != nil {
		return UploadInfo{}, err
	}
	if err := s3utils.CheckValidObjectName(objectName); err != nil {
		return UploadInfo{}, err
	}

	// Set headers.
	customHeader := opts.Header()

	// Populate request metadata.
	reqMetadata := requestMetadata{
		bucketName:    bucketName,
		objectName:    objectName,
		customHeader:  customHeader,
		contentBody:   reader,
		contentLength: size,
		streamSha256:  !opts.DisableContentSha256,
	}

	if opts.checksumType.IsSet() {
		reqMetadata.addCrc = &opts.checksumType
	}

	// Execute PUT an objectName.
	resp, err := c.executeMethod(ctx, http.MethodPut, reqMetadata)
	defer closeResponse(resp)
	if err != nil {
		return UploadInfo{}, err
	}
	if resp != nil {
		if resp.StatusCode != http.StatusOK {
			return UploadInfo{}, httpRespToErrorResponse(resp, bucketName, objectName)
		}
	}

	h := resp.Header

	// When AppendObject() is used, S3 Express will return final object size as x-amz-object-size
	if amzSize := h.Get("x-amz-object-size"); amzSize != "" {
		size, err = strconv.ParseInt(amzSize, 10, 64)
		if err != nil {
			return UploadInfo{}, err
		}
	}

	return UploadInfo{
		Bucket: bucketName,
		Key:    objectName,
		ETag:   trimEtag(h.Get("ETag")),
		Size:   size,

		// Checksum values
		ChecksumCRC32:     h.Get(ChecksumCRC32.Key()),
		ChecksumCRC32C:    h.Get(ChecksumCRC32C.Key()),
		ChecksumSHA1:      h.Get(ChecksumSHA1.Key()),
		ChecksumSHA256:    h.Get(ChecksumSHA256.Key()),
		ChecksumCRC64NVME: h.Get(ChecksumCRC64NVME.Key()),
		ChecksumMode:      h.Get(ChecksumFullObjectMode.Key()),
	}, nil
}

// AppendObject - S3 Express Zone https://docs.aws.amazon.com/AmazonS3/latest/userguide/directory-buckets-objects-append.html
func (c *Client) AppendObject(ctx context.Context, bucketName, objectName string, reader io.Reader, objectSize int64,
	opts AppendObjectOptions,
) (info UploadInfo, err error) {
	if objectSize < 0 && opts.ChunkSize == 0 {
		return UploadInfo{}, errors.New("object size must be provided when no chunk size is provided")
	}

	if err = opts.validate(c); err != nil {
		return UploadInfo{}, err
	}

	oinfo, err := c.StatObject(ctx, bucketName, objectName, StatObjectOptions{Checksum: true})
	if err != nil {
		return UploadInfo{}, err
	}
	if oinfo.ChecksumMode != ChecksumFullObjectMode.String() {
		return UploadInfo{}, fmt.Errorf("append API is not allowed on objects that are not full_object checksum type: %s", oinfo.ChecksumMode)
	}
	opts.setChecksumParams(oinfo)   // set the appropriate checksum params based on the existing object checksum metadata.
	opts.setWriteOffset(oinfo.Size) // First append must set the current object size as the offset.

	if opts.ChunkSize > 0 {
		finalObjSize := int64(-1)
		if objectSize > 0 {
			finalObjSize = info.Size + objectSize
		}
		totalPartsCount, partSize, lastPartSize, err := OptimalPartInfo(finalObjSize, opts.ChunkSize)
		if err != nil {
			return UploadInfo{}, err
		}
		buf := make([]byte, partSize)
		var partNumber int
		for partNumber = 1; partNumber <= totalPartsCount; partNumber++ {
			// Proceed to upload the part.
			if partNumber == totalPartsCount {
				partSize = lastPartSize
			}
			n, err := readFull(reader, buf)
			if err != nil {
				return info, err
			}
			if n != int(partSize) {
				return info, io.ErrUnexpectedEOF
			}
			rd := newHook(bytes.NewReader(buf[:n]), opts.Progress)
			uinfo, err := c.appendObjectDo(ctx, bucketName, objectName, rd, partSize, opts)
			if err != nil {
				return info, err
			}
			opts.setWriteOffset(uinfo.Size)
		}
	}

	rd := newHook(reader, opts.Progress)
	return c.appendObjectDo(ctx, bucketName, objectName, rd, objectSize, opts)
}
