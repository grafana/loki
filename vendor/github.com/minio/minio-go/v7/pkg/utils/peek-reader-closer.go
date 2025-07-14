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

package utils

import (
	"bytes"
	"errors"
	"io"
)

// PeekReadCloser offers a way to peek a ReadCloser stream and then
// return the exact stream of the underlying ReadCloser
type PeekReadCloser struct {
	io.ReadCloser

	recordMode   bool
	recordMaxBuf int
	recordBuf    *bytes.Buffer
}

// ReplayFromStart ensures next Read() will restart to stream the
// underlying ReadCloser stream from the beginning
func (prc *PeekReadCloser) ReplayFromStart() {
	prc.recordMode = false
}

func (prc *PeekReadCloser) Read(p []byte) (int, error) {
	if prc.recordMode {
		if prc.recordBuf.Len() > prc.recordMaxBuf {
			return 0, errors.New("maximum peek buffer exceeded")
		}
		n, err := prc.ReadCloser.Read(p)
		prc.recordBuf.Write(p[:n])
		return n, err
	}
	// Replay mode
	if prc.recordBuf.Len() > 0 {
		pn, _ := prc.recordBuf.Read(p)
		return pn, nil
	}
	return prc.ReadCloser.Read(p)
}

// Close releases the record buffer memory and close the underlying ReadCloser
func (prc *PeekReadCloser) Close() error {
	prc.recordBuf.Reset()
	return prc.ReadCloser.Close()
}

// NewPeekReadCloser returns a new peek reader
func NewPeekReadCloser(rc io.ReadCloser, maxBufSize int) *PeekReadCloser {
	return &PeekReadCloser{
		ReadCloser:   rc,
		recordMode:   true, // recording mode by default
		recordBuf:    bytes.NewBuffer(make([]byte, 0, 1024)),
		recordMaxBuf: maxBufSize,
	}
}
