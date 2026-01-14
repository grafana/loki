// Copyright 2024 The Prometheus Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package remote

import (
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
)

const (
	versionHeader        = "X-Prometheus-Remote-Write-Version"
	version1HeaderValue  = "0.1.0"
	version20HeaderValue = "2.0.0"
	appProtoContentType  = "application/x-protobuf"
)

// Compression represents the encoding. Currently remote storage supports only
// one, but we experiment with more, thus leaving the compression scaffolding
// for now.
type Compression string

const (
	// SnappyBlockCompression represents https://github.com/google/snappy/blob/2c94e11145f0b7b184b831577c93e5a41c4c0346/format_description.txt
	SnappyBlockCompression Compression = "snappy"
)

// WriteMessageType represents the fully qualified name of the protobuf message
// to use in Remote write 1.0 and 2.0 protocols.
// See https://prometheus.io/docs/specs/remote_write_spec_2_0/#protocol.
type WriteMessageType string

const (
	// WriteV1MessageType represents the `prometheus.WriteRequest` protobuf
	// message introduced in the https://prometheus.io/docs/specs/remote_write_spec/.
	// DEPRECATED: Use WriteV2MessageType instead.
	WriteV1MessageType WriteMessageType = "prometheus.WriteRequest"
	// WriteV2MessageType represents the `io.prometheus.write.v2.Request` protobuf
	// message introduced in https://prometheus.io/docs/specs/remote_write_spec_2_0/
	WriteV2MessageType WriteMessageType = "io.prometheus.write.v2.Request"
)

// Validate returns error if the given reference for the protobuf message is not supported.
func (n WriteMessageType) Validate() error {
	switch n {
	case WriteV1MessageType, WriteV2MessageType:
		return nil
	default:
		return fmt.Errorf("unknown type for remote write protobuf message %v, supported: %v", n, MessageTypes{WriteV1MessageType, WriteV2MessageType}.String())
	}
}

type MessageTypes []WriteMessageType

func (m MessageTypes) Strings() []string {
	ret := make([]string, 0, len(m))
	for _, typ := range m {
		ret = append(ret, string(typ))
	}
	return ret
}

func (m MessageTypes) String() string {
	return strings.Join(m.Strings(), ", ")
}

func (m MessageTypes) Contains(mType WriteMessageType) bool {
	return slices.Contains(m, mType)
}

var contentTypeHeaders = map[WriteMessageType]string{
	WriteV1MessageType: appProtoContentType, // Also application/x-protobuf;proto=prometheus.WriteRequest but simplified for compatibility with 1.x spec.
	WriteV2MessageType: appProtoContentType + ";proto=io.prometheus.write.v2.Request",
}

// ContentTypeHeader returns content type header value for the given proto message
// or empty string for unknown proto message.
func contentTypeHeader(m WriteMessageType) string {
	return contentTypeHeaders[m]
}

const (
	writtenSamplesHeader    = "X-Prometheus-Remote-Write-Samples-Written"
	writtenHistogramsHeader = "X-Prometheus-Remote-Write-Histograms-Written"
	writtenExemplarsHeader  = "X-Prometheus-Remote-Write-Exemplars-Written"
)

// WriteResponse represents the response from the remote storage upon receiving a remote write request.
type WriteResponse struct {
	WriteResponseStats
	statusCode   int
	extraHeaders http.Header
}

// NewWriteResponse creates a new WriteResponse with empty stats and status code http.StatusNoContent.
func NewWriteResponse() *WriteResponse {
	return &WriteResponse{
		WriteResponseStats: WriteResponseStats{},
		statusCode:         http.StatusNoContent,
		extraHeaders:       make(http.Header),
	}
}

// Stats returns the current statistics.
func (w *WriteResponse) Stats() WriteResponseStats {
	return w.WriteResponseStats
}

// SetStatusCode sets the HTTP status code for the response. http.StatusNoContent is the default unless 5xx is set.
func (w *WriteResponse) SetStatusCode(code int) {
	w.statusCode = code
}

// SetExtraHeader adds additional headers to be set in the response (apart from stats headers)
func (w *WriteResponse) SetExtraHeader(key, value string) {
	w.extraHeaders.Set(key, value)
}

// writeHeaders sets response headers in a given response writer.
// Make sure to use it before http.ResponseWriter.WriteHeader and .Write.
func (w *WriteResponse) writeHeaders(msgType WriteMessageType, rw http.ResponseWriter) {
	h := rw.Header()

	// TODO make it easier to indicate if the stats are valid before adding the headers. WriteResponseStats.confirmed
	//  could be used if there was a reliable way for it to be set without parsing headers. For now ensure we don't
	//  add stats headers for v1 messages which can cause confusion/false positive errors logs.
	if msgType != WriteV1MessageType {
		h.Set(writtenSamplesHeader, strconv.Itoa(w.Samples))
		h.Set(writtenHistogramsHeader, strconv.Itoa(w.Histograms))
		h.Set(writtenExemplarsHeader, strconv.Itoa(w.Exemplars))
	}

	for k, v := range w.extraHeaders {
		for _, vv := range v {
			h.Add(k, vv)
		}
	}
}

// WriteResponseStats represents the response, remote write statistics.
type WriteResponseStats struct {
	// Samples represents X-Prometheus-Remote-Write-Written-Samples
	Samples int
	// Histograms represents X-Prometheus-Remote-Write-Written-Histograms
	Histograms int
	// Exemplars represents X-Prometheus-Remote-Write-Written-Exemplars
	Exemplars int

	// Confirmed means we can trust those statistics from the point of view
	// of the PRW 2.0 spec. When parsed from headers, it means we got at least one
	// response header from the Receiver to confirm those numbers, meaning it must
	// be at least 2.0 Receiver. See ParseWriteResponseStats for details.
	confirmed bool
}

// NoDataWritten returns true if statistics indicate no data was written.
func (s WriteResponseStats) NoDataWritten() bool {
	return (s.Samples + s.Histograms + s.Exemplars) == 0
}

// AllSamples returns both float and histogram sample numbers.
func (s WriteResponseStats) AllSamples() int {
	return s.Samples + s.Histograms
}

// Add adds the given WriteResponseStats to this WriteResponseStats.
// If this WriteResponseStats is empty, it will be replaced by the given WriteResponseStats.
func (s *WriteResponseStats) Add(rs WriteResponseStats) {
	s.confirmed = rs.confirmed
	s.Samples += rs.Samples
	s.Histograms += rs.Histograms
	s.Exemplars += rs.Exemplars
}

// parseWriteResponseStats returns WriteResponseStats parsed from the response headers.
//
// As per 2.0 spec, missing header means 0. However, abrupt HTTP errors, 1.0 Receivers
// or buggy 2.0 Receivers might result in no response headers specified and that
// might NOT necessarily mean nothing was written. To represent that we set
// s.Confirmed = true only when see at least on response header.
//
// Error is returned when any of the header fails to parse as int64.
func parseWriteResponseStats(r *http.Response) (s WriteResponseStats, err error) {
	var (
		errs []error
		h    = r.Header
	)
	if v := h.Get(writtenSamplesHeader); v != "" { // Empty means zero.
		s.confirmed = true
		if s.Samples, err = strconv.Atoi(v); err != nil {
			s.Samples = 0
			errs = append(errs, err)
		}
	}
	if v := h.Get(writtenHistogramsHeader); v != "" { // Empty means zero.
		s.confirmed = true
		if s.Histograms, err = strconv.Atoi(v); err != nil {
			s.Histograms = 0
			errs = append(errs, err)
		}
	}
	if v := h.Get(writtenExemplarsHeader); v != "" { // Empty means zero.
		s.confirmed = true
		if s.Exemplars, err = strconv.Atoi(v); err != nil {
			s.Exemplars = 0
			errs = append(errs, err)
		}
	}
	return s, errors.Join(errs...)
}
