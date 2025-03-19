package push

import (
	"bytes"
	"compress/flate"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/plog"

	"github.com/grafana/dskit/flagext"

	util_log "github.com/grafana/loki/v3/pkg/util/log"
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
		enableServiceDiscovery          bool
		expectedStructuredMetadataBytes map[string]int
		expectedBytes                   map[string]int
		expectedLines                   map[string]int
		expectedBytesUsageTracker       map[string]float64
		expectedLabels                  []labels.Labels
		aggregatedMetric                bool
		fakeLimits                      *fakeLimits
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
			expectedBytes:             map[string]int{"": len("fizzbuzz")},
			expectedLines:             map[string]int{"": 1},
			expectedBytesUsageTracker: map[string]float64{`{foo="bar2"}`: float64(len("fizzbuss"))},
			expectedLabels:            []labels.Labels{labels.FromStrings("foo", "bar2")},
		},
		{
			path:                      `/loki/api/v1/push`,
			body:                      `{"streams": [{ "stream": { "foo": "bar2" }, "values": [ [ "1570818238000000000", "fizzbuzz" ] ] }]}`,
			contentType:               `application/json`,
			contentEncoding:           ``,
			valid:                     true,
			expectedBytes:             map[string]int{"": len("fizzbuzz")},
			expectedLines:             map[string]int{"": 1},
			expectedBytesUsageTracker: map[string]float64{`{foo="bar2"}`: float64(len("fizzbuss"))},
			expectedLabels:            []labels.Labels{labels.FromStrings("foo", "bar2")},
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
			expectedBytes:             map[string]int{"": len("fizzbuzz")},
			expectedLines:             map[string]int{"": 1},
			expectedBytesUsageTracker: map[string]float64{`{foo="bar2"}`: float64(len("fizzbuss"))},
			expectedLabels:            []labels.Labels{labels.FromStrings("foo", "bar2")},
		},
		{
			path:                      `/loki/api/v1/push`,
			body:                      deflateString(`{"streams": [{ "stream": { "foo": "bar2" }, "values": [ [ "1570818238000000000", "fizzbuzz" ] ] }]}`),
			contentType:               `application/json`,
			contentEncoding:           `deflate`,
			valid:                     true,
			expectedBytes:             map[string]int{"": len("fizzbuzz")},
			expectedLines:             map[string]int{"": 1},
			expectedBytesUsageTracker: map[string]float64{`{foo="bar2"}`: float64(len("fizzbuss"))},
			expectedLabels:            []labels.Labels{labels.FromStrings("foo", "bar2")},
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
			expectedBytes:             map[string]int{"": len("fizzbuzz")},
			expectedLines:             map[string]int{"": 1},
			expectedBytesUsageTracker: map[string]float64{`{foo="bar2"}`: float64(len("fizzbuss"))},
			expectedLabels:            []labels.Labels{labels.FromStrings("foo", "bar2")},
		},
		{
			path:                      `/loki/api/v1/push`,
			body:                      deflateString(`{"streams": [{ "stream": { "foo": "bar2" }, "values": [ [ "1570818238000000000", "fizzbuzz" ] ] }]}`),
			contentType:               `application/json; charset=utf-8`,
			contentEncoding:           `deflate`,
			valid:                     true,
			expectedBytes:             map[string]int{"": len("fizzbuzz")},
			expectedLines:             map[string]int{"": 1},
			expectedBytesUsageTracker: map[string]float64{`{foo="bar2"}`: float64(len("fizzbuss"))},
			expectedLabels:            []labels.Labels{labels.FromStrings("foo", "bar2")},
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
			expectedStructuredMetadataBytes: map[string]int{"": 2*len("a") + 2*len("b")},
			expectedBytes:                   map[string]int{"": len("fizzbuzz") + 2*len("a") + 2*len("b")},
			expectedLines:                   map[string]int{"": 1},
			expectedBytesUsageTracker:       map[string]float64{`{foo="bar2"}`: float64(len("fizzbuzz") + 2*len("a") + 2*len("b"))},
			expectedLabels:                  []labels.Labels{labels.FromStrings("foo", "bar2")},
		},
		{
			path:                      `/loki/api/v1/push`,
			body:                      `{"streams": [{ "stream": { "foo": "bar2", "job": "stuff" }, "values": [ [ "1570818238000000000", "fizzbuzz" ] ] }]}`,
			contentType:               `application/json`,
			valid:                     true,
			enableServiceDiscovery:    true,
			expectedBytes:             map[string]int{"": len("fizzbuss")},
			expectedLines:             map[string]int{"": 1},
			expectedBytesUsageTracker: map[string]float64{`{foo="bar2", job="stuff", service_name="stuff"}`: float64(len("fizzbuss"))},
			expectedLabels:            []labels.Labels{labels.FromStrings("foo", "bar2", "job", "stuff", LabelServiceName, "stuff")},
		},
		{
			path:                      `/loki/api/v1/push`,
			body:                      `{"streams": [{ "stream": { "foo": "bar2" }, "values": [ [ "1570818238000000000", "fizzbuzz" ] ] }]}`,
			contentType:               `application/json`,
			valid:                     true,
			enableServiceDiscovery:    true,
			expectedBytes:             map[string]int{"": len("fizzbuzz")},
			expectedLines:             map[string]int{"": 1},
			expectedBytesUsageTracker: map[string]float64{`{foo="bar2", service_name="unknown_service"}`: float64(len("fizzbuss"))},
			expectedLabels:            []labels.Labels{labels.FromStrings("foo", "bar2", LabelServiceName, ServiceUnknown)},
		},
		{
			path:                      `/loki/api/v1/push`,
			body:                      `{"streams": [{ "stream": { "__aggregated_metric__": "stuff", "foo": "bar2", "job": "stuff" }, "values": [ [ "1570818238000000000", "fizzbuzz" ] ] }]}`,
			contentType:               `application/json`,
			valid:                     true,
			enableServiceDiscovery:    true,
			expectedBytes:             map[string]int{"": len("fizzbuss")},
			expectedLines:             map[string]int{"": 1},
			expectedBytesUsageTracker: map[string]float64{`{__aggregated_metric__="stuff", foo="bar2", job="stuff"}`: float64(len("fizzbuss"))},
			expectedLabels:            []labels.Labels{labels.FromStrings("__aggregated_metric__", "stuff", "foo", "bar2", "job", "stuff")},
			aggregatedMetric:          true,
		},
		{
			path:                      `/loki/api/v1/push`,
			body:                      `{"streams": [{ "stream": { "foo": "bar2", "environment": "prod" }, "values": [ [ "1570818238000000000", "fizzbuzz" ] ] }]}`,
			contentType:               `application/json`,
			valid:                     true,
			expectedBytes:             map[string]int{"prod": len("fizzbuzz")},
			expectedLines:             map[string]int{"prod": 1},
			expectedBytesUsageTracker: map[string]float64{"{environment=\"prod\", foo=\"bar2\"}": float64(len("fizzbuzz"))},
			expectedLabels:            []labels.Labels{labels.FromStrings("foo", "bar2", "environment", "prod")},
		},
		{
			path:                      `/loki/api/v1/push`,
			body:                      `{"streams": [{ "stream": { "foo": "bar2", "environment": "dev" }, "values": [ [ "1570818238000000000", "fizzbuzz" ] ] }]}`,
			contentType:               `application/json`,
			valid:                     true,
			expectedBytes:             map[string]int{"dev": len("fizzbuzz")},
			expectedLines:             map[string]int{"dev": 1},
			expectedBytesUsageTracker: map[string]float64{"{environment=\"dev\", foo=\"bar2\"}": float64(len("fizzbuzz"))},
			expectedLabels:            []labels.Labels{labels.FromStrings("foo", "bar2", "environment", "dev")},
		}, {
			path:                      `/loki/api/v1/push`,
			body:                      `{"streams": [{ "stream": { "foo": "bar2", "environment": "dev" }, "values": [ [ "1570818238000000000", "fizzbuzz" ] ] }, {"stream": { "foo": "bar2", "environment": "prod" }, "values": [ [ "1570818238000000000", "xx" ] ] }, {"stream": { "foo": "bar2", "dass": "zasss" }, "values": [ [ "1570818238000000000", "graf" ] ] }]}`,
			contentType:               `application/json`,
			valid:                     true,
			expectedBytes:             map[string]int{"dev": len("fizzbuzz"), "prod": len("xx"), "": len("graf")},
			expectedLines:             map[string]int{"dev": 1, "prod": 1, "": 1},
			expectedBytesUsageTracker: map[string]float64{"{environment=\"dev\", foo=\"bar2\"}": float64(len("fizzbuzz")), "{environment=\"prod\", foo=\"bar2\"}": float64(len("xx")), "{dass=\"zasss\", foo=\"bar2\"}": float64(len("graf"))},
			expectedLabels:            []labels.Labels{labels.FromStrings("foo", "bar2", "environment", "dev"), labels.FromStrings("foo", "bar2", "environment", "prod"), labels.FromStrings("foo", "bar2", "dass", "zasss")},
		},
	} {
		t.Run(fmt.Sprintf("test %d", index), func(t *testing.T) {
			streamResolver := newMockStreamResolver("fake", test.fakeLimits)

			structuredMetadataBytesIngested.Reset()
			bytesIngested.Reset()
			linesIngested.Reset()
			if test.fakeLimits == nil {
				test.fakeLimits = &fakeLimits{enabled: test.enableServiceDiscovery}
			}

			request := httptest.NewRequest("POST", test.path, strings.NewReader(test.body))
			if len(test.contentType) > 0 {
				request.Header.Add("Content-Type", test.contentType)
			}
			if len(test.contentEncoding) > 0 {
				request.Header.Add("Content-Encoding", test.contentEncoding)
			}

			tracker := NewMockTracker()
			data, err := ParseRequest(
				util_log.Logger,
				"fake",
				request,
				test.fakeLimits,
				ParseLokiRequest,
				tracker,
				streamResolver,
				false,
			)

			structuredMetadataBytesReceived := int(structuredMetadataBytesReceivedStats.Value()["total"].(int64)) - previousStructuredMetadataBytesReceived
			previousStructuredMetadataBytesReceived += structuredMetadataBytesReceived
			bytesReceived := int(bytesReceivedStats.Value()["total"].(int64)) - previousBytesReceived
			previousBytesReceived += bytesReceived
			linesReceived := int(linesReceivedStats.Value()["total"].(int64)) - previousLinesReceived
			previousLinesReceived += linesReceived

			totalStructuredMetadataBytes := 0
			for _, bytes := range test.expectedStructuredMetadataBytes {
				totalStructuredMetadataBytes += bytes
			}

			totalBytes := 0
			for _, bytes := range test.expectedBytes {
				totalBytes += bytes
			}

			totalLines := 0
			for _, lines := range test.expectedLines {
				totalLines += lines
			}

			if test.valid {
				assert.NoErrorf(t, err, "Should not give error for %d", index)
				assert.NotNil(t, data, "Should give data for %d", index)
				require.Equal(t, totalStructuredMetadataBytes, structuredMetadataBytesReceived)
				require.Equal(t, totalBytes, bytesReceived)
				require.Equalf(t, tracker.Total(), float64(bytesReceived), "tracked usage bytes must equal bytes received metric")
				require.Equal(t, totalLines, linesReceived)

				for policyName, bytes := range test.expectedStructuredMetadataBytes {
					require.Equal(
						t,
						float64(bytes),
						testutil.ToFloat64(structuredMetadataBytesIngested.WithLabelValues("fake", "1" /* We use "1" here because fakeLimits.RetentionHoursFor returns "1" */, fmt.Sprintf("%t", test.aggregatedMetric), policyName)),
					)
				}

				for policyName, bytes := range test.expectedBytes {
					require.Equal(
						t,
						float64(bytes),
						testutil.ToFloat64(
							bytesIngested.WithLabelValues(
								"fake",
								"1", // We use "1" here because fakeLimits.RetentionHoursFor returns "1"
								fmt.Sprintf("%t", test.aggregatedMetric),
								policyName,
							),
						),
					)
				}

				for policyName, lines := range test.expectedLines {
					require.Equal(
						t,
						float64(lines),
						testutil.ToFloat64(
							linesIngested.WithLabelValues(
								"fake",
								fmt.Sprintf("%t", test.aggregatedMetric),
								policyName,
							),
						),
						"policy %s with %d lines",
						policyName,
						lines,
					)
				}

				for i := range test.expectedLabels {
					require.EqualValues(t, test.expectedLabels[i].String(), data.Streams[i].Labels)
				}
				require.InDeltaMapValuesf(t, test.expectedBytesUsageTracker, tracker.receivedBytes, 0.0, "%s != %s", test.expectedBytesUsageTracker, tracker.receivedBytes)
			} else {
				assert.Errorf(t, err, "Should give error for %d", index)
				assert.Nil(t, data, "Should not give data for %d", index)
				require.Equal(t, 0, structuredMetadataBytesReceived)
				require.Equal(t, 0, bytesReceived)
				require.Equal(t, 0, linesReceived)
				policy := ""
				require.Equal(t, float64(0), testutil.ToFloat64(structuredMetadataBytesIngested.WithLabelValues("fake", "1" /* We use "1" here because fakeLimits.RetentionHoursFor returns "1" */, fmt.Sprintf("%t", test.aggregatedMetric), policy)))
				require.Equal(t, float64(0), testutil.ToFloat64(bytesIngested.WithLabelValues("fake", "1" /* We use "1" here because fakeLimits.RetentionHoursFor returns "1" */, fmt.Sprintf("%t", test.aggregatedMetric), policy)))
				require.Equal(t, float64(0), testutil.ToFloat64(linesIngested.WithLabelValues("fake", fmt.Sprintf("%t", test.aggregatedMetric), policy)))
			}
		})
	}
}

func Test_ServiceDetection(t *testing.T) {
	tracker := NewMockTracker()

	createOtlpLogs := func(labels ...string) []byte {
		now := time.Unix(0, time.Now().UnixNano())
		ld := plog.NewLogs()
		for i := 0; i < len(labels); i += 2 {
			ld.ResourceLogs().AppendEmpty().Resource().Attributes().PutStr(labels[i], labels[i+1])
		}
		ld.ResourceLogs().At(0).ScopeLogs().AppendEmpty().LogRecords().AppendEmpty().Body().SetStr("test body")
		ld.ResourceLogs().At(0).ScopeLogs().At(0).LogRecords().At(0).SetTimestamp(pcommon.Timestamp(now.UnixNano()))

		jsonMarshaller := plog.JSONMarshaler{}
		body, err := jsonMarshaller.MarshalLogs(ld)

		require.NoError(t, err)
		return body
	}

	createRequest := func(path string, body io.Reader) *http.Request {
		request := httptest.NewRequest(
			"POST",
			path,
			body,
		)
		request.Header.Add("Content-Type", "application/json")

		return request
	}

	t.Run("detects servce from loki push requests", func(t *testing.T) {
		body := `{"streams": [{ "stream": { "foo": "bar" }, "values": [ [ "1570818238000000000", "fizzbuzz" ] ] }]}`
		request := createRequest("/loki/api/v1/push", strings.NewReader(body))

		limits := &fakeLimits{enabled: true, labels: []string{"foo"}}
		streamResolver := newMockStreamResolver("fake", limits)
		data, err := ParseRequest(util_log.Logger, "fake", request, limits, ParseLokiRequest, tracker, streamResolver, false)

		require.NoError(t, err)
		require.Equal(t, labels.FromStrings("foo", "bar", LabelServiceName, "bar").String(), data.Streams[0].Labels)
	})

	t.Run("detects servce from OTLP push requests using default indexing", func(t *testing.T) {
		body := createOtlpLogs("k8s.job.name", "bar")
		request := createRequest("/otlp/v1/push", bytes.NewReader(body))

		limits := &fakeLimits{enabled: true}
		streamResolver := newMockStreamResolver("fake", limits)
		data, err := ParseRequest(util_log.Logger, "fake", request, limits, ParseOTLPRequest, tracker, streamResolver, false)
		require.NoError(t, err)
		require.Equal(t, labels.FromStrings("k8s_job_name", "bar", LabelServiceName, "bar").String(), data.Streams[0].Labels)
	})

	t.Run("detects servce from OTLP push requests using custom indexing", func(t *testing.T) {
		body := createOtlpLogs("special", "sauce")
		request := createRequest("/otlp/v1/push", bytes.NewReader(body))

		limits := &fakeLimits{
			enabled:         true,
			labels:          []string{"special"},
			indexAttributes: []string{"special"},
		}
		streamResolver := newMockStreamResolver("fake", limits)
		data, err := ParseRequest(util_log.Logger, "fake", request, limits, ParseOTLPRequest, tracker, streamResolver, false)
		require.NoError(t, err)
		require.Equal(t, labels.FromStrings("special", "sauce", LabelServiceName, "sauce").String(), data.Streams[0].Labels)
	})

	t.Run("only detects custom service label from indexed labels", func(t *testing.T) {
		body := createOtlpLogs("special", "sauce")
		request := createRequest("/otlp/v1/push", bytes.NewReader(body))

		limits := &fakeLimits{
			enabled:         true,
			labels:          []string{"special"},
			indexAttributes: []string{},
		}
		streamResolver := newMockStreamResolver("fake", limits)
		data, err := ParseRequest(util_log.Logger, "fake", request, limits, ParseOTLPRequest, tracker, streamResolver, false)
		require.NoError(t, err)
		require.Equal(t, labels.FromStrings(LabelServiceName, ServiceUnknown).String(), data.Streams[0].Labels)
	})
}

func BenchmarkRetentionPeriodToString(b *testing.B) {
	testCases := []struct {
		name            string
		retentionPeriod time.Duration
	}{
		{
			name:            "744h",
			retentionPeriod: 744 * time.Hour,
		},
		{
			name:            "840h",
			retentionPeriod: 840 * time.Hour,
		},
		{
			name:            "2232h",
			retentionPeriod: 2232 * time.Hour,
		},
		{
			name:            "8784h",
			retentionPeriod: 8784 * time.Hour,
		},
		{
			name:            "1000h",
			retentionPeriod: 1000 * time.Hour,
		},
		{
			name:            "zero retention period",
			retentionPeriod: 0,
		},
	}

	for _, tc := range testCases {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			for n := 0; n < b.N; n++ {
				RetentionPeriodToString(tc.retentionPeriod)
			}
		})
	}
}

func TestRetentionPeriodToString(t *testing.T) {
	testCases := []struct {
		name            string
		retentionPeriod time.Duration
		expected        string
	}{
		{
			name:            "744h",
			retentionPeriod: 744 * time.Hour,
			expected:        "744",
		},
		{
			name:            "840h",
			retentionPeriod: 840 * time.Hour,
			expected:        "840",
		},
		{
			name:            "2232h",
			retentionPeriod: 2232 * time.Hour,
			expected:        "2232",
		},
		{
			name:            "8784h",
			retentionPeriod: 8784 * time.Hour,
			expected:        "8784",
		},
		{
			name:            "1000h",
			retentionPeriod: 1000 * time.Hour,
			expected:        "1000",
		},
		{
			name:            "zero retention period",
			retentionPeriod: 0,
			expected:        "",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			actual := RetentionPeriodToString(tc.retentionPeriod)
			assert.Equal(t, tc.expected, actual)
		})
	}
}

type fakeLimits struct {
	enabled         bool
	labels          []string
	indexAttributes []string
}

func (f *fakeLimits) RetentionPeriodFor(_ string, _ labels.Labels) time.Duration {
	return time.Hour
}

func (f *fakeLimits) RetentionHoursFor(_ string, _ labels.Labels) string {
	return "1"
}

func (f *fakeLimits) OTLPConfig(_ string) OTLPConfig {
	if len(f.indexAttributes) > 0 {
		return OTLPConfig{
			ResourceAttributes: ResourceAttributesConfig{
				AttributesConfig: []AttributesConfig{
					{
						Action:     IndexLabel,
						Attributes: f.indexAttributes,
					},
				},
			},
		}
	}

	defaultGlobalOTLPConfig := GlobalOTLPConfig{}
	flagext.DefaultValues(&defaultGlobalOTLPConfig)
	return DefaultOTLPConfig(defaultGlobalOTLPConfig)
}

func (f *fakeLimits) PolicyFor(_ string, lbs labels.Labels) string {
	return lbs.Get("environment")
}

func (f *fakeLimits) DiscoverServiceName(_ string) []string {
	if !f.enabled {
		return nil
	}

	if len(f.labels) > 0 {
		return f.labels
	}

	return []string{
		"service",
		"app",
		"application",
		"name",
		"app_kubernetes_io_name",
		"container",
		"container_name",
		"k8s_container_name",
		"component",
		"workload",
		"job",
		"k8s_job_name",
	}
}

type mockStreamResolver struct {
	tenant string
	limits *fakeLimits

	policyForOverride func(lbs labels.Labels) string
}

func newMockStreamResolver(tenant string, limits *fakeLimits) *mockStreamResolver {
	return &mockStreamResolver{
		tenant: tenant,
		limits: limits,
	}
}

func (m mockStreamResolver) RetentionPeriodFor(lbs labels.Labels) time.Duration {
	return m.limits.RetentionPeriodFor(m.tenant, lbs)
}

func (m mockStreamResolver) RetentionHoursFor(lbs labels.Labels) string {
	return m.limits.RetentionHoursFor(m.tenant, lbs)
}

func (m mockStreamResolver) PolicyFor(lbs labels.Labels) string {
	if m.policyForOverride != nil {
		return m.policyForOverride(lbs)
	}

	return m.limits.PolicyFor(m.tenant, lbs)
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

func (t *MockCustomTracker) Total() float64 {
	total := float64(0)
	for _, v := range t.receivedBytes {
		total += v
	}
	return total
}

// DiscardedBytesAdd implements CustomTracker.
func (t *MockCustomTracker) DiscardedBytesAdd(_ context.Context, _, _ string, labels labels.Labels, value float64) {
	t.discardedBytes[labels.String()] += value
}

// ReceivedBytesAdd implements CustomTracker.
func (t *MockCustomTracker) ReceivedBytesAdd(_ context.Context, _ string, _ time.Duration, labels labels.Labels, value float64) {
	t.receivedBytes[labels.String()] += value
}
