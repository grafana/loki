/*
 * Copyright (c) 2025, NVIDIA CORPORATION.  All rights reserved.
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
package middleware

import (
	"net/http"
)

// RequestSizeFunc is a function that can retrieve the actual request size
type RequestSizeFunc func(r *http.Request) (int64, bool)

// RequestAwareByteCounterWriter tracks actual request bytes instead of response bytes
type RequestAwareByteCounterWriter struct {
	*ByteCounterWriter
	request         *http.Request
	requestSizeFunc RequestSizeFunc
	bytesRecorded   bool
}

// NewRequestAwareByteCounterWriter creates a byte counter that can use request size
func NewRequestAwareByteCounterWriter(w http.ResponseWriter, r *http.Request, sizeFunc RequestSizeFunc) *RequestAwareByteCounterWriter {
	return &RequestAwareByteCounterWriter{
		ByteCounterWriter: NewByteCounterWriter(w),
		request:           r,
		requestSizeFunc:   sizeFunc,
	}
}

// Write intercepts to use request bytes if available
func (rbcw *RequestAwareByteCounterWriter) Write(b []byte) (int, error) {
	// On first write, check if we should use request size instead
	if !rbcw.bytesRecorded && rbcw.requestSizeFunc != nil {
		if requestBytes, ok := rbcw.requestSizeFunc(rbcw.request); ok && requestBytes > 0 {
			// Use the actual request size
			rbcw.BytesWritten = requestBytes
			rbcw.bytesRecorded = true
		}
	}

	// Still write the actual response
	n, err := rbcw.ResponseWriter.Write(b)

	// Only update BytesWritten if we haven't recorded request bytes
	if !rbcw.bytesRecorded {
		rbcw.BytesWritten += int64(n)
	}

	return n, err
}
