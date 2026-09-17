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

package signer

import (
	"crypto/hmac"
	"crypto/sha256"
	"net/http"
	"strings"

	"golang.org/x/net/http/httpguts"
)

// unsignedPayload - value to be set to X-Amz-Content-Sha256 header when
const unsignedPayload = "UNSIGNED-PAYLOAD"

// sum256 calculate sha256 sum for an input byte array.
func sum256(data []byte) []byte {
	hash := sha256.New()
	hash.Write(data)
	return hash.Sum(nil)
}

// sumHMAC calculate hmac between two input byte array.
func sumHMAC(key, data []byte) []byte {
	hash := hmac.New(sha256.New, key)
	hash.Write(data)
	return hash.Sum(nil)
}

// getHostAddr returns host header if available, otherwise returns host from URL
func getHostAddr(req *http.Request) string {
	host := req.Header.Get("host")
	if host != "" && req.Host != host {
		return host
	}
	if req.Host != "" {
		return req.Host
	}
	return req.URL.Host
}

// Trim leading and trailing spaces and replace sequential spaces with one space, following Trimall()
// in http://docs.aws.amazon.com/general/latest/gr/sigv4-create-canonical-request.html
func signV4TrimAll(input string) string {
	// Compress adjacent spaces (a space is determined by
	// unicode.IsSpace() internally here) to one space and return
	return strings.Join(strings.Fields(input), " ")
}

// awsChunkedEncoding is the content coding AWS SigV4 streaming uploads
// declare on the Content-Encoding header.
const awsChunkedEncoding = "aws-chunked"

// setAwsChunkedContentEncoding marks the request payload as aws-chunked
// encoded, keeping any content encoding the caller already set, for example
// "aws-chunked,gzip". AWS SigV4 streaming uploads require this header.
// Assigning "aws-chunked" to http.Request.TransferEncoding never reaches
// the wire — net/http silently drops assigned values other than "chunked" —
// so this header is the only signal that declares the aws-chunked framing.
func setAwsChunkedContentEncoding(req *http.Request) {
	encodings := req.Header.Values("Content-Encoding")
	if httpguts.HeaderValuesContainsToken(encodings, awsChunkedEncoding) {
		return
	}
	existing := strings.TrimSpace(strings.Join(encodings, ","))
	if existing == "" {
		req.Header.Set("Content-Encoding", awsChunkedEncoding)
		return
	}
	req.Header.Set("Content-Encoding", awsChunkedEncoding+","+existing)
}
