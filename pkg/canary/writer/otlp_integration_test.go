package writer_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/backoff"
	"github.com/prometheus/common/config"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/canary/writer"
	"github.com/grafana/loki/v3/pkg/loghttp/push"
	"github.com/grafana/loki/v3/pkg/logproto"
)

// Exercise the actual canary HTTP writer against Loki's ingestion parser so
// gzip decompression, resource-label promotion, timestamp precision and body preservation
// are checked together, including batched records in a single stream.
func TestCanaryOTLPRoundTrip(t *testing.T) {
	for _, batchSize := range []int{1, 3} {
		t.Run(fmt.Sprint(batchSize), func(t *testing.T) {
			type result struct {
				request  *logproto.InternalPushRequest
				encoding string
				err      error
			}
			results := make(chan result, 1)
			parserConfig := canaryOTLPTestConfig{}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				request, _, err := push.NewOTLPRequestParser(false)("canary", r, parserConfig, nil, 1024*1024, 1024*1024, nil, parserConfig, log.NewNopLogger())
				results <- result{request, r.Header.Get("Content-Encoding"), err}
				if err != nil {
					http.Error(w, err.Error(), http.StatusBadRequest)
					return
				}
				w.WriteHeader(http.StatusNoContent)
			}))
			defer server.Close()
			pusher, err := writer.NewPush(strings.TrimPrefix(server.URL, "http://"), "", "canary", time.Second,
				config.DefaultHTTPClientConfig, "canary_instance", "replica-1", "canary_protocol", "otlp",
				false, nil, "", "", "", "", "", &backoff.Config{MaxRetries: 1}, batchSize, writer.PushProtocolOTLP, log.NewNopLogger())
			require.NoError(t, err)
			defer pusher.Stop()
			start := time.Unix(1700000000, 123456789)
			for i := 0; i < batchSize; i++ {
				ts := start.Add(time.Duration(i) * time.Nanosecond)
				pusher.WriteEntry(ts, fmt.Sprintf("%d padding\n", ts.UnixNano()))
			}
			select {
			case got := <-results:
				require.NoError(t, got.err)
				require.Equal(t, "gzip", got.encoding)
				require.Len(t, got.request.Streams, 1)
				stream := got.request.Streams[0].FlatView()
				require.Equal(t, `{canary_instance="replica-1", canary_protocol="otlp", service_name="loki-canary"}`, stream.Labels)
				require.Len(t, stream.Entries, batchSize)
				for i, entry := range stream.Entries {
					ts := start.Add(time.Duration(i) * time.Nanosecond)
					require.Equal(t, ts.UnixNano(), entry.Timestamp.UnixNano())
					require.Equal(t, fmt.Sprintf("%d padding\n", ts.UnixNano()), entry.Line)
					require.Contains(t, entry.StructuredMetadata, logproto.LabelAdapter{Name: "scope_name", Value: "loki-canary"})
					require.Contains(t, entry.StructuredMetadata, logproto.LabelAdapter{Name: "canary_timestamp", Value: fmt.Sprint(ts.UnixNano())})
				}
			case <-time.After(5 * time.Second):
				t.Fatal("canary did not reach Loki's OTLP parser")
			}
		})
	}
}

// canaryOTLPTestConfig supplies tenant settings and stream resolution to the parser.
type canaryOTLPTestConfig struct{}

func (canaryOTLPTestConfig) OTLPConfig(string) push.OTLPConfig {
	return push.OTLPConfig{
		ResourceAttributes: push.ResourceAttributesConfig{
			AttributesConfig: []push.AttributesConfig{{
				Action:     push.IndexLabel,
				Attributes: []string{"service.name", "canary_instance", "canary_protocol"},
			}},
		},
	}
}

func (canaryOTLPTestConfig) DiscoverServiceName(string) []string             { return nil }
func (canaryOTLPTestConfig) MaxPushSize(string) int                          { return 1024 * 1024 }
func (canaryOTLPTestConfig) RetentionPeriodFor(labels.Labels) time.Duration  { return time.Hour }
func (canaryOTLPTestConfig) RetentionHoursFor(labels.Labels) string          { return "1" }
func (canaryOTLPTestConfig) PolicyFor(context.Context, labels.Labels) string { return "" }
