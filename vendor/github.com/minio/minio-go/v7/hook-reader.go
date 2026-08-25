/*
 * MinIO Go Library for Amazon S3 Compatible Cloud Storage
 * Copyright 2015-2017 MinIO, Inc.
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
	"fmt"
	"io"
)

// hookReader hooks additional reader in the source stream. It is
// useful for making progress bars. Second reader is appropriately
// notified about the exact number of bytes read from the primary
// source on each Read operation. It deliberately implements neither
// io.Seeker nor io.Closer: retry logic treats a seekable body as
// rewindable, and executeMethod closes request bodies that implement
// io.Closer — a caller-supplied reader must be shielded from both.
type hookReader struct {
	source io.Reader
	hook   io.Reader
}

// hookReadSeeker extends hookReader with seeking support. It is
// constructed only when the source implements io.Seeker, so a wrapped
// reader exposes Seek if and only if it can actually rewind. This lets
// retry logic disable retries for non-seekable bodies instead of
// retrying over a drained reader.
type hookReadSeeker struct {
	hookReader
	seeker io.Seeker
}

// Seek implements io.Seeker. Seeks source first, and if necessary
// seeks hook if Seek method is appropriately found.
func (hr *hookReadSeeker) Seek(offset int64, whence int) (n int64, err error) {
	n, err = hr.seeker.Seek(offset, whence)
	if err != nil {
		return 0, err
	}

	if hr.hook != nil {
		// Verify if hook has embedded Seeker, use it.
		hookSeeker, ok := hr.hook.(io.Seeker)
		if ok {
			var m int64
			m, err = hookSeeker.Seek(offset, whence)
			if err != nil {
				return 0, err
			}
			if n != m {
				return 0, fmt.Errorf("hook seeker sought to offset %d, expected source offset %d", m, n)
			}
		}
	}

	return n, nil
}

// Read implements io.Reader. Always reads from the source, the return
// value 'n' number of bytes are reported through the hook. Returns
// error for all non io.EOF conditions.
func (hr *hookReader) Read(b []byte) (n int, err error) {
	n, err = hr.source.Read(b)
	if err != nil && err != io.EOF {
		return n, err
	}
	if hr.hook != nil {
		// Progress the hook with the total read bytes from the source.
		if _, herr := hr.hook.Read(b[:n]); herr != nil {
			if herr != io.EOF {
				return n, herr
			}
		}
	}
	return n, err
}

// newHook returns an io.Reader that reports the data read from the
// source to the hook. The returned reader implements io.Seeker only
// when the source does.
func newHook(source, hook io.Reader) io.Reader {
	hr := hookReader{source: source, hook: hook}
	if seeker, ok := source.(io.Seeker); ok {
		return &hookReadSeeker{hookReader: hr, seeker: seeker}
	}
	return &hr
}
