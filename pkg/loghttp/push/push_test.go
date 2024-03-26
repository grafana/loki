package push

import (
	"bytes"
	"compress/flate"
	"compress/gzip"
	"context"
	"fmt"
	"log"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	util_log "github.com/grafana/loki/pkg/util/log"
)

// GZip source string and return compressed string
func gzipString(source string) string {
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write([]byte(source)); err != nil {
		log.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		log.Fatal(err)
	}
	return buf.String()
}

// Deflate source string and return compressed string
func deflateString(source string) string {
	var buf bytes.Buffer
	zw, _ := flate.NewWriter(&buf, 6)
	if _, err := zw.Write([]byte(source)); err != nil {
		log.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		log.Fatal(err)
	}
	return buf.String()
}

func TestParseRequest(t *testing.T) {
	var previousBytesReceived, previousStructuredMetadataBytesReceived, previousLinesReceived int
	for index, test := range []struct {
		path                            string
		body                            string
		contentType                     string
		contentEncoding                 string
		valid                           bool
		expectedStructuredMetadataBytes int
		expectedBytes                   int
		expectedLines                   int
		expectedBytesUsageTracker       map[string]float64
	}{
		{
			path:        `/loki/api/v1/push`,
			body:        ``,
			contentType: `application/json`,
			valid:       false,
		},
		{
			path:        `/loki/api/v1/push`,
			body:        `{"streams": [{ "stream": { "foo": "bar2" }, "values": [ [ "1570818238000000000", "fizzbuzz" ] ] }]}`,
			contentType: ``,
			valid:       false,
		},
		{
			path:                      `/loki/api/v1/push`,
			body:                      `{"streams": [{ "stream": { "foo": "bar2" }, "values": [ [ "1570818238000000000", "fizzbuzz" ] ] }]}`,
			contentType:               `application/json`,
			valid:                     true,
			expectedBytes:             len("fizzbuzz"),
			expectedLines:             1,
			expectedBytesUsageTracker: map[string]float64{`{foo="bar2"}`: float64(len("fizzbuss"))},
		},
		{
			path:                      `/loki/api/v1/push`,
			body:                      `{"streams": [{ "stream": { "foo": "bar2" }, "values": [ [ "1570818238000000000", "fizzbuzz" ] ] }]}`,
			contentType:               `application/json`,
			contentEncoding:           ``,
			valid:                     true,
			expectedBytes:             len("fizzbuzz"),
			expectedLines:             1,
			expectedBytesUsageTracker: map[string]float64{`{foo="bar2"}`: float64(len("fizzbuss"))},
		},
		{
			path:            `/loki/api/v1/push`,
			body:            `{"streams": [{ "stream": { "foo": "bar2" }, "values": [ [ "1570818238000000000", {"fizz": "buzz"} ] ] }]}`,
			contentType:     `application/json`,
			contentEncoding: ``,
			valid:           false,
		},
		{
			path:                      `/loki/api/v1/push`,
			body:                      gzipString(`{"streams": [{ "stream": { "foo": "bar2" }, "values": [ [ "1570818238000000000", "fizzbuzz" ] ] }]}`),
			contentType:               `application/json`,
			contentEncoding:           `gzip`,
			valid:                     true,
			expectedBytes:             len("fizzbuzz"),
			expectedLines:             1,
			expectedBytesUsageTracker: map[string]float64{`{foo="bar2"}`: float64(len("fizzbuss"))},
		},
		{
			path:                      `/loki/api/v1/push`,
			body:                      deflateString(`{"streams": [{ "stream": { "foo": "bar2" }, "values": [ [ "1570818238000000000", "fizzbuzz" ] ] }]}`),
			contentType:               `application/json`,
			contentEncoding:           `deflate`,
			valid:                     true,
			expectedBytes:             len("fizzbuzz"),
			expectedLines:             1,
			expectedBytesUsageTracker: map[string]float64{`{foo="bar2"}`: float64(len("fizzbuss"))},
		},
		{
			path:            `/loki/api/v1/push`,
			body:            gzipString(`{"streams": [{ "stream": { "foo": "bar2" }, "values": [ [ "1570818238000000000", "fizzbuzz" ] ] }]}`),
			contentType:     `application/json`,
			contentEncoding: `snappy`,
			valid:           false,
		},
		{
			path:                      `/loki/api/v1/push`,
			body:                      gzipString(`{"streams": [{ "stream": { "foo": "bar2" }, "values": [ [ "1570818238000000000", "fizzbuzz" ] ] }]}`),
			contentType:               `application/json; charset=utf-8`,
			contentEncoding:           `gzip`,
			valid:                     true,
			expectedBytes:             len("fizzbuzz"),
			expectedLines:             1,
			expectedBytesUsageTracker: map[string]float64{`{foo="bar2"}`: float64(len("fizzbuss"))},
		},
		{
			path:                      `/loki/api/v1/push`,
			body:                      deflateString(`{"streams": [{ "stream": { "foo": "bar2" }, "values": [ [ "1570818238000000000", "fizzbuzz" ] ] }]}`),
			contentType:               `application/json; charset=utf-8`,
			contentEncoding:           `deflate`,
			valid:                     true,
			expectedBytes:             len("fizzbuzz"),
			expectedLines:             1,
			expectedBytesUsageTracker: map[string]float64{`{foo="bar2"}`: float64(len("fizzbuss"))},
		},
		{
			path:            `/loki/api/v1/push`,
			body:            gzipString(`{"streams": [{ "stream": { "foo": "bar2" }, "values": [ [ "1570818238000000000", "fizzbuzz" ] ] }]}`),
			contentType:     `application/jsonn; charset=utf-8`,
			contentEncoding: `gzip`,
			valid:           false,
		},
		{
			path:            `/loki/api/v1/push`,
			body:            deflateString(`{"streams": [{ "stream": { "foo": "bar2" }, "values": [ [ "1570818238000000000", "fizzbuzz" ] ] }]}`),
			contentType:     `application/jsonn; charset=utf-8`,
			contentEncoding: `deflate`,
			valid:           false,
		},
		{
			path:            `/loki/api/v1/push`,
			body:            gzipString(`{"streams": [{ "stream": { "foo4": "bar2" }, "values": [ [ "1570818238000000000", "fizzbuzz" ] ] }]}`),
			contentType:     `application/json; charsetutf-8`,
			contentEncoding: `gzip`,
			valid:           false,
		},
		{
			path:            `/loki/api/v1/push`,
			body:            deflateString(`{"streams": [{ "stream": { "foo4": "bar2" }, "values": [ [ "1570818238000000000", "fizzbuzz" ] ] }]}`),
			contentType:     `application/json; charsetutf-8`,
			contentEncoding: `deflate`,
			valid:           false,
		},
		{
			path:            `/loki/api/v1/push`,
			body:            deflateString(`{"streams": [{ "stream": { "foo": "bar2" }, "values": [ [ "1570818238000000000", "fizzbuzz" ] ] }]}`),
			contentType:     `application/jsonn; charset=utf-8`,
			contentEncoding: `deflate`,
			valid:           false,
		},
		{
			path:            `/loki/api/v1/push`,
			body:            deflateString(`{"streams": [{ "stream": { "foo4": "bar2" }, "values": [ [ "1570818238000000000", "fizzbuzz" ] ] }]}`),
			contentType:     `application/json; charsetutf-8`,
			contentEncoding: `deflate`,
			valid:           false,
		},
		{
			path:                            `/loki/api/v1/push`,
			body:                            deflateString(`{"streams": [{ "stream": { "foo": "bar2" }, "values": [ [ "1570818238000000000", "fizzbuzz", {"a": "a", "b": "b"} ] ] }]}`),
			contentType:                     `application/json; charset=utf-8`,
			contentEncoding:                 `deflate`,
			valid:                           true,
			expectedStructuredMetadataBytes: 2*len("a") + 2*len("b"),
			expectedBytes:                   len("fizzbuzz") + 2*len("a") + 2*len("b"),
			expectedLines:                   1,
			expectedBytesUsageTracker:       map[string]float64{`{foo="bar2"}`: float64(len("fizzbuzz") + 2*len("a") + 2*len("b"))},
		},
	} {
		t.Run(fmt.Sprintf("test %d", index), func(t *testing.T) {
			structuredMetadataBytesIngested.Reset()
			bytesIngested.Reset()
			linesIngested.Reset()

			request := httptest.NewRequest("POST", test.path, strings.NewReader(test.body))
			if len(test.contentType) > 0 {
				request.Header.Add("Content-Type", test.contentType)
			}
			if len(test.contentEncoding) > 0 {
				request.Header.Add("Content-Encoding", test.contentEncoding)
			}

			tracker := NewMockTracker()
			data, err := ParseRequest(util_log.Logger, "fake", request, nil, nil, ParseLokiRequest, tracker)

			structuredMetadataBytesReceived := int(structuredMetadataBytesReceivedStats.Value()["total"].(int64)) - previousStructuredMetadataBytesReceived
			previousStructuredMetadataBytesReceived += structuredMetadataBytesReceived
			bytesReceived := int(bytesReceivedStats.Value()["total"].(int64)) - previousBytesReceived
			previousBytesReceived += bytesReceived
			linesReceived := int(linesReceivedStats.Value()["total"].(int64)) - previousLinesReceived
			previousLinesReceived += linesReceived

			if test.valid {
				assert.NoErrorf(t, err, "Should not give error for %d", index)
				assert.NotNil(t, data, "Should give data for %d", index)
				require.Equal(t, test.expectedStructuredMetadataBytes, structuredMetadataBytesReceived)
				require.Equal(t, test.expectedBytes, bytesReceived)
				require.Equal(t, test.expectedLines, linesReceived)
				require.Equal(t, float64(test.expectedStructuredMetadataBytes), testutil.ToFloat64(structuredMetadataBytesIngested.WithLabelValues("fake", "")))
				require.Equal(t, float64(test.expectedBytes), testutil.ToFloat64(bytesIngested.WithLabelValues("fake", "")))
				require.Equal(t, float64(test.expectedLines), testutil.ToFloat64(linesIngested.WithLabelValues("fake")))
				require.InDeltaMapValuesf(t, test.expectedBytesUsageTracker, tracker.receivedBytes, 0.0, "%s != %s", test.expectedBytesUsageTracker, tracker.receivedBytes)
			} else {
				assert.Errorf(t, err, "Should give error for %d", index)
				assert.Nil(t, data, "Should not give data for %d", index)
				require.Equal(t, 0, structuredMetadataBytesReceived)
				require.Equal(t, 0, bytesReceived)
				require.Equal(t, 0, linesReceived)
				require.Equal(t, float64(0), testutil.ToFloat64(structuredMetadataBytesIngested.WithLabelValues("fake", "")))
				require.Equal(t, float64(0), testutil.ToFloat64(bytesIngested.WithLabelValues("fake", "")))
				require.Equal(t, float64(0), testutil.ToFloat64(linesIngested.WithLabelValues("fake")))
			}
		})
	}
}

type MockCustomTracker struct {
	receivedBytes  map[string]float64
	discardedBytes map[string]float64
}

func NewMockTracker() *MockCustomTracker {
	return &MockCustomTracker{
		receivedBytes:  map[string]float64{},
		discardedBytes: map[string]float64{},
	}
}

// DiscardedBytesAdd implements CustomTracker.
func (t *MockCustomTracker) DiscardedBytesAdd(_ context.Context, _, _ string, labels labels.Labels, value float64) {
	t.discardedBytes[labels.String()] += value
}

// ReceivedBytesAdd implements CustomTracker.
func (t *MockCustomTracker) ReceivedBytesAdd(_ context.Context, _ string, _ time.Duration, labels labels.Labels, value float64) {
	t.receivedBytes[labels.String()] += value
}
