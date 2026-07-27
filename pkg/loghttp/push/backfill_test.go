package push

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	gokitlog "github.com/go-kit/log"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/plog"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/util/constants"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
)

const testBackfillShard = "3f8b1c9a-uuid"

func TestExtractAndValidateBackfillShard(t *testing.T) {
	for _, tc := range []struct {
		name      string
		setHeader bool
		value     string
		wantShard string
		wantOK    bool
		wantErr   bool
	}{
		{name: "no header"},
		{name: "empty header", setHeader: true, value: ""},
		{name: "uuid", setHeader: true, value: "3f8b1c9a-uuid", wantShard: "3f8b1c9a-uuid", wantOK: true},
		{name: "date bucket", setHeader: true, value: "2026-06-10", wantShard: "2026-06-10", wantOK: true},
		{name: "arbitrary opaque", setHeader: true, value: "worker-7/bucket=1h", wantShard: "worker-7/bucket=1h", wantOK: true},
		{name: "too long", setHeader: true, value: strings.Repeat("a", maxBackfillShardLen+1), wantErr: true},
		{name: "invalid label value", setHeader: true, value: "bad\xffvalue", wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPost, "/loki/api/v1/push", nil)
			if tc.setHeader {
				r.Header.Set(HTTPHeaderBackfillShardKey, tc.value)
			}

			shard, ok, err := ExtractAndValidateBackfillShard(r)
			if tc.wantErr {
				require.Error(t, err)
				require.Contains(t, err.Error(), HTTPHeaderBackfillShardKey)
				require.False(t, ok)
				require.Empty(t, shard)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.wantOK, ok)
			require.Equal(t, tc.wantShard, shard)
		})
	}
}

func TestBackfillShardContext(t *testing.T) {
	require.Empty(t, ExtractBackfillShardContext(context.Background()))

	ctx := InjectBackfillShardContext(context.Background(), testBackfillShard)
	require.Equal(t, testBackfillShard, ExtractBackfillShardContext(ctx))
}

func TestParseRequest_BackfillShard(t *testing.T) {
	const lokiBody = `{"streams":[{"stream":{"foo":"bar"},"values":[["1570818238000000000","fizzbuzz"]]}]}`

	parse := func(r *http.Request, parser RequestParser, limits *fakeLimits) (*logproto.PushRequest, error) {
		streamResolver := newMockStreamResolver("fake", limits)
		data, _, err := ParseRequest(util_log.Logger, "fake", 100<<20, 100<<20, r, limits, nil, parser, NewMockTracker(), streamResolver, "", "loki")
		return data, err
	}

	t.Run("loki: header adds backfill labels to every stream", func(t *testing.T) {
		req, err := parse(newBackfillLokiRequest(lokiBody, testBackfillShard), ParseLokiRequest, &fakeLimits{enabled: true, labels: []string{"foo"}})
		require.NoError(t, err)
		require.Len(t, req.Streams, 1)
		requireBackfillLabels(t, req.Streams[0].Labels)
		// Original labels are preserved.
		lbs, err := syntax.ParseLabels(req.Streams[0].Labels)
		require.NoError(t, err)
		require.Equal(t, "bar", lbs.Get("foo"))
	})

	t.Run("loki: no header leaves labels untouched", func(t *testing.T) {
		req, err := parse(newBackfillLokiRequest(lokiBody, ""), ParseLokiRequest, &fakeLimits{enabled: true, labels: []string{"foo"}})
		require.NoError(t, err)
		require.Len(t, req.Streams, 1)
		requireNoBackfillLabels(t, req.Streams[0].Labels)
	})

	t.Run("malformed header rejects the whole push", func(t *testing.T) {
		req, err := parse(newBackfillLokiRequest(lokiBody, "bad\xffvalue"), ParseLokiRequest, &fakeLimits{enabled: true})
		require.Error(t, err)
		require.Contains(t, err.Error(), HTTPHeaderBackfillShardKey)
		require.Nil(t, req)
	})

	t.Run("otlp: header adds backfill labels", func(t *testing.T) {
		req, err := parse(newBackfillOTLPRequest(t, singleResourceLogs("service.name", "service-1"), testBackfillShard), ParseOTLPRequest, &fakeLimits{enabled: true})
		require.NoError(t, err)
		require.Len(t, req.Streams, 1)
		requireBackfillLabels(t, req.Streams[0].Labels)
	})

	t.Run("otlp: restrictive tenant config cannot drop backfill labels", func(t *testing.T) {
		// Service-name discovery is off and the only indexed attribute does not match the resource,
		// so without injection this stream would carry no index labels at all.
		req, err := parse(newBackfillOTLPRequest(t, singleResourceLogs("service.name", "service-1"), testBackfillShard), ParseOTLPRequest, &fakeLimits{enabled: false, indexAttributes: []string{"nonexistent"}})
		require.NoError(t, err)
		require.Len(t, req.Streams, 1)
		requireBackfillLabels(t, req.Streams[0].Labels)
	})
}

// TestOTLPBackfillLabelsOnCombinedStreams covers the per-log-record stream path, where a log
// attribute promoted to an index label produces a stream from combinedLabels (a copy of the resource
// stream labels). The backfill labels must be inherited there too.
func TestOTLPBackfillLabelsOnCombinedStreams(t *testing.T) {
	now := time.Unix(0, time.Now().UnixNano())

	cfg := DefaultOTLPConfig(GlobalOTLPConfig{DefaultOTLPResourceAttributesAsIndexLabels: []string{"service.name"}})
	cfg.LogAttributes = []AttributesConfig{{Action: IndexLabel, Attributes: []string{"detected_level"}}}

	ld := plog.NewLogs()
	rl := ld.ResourceLogs().AppendEmpty()
	rl.Resource().Attributes().PutStr("service.name", "svc")
	rec := rl.ScopeLogs().AppendEmpty().LogRecords().AppendEmpty()
	rec.Body().SetStr("hello")
	rec.SetTimestamp(pcommon.Timestamp(now.UnixNano()))
	rec.Attributes().PutStr("detected_level", "info")

	stats := NewPushStats()
	streamResolver := newMockStreamResolver("fake", &fakeLimits{})
	ctx := InjectBackfillShardContext(context.Background(), testBackfillShard)

	pushReq, err := otlpToLokiPushRequest(ctx, ld, "fake", cfg, nil, []string{}, NewMockTracker(), stats, gokitlog.NewNopLogger(), streamResolver, constants.OTLP)
	require.NoError(t, err)

	nonEmpty := 0
	for _, s := range pushReq.Streams {
		if len(s.Entries) == 0 {
			continue
		}
		nonEmpty++
		requireBackfillLabels(t, s.Labels)
		require.Contains(t, s.Labels, `detected_level="info"`)
	}
	require.Equal(t, 1, nonEmpty, "expected one combined stream carrying the indexed log attribute")
}

func newBackfillLokiRequest(body, backfillShard string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/loki/api/v1/push", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	if backfillShard != "" {
		r.Header.Set(HTTPHeaderBackfillShardKey, backfillShard)
	}
	return r
}

func newBackfillOTLPRequest(t *testing.T, ld plog.Logs, backfillShard string) *http.Request {
	t.Helper()
	body, err := (&plog.JSONMarshaler{}).MarshalLogs(ld)
	require.NoError(t, err)
	r := httptest.NewRequest(http.MethodPost, "/otlp/v1/logs", bytes.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	if backfillShard != "" {
		r.Header.Set(HTTPHeaderBackfillShardKey, backfillShard)
	}
	return r
}

func singleResourceLogs(resourceAttrs ...string) plog.Logs {
	ld := plog.NewLogs()
	rl := ld.ResourceLogs().AppendEmpty()
	for i := 0; i+1 < len(resourceAttrs); i += 2 {
		rl.Resource().Attributes().PutStr(resourceAttrs[i], resourceAttrs[i+1])
	}
	rec := rl.ScopeLogs().AppendEmpty().LogRecords().AppendEmpty()
	rec.Body().SetStr("test body")
	rec.SetTimestamp(pcommon.Timestamp(time.Now().UnixNano()))
	return ld
}

func requireBackfillLabels(t *testing.T, labelsStr string) {
	t.Helper()
	lbs, err := syntax.ParseLabels(labelsStr)
	require.NoError(t, err, "labels=%s", labelsStr)
	require.Equal(t, "true", lbs.Get(constants.BackfillLabel), "labels=%s", labelsStr)
	require.Equal(t, testBackfillShard, lbs.Get(constants.BackfillShardLabel), "labels=%s", labelsStr)
}

func requireNoBackfillLabels(t *testing.T, labelsStr string) {
	t.Helper()
	lbs, err := syntax.ParseLabels(labelsStr)
	require.NoError(t, err, "labels=%s", labelsStr)
	require.False(t, lbs.Has(constants.BackfillLabel), "labels=%s", labelsStr)
	require.False(t, lbs.Has(constants.BackfillShardLabel), "labels=%s", labelsStr)
}
