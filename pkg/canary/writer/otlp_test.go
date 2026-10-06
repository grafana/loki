package writer

import (
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/backoff"
	"github.com/prometheus/common/config"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/pdata/plog/plogotlp"
	"go.uber.org/atomic"
)

func TestOTLPPush(t *testing.T) {
	for _, tc := range []struct {
		name, prefix string
		batch        int
		tls          bool
	}{
		{name: "single", batch: 1},
		{name: "batch", batch: 3},
		{name: "prefix", prefix: "/loki/", batch: 1},
		{name: "tls", prefix: "/custom", batch: 3, tls: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			type request struct {
				path, contentType, encoding, tenant, user, pass string
				body                                            []byte
				err                                             error
			}
			requests := make(chan request, 1)
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, err := io.ReadAll(r.Body)
				user, pass, _ := r.BasicAuth()
				requests <- request{r.URL.Path, r.Header.Get("Content-Type"), r.Header.Get("Content-Encoding"), r.Header.Get("X-Scope-OrgID"), user, pass, body, err}
				w.WriteHeader(http.StatusOK)
			})
			server := httptest.NewUnstartedServer(handler)
			if tc.tls {
				server.StartTLS()
			} else {
				server.Start()
			}
			defer server.Close()
			httpCfg := config.DefaultHTTPClientConfig
			httpCfg.TLSConfig.InsecureSkipVerify = tc.tls // Test server's self-signed certificate.
			addr := strings.TrimPrefix(strings.TrimPrefix(server.URL, "http://"), "https://")
			wantPath := strings.TrimSuffix(tc.prefix, "/") + "/otlp/v1/logs"
			p, err := NewPush(addr, tc.prefix, "write-tenant", time.Second, httpCfg,
				"canary_instance", "replica-1", "canary_protocol", "otlp", tc.tls, nil, "", "", "",
				"canary-user", "canary-token", &backoff.Config{MaxRetries: 1}, tc.batch, PushProtocolOTLP, log.NewNopLogger())
			require.NoError(t, err)
			defer p.Stop()
			start := time.Unix(1700000000, 123456789)
			for i := 0; i < tc.batch; i++ {
				ts := start.Add(time.Duration(i) * time.Nanosecond)
				p.WriteEntry(ts, fmt.Sprintf("%d padding\n", ts.UnixNano()))
			}
			select {
			case r := <-requests:
				require.NoError(t, r.err)
				require.Equal(t, wantPath, r.path)
				require.Equal(t, "application/x-protobuf", r.contentType)
				require.Equal(t, "gzip", r.encoding)
				require.Equal(t, "write-tenant", r.tenant)
				require.Equal(t, "canary-user", r.user)
				require.Equal(t, "canary-token", r.pass)
				zr, err := gzip.NewReader(bytes.NewReader(r.body))
				require.NoError(t, err)
				payload, err := io.ReadAll(zr)
				require.NoError(t, zr.Close())
				require.NoError(t, err)
				decoded := plogotlp.NewExportRequest()
				require.NoError(t, decoded.UnmarshalProto(payload))
				require.Equal(t, tc.batch, decoded.Logs().LogRecordCount())
				resource := decoded.Logs().ResourceLogs().At(0)
				require.Equal(t, map[string]any{"service.name": "loki-canary", "canary_instance": "replica-1", "canary_protocol": "otlp"}, resource.Resource().Attributes().AsRaw())
				records := resource.ScopeLogs().At(0).LogRecords()
				for i := 0; i < tc.batch; i++ {
					ts := start.Add(time.Duration(i) * time.Nanosecond)
					require.Equal(t, ts.UnixNano(), records.At(i).Timestamp().AsTime().UnixNano())
					require.Equal(t, fmt.Sprintf("%d padding\n", ts.UnixNano()), records.At(i).Body().Str())
					require.Equal(t, map[string]any{"canary_timestamp": ts.UnixNano()}, records.At(i).Attributes().AsRaw())
				}
			case <-time.After(5 * time.Second):
				t.Fatal("OTLP request did not arrive")
			}
		})
	}
}

func TestOTLPResponse(t *testing.T) {
	partial := plogotlp.NewExportResponse()
	partial.PartialSuccess().SetRejectedLogRecords(2)
	partial.PartialSuccess().SetErrorMessage("rejected")
	data, err := partial.MarshalProto()
	require.NoError(t, err)
	for _, tc := range []struct {
		name string
		body []byte
		want string
	}{
		{name: "empty success"},
		{name: "partial success", body: data, want: "rejected 2 log records"},
		{name: "invalid protobuf", body: []byte("invalid"), want: "decode OTLP response"},
		{name: "oversized", body: bytes.Repeat([]byte("x"), 65537), want: "exceeds"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write(tc.body)
			}))
			defer server.Close()
			p := &Push{lokiURL: server.URL, protocol: PushProtocolOTLP, httpClient: server.Client(), logger: log.NewNopLogger()}
			p.httpClient.Timeout = time.Second
			status, err := p.send(context.Background(), nil)
			require.Equal(t, http.StatusOK, status) // Non-retryable, including partial success.
			if tc.want == "" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, tc.want)
			}
		})
	}
}

func TestOTLPRetry(t *testing.T) {
	for _, batchSize := range []int{1, 3} {
		t.Run(fmt.Sprint(batchSize), func(t *testing.T) {
			var attempts atomic.Int32
			type request struct {
				body     []byte
				encoding string
				err      error
			}
			requests := make(chan request, 2)
			done := make(chan struct{}, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, err := io.ReadAll(r.Body)
				requests <- request{body, r.Header.Get("Content-Encoding"), err}
				if attempts.Add(1) == 1 {
					w.WriteHeader(http.StatusTooManyRequests)
					return
				}
				w.WriteHeader(http.StatusNoContent)
				done <- struct{}{}
			}))
			defer server.Close()
			p, err := NewPush(strings.TrimPrefix(server.URL, "http://"), "", "", time.Second, config.DefaultHTTPClientConfig,
				"name", "replica", "stream", "otlp", false, nil, "", "", "", "", "",
				&backoff.Config{MinBackoff: time.Millisecond, MaxBackoff: time.Millisecond, MaxRetries: 2},
				batchSize, PushProtocolOTLP, log.NewNopLogger())
			require.NoError(t, err)
			defer p.Stop()
			for i := 0; i < batchSize; i++ {
				p.WriteEntry(time.Now(), "entry")
			}
			select {
			case <-done:
				require.EqualValues(t, 2, attempts.Load())
				first, retry := <-requests, <-requests
				require.NoError(t, first.err)
				require.NoError(t, retry.err)
				require.Equal(t, "gzip", first.encoding)
				require.Equal(t, first.encoding, retry.encoding)
				require.NotEmpty(t, first.body)
				require.Equal(t, first.body, retry.body)
			case <-time.After(5 * time.Second):
				t.Fatal("OTLP write was not retried")
			}
		})
	}
}
