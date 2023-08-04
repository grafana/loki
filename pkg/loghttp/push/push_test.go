package push

import (
	"bytes"
	"compress/flate"
	"compress/gzip"
	"fmt"
	"log"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
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
	var previousBytesReceived, previousNonIndexedLabelsBytesReceived, previousLinesReceived int
	for index, test := range []struct {
		path                          string
		body                          string
		contentType                   string
		contentEncoding               string
		valid                         bool
		expectedNonIndexedLabelsBytes int
		expectedBytes                 int
		expectedLines                 int
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
			path:          `/loki/api/v1/push`,
			body:          `{"streams": [{ "stream": { "foo": "bar2" }, "values": [ [ "1570818238000000000", "fizzbuzz" ] ] }]}`,
			contentType:   `application/json`,
			valid:         true,
			expectedBytes: len("fizzbuzz"),
			expectedLines: 1,
		},
		{
			path:            `/loki/api/v1/push`,
			body:            `{"streams": [{ "stream": { "foo": "bar2" }, "values": [ [ "1570818238000000000", "fizzbuzz" ] ] }]}`,
			contentType:     `application/json`,
			contentEncoding: ``,
			valid:           true,
			expectedBytes:   len("fizzbuzz"),
			expectedLines:   1,
		},
		{
			path:            `/loki/api/v1/push`,
			body:            `{"streams": [{ "stream": { "foo": "bar2" }, "values": [ [ "1570818238000000000", {"fizz": "buzz"} ] ] }]}`,
			contentType:     `application/json`,
			contentEncoding: ``,
			valid:           false,
		},
		{
			path:            `/loki/api/v1/push`,
			body:            gzipString(`{"streams": [{ "stream": { "foo": "bar2" }, "values": [ [ "1570818238000000000", "fizzbuzz" ] ] }]}`),
			contentType:     `application/json`,
			contentEncoding: `gzip`,
			valid:           true,
			expectedBytes:   len("fizzbuzz"),
			expectedLines:   1,
		},
		{
			path:            `/loki/api/v1/push`,
			body:            deflateString(`{"streams": [{ "stream": { "foo": "bar2" }, "values": [ [ "1570818238000000000", "fizzbuzz" ] ] }]}`),
			contentType:     `application/json`,
			contentEncoding: `deflate`,
			valid:           true,
			expectedBytes:   len("fizzbuzz"),
			expectedLines:   1,
		},
		{
			path:            `/loki/api/v1/push`,
			body:            gzipString(`{"streams": [{ "stream": { "foo": "bar2" }, "values": [ [ "1570818238000000000", "fizzbuzz" ] ] }]}`),
			contentType:     `application/json`,
			contentEncoding: `snappy`,
			valid:           false,
		},
		{
			path:            `/loki/api/v1/push`,
			body:            gzipString(`{"streams": [{ "stream": { "foo": "bar2" }, "values": [ [ "1570818238000000000", "fizzbuzz" ] ] }]}`),
			contentType:     `application/json; charset=utf-8`,
			contentEncoding: `gzip`,
			valid:           true,
			expectedBytes:   len("fizzbuzz"),
			expectedLines:   1,
		},
		{
			path:            `/loki/api/v1/push`,
			body:            deflateString(`{"streams": [{ "stream": { "foo": "bar2" }, "values": [ [ "1570818238000000000", "fizzbuzz" ] ] }]}`),
			contentType:     `application/json; charset=utf-8`,
			contentEncoding: `deflate`,
			valid:           true,
			expectedBytes:   len("fizzbuzz"),
			expectedLines:   1,
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
			path:                          `/loki/api/v1/push`,
			body:                          deflateString(`{"streams": [{ "stream": { "foo": "bar2" }, "values": [ [ "1570818238000000000", "fizzbuzz", {"a": "a", "b": "b"} ] ] }]}`),
			contentType:                   `application/json; charset=utf-8`,
			contentEncoding:               `deflate`,
			valid:                         true,
			expectedNonIndexedLabelsBytes: 2*len("a") + 2*len("b"),
			expectedBytes:                 len("fizzbuzz") + 2*len("a") + 2*len("b"),
			expectedLines:                 1,
		},
	} {
		t.Run(fmt.Sprintf("test %d", index), func(t *testing.T) {
			nonIndexedLabelsBytesIngested.Reset()
			bytesIngested.Reset()
			linesIngested.Reset()

			request := httptest.NewRequest("POST", test.path, strings.NewReader(test.body))
			if len(test.contentType) > 0 {
				request.Header.Add("Content-Type", test.contentType)
			}
			if len(test.contentEncoding) > 0 {
				request.Header.Add("Content-Encoding", test.contentEncoding)
			}

			data, err := ParseRequest(util_log.Logger, "fake", request, nil)

			nonIndexedLabelsBytesReceived := int(nonIndexedLabelsBytesReceivedStats.Value()["total"].(int64)) - previousNonIndexedLabelsBytesReceived
			previousNonIndexedLabelsBytesReceived += nonIndexedLabelsBytesReceived
			bytesReceived := int(bytesReceivedStats.Value()["total"].(int64)) - previousBytesReceived
			previousBytesReceived += bytesReceived
			linesReceived := int(linesReceivedStats.Value()["total"].(int64)) - previousLinesReceived
			previousLinesReceived += linesReceived

			if test.valid {
				assert.Nil(t, err, "Should not give error for %d", index)
				assert.NotNil(t, data, "Should give data for %d", index)
				require.Equal(t, test.expectedNonIndexedLabelsBytes, nonIndexedLabelsBytesReceived)
				require.Equal(t, test.expectedBytes, bytesReceived)
				require.Equal(t, test.expectedLines, linesReceived)
				require.Equal(t, float64(test.expectedNonIndexedLabelsBytes), testutil.ToFloat64(nonIndexedLabelsBytesIngested.WithLabelValues("fake", "")))
				require.Equal(t, float64(test.expectedBytes), testutil.ToFloat64(bytesIngested.WithLabelValues("fake", "")))
				require.Equal(t, float64(test.expectedLines), testutil.ToFloat64(linesIngested.WithLabelValues("fake")))
			} else {
				assert.NotNil(t, err, "Should give error for %d", index)
				assert.Nil(t, data, "Should not give data for %d", index)
				require.Equal(t, 0, nonIndexedLabelsBytesReceived)
				require.Equal(t, 0, bytesReceived)
				require.Equal(t, 0, linesReceived)
				require.Equal(t, float64(0), testutil.ToFloat64(nonIndexedLabelsBytesIngested.WithLabelValues("fake", "")))
				require.Equal(t, float64(0), testutil.ToFloat64(bytesIngested.WithLabelValues("fake", "")))
				require.Equal(t, float64(0), testutil.ToFloat64(linesIngested.WithLabelValues("fake")))
			}
		})
	}
}
