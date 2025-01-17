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
		expectedStructuredMetadataBytes int
		expectedBytes                   int
		expectedLines                   int
		expectedBytesUsageTracker       map[string]float64
		expectedLabels                  labels.Labels
		aggregatedMetric                bool
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
			expectedLabels:            labels.FromStrings("foo", "bar2"),
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
			expectedLabels:            labels.FromStrings("foo", "bar2"),
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
			expectedLabels:            labels.FromStrings("foo", "bar2"),
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
			expectedLabels:            labels.FromStrings("foo", "bar2"),
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
			expectedLabels:            labels.FromStrings("foo", "bar2"),
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
			expectedLabels:            labels.FromStrings("foo", "bar2"),
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
			expectedLabels:                  labels.FromStrings("foo", "bar2"),
		},
		{
			path:                      `/loki/api/v1/push`,
			body:                      `{"streams": [{ "stream": { "foo": "bar2", "job": "stuff" }, "values": [ [ "1570818238000000000", "fizzbuzz" ] ] }]}`,
			contentType:               `application/json`,
			valid:                     true,
			enableServiceDiscovery:    true,
			expectedBytes:             len("fizzbuzz"),
			expectedLines:             1,
			expectedBytesUsageTracker: map[string]float64{`{foo="bar2", job="stuff", service_name="stuff"}`: float64(len("fizzbuss"))},
			expectedLabels:            labels.FromStrings("foo", "bar2", "job", "stuff", LabelServiceName, "stuff"),
		},
		{
			path:                      `/loki/api/v1/push`,
			body:                      `{"streams": [{ "stream": { "foo": "bar2" }, "values": [ [ "1570818238000000000", "fizzbuzz" ] ] }]}`,
			contentType:               `application/json`,
			valid:                     true,
			enableServiceDiscovery:    true,
			expectedBytes:             len("fizzbuzz"),
			expectedLines:             1,
			expectedBytesUsageTracker: map[string]float64{`{foo="bar2", service_name="unknown_service"}`: float64(len("fizzbuss"))},
			expectedLabels:            labels.FromStrings("foo", "bar2", LabelServiceName, ServiceUnknown),
		},
		{
			path:                      `/loki/api/v1/push`,
			body:                      `{"streams": [{ "stream": { "__aggregated_metric__": "stuff", "foo": "bar2", "job": "stuff" }, "values": [ [ "1570818238000000000", "fizzbuzz" ] ] }]}`,
			contentType:               `application/json`,
			valid:                     true,
			enableServiceDiscovery:    true,
			expectedBytes:             len("fizzbuzz"),
			expectedLines:             1,
			expectedBytesUsageTracker: map[string]float64{`{__aggregated_metric__="stuff", foo="bar2", job="stuff"}`: float64(len("fizzbuss"))},
			expectedLabels:            labels.FromStrings("__aggregated_metric__", "stuff", "foo", "bar2", "job", "stuff"),
			aggregatedMetric:          true,
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
			data, err := ParseRequest(
				util_log.Logger,
				"fake",
				request,
				nil,
				&fakeLimits{enabled: test.enableServiceDiscovery},
				ParseLokiRequest,
				tracker,
				false,
			)

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
				require.Equalf(t, tracker.Total(), float64(bytesReceived), "tracked usage bytes must equal bytes received metric")
				require.Equal(t, test.expectedLines, linesReceived)
				require.Equal(
					t,
					float64(test.expectedStructuredMetadataBytes),
					testutil.ToFloat64(structuredMetadataBytesIngested.WithLabelValues("fake", "", fmt.Sprintf("%t", test.aggregatedMetric))),
				)
				require.Equal(
					t,
					float64(test.expectedBytes),
					testutil.ToFloat64(
						bytesIngested.WithLabelValues(
							"fake",
							"",
							fmt.Sprintf("%t", test.aggregatedMetric),
						),
					),
				)
				require.Equal(
					t,
					float64(test.expectedLines),
					testutil.ToFloat64(
						linesIngested.WithLabelValues(
							"fake",
							fmt.Sprintf("%t", test.aggregatedMetric),
						),
					),
				)
				require.Equal(t, test.expectedLabels.String(), data.Streams[0].Labels)
				require.InDeltaMapValuesf(t, test.expectedBytesUsageTracker, tracker.receivedBytes, 0.0, "%s != %s", test.expectedBytesUsageTracker, tracker.receivedBytes)
			} else {
				assert.Errorf(t, err, "Should give error for %d", index)
				assert.Nil(t, data, "Should not give data for %d", index)
				require.Equal(t, 0, structuredMetadataBytesReceived)
				require.Equal(t, 0, bytesReceived)
				require.Equal(t, 0, linesReceived)
				require.Equal(t, float64(0), testutil.ToFloat64(structuredMetadataBytesIngested.WithLabelValues("fake", "", fmt.Sprintf("%t", test.aggregatedMetric))))
				require.Equal(t, float64(0), testutil.ToFloat64(bytesIngested.WithLabelValues("fake", "", fmt.Sprintf("%t", test.aggregatedMetric))))
				require.Equal(t, float64(0), testutil.ToFloat64(linesIngested.WithLabelValues("fake", fmt.Sprintf("%t", test.aggregatedMetric))))
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
		data, err := ParseRequest(util_log.Logger, "fake", request, nil, limits, ParseLokiRequest, tracker, false)

		require.NoError(t, err)
		require.Equal(t, labels.FromStrings("foo", "bar", LabelServiceName, "bar").String(), data.Streams[0].Labels)
	})

	t.Run("detects servce from OTLP push requests using default indexing", func(t *testing.T) {
		body := createOtlpLogs("k8s.job.name", "bar")
		request := createRequest("/otlp/v1/push", bytes.NewReader(body))

		limits := &fakeLimits{enabled: true}
		data, err := ParseRequest(util_log.Logger, "fake", request, limits, limits, ParseOTLPRequest, tracker, false)
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
		data, err := ParseRequest(util_log.Logger, "fake", request, limits, limits, ParseOTLPRequest, tracker, false)
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
		data, err := ParseRequest(util_log.Logger, "fake", request, limits, limits, ParseOTLPRequest, tracker, false)
		require.NoError(t, err)
		require.Equal(t, labels.FromStrings(LabelServiceName, ServiceUnknown).String(), data.Streams[0].Labels)
	})
}

type fakeLimits struct {
	enabled         bool
	labels          []string
	indexAttributes []string
}

func (f *fakeLimits) RetentionPeriodFor(_ string, _ labels.Labels) time.Duration {
	return time.Hour
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
