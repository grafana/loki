/*
 * MinIO Go Library for Amazon S3 Compatible Cloud Storage
 * Copyright 2026 MinIO, Inc.
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
	"fmt"
	"io"
	"maps"
)

func (o GetObjectOptions) parallelGetEnabled() bool {
	return o.ParallelWorkers > 1 && o.PartNumber == 0 && !o.Checksum && o.RDMABuffer == nil && o.headers["Range"] == ""
}

func parallelGetChunkSize(chunkSize, objectSize int64) int64 {
	chunkSize = max(chunkSize, minPartSize)
	if (objectSize+chunkSize-1)/chunkSize > defaultMaxPartsCount {
		chunkSize = (objectSize + defaultMaxPartsCount - 1) / defaultMaxPartsCount
	}
	return chunkSize
}

type parallelGetReader struct {
	*io.PipeReader
	cancel context.CancelFunc
}

func (r parallelGetReader) Close() error {
	r.cancel()
	return r.PipeReader.Close()
}

// getObjectParallel reads the object with concurrent range GETs pinned to
// its ETag, falling back to a single GET when it fits in one chunk or has
// no ETag.
func (c *Client) getObjectParallel(ctx context.Context, bucketName, objectName string, opts GetObjectOptions) (io.ReadCloser, ObjectInfo, error) {
	objectInfo, err := c.StatObject(ctx, bucketName, objectName, opts)
	if err != nil {
		return nil, ObjectInfo{}, err
	}
	chunkSize := parallelGetChunkSize(opts.ParallelChunkSize, objectInfo.Size)
	if objectInfo.Size <= chunkSize || objectInfo.ETag == "" {
		body, objectInfo, _, err := c.getObject(ctx, bucketName, objectName, opts)
		return body, objectInfo, err
	}

	// The caller keeps reusing opts.headers for later requests.
	opts.headers = maps.Clone(opts.headers)
	opts.SetMatchETag(objectInfo.ETag)

	ctx, cancel := context.WithCancel(ctx)
	pr, pw := io.Pipe()
	go func() {
		pw.CloseWithError(c.parallelGet(ctx, bucketName, objectName, opts, objectInfo.Size, chunkSize, pw))
	}()
	return parallelGetReader{PipeReader: pr, cancel: cancel}, objectInfo, nil
}

// parallelGet writes the object to w in order. At most opts.ParallelWorkers
// chunks are being fetched or held in memory at any time.
func (c *Client) parallelGet(ctx context.Context, bucketName, objectName string, opts GetObjectOptions, size, chunkSize int64, w io.Writer) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	type chunk struct {
		data []byte
		err  error
	}
	numChunks := (size + chunkSize - 1) / chunkSize
	pending := make(chan chan chunk, min(int64(opts.ParallelWorkers), numChunks)-1)
	go func() {
		defer close(pending)
		for start := int64(0); start < size; start += chunkSize {
			res := make(chan chunk, 1)
			select {
			case pending <- res:
			case <-ctx.Done():
				return
			}
			go func() {
				data, err := c.getObjectChunk(ctx, bucketName, objectName, opts, start, min(start+chunkSize, size)-1, size)
				res <- chunk{data: data, err: err}
			}()
		}
	}()

	for res := range pending {
		var r chunk
		select {
		case r = <-res:
		case <-ctx.Done():
			return ctx.Err()
		}
		if r.err != nil {
			return r.err
		}
		if _, err := w.Write(r.data); err != nil {
			return err
		}
	}
	return ctx.Err()
}

func (c *Client) getObjectChunk(ctx context.Context, bucketName, objectName string, opts GetObjectOptions, start, end, size int64) ([]byte, error) {
	opts.headers = maps.Clone(opts.headers)
	if err := opts.SetRange(start, end); err != nil {
		return nil, err
	}
	body, _, header, err := c.getObject(ctx, bucketName, objectName, opts)
	if err != nil {
		return nil, err
	}
	defer body.Close()

	if cr := header.Get("Content-Range"); cr != fmt.Sprintf("bytes %d-%d/%d", start, end, size) {
		return nil, fmt.Errorf("unexpected Content-Range %q for range %d-%d", cr, start, end)
	}
	data := make([]byte, end-start+1)
	if _, err := io.ReadFull(body, data); err != nil {
		return nil, err
	}
	return data, nil
}
