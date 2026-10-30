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

func TestExtractAndValidateTrustedLabels(t *testing.T) {
	for _, tc := range []struct {
		name       string
		setHeader  bool
		value      string
		wantLabels labels.Labels
		wantOK     bool
		wantErr    string
	}{
		{name: "no header", wantLabels: labels.EmptyLabels()},
		{name: "empty header", setHeader: true, value: "", wantLabels: labels.EmptyLabels()},
		{name: "single label", setHeader: true, value: `{username="userA"}`, wantLabels: labels.FromStrings("username", "userA"), wantOK: true},
		{name: "multiple labels", setHeader: true, value: `{username="userA", team="payments"}`, wantLabels: labels.FromStrings("team", "payments", "username", "userA"), wantOK: true},
		{name: "value with separators", setHeader: true, value: `{owner="a=b,c"}`, wantLabels: labels.FromStrings("owner", "a=b,c"), wantOK: true},
		{name: "not a label set", setHeader: true, value: "username=userA", wantErr: "must be a label set"},
		{name: "empty label set", setHeader: true, value: "{}", wantErr: "at least one label"},
		{name: "empty value", setHeader: true, value: `{username=""}`, wantErr: "non-empty value"},
		{name: "duplicate name", setHeader: true, value: `{username="userA", username="userB"}`, wantErr: "duplicate label name"},
		{name: "reserved name", setHeader: true, value: `{__name__="foo"}`, wantErr: "reserved"},
		{name: "reserved backfill label", setHeader: true, value: `{` + constants.BackfillLabel + `="true"}`, wantErr: "reserved"},
		{name: "too long", setHeader: true, value: `{username="` + strings.Repeat("a", maxTrustedLabelsLen) + `"}`, wantErr: "exceeds limit"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPost, "/loki/api/v1/push", nil)
			if tc.setHeader {
				r.Header.Set(HTTPHeaderTrustedLabelsKey, tc.value)
			}

			lbs, ok, err := ExtractAndValidateTrustedLabels(r)
			if tc.wantErr != "" {
				require.Error(t, err)
				require.Contains(t, err.Error(), HTTPHeaderTrustedLabelsKey)
				require.Contains(t, err.Error(), tc.wantErr)
				require.False(t, ok)
				require.True(t, lbs.IsEmpty())
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.wantOK, ok)
			require.Equal(t, tc.wantLabels, lbs)
		})
	}
}

func TestTrustedLabelsContext(t *testing.T) {
	require.True(t, ExtractTrustedLabelsContext(context.Background()).IsEmpty())

	want := labels.FromStrings("username", "userA")
	ctx := InjectTrustedLabelsContext(context.Background(), want)
	require.Equal(t, want, ExtractTrustedLabelsContext(ctx))
}

func TestParseRequest_TrustedLabels(t *testing.T) {
	parse := func(r *http.Request, parser RequestParser, limits *fakeLimits) (*logproto.InternalPushRequest, error) {
		streamResolver := newMockStreamResolver("fake", limits)

		// Mirrors the trusted-labels wiring the distributor's pushHandler does, for tenants that
		// enable it, before invoking the format parser.
		if lbs, ok, err := ExtractAndValidateTrustedLabels(r); err != nil {
			return nil, err
		} else if ok {
			r = r.Clone(InjectTrustedLabelsContext(r.Context(), lbs))
		}

		data, _, err := parser("fake", r, limits, nil, 100<<20, 100<<20, NewMockTracker(), streamResolver, util_log.Logger)
		return data, err
	}

	for _, tc := range []struct {
		name                string
		body                string
		header              string
		discoverServiceName bool
		wantLabels          []labels.Labels
	}{
		{
			name:       "overrides a body label with the same name",
			body:       `{"streams":[{"stream":{"app":"payment","username":"userB"},"values":[["1570818238000000000","fizzbuzz"]]}]}`,
			header:     `{username="userA"}`,
			wantLabels: []labels.Labels{labels.FromStrings("app", "payment", "username", "userA")},
		},
		{
			name:       "adds a label the body doesn't have",
			body:       `{"streams":[{"stream":{"app":"payment"},"values":[["1570818238000000000","fizzbuzz"]]}]}`,
			header:     `{username="userA"}`,
			wantLabels: []labels.Labels{labels.FromStrings("app", "payment", "username", "userA")},
		},
		{
			name: "applies to every stream",
			body: `{"streams":[` +
				`{"stream":{"app":"payment","username":"userB"},"values":[["1570818238000000000","fizzbuzz"]]},` +
				`{"stream":{"app":"checkout"},"values":[["1570818238000000000","fizzbuzz"]]}]}`,
			header: `{username="userA"}`,
			wantLabels: []labels.Labels{
				labels.FromStrings("app", "payment", "username", "userA"),
				labels.FromStrings("app", "checkout", "username", "userA"),
			},
		},
		{
			name:       "no header leaves labels untouched",
			body:       `{"streams":[{"stream":{"app":"payment","username":"userB"},"values":[["1570818238000000000","fizzbuzz"]]}]}`,
			wantLabels: []labels.Labels{labels.FromStrings("app", "payment", "username", "userB")},
		},
		{
			name:                "trusted service_name skips service name discovery",
			body:                `{"streams":[{"stream":{"app":"payment"},"values":[["1570818238000000000","fizzbuzz"]]}]}`,
			header:              `{service_name="gateway-set"}`,
			discoverServiceName: true,
			wantLabels:          []labels.Labels{labels.FromStrings("app", "payment", LabelServiceName, "gateway-set")},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newTrustedLabelsRequest("/loki/api/v1/push", strings.NewReader(tc.body), tc.header)
			req, err := parse(r, ParseLokiRequest, &fakeLimits{enabled: tc.discoverServiceName, labels: []string{"app"}})
			require.NoError(t, err)
			require.Len(t, req.Streams, len(tc.wantLabels))
			for i, want := range tc.wantLabels {
				got, err := syntax.ParseLabels(req.Streams[i].Labels)
				require.NoError(t, err)
				require.Equal(t, want, got)
			}
		})
	}

	t.Run("malformed header rejects the whole push", func(t *testing.T) {
		body := `{"streams":[{"stream":{"app":"payment"},"values":[["1570818238000000000","fizzbuzz"]]}]}`
		r := newTrustedLabelsRequest("/loki/api/v1/push", strings.NewReader(body), "username=userA")
		req, err := parse(r, ParseLokiRequest, &fakeLimits{enabled: true})
		require.Error(t, err)
		require.Contains(t, err.Error(), HTTPHeaderTrustedLabelsKey)
		require.Nil(t, req)
	})

	t.Run("otlp: overrides an indexed resource attribute", func(t *testing.T) {
		ld := singleResourceLogs("service.name", "service-1", "username", "userB")
		body, err := (&plog.JSONMarshaler{}).MarshalLogs(ld)
		require.NoError(t, err)

		r := newTrustedLabelsRequest("/otlp/v1/logs", strings.NewReader(string(body)), `{username="userA"}`)
		req, err := parse(r, NewOTLPRequestParser(false), &fakeLimits{enabled: true, indexAttributes: []string{"service.name", "username"}})
		require.NoError(t, err)
		require.Len(t, req.Streams, 1)
		requireTrustedLabel(t, req.Streams[0].Labels, "username", "userA")
	})

	t.Run("otlp: restrictive tenant config cannot drop trusted labels", func(t *testing.T) {
		ld := singleResourceLogs("service.name", "service-1")
		body, err := (&plog.JSONMarshaler{}).MarshalLogs(ld)
		require.NoError(t, err)

		r := newTrustedLabelsRequest("/otlp/v1/logs", strings.NewReader(string(body)), `{username="userA"}`)
		req, err := parse(r, NewOTLPRequestParser(false), &fakeLimits{enabled: false, indexAttributes: []string{"nonexistent"}})
		require.NoError(t, err)
		require.Len(t, req.Streams, 1)
		requireTrustedLabel(t, req.Streams[0].Labels, "username", "userA")
	})
}

// TestOTLPTrustedLabelsOnCombinedStreams covers the per-log-record stream path, where a log attribute
// promoted to an index label is merged into the stream labels. It must not override a trusted label.
func TestOTLPTrustedLabelsOnCombinedStreams(t *testing.T) {
	cfg := DefaultOTLPConfig(GlobalOTLPConfig{DefaultOTLPResourceAttributesAsIndexLabels: []string{"service.name"}})
	cfg.LogAttributes = []AttributesConfig{{Action: IndexLabel, Attributes: []string{"username", "detected_level"}}}

	ld := plog.NewLogs()
	rl := ld.ResourceLogs().AppendEmpty()
	rl.Resource().Attributes().PutStr("service.name", "svc")
	rec := rl.ScopeLogs().AppendEmpty().LogRecords().AppendEmpty()
	rec.Body().SetStr("hello")
	rec.SetTimestamp(pcommon.Timestamp(time.Now().UnixNano()))
	rec.Attributes().PutStr("username", "userB")
	rec.Attributes().PutStr("detected_level", "info")

	stats := NewPushStats()
	streamResolver := newMockStreamResolver("fake", &fakeLimits{})
	ctx := InjectTrustedLabelsContext(context.Background(), labels.FromStrings("username", "userA"))

	pushReq, err := otlpToLokiPushRequest(ctx, ld, "fake", cfg, nil, []string{}, NewMockTracker(), stats, gokitlog.NewNopLogger(), streamResolver, constants.OTLP, false)
	require.NoError(t, err)

	nonEmpty := 0
	for _, s := range pushReq.Streams {
		if s.EntryCount() == 0 {
			continue
		}
		nonEmpty++
		requireTrustedLabel(t, s.Labels, "username", "userA")
		require.Contains(t, s.Labels, `detected_level="info"`)
	}
	require.Equal(t, 1, nonEmpty, "expected one combined stream carrying the indexed log attributes")
}

func newTrustedLabelsRequest(target string, body *strings.Reader, trustedLabels string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, target, body)
	r.Header.Set("Content-Type", "application/json")
	if trustedLabels != "" {
		r.Header.Set(HTTPHeaderTrustedLabelsKey, trustedLabels)
	}
	return r
}

func requireTrustedLabel(t *testing.T, labelsStr, name, value string) {
	t.Helper()
	lbs, err := syntax.ParseLabels(labelsStr)
	require.NoError(t, err, "labels=%s", labelsStr)
	require.Equal(t, value, lbs.Get(name), "labels=%s", labelsStr)
}
