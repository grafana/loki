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
	"bufio"
	"net"
	"net/http"
)

// ByteCounterWriter is a wrapper around http.ResponseWriter that counts bytes written
type ByteCounterWriter struct {
	http.ResponseWriter
	BytesWritten int64
	StatusCode   int
}

// NewByteCounterWriter creates a new ByteCounterWriter
func NewByteCounterWriter(w http.ResponseWriter) *ByteCounterWriter {
	return &ByteCounterWriter{
		ResponseWriter: w,
		StatusCode:     http.StatusOK, // Default status code
	}
}

// Write counts bytes written and passes to the underlying ResponseWriter
func (bcw *ByteCounterWriter) Write(b []byte) (int, error) {
	n, err := bcw.ResponseWriter.Write(b)
	bcw.BytesWritten += int64(n)
	return n, err
}

// WriteHeader captures the status code and passes to the underlying ResponseWriter
func (bcw *ByteCounterWriter) WriteHeader(statusCode int) {
	bcw.StatusCode = statusCode
	bcw.ResponseWriter.WriteHeader(statusCode)
}

// Flush implements http.Flusher if the underlying ResponseWriter supports it
func (bcw *ByteCounterWriter) Flush() {
	if flusher, ok := bcw.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

// Hijack implements http.Hijacker if the underlying ResponseWriter supports it
func (bcw *ByteCounterWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if hijacker, ok := bcw.ResponseWriter.(http.Hijacker); ok {
		return hijacker.Hijack()
	}
	return nil, nil, http.ErrNotSupported
}
