package push

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	gokitlog "github.com/go-kit/log"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/plog"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/util/constants"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
)

func TestExtractAndValidateForcedLabels(t *testing.T) {
	manyLabels := make([]string, 0, maxForcedLabels+1)
	for i := 0; i <= maxForcedLabels; i++ {
		manyLabels = append(manyLabels, "l"+strings.Repeat("x", i)+`="v"`)
	}

	for _, tc := range []struct {
		name        string
		values      []string // header values; nil means the header is not set
		want        labels.Labels
		wantOK      bool
		errContains string
	}{
		{name: "no header"},
		{name: "empty header", values: []string{""}},
		{name: "one label", values: []string{`{user="alice"}`}, want: labels.FromStrings("user", "alice"), wantOK: true},
		{name: "several labels", values: []string{`{user="alice", site="edge-07"}`}, want: labels.FromStrings("site", "edge-07", "user", "alice"), wantOK: true},
		{name: "value with comma, equals and quotes", values: []string{`{user="a,b=\"c\""}`}, want: labels.FromStrings("user", `a,b="c"`), wantOK: true},
		{name: "surrounding whitespace", values: []string{`  { user = "alice" }  `}, want: labels.FromStrings("user", "alice"), wantOK: true},
		{name: "max number of labels", values: []string{"{" + strings.Join(manyLabels[:maxForcedLabels], ",") + "}"}, wantOK: true},

		{name: "empty braces", values: []string{`{}`}, errContains: "at least one label"},
		{name: "reserved name", values: []string{`{__name__="x"}`}, errContains: `reserved label name "__name__"`},
		{name: "reserved backfill label", values: []string{`{` + constants.BackfillLabel + `="true"}`}, errContains: "reserved label name"},
		{name: "invalid name", values: []string{`{"my.label"="x"}`}, errContains: `invalid label name "my.label"`},
		{name: "empty value", values: []string{`{user="", site="edge-07"}`}, errContains: `label "user" has an empty value`},
		{name: "value not valid UTF-8", values: []string{"{user=\"bad\xffvalue\"}"}, errContains: "invalid UTF-8"},
		{name: "duplicate name", values: []string{`{user="alice", user="bob"}`}, errContains: `duplicate label name "user"`},
		{name: "too many labels", values: []string{"{" + strings.Join(manyLabels, ",") + "}"}, errContains: "exceeds limit of 10"},
		{name: "too long", values: []string{`{user="` + strings.Repeat("a", maxForcedLabelsHeaderLen) + `"}`}, errContains: "exceeds limit of 1024"},
		{name: "malformed: not a selector", values: []string{`user=alice`}, errContains: "must be a label set"},
		{name: "malformed: matcher operator", values: []string{`{user=~"alice"}`}, errContains: "must be a label set"},
		{name: "malformed: unclosed", values: []string{`{user="alice"`}, errContains: "must be a label set"},
		{name: "header set more than once", values: []string{`{user="alice"}`, `{user="mallory"}`}, errContains: "set more than once"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPost, "/loki/api/v1/push", nil)
			for _, v := range tc.values {
				r.Header.Add(HTTPHeaderForcedLabelsKey, v)
			}

			lbs, ok, err := ExtractAndValidateForcedLabels(r)
			if tc.errContains != "" {
				require.Error(t, err)
				require.Contains(t, err.Error(), HTTPHeaderForcedLabelsKey)
				require.Contains(t, err.Error(), tc.errContains)
				require.False(t, ok)
				require.True(t, lbs.IsEmpty())
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.wantOK, ok)
			if tc.want.Len() > 0 {
				require.Equal(t, tc.want, lbs)
			}
			require.Equal(t, tc.wantOK, !lbs.IsEmpty())
		})
	}
}

func TestForcedLabelsContext(t *testing.T) {
	require.True(t, ExtractForcedLabelsContext(context.Background()).IsEmpty())

	want := labels.FromStrings("user", "alice")
	ctx := InjectForcedLabelsContext(context.Background(), want)
	require.Equal(t, want, ExtractForcedLabelsContext(ctx))
}

func TestParseRequest_ForcedLabels(t *testing.T) {
	const forced = `{user="alice", site="edge-07"}`

	parse := func(t *testing.T, r *http.Request, parser RequestParser, limits *fakeLimits) *logproto.InternalPushRequest {
		t.Helper()
		streamResolver := newMockStreamResolver("fake", limits)

		// Mirrors the wiring the distributor's pushHandler does, when the tenant enables the
		// header, before invoking the format parser.
		lbs, ok, err := ExtractAndValidateForcedLabels(r)
		require.NoError(t, err)
		if ok {
			r = r.Clone(InjectForcedLabelsContext(r.Context(), lbs))
		}

		data, _, err := parser("fake", r, limits, nil, 100<<20, 100<<20, NewMockTracker(), streamResolver, util_log.Logger)
		require.NoError(t, err)
		return data
	}

	t.Run("loki: forced labels are set on every stream and overwrite client labels", func(t *testing.T) {
		body := `{"streams":[
			{"stream":{"app":"web","user":"mallory"},"values":[["1570818238000000000","one"]]},
			{"stream":{"app":"db"},"values":[["1570818238000000000","two"]]}
		]}`
		req := parse(t, newForcedLabelsLokiRequest(body, forced), ParseLokiRequest, &fakeLimits{})
		require.Len(t, req.Streams, 2)
		for _, s := range req.Streams {
			lbs := mustParseLabels(t, s.Labels)
			require.Equal(t, "alice", lbs.Get("user"), "labels=%s", s.Labels)
			require.Equal(t, "edge-07", lbs.Get("site"), "labels=%s", s.Labels)
			require.NotEmpty(t, lbs.Get("app"), "other client labels are kept: labels=%s", s.Labels)
		}
	})

	t.Run("loki: no header leaves labels untouched", func(t *testing.T) {
		body := `{"streams":[{"stream":{"app":"web","user":"mallory"},"values":[["1570818238000000000","one"]]}]}`
		req := parse(t, newForcedLabelsLokiRequest(body, ""), ParseLokiRequest, &fakeLimits{})
		require.Len(t, req.Streams, 1)
		require.Equal(t, `{app="web", user="mallory"}`, req.Streams[0].Labels)
	})

	t.Run("loki: service_name is discovered from the forced value, not the client value", func(t *testing.T) {
		// Forced labels are applied before service-name discovery: otherwise a client could
		// spoof service_name through a label the proxy forces.
		body := `{"streams":[{"stream":{"app":"payments"},"values":[["1570818238000000000","one"]]}]}`
		req := parse(t, newForcedLabelsLokiRequest(body, `{app="edge-app"}`), ParseLokiRequest, &fakeLimits{enabled: true, labels: []string{"app"}})
		require.Len(t, req.Streams, 1)
		lbs := mustParseLabels(t, req.Streams[0].Labels)
		require.Equal(t, "edge-app", lbs.Get("app"))
		require.Equal(t, "edge-app", lbs.Get(LabelServiceName))
	})

	t.Run("loki: forced service_name overwrites the client value", func(t *testing.T) {
		body := `{"streams":[{"stream":{"app":"web","service_name":"spoofed"},"values":[["1570818238000000000","one"]]}]}`
		req := parse(t, newForcedLabelsLokiRequest(body, `{service_name="edge"}`), ParseLokiRequest, &fakeLimits{enabled: true, labels: []string{"app"}})
		require.Len(t, req.Streams, 1)
		require.Equal(t, "edge", mustParseLabels(t, req.Streams[0].Labels).Get(LabelServiceName))
	})

	t.Run("loki: forced and backfill labels are both added", func(t *testing.T) {
		body := `{"streams":[{"stream":{"app":"web"},"values":[["1570818238000000000","one"]]}]}`
		r := newForcedLabelsLokiRequest(body, forced)
		r.Header.Set(HTTPHeaderBackfillShardKey, testBackfillShard)
		shard, ok, err := ExtractAndValidateBackfillShard(r)
		require.NoError(t, err)
		require.True(t, ok)
		r = r.Clone(InjectBackfillShardContext(r.Context(), shard))

		req := parse(t, r, ParseLokiRequest, &fakeLimits{})
		require.Len(t, req.Streams, 1)
		requireBackfillLabels(t, req.Streams[0].Labels)
		require.Equal(t, "alice", mustParseLabels(t, req.Streams[0].Labels).Get("user"))
	})

	t.Run("otlp: forced labels overwrite an indexed resource attribute", func(t *testing.T) {
		ld := singleResourceLogs("service.name", "service-1", "user", "mallory")
		req := parse(t, newForcedLabelsOTLPRequest(t, ld, forced), NewOTLPRequestParser(false), &fakeLimits{enabled: true, indexAttributes: []string{"service.name", "user"}})
		require.Len(t, req.Streams, 1)
		lbs := mustParseLabels(t, req.Streams[0].Labels)
		require.Equal(t, "alice", lbs.Get("user"))
		require.Equal(t, "edge-07", lbs.Get("site"))
		require.Equal(t, "service-1", lbs.Get(LabelServiceName))
	})

	t.Run("otlp: no header leaves labels untouched", func(t *testing.T) {
		ld := singleResourceLogs("service.name", "service-1", "user", "mallory")
		req := parse(t, newForcedLabelsOTLPRequest(t, ld, ""), NewOTLPRequestParser(false), &fakeLimits{enabled: true, indexAttributes: []string{"service.name", "user"}})
		require.Len(t, req.Streams, 1)
		require.Equal(t, "mallory", mustParseLabels(t, req.Streams[0].Labels).Get("user"))
	})
}

// TestOTLPForcedLabelsOnCombinedStreams covers the per-log-record stream path. A log attribute
// promoted to an index label is merged over the resource stream labels, so a client could use it to
// overwrite a forced label unless forced labels are applied again after the merge.
func TestOTLPForcedLabelsOnCombinedStreams(t *testing.T) {
	now := time.Unix(0, time.Now().UnixNano())

	cfg := DefaultOTLPConfig(GlobalOTLPConfig{DefaultOTLPResourceAttributesAsIndexLabels: []string{"service.name"}})
	cfg.LogAttributes = []AttributesConfig{{Action: IndexLabel, Attributes: []string{"user", "detected_level"}}}

	ld := plog.NewLogs()
	rl := ld.ResourceLogs().AppendEmpty()
	rl.Resource().Attributes().PutStr("service.name", "svc")
	rec := rl.ScopeLogs().AppendEmpty().LogRecords().AppendEmpty()
	rec.Body().SetStr("hello")
	rec.SetTimestamp(pcommon.Timestamp(now.UnixNano()))
	rec.Attributes().PutStr("user", "mallory") // spoofing attempt through a log attribute
	rec.Attributes().PutStr("detected_level", "info")

	stats := NewPushStats()
	streamResolver := newMockStreamResolver("fake", &fakeLimits{})
	ctx := InjectForcedLabelsContext(context.Background(), labels.FromStrings("user", "alice"))

	pushReq, err := otlpToLokiPushRequest(ctx, ld, "fake", cfg, nil, []string{}, NewMockTracker(), stats, gokitlog.NewNopLogger(), streamResolver, constants.OTLP, false)
	require.NoError(t, err)

	nonEmpty := 0
	for _, s := range pushReq.Streams {
		if s.EntryCount() == 0 {
			continue
		}
		nonEmpty++
		lbs := mustParseLabels(t, s.Labels)
		require.Equal(t, "alice", lbs.Get("user"), "labels=%s", s.Labels)
		require.Equal(t, "info", lbs.Get("detected_level"), "labels=%s", s.Labels)
	}
	require.Equal(t, 1, nonEmpty, "expected one combined stream carrying the indexed log attributes")
}

func newForcedLabelsLokiRequest(body, forced string) *http.Request {
	r := newBackfillLokiRequest(body, "")
	if forced != "" {
		r.Header.Set(HTTPHeaderForcedLabelsKey, forced)
	}
	return r
}

func newForcedLabelsOTLPRequest(t *testing.T, ld plog.Logs, forced string) *http.Request {
	t.Helper()
	r := newBackfillOTLPRequest(t, ld, "")
	if forced != "" {
		r.Header.Set(HTTPHeaderForcedLabelsKey, forced)
	}
	return r
}

func mustParseLabels(t *testing.T, s string) labels.Labels {
	t.Helper()
	lbs, err := syntax.ParseLabels(s)
	require.NoError(t, err, "labels=%s", s)
	return lbs
}
