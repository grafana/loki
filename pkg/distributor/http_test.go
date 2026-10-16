package distributor

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/gogo/protobuf/proto"
	"github.com/golang/snappy"
	"github.com/grafana/dskit/concurrency"
	ring_client "github.com/grafana/dskit/ring/client"
	"github.com/grafana/dskit/user"

	"github.com/grafana/loki/v3/pkg/runtime"
	"github.com/grafana/loki/v3/pkg/util/constants"

	"github.com/grafana/loki/v3/pkg/kafka"
	"github.com/grafana/loki/v3/pkg/loghttp/push"
	"github.com/grafana/loki/v3/pkg/logproto"

	"github.com/grafana/dskit/flagext"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/plog"
	"go.opentelemetry.io/collector/pdata/plog/plogotlp"

	loki_flagext "github.com/grafana/loki/v3/pkg/util/flagext"
	"github.com/grafana/loki/v3/pkg/validation"
)

func TestDistributorRingHandler(t *testing.T) {
	limits := &validation.Limits{}
	flagext.DefaultValues(limits)

	runServer := func() *httptest.Server {
		distributors, _ := prepare(t, 1, 3, limits, nil)

		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			distributors[0].ServeHTTP(w, r)
		}))
	}

	t.Run("renders ring status for global rate limiting", func(t *testing.T) {
		limits.IngestionRateStrategy = validation.GlobalIngestionRateStrategy
		svr := runServer()
		defer svr.Close()

		resp, err := svr.Client().Get(svr.URL)
		require.NoError(t, err)

		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		require.Contains(t, string(body), "<th>Instance ID</th>")
		require.NotContains(t, string(body), "Not running with Global Rating Limit - ring not being used by the Distributor")
	})

	t.Run("doesn't return ring status for local rate limiting", func(t *testing.T) {
		limits.IngestionRateStrategy = validation.LocalIngestionRateStrategy
		svr := runServer()
		defer svr.Close()

		resp, err := svr.Client().Get(svr.URL)
		require.NoError(t, err)

		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		require.Contains(t, string(body), "Not running with Global Rating Limit - ring not being used by the Distributor")
		require.NotContains(t, string(body), "<th>Instance ID</th>")
	})
}

func TestPushHandlerMaxPushSize(t *testing.T) {
	line := strings.Repeat("a ", 1000)

	limits := &validation.Limits{}
	flagext.DefaultValues(limits)
	limits.RejectOldSamples = false
	_ = limits.MaxPushSize.Set("1000")
	distributors, _ := prepare(t, 1, 3, limits, nil)

	newPushRequest := func() *logproto.PushRequest {
		return &logproto.PushRequest{
			Streams: []logproto.Stream{
				{
					Labels:  `{foo="bar"}`,
					Entries: []logproto.Entry{{Timestamp: time.Now(), Line: line}},
				},
			},
		}
	}

	withGzip := func(t *testing.T, b []byte) []byte {
		t.Helper()
		buf := bytes.Buffer{}
		w := gzip.NewWriter(&buf)
		_, err := w.Write(b)
		require.NoError(t, err)
		require.NoError(t, w.Close())
		return buf.Bytes()
	}

	for _, tc := range []struct {
		name            string
		path            string
		contentType     string
		contentEncoding string
		format          string
		parser          push.RequestParser
		errorWriter     push.ErrorWriter
		buildBody       func(t *testing.T) []byte
	}{
		{
			name:        "plain proto returns 413 because its over max size",
			path:        "/loki/api/v1/push",
			contentType: "application/x-protobuf",
			format:      constants.Loki,
			parser:      push.ParseLokiRequest,
			errorWriter: push.HTTPError,
			buildBody: func(_ *testing.T) []byte {
				body, err := proto.Marshal(newPushRequest())
				require.NoError(t, err)
				return body
			},
		},
		{
			name:        "Plain JSON returns 413 because its over max size",
			path:        "/loki/api/v1/push",
			contentType: "application/json",
			format:      constants.Loki,
			parser:      push.ParseLokiRequest,
			errorWriter: push.HTTPError,
			buildBody: func(_ *testing.T) []byte {
				return []byte(`{"streams":[{"stream":{"foo":"bar"},"values":[["1234567890000000000","` + line + `"]]}]}`)
			},
		},
		{
			name:        "Plain OTLP returns 413 because its over max size",
			path:        "/otlp/v1/logs",
			contentType: "application/json",
			format:      constants.OTLP,
			parser:      push.NewOTLPRequestParser(false),
			errorWriter: push.OTLPError,
			buildBody: func(t *testing.T) []byte {
				otlpLogs := plog.NewLogs()
				rl := otlpLogs.ResourceLogs().AppendEmpty()
				rl.Resource().Attributes().PutStr("service.name", "test-service")
				lr := rl.ScopeLogs().AppendEmpty().LogRecords().AppendEmpty()
				lr.Body().SetStr(line)
				lr.SetTimestamp(pcommon.Timestamp(time.Now().UnixNano()))
				body, err := plogotlp.NewExportRequestFromLogs(otlpLogs).MarshalJSON()
				require.NoError(t, err)
				return body
			},
		},
		{
			name:        "snappy compressed protobuf returns 413 because the decompressed data is over max size",
			path:        "/loki/api/v1/push",
			contentType: "application/x-protobuf",
			format:      constants.Loki,
			parser:      push.ParseLokiRequest,
			errorWriter: push.HTTPError,
			buildBody: func(t *testing.T) []byte {
				protoBytes, err := proto.Marshal(newPushRequest())
				require.NoError(t, err)
				return snappy.Encode(nil, protoBytes)
			},
		},
		{
			name:            "gzip compressed Loki JSON returns 413 because the decompressed size is over max size",
			path:            "/loki/api/v1/push",
			contentType:     "application/json",
			contentEncoding: "gzip",
			format:          constants.Loki,
			parser:          push.ParseLokiRequest,
			errorWriter:     push.HTTPError,
			buildBody: func(t *testing.T) []byte {
				lokiJSON := []byte(`{"streams":[{"stream":{"foo":"bar"},"values":[["1234567890000000000","` + line + `"]]}]}`)
				return withGzip(t, lokiJSON)
			},
		},
		{
			name:            "gzip compressed OTLP JSON returns 413 because the decompressed size is over max size",
			path:            "/otlp/v1/logs",
			contentType:     "application/json",
			contentEncoding: "gzip",
			format:          constants.OTLP,
			parser:          push.NewOTLPRequestParser(false),
			errorWriter:     push.OTLPError,
			buildBody: func(t *testing.T) []byte {
				otlpLogs := plog.NewLogs()
				rl := otlpLogs.ResourceLogs().AppendEmpty()
				rl.Resource().Attributes().PutStr("service.name", "test-service")
				lr := rl.ScopeLogs().AppendEmpty().LogRecords().AppendEmpty()
				lr.Body().SetStr(line)
				lr.SetTimestamp(pcommon.Timestamp(time.Now().UnixNano()))
				otlpJSON, err := plogotlp.NewExportRequestFromLogs(otlpLogs).MarshalJSON()
				require.NoError(t, err)
				return withGzip(t, otlpJSON)
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := tc.buildBody(t)

			req := httptest.NewRequest(http.MethodPost, tc.path, bytes.NewReader(body))
			ctx := user.InjectOrgID(t.Context(), "test")
			req = req.WithContext(ctx)
			req.Header.Set("Content-Type", tc.contentType)
			if tc.contentEncoding != "" {
				req.Header.Set("Content-Encoding", tc.contentEncoding)
			}

			// The metric is a global counter shared across tests, so measure the
			// delta produced by this request rather than an absolute value.
			discardedBytes := validation.DiscardedBytes.WithLabelValues(validation.RequestBodyTooLarge, "test", "", "", tc.format)
			before := testutil.ToFloat64(discardedBytes)

			rec := httptest.NewRecorder()
			distributors[0].pushHandler(rec, req, tc.parser, tc.errorWriter, tc.format)

			require.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)
			require.Equal(t, float64(req.ContentLength), testutil.ToFloat64(discardedBytes)-before)
		})
	}
}

func TestPushHandlerLogPushRequestStreams(t *testing.T) {
	limits := &validation.Limits{}
	flagext.DefaultValues(limits)
	limits.RejectOldSamples = false
	distributors, _ := prepare(t, 1, 3, limits, nil)
	d := distributors[0]

	// Capture the log output.
	out := &concurrency.SyncBuffer{}
	d.logger = log.NewLogfmtLogger(out)

	labelValues := []string{"bar", "baz"}
	labels := make([]string, 0, len(labelValues))
	for _, v := range labelValues {
		labels = append(labels, fmt.Sprintf("{foo=%q}", v))
	}
	b, err := proto.Marshal(makeWriteRequestWithLabels(1, 10, labels, false, false, false))
	require.NoError(t, err)
	b = snappy.Encode(nil, b)

	for _, tc := range []struct {
		name             string
		cfg              runtime.Config
		forwardedFor     string
		expectedLines    int
		expectedFields   []string
		unexpectedFields []string
	}{
		{
			name:          "logs nothing when disabled",
			cfg:           runtime.Config{},
			expectedLines: 0,
		},
		{
			name:             "logs one line per stream when enabled",
			cfg:              runtime.Config{LogPushRequestStreams: true},
			expectedLines:    2,
			expectedFields:   []string{"level=debug", "org_id=test", "mostRecentLagMs=", "policy="},
			unexpectedFields: []string{"presumedAgentIp", `streamSizeBytes="0 B"`},
		},
		{
			name:             "logs the first X-Forwarded-For address as the presumed agent IP",
			cfg:              runtime.Config{LogPushRequestStreams: true},
			forwardedFor:     "10.0.0.1, 10.0.0.2",
			expectedLines:    2,
			expectedFields:   []string{"presumedAgentIp=10.0.0.1"},
			unexpectedFields: []string{"10.0.0.2"},
		},
		{
			name: "logs when the presumed agent IP is in the filter list",
			cfg: runtime.Config{
				LogPushRequestStreams:       true,
				FilterPushRequestStreamsIPs: []string{"10.0.0.1"},
			},
			forwardedFor:  "10.0.0.1, 10.0.0.2",
			expectedLines: 2,
		},
		{
			name: "logs nothing when the presumed agent IP is not in the filter list",
			cfg: runtime.Config{
				LogPushRequestStreams:       true,
				FilterPushRequestStreamsIPs: []string{"10.0.0.1"},
			},
			forwardedFor:  "10.0.0.9",
			expectedLines: 0,
		},
		{
			name: "logs nothing when the filter list is set but there is no presumed agent IP",
			cfg: runtime.Config{
				LogPushRequestStreams:       true,
				FilterPushRequestStreamsIPs: []string{"10.0.0.1"},
			},
			expectedLines: 0,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out.Reset()

			d.tenantConfigs, err = runtime.NewTenantConfigs(&fakeTenantConfigProvider{cfg: tc.cfg})
			require.NoError(t, err)

			req := httptest.NewRequest(http.MethodPost, "/loki/api/v1/push", bytes.NewReader(b))
			req = req.WithContext(user.InjectOrgID(t.Context(), "test"))
			req.Header.Set("Content-Type", "application/x-protobuf")
			req.Header.Set("Content-Encoding", "snappy")
			if tc.forwardedFor != "" {
				req.Header.Set("X-Forwarded-For", tc.forwardedFor)
			}

			rec := httptest.NewRecorder()
			d.pushHandler(rec, req, push.ParseLokiRequest, push.HTTPError, constants.Loki)
			require.Equal(t, http.StatusNoContent, rec.Code)

			// Filter just "push request streams" lines from the output.
			lines := strings.Split(out.String(), "\n")
			containsLines := make([]string, 0, len(lines))
			for _, line := range lines {
				if strings.Contains(line, "msg=\"push request streams\"") {
					containsLines = append(containsLines, line)
				}
			}
			require.Len(t, containsLines, tc.expectedLines)

			for i, line := range containsLines {
				require.Contains(t, line, fmt.Sprintf("foo=\\\"%s\\\"", labelValues[i]))
				for _, field := range tc.expectedFields {
					require.Contains(t, line, field)
				}
				for _, field := range tc.unexpectedFields {
					require.NotContains(t, line, field)
				}
			}
		})
	}
}

type fakeTenantConfigProvider struct {
	cfg runtime.Config
}

func (p *fakeTenantConfigProvider) TenantConfig(_ string) *runtime.Config {
	return &p.cfg
}

func TestOTLPPushHandlerAttributeExpansion(t *testing.T) {
	for _, tc := range []struct {
		name          string
		metadataSize  int
		metadataCount int
		kafkaEnabled  bool
		status        int
	}{
		{name: "accepted", metadataSize: 1024, metadataCount: 10, status: http.StatusNoContent},
		{name: "accepted with Kafka and ingesters", metadataSize: 1024, metadataCount: 10, kafkaEnabled: true, status: http.StatusNoContent},
		{name: "shared metadata exceeds size limit", metadataSize: 14, metadataCount: 10, status: http.StatusBadRequest},
		{name: "shared metadata exceeds count limit", metadataSize: 1024, metadataCount: 2, status: http.StatusBadRequest},
	} {
		for _, deferExpansion := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/deferExpansion=%t", tc.name, deferExpansion), func(t *testing.T) {
				// set the configurations
				var cfg Config
				flagext.DefaultValues(&cfg)
				require.False(t, cfg.OTLPConfig.DeferAttributeExpansion)
				lim := &validation.Limits{}
				flagext.DefaultValues(lim)
				lim.SetGlobalOTLPConfig(cfg.OTLPConfig)
				lim.DiscoverLogLevels = false
				lim.MaxStructuredMetadataSize = loki_flagext.ByteSize(tc.metadataSize)
				lim.MaxStructuredMetadataEntriesCount = tc.metadataCount
				ing := &mockIngester{}
				distributors, _ := prepareButDontStart(t, 1, 3, lim, func(string) (ring_client.PoolClient, error) { return ing, nil })
				d := distributors[0]
				d.cfg.OTLPConfig.DeferAttributeExpansion = deferExpansion
				tee := &mockTee{}
				d.tee = tee
				producer := &mockKafkaProducer{}
				if tc.kafkaEnabled {
					d.cfg.KafkaEnabled = true
					d.cfg.KafkaConfig.ProducerMaxRecordSizeBytes = 1024
					d.kafkaWriter = producer
				}
				startAndWaitRunningDistributors(t, distributors)

				// prepare the otlp push request
				logs := plog.NewLogs()
				resource := logs.ResourceLogs().AppendEmpty()
				resource.Resource().Attributes().PutStr("service.name", "test-service")
				resource.Resource().Attributes().PutStr("resource.key", "resource")
				scope := resource.ScopeLogs().AppendEmpty()
				scope.Scope().Attributes().PutStr("scope.key", "scope")
				at := time.Now().UTC()
				for i := range 2 {
					record := scope.LogRecords().AppendEmpty()
					record.SetTimestamp(pcommon.Timestamp(at.Add(time.Duration(i) * time.Second).UnixNano()))
					record.Body().SetStr(fmt.Sprint(i))
					record.Attributes().PutStr("entry.key", "entry")
				}
				body, err := plogotlp.NewExportRequestFromLogs(logs).MarshalProto()
				require.NoError(t, err)

				// send the push request
				req := httptest.NewRequest(http.MethodPost, "/otlp/v1/logs", bytes.NewReader(body))
				req = req.WithContext(user.InjectOrgID(t.Context(), "test"))
				req.Header.Set("Content-Type", "application/x-protobuf")
				response := httptest.NewRecorder()
				d.OTLPPushHandler(response, req)
				require.Equal(t, tc.status, response.Code, response.Body.String())

				// if the push is expected to fail, ingesters should not receive any data
				if tc.status != http.StatusNoContent {
					require.Nil(t, ing.Peek())
					return
				}

				// read the data pushed to mock ingester and verify it
				got := ing.Peek()
				require.NotNil(t, got)
				require.Len(t, got.Streams, 1)
				require.Equal(t, `{service_name="test-service"}`, got.Streams[0].Labels)
				require.Len(t, got.Streams[0].Entries, 2)
				for i, entry := range got.Streams[0].Entries {
					require.Equal(t, fmt.Sprint(i), entry.Line)
					require.True(t, at.Add(time.Duration(i)*time.Second).Equal(entry.Timestamp))
					require.ElementsMatch(t, []logproto.LabelAdapter{
						{Name: "entry_key", Value: "entry"},
						{Name: "resource_key", Value: "resource"},
						{Name: "scope_key", Value: "scope"},
					}, entry.StructuredMetadata)
				}
				if tc.kafkaEnabled {
					require.Len(t, producer.records, 1)
					var nested logproto.InternalStreamAdapter
					if deferExpansion {
						require.NoError(t, nested.Unmarshal(producer.records[0].Value))
						require.Equal(t, []logproto.LabelAdapter{{Name: "resource_key", Value: "resource"}}, nested.ResourceLogs[0].Attrs)
						require.Equal(t, []logproto.LabelAdapter{{Name: "scope_key", Value: "scope"}}, nested.ResourceLogs[0].ScopeLogs[0].Attrs)
					} else {
						require.Error(t, nested.Unmarshal(producer.records[0].Value))
					}
					decoder, err := kafka.NewDecoder()
					require.NoError(t, err)
					decoded, err := decoder.DecodeWithoutLabels(producer.records[0].Value)
					require.NoError(t, err)
					require.Equal(t, got.Streams[0].Labels, decoded.Labels)
					require.Equal(t, got.Streams[0].Hash, decoded.Hash)
					require.Len(t, decoded.Entries, len(got.Streams[0].Entries))
					for i, entry := range decoded.Entries {
						want := got.Streams[0].Entries[i]
						require.True(t, want.Timestamp.Equal(entry.Timestamp))
						require.Equal(t, want.Line, entry.Line)
						require.ElementsMatch(t, want.StructuredMetadata, entry.StructuredMetadata)
					}
				}
				tee.mu.Lock()
				defer tee.mu.Unlock()
				require.Len(t, tee.duplicated, 1)
				res := tee.duplicated[0][0].Stream.ResourceLogs[0]
				if !deferExpansion {
					require.Empty(t, res.Attrs)
					require.Empty(t, res.ScopeLogs[0].Attrs)
				} else {
					require.Equal(t, []logproto.LabelAdapter{{Name: "resource_key", Value: "resource"}}, res.Attrs)
					require.Equal(t, []logproto.LabelAdapter{{Name: "scope_key", Value: "scope"}}, res.ScopeLogs[0].Attrs)
					require.Equal(t, []logproto.LabelAdapter{{Name: "entry_key", Value: "entry"}}, []logproto.LabelAdapter(res.ScopeLogs[0].Entries[0].StructuredMetadata))
				}
			})
		}
	}
}

func TestOTLPPushHandlerValidation(t *testing.T) {
	type record struct {
		route string
		age   time.Duration
	}
	type forwardedEntry struct {
		labels    string
		line      string
		timestamp int64
	}
	for _, tc := range []struct {
		name           string
		service        string
		records        []record
		maxValueLength int
		status         int
		accepted       []int // Record indexes expected at the ingester.
	}{
		{name: "empty request", status: http.StatusUnprocessableEntity},
		{name: "resource with no logs", service: "svc", status: http.StatusUnprocessableEntity},
		{name: "valid", service: "svc", records: []record{{route: "ok"}}, status: http.StatusNoContent, accepted: []int{0}},
		{name: "non UTF-8 log label value", service: "svc", records: []record{{route: "\xff"}}, status: http.StatusUnprocessableEntity},
		{name: "mixed UTF-8 and non UTF-8 log label values", service: "svc", records: []record{{route: "ok"}, {route: "\xff"}}, status: http.StatusNoContent, accepted: []int{0}},
		{name: "non UTF-8 resource label value", service: "\xff", records: []record{{route: "ok"}}, status: http.StatusUnprocessableEntity},
		{name: "resource label value exceeds limit", service: "toolong", records: []record{{route: "ok"}}, maxValueLength: 4, status: http.StatusBadRequest},
		{name: "all log label values exceed limit", service: "svc", records: []record{{route: "toolong"}}, maxValueLength: 4, status: http.StatusBadRequest},
		{name: "some log label values exceed limit", service: "svc", records: []record{{route: "ok"}, {route: "toolong"}}, maxValueLength: 4, status: http.StatusBadRequest, accepted: []int{0}},
		{name: "all entries too old", service: "svc", records: []record{{route: "ok", age: 200 * time.Hour}}, status: http.StatusBadRequest},
		{name: "some entries too old", service: "svc", records: []record{{route: "ok"}, {route: "ok", age: 200 * time.Hour}}, status: http.StatusBadRequest, accepted: []int{0}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			now := time.Now().UTC()
			logs := plog.NewLogs()
			if tc.service != "" {
				resource := logs.ResourceLogs().AppendEmpty()
				resource.Resource().Attributes().PutStr("service.name", tc.service)
				scope := resource.ScopeLogs().AppendEmpty()
				for i, r := range tc.records {
					logRecord := scope.LogRecords().AppendEmpty()
					logRecord.SetTimestamp(pcommon.Timestamp(now.Add(-r.age).UnixNano()))
					logRecord.Body().SetStr(fmt.Sprintf("entry-%d", i))
					logRecord.Attributes().PutStr("route", r.route)
				}
			}
			body, err := plogotlp.NewExportRequestFromLogs(logs).MarshalProto()
			require.NoError(t, err)

			var want []forwardedEntry
			for _, i := range tc.accepted {
				r := tc.records[i]
				want = append(want, forwardedEntry{
					labels:    fmt.Sprintf("{route=%q, service_name=%q}", r.route, tc.service),
					line:      fmt.Sprintf("entry-%d", i),
					timestamp: now.Add(-r.age).UnixNano(),
				})
			}

			for _, deferExpansion := range []bool{false, true} {
				t.Run(fmt.Sprintf("deferExpansion=%t", deferExpansion), func(t *testing.T) {
					var cfg Config
					flagext.DefaultValues(&cfg)
					lim := &validation.Limits{}
					flagext.DefaultValues(lim)
					lim.SetGlobalOTLPConfig(cfg.OTLPConfig)
					lim.OTLPConfig.LogAttributes = []push.AttributesConfig{{Action: push.IndexLabel, Attributes: []string{"route"}}}
					lim.DiscoverLogLevels = false
					if tc.maxValueLength > 0 {
						lim.MaxLabelValueLength = tc.maxValueLength
					}
					ing := &mockIngester{}
					distributors, _ := prepareButDontStart(t, 1, 3, lim, func(string) (ring_client.PoolClient, error) { return ing, nil })
					d := distributors[0]
					d.cfg.OTLPConfig.DeferAttributeExpansion = deferExpansion
					startAndWaitRunningDistributors(t, distributors)

					req := httptest.NewRequest(http.MethodPost, "/otlp/v1/logs", bytes.NewReader(body))
					req = req.WithContext(user.InjectOrgID(t.Context(), "test"))
					req.Header.Set("Content-Type", "application/x-protobuf")
					response := httptest.NewRecorder()
					d.OTLPPushHandler(response, req)
					require.Equal(t, tc.status, response.Code, response.Body.String())

					var got []forwardedEntry
					if request := ing.Peek(); request != nil {
						for _, stream := range request.Streams {
							for _, entry := range stream.Entries {
								got = append(got, forwardedEntry{labels: stream.Labels, line: entry.Line, timestamp: entry.Timestamp.UnixNano()})
							}
						}
					}
					require.ElementsMatch(t, want, got)
					if len(want) == 0 {
						require.Nil(t, ing.Peek())
					}
				})
			}
		})
	}
}
