package reader

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/loghttp"
	legacy "github.com/grafana/loki/v3/pkg/loghttp/legacy"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/logqlmodel"
	"github.com/grafana/loki/v3/pkg/logqlmodel/stats"
	"github.com/grafana/loki/v3/pkg/util/httpreq"
	"github.com/grafana/loki/v3/pkg/util/marshal"
)

func TestBuildLabelSelector(t *testing.T) {
	tests := []struct {
		name     string
		labels   string
		sName    string
		sValue   string
		lName    string
		lVal     string
		expected string
		wantErr  bool
	}{
		{
			name:     "uses legacy params when labels is empty",
			labels:   "",
			sName:    "stream",
			sValue:   "stdout",
			lName:    "pod",
			lVal:     "loki-canary-abc",
			expected: `{stream="stdout",pod="loki-canary-abc"}`,
		},
		{
			name:     "uses labels param when set",
			labels:   "service_name=containerd,namespace=loki,container=loki-canary",
			sName:    "stream",
			sValue:   "stdout",
			lName:    "pod",
			lVal:     "loki-canary-abc",
			expected: `{service_name="containerd",namespace="loki",container="loki-canary"}`,
		},
		{
			name:     "single label",
			labels:   "app=loki",
			sName:    "stream",
			sValue:   "stdout",
			lName:    "pod",
			lVal:     "x",
			expected: `{app="loki"}`,
		},
		{
			name:     "label value containing equals sign",
			labels:   "env=key=value",
			sName:    "stream",
			sValue:   "stdout",
			lName:    "pod",
			lVal:     "x",
			expected: `{env="key=value"}`,
		},
		{
			name:    "invalid label format returns error",
			labels:  "badlabel",
			sName:   "stream",
			sValue:  "stdout",
			lName:   "pod",
			lVal:    "x",
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := buildLabelSelector(tc.labels, tc.sName, tc.sValue, tc.lName, tc.lVal)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.expected, got)
		})
	}
}

func TestBuildMetricQuery(t *testing.T) {
	r := &Reader{
		labelSelector: `{service_name="containerd",namespace="loki"}`,
	}

	tests := []struct {
		name         string
		queryAppend  string
		queryRange   string
		expected     string
		validateOTLP bool
	}{
		{
			name:        "no query-append",
			queryAppend: "",
			queryRange:  "5m",
			expected:    `count_over_time({service_name="containerd",namespace="loki"}[5m])`,
		},
		{
			name:        "with query-append filter",
			queryAppend: `| pod="loki-canary-abc"`,
			queryRange:  "5m",
			expected:    `count_over_time({service_name="containerd",namespace="loki"} | pod="loki-canary-abc"[5m])`,
		},
		{
			name:         "OTLP excludes per-record timestamp from count series",
			queryAppend:  `| pod="loki-canary-abc"`,
			queryRange:   "5m",
			expected:     `count_over_time({service_name="containerd",namespace="loki"} | pod="loki-canary-abc" | drop canary_timestamp[5m])`,
			validateOTLP: true,
		},
		{
			name:         "OTLP without query-append",
			queryRange:   "5m",
			expected:     `count_over_time({service_name="containerd",namespace="loki"} | drop canary_timestamp[5m])`,
			validateOTLP: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r.validateOTLP = tc.validateOTLP
			r.queryAppend = tc.queryAppend
			got := r.buildMetricQuery(tc.queryRange)
			require.Equal(t, tc.expected, got)
			_, err := syntax.ParseSampleExpr(got)
			require.NoError(t, err)
		})
	}
}

func TestParseResponseOTLPAttributes(t *testing.T) {
	const timestamp = "1700000000123456789"
	for _, tc := range []struct {
		name       string
		value      string
		parsedOnly bool
		missing    bool
		native     bool
		wantErr    bool
	}{
		{name: "exact nanosecond timestamp", value: timestamp},
		{name: "missing", missing: true, wantErr: true},
		{name: "empty", value: "", wantErr: true},
		{name: "another record", value: "1700000000123456790", wantErr: true},
		{name: "precision lost", value: "1700000000123456800", wantErr: true},
		{name: "malformed", value: "invalid", wantErr: true},
		{name: "parsed label is not structured metadata", value: timestamp, parsedOnly: true, wantErr: true},
		{name: "native does not require attributes", native: true, missing: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := &Reader{validateOTLP: !tc.native}
			entry := loghttp.Entry{Line: timestamp + " padding\n"}
			if !tc.missing {
				entry.StructuredMetadata = labels.FromStrings("canary_timestamp", tc.value)
			}
			if tc.parsedOnly {
				entry.Parsed = entry.StructuredMetadata
				entry.StructuredMetadata = labels.EmptyLabels()
			}
			before := testutil.ToFloat64(otlpValidationErrors)
			got, err := r.parseResponse(&entry)
			if tc.wantErr {
				require.ErrorContains(t, err, "otlp attribute validation failed")
				require.Nil(t, got)
				require.Equal(t, before+1, testutil.ToFloat64(otlpValidationErrors))
			} else {
				require.NoError(t, err)
				require.Equal(t, int64(1700000000123456789), got.UnixNano())
				require.Equal(t, before, testutil.ToFloat64(otlpValidationErrors))
			}
		})
	}
}

func TestOTLPValidationTailAndQuery(t *testing.T) {
	start := time.Unix(1700000000, 123456789)
	stream := logproto.Stream{Labels: `{pod="canary",stream="otlp"}`}
	// Missing metadata, metadata from another record, and a valid record. Put
	// the valid record last so receiving it means the whole tail was processed.
	for i := 0; i < 3; i++ {
		ts := start.Add(time.Duration(i) * time.Nanosecond)
		entry := logproto.Entry{Timestamp: ts, Line: fmt.Sprintf("%d padding\n", ts.UnixNano())}
		if i > 0 {
			value := start.UnixNano()
			if i == 2 {
				value = ts.UnixNano()
			}
			entry.StructuredMetadata = []logproto.LabelAdapter{{Name: "canary_timestamp", Value: fmt.Sprint(value)}}
		}
		stream.Entries = append(stream.Entries, entry)
	}
	flags := httpreq.NewEncodingFlags(httpreq.FlagCategorizeLabels)
	var tailBody, queryBody bytes.Buffer
	require.NoError(t, marshal.WriteTailResponseJSON(legacy.TailResponse{Streams: []logproto.Stream{stream}}, &tailBody, flags))
	require.NoError(t, marshal.WriteQueryResponseJSON(logqlmodel.Streams{stream}, nil, stats.Result{}, &queryBody, flags))

	requests := make(chan *http.Request, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		requests <- req
		switch req.URL.Path {
		case "/loki/api/v1/tail":
			upgrader := websocket.Upgrader{}
			conn, err := upgrader.Upgrade(w, req, nil)
			if err != nil {
				t.Error(err)
				return
			}
			defer conn.Close()
			if err := conn.WriteMessage(websocket.TextMessage, tailBody.Bytes()); err != nil {
				t.Error(err)
				return
			}
			// Keep the websocket open until the reader stops.
			_, _, _ = conn.ReadMessage()
		case "/loki/api/v1/query_range":
			_, _ = w.Write(queryBody.Bytes())
		default:
			http.NotFound(w, req)
		}
	}))
	defer server.Close()

	received := make(chan time.Time, 3)
	before := testutil.ToFloat64(otlpValidationErrors)
	r, err := NewReader(io.Discard, received, false, nil, "", "", "", strings.TrimPrefix(server.URL, "http://"), "",
		"user", "password", "tenant", time.Second, "pod", "canary", "stream", "otlp", time.Minute, "", "", true)
	require.NoError(t, err)
	defer r.Stop()
	want := start.Add(2 * time.Nanosecond)
	select {
	case got := <-received:
		require.Equal(t, want.UnixNano(), got.UnixNano())
	case <-time.After(5 * time.Second):
		t.Fatal("did not receive valid OTLP record from tail")
	}
	require.Empty(t, received)
	got, err := r.Query(start, want.Add(time.Nanosecond))
	require.NoError(t, err)
	require.Equal(t, []time.Time{want}, got)
	require.Equal(t, before+4, testutil.ToFloat64(otlpValidationErrors))
	for i := 0; i < 2; i++ {
		req := <-requests
		require.Equal(t, string(httpreq.FlagCategorizeLabels), req.Header.Get(httpreq.LokiEncodingFlagsHeader))
		require.Equal(t, "tenant", req.Header.Get("X-Scope-OrgID"))
		user, password, ok := req.BasicAuth()
		require.True(t, ok)
		require.Equal(t, "user", user)
		require.Equal(t, "password", password)
	}
}
