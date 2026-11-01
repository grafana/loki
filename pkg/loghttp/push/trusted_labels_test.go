package push

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/plog"

	"github.com/grafana/loki/v3/pkg/util/constants"
)

func TestExtractAndValidateTrustedLabels(t *testing.T) {
	for _, tc := range []struct {
		name        string
		setHeader   bool
		value       string
		wantLabels  labels.Labels
		wantOK      bool
		wantErrText string
	}{
		{name: "header absent"},
		{
			name:       "valid label set",
			setHeader:  true,
			value:      `{username="userA", team="payments"}`,
			wantLabels: labels.FromStrings("team", "payments", "username", "userA"),
			wantOK:     true,
		},
		{
			name:        "empty header",
			setHeader:   true,
			wantErrText: "label set must not be empty",
		},
		{
			name:        "empty label set",
			setHeader:   true,
			value:       `{}`,
			wantErrText: "label set must not be empty",
		},
		{
			name:        "malformed label set",
			setHeader:   true,
			value:       `{username="userA"`,
			wantErrText: "invalid " + HTTPHeaderTrustedLabelsKey + " header",
		},
		{
			name:        "oversized label set",
			setHeader:   true,
			value:       `{team="` + strings.Repeat("a", maxTrustedLabelsHeaderSize) + `"}`,
			wantErrText: "exceeds limit",
		},
		{
			name:        "invalid label name",
			setHeader:   true,
			value:       `{bad-name="value"}`,
			wantErrText: "invalid " + HTTPHeaderTrustedLabelsKey + " header",
		},
		{
			name:        "invalid label value encoding",
			setHeader:   true,
			value:       "{team=\"bad\xffvalue\"}",
			wantErrText: "invalid " + HTTPHeaderTrustedLabelsKey + " header",
		},
		{
			name:        "reserved aggregated metric label",
			setHeader:   true,
			value:       `{` + constants.AggregatedMetricLabel + `="true"}`,
			wantErrText: "is reserved for Loki",
		},
		{
			name:        "reserved pattern label",
			setHeader:   true,
			value:       `{` + constants.PatternLabel + `="pattern"}`,
			wantErrText: "is reserved for Loki",
		},
		{
			name:        "reserved time shard label",
			setHeader:   true,
			value:       `{` + constants.TimeShardLabel + `="shard"}`,
			wantErrText: "is reserved for Loki",
		},
		{
			name:        "reserved stream shard label",
			setHeader:   true,
			value:       `{` + constants.StreamShardLabel + `="shard"}`,
			wantErrText: "is reserved for Loki",
		},
		{
			name:        "reserved backfill label",
			setHeader:   true,
			value:       `{` + constants.BackfillLabel + `="true"}`,
			wantErrText: "is reserved for Loki",
		},
		{
			name:        "reserved backfill shard label",
			setHeader:   true,
			value:       `{` + constants.BackfillShardLabel + `="shard"}`,
			wantErrText: "is reserved for Loki",
		},
		{
			name:       "other double underscore label is allowed",
			setHeader:  true,
			value:      `{__custom_label="value"}`,
			wantLabels: labels.FromStrings("__custom_label", "value"),
			wantOK:     true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPost, "/loki/api/v1/push", nil)
			if tc.setHeader {
				r.Header.Set(HTTPHeaderTrustedLabelsKey, tc.value)
			}

			got, ok, err := ExtractAndValidateTrustedLabels(r)
			if tc.wantErrText != "" {
				require.ErrorContains(t, err, tc.wantErrText)
				require.False(t, ok)
				require.True(t, got.IsEmpty())
				return
			}

			require.NoError(t, err)
			require.Equal(t, tc.wantOK, ok)
			require.Equal(t, tc.wantLabels, got)
		})
	}
}

func TestExtractTrustedLabelsHeaderValues(t *testing.T) {
	tests := []struct {
		name        string
		headers     []string
		headerName  string
		wantLabels  labels.Labels
		wantErrText string
	}{
		{
			name:       "case-insensitive header name",
			headerName: "x-loki-labels",
			headers:    []string{`{team="payments"}`},
			wantLabels: labels.FromStrings("team", "payments"),
		},
		{
			name:        "duplicate header lines are rejected",
			headers:     []string{`{team="payments"}`, `{team="other"}`},
			wantErrText: "multiple values are not supported",
		},
		{
			name:        "whitespace-only header is rejected",
			headers:     []string{" \t "},
			wantErrText: "label set must not be empty",
		},
		{
			name:       "duplicate label names keep the last value",
			headers:    []string{`{team="payments", team="other"}`},
			wantLabels: labels.FromStrings("team", "other"),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPost, "/loki/api/v1/push", nil)
			headerName := test.headerName
			if headerName == "" {
				headerName = HTTPHeaderTrustedLabelsKey
			}
			for _, value := range test.headers {
				r.Header.Add(headerName, value)
			}

			got, ok, err := ExtractAndValidateTrustedLabels(r)
			if test.wantErrText != "" {
				require.ErrorContains(t, err, test.wantErrText)
				require.False(t, ok)
				require.True(t, got.IsEmpty())
				return
			}

			require.NoError(t, err)
			require.True(t, ok)
			if test.name == "duplicate label names keep the last value" {
				_, duplicate := got.HasDuplicateLabelNames()
				require.True(t, duplicate, "the parser currently retains duplicate label names")
				require.Equal(t, labels.FromStrings("team", "other"), MergeTrustedLabels(labels.EmptyLabels(), got))
				return
			}
			require.Equal(t, test.wantLabels, got)
		})
	}
}

func TestTrustedLabelsCanonicalOrder(t *testing.T) {
	mergedLabels := func(value string) labels.Labels {
		r := httptest.NewRequest(http.MethodPost, "/loki/api/v1/push", nil)
		r.Header.Set(HTTPHeaderTrustedLabelsKey, value)
		forced, ok, err := ExtractAndValidateTrustedLabels(r)
		require.NoError(t, err)
		require.True(t, ok)
		return MergeTrustedLabels(labels.FromStrings("keep", "value"), forced)
	}

	first := mergedLabels(`{b="1",a="2"}`)
	second := mergedLabels(`{a="2",b="1"}`)
	require.Equal(t, first.String(), second.String())
	require.Equal(t, labels.StableHash(first), labels.StableHash(second))
}

func TestTrustedLabelsContext(t *testing.T) {
	forced := labels.FromStrings("team", "payments")
	require.True(t, ExtractTrustedLabelsContext(context.Background()).IsEmpty())

	ctx := InjectTrustedLabelsContext(context.Background(), forced)
	require.Equal(t, forced, ExtractTrustedLabelsContext(ctx))
}

func TestMergeTrustedLabels(t *testing.T) {
	body := labels.FromStrings("team", "old", "username", "body", "zone", "west")
	forced := labels.FromStrings("team", "payments", "username", "gateway")

	got := MergeTrustedLabels(body, forced)

	require.Equal(t, labels.FromStrings("team", "payments", "username", "gateway", "zone", "west"), got)
}

func TestOTLPTrustedLabels(t *testing.T) {
	tests := []struct {
		name                string
		resources           []map[string]string
		logAttributes       map[string]string
		indexedResourceAttr []string
		indexedLogAttr      []string
		trustedLabels       labels.Labels
		discoverServiceName []string
		expectedLabels      []string
	}{
		{
			name:                "replace and add resource labels",
			resources:           []map[string]string{{"app": "body", "keep": "value"}},
			indexedResourceAttr: []string{"app", "keep"},
			trustedLabels:       labels.FromStrings("app", "gateway", "team", "payments"),
			expectedLabels:      []string{labels.FromStrings("app", "gateway", "keep", "value", "team", "payments").String()},
		},
		{
			name:                "apply labels to multiple resource streams",
			resources:           []map[string]string{{"region": "west"}, {"region": "east"}},
			indexedResourceAttr: []string{"region"},
			trustedLabels:       labels.FromStrings("team", "payments"),
			expectedLabels: []string{
				labels.FromStrings("region", "west", "team", "payments").String(),
				labels.FromStrings("region", "east", "team", "payments").String(),
			},
		},
		{
			name:                "no trusted labels in context is a no-op",
			resources:           []map[string]string{{"app": "body"}},
			indexedResourceAttr: []string{"app"},
			expectedLabels:      []string{labels.FromStrings("app", "body").String()},
		},
		{
			name:                "trusted labels participate in service name discovery",
			resources:           []map[string]string{{"region": "west"}},
			indexedResourceAttr: []string{"region"},
			trustedLabels:       labels.FromStrings("app", "gateway"),
			discoverServiceName: []string{"app"},
			expectedLabels: []string{
				labels.FromStrings("app", "gateway", "region", "west", LabelServiceName, "gateway").String(),
			},
		},
		{
			name:                "forced service name replaces resource service name",
			resources:           []map[string]string{{"service.name": "resource-service"}},
			indexedResourceAttr: []string{"service.name"},
			trustedLabels:       labels.FromStrings(LabelServiceName, "gateway"),
			discoverServiceName: []string{"service_name"},
			expectedLabels: []string{
				labels.FromStrings(LabelServiceName, "gateway").String(),
			},
		},
		{
			name:                "trusted labels override indexed log attributes",
			resources:           []map[string]string{{"app": "body"}},
			logAttributes:       map[string]string{"username": "log-value", "environment": "prod"},
			indexedResourceAttr: []string{"app"},
			indexedLogAttr:      []string{"username", "environment"},
			trustedLabels:       labels.FromStrings("username", "gateway"),
			expectedLabels: []string{
				labels.FromStrings("app", "body", "environment", "prod", "username", "gateway").String(),
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config := DefaultOTLPConfig(GlobalOTLPConfig{
				DefaultOTLPResourceAttributesAsIndexLabels: test.indexedResourceAttr,
			})
			if len(test.indexedLogAttr) > 0 {
				config.LogAttributes = []AttributesConfig{{
					Action:     IndexLabel,
					Attributes: test.indexedLogAttr,
				}}
			}

			ctx := context.Background()
			if !test.trustedLabels.IsEmpty() {
				ctx = InjectTrustedLabelsContext(ctx, test.trustedLabels)
			}

			stats := NewPushStats()
			streamResolver := newMockStreamResolver("fake", &fakeLimits{})
			req, err := otlpToLokiPushRequest(
				ctx,
				trustedLabelsTestLogs(test.resources, test.logAttributes),
				"fake",
				config,
				nil,
				test.discoverServiceName,
				nil,
				stats,
				log.NewNopLogger(),
				streamResolver,
				constants.OTLP,
				false,
			)
			require.NoError(t, err)

			actualLabels := make([]string, 0, len(req.Streams))
			for _, stream := range req.Streams {
				actualLabels = append(actualLabels, stream.Labels)
			}
			require.ElementsMatch(t, test.expectedLabels, actualLabels)
		})
	}
}

func TestOTLPTrustedLabelAndStructuredMetadataNameCanCoexist(t *testing.T) {
	config := DefaultOTLPConfig(GlobalOTLPConfig{})
	ctx := InjectTrustedLabelsContext(context.Background(), labels.FromStrings("username", "gateway"))
	stats := NewPushStats()

	req, err := otlpToLokiPushRequest(
		ctx,
		trustedLabelsTestLogs([]map[string]string{{"username": "resource-value"}}, nil),
		"fake",
		config,
		nil,
		nil,
		nil,
		stats,
		log.NewNopLogger(),
		newMockStreamResolver("fake", &fakeLimits{}),
		constants.OTLP,
		false,
	)
	require.NoError(t, err)
	require.Len(t, req.Streams, 1)

	stream := req.Streams[0].FlatView()
	require.Equal(t, labels.FromStrings("username", "gateway").String(), stream.Labels)
	require.Len(t, stream.Entries[0].StructuredMetadata, 1)
	require.Equal(t, "username", stream.Entries[0].StructuredMetadata[0].Name)
	require.Equal(t, "resource-value", stream.Entries[0].StructuredMetadata[0].Value)
}

func TestOTLPTrustedLabelAndLogStructuredMetadataNameCanCoexist(t *testing.T) {
	config := DefaultOTLPConfig(GlobalOTLPConfig{})
	ctx := InjectTrustedLabelsContext(context.Background(), labels.FromStrings("username", "gateway"))
	stats := NewPushStats()

	req, err := otlpToLokiPushRequest(
		ctx,
		trustedLabelsTestLogs([]map[string]string{{}}, map[string]string{"username": "log-value"}),
		"fake",
		config,
		nil,
		nil,
		nil,
		stats,
		log.NewNopLogger(),
		newMockStreamResolver("fake", &fakeLimits{}),
		constants.OTLP,
		false,
	)
	require.NoError(t, err)
	require.Len(t, req.Streams, 1)

	stream := req.Streams[0].FlatView()
	require.Equal(t, labels.FromStrings("username", "gateway").String(), stream.Labels)
	require.Len(t, stream.Entries[0].StructuredMetadata, 1)
	require.Equal(t, "username", stream.Entries[0].StructuredMetadata[0].Name)
	require.Equal(t, "log-value", stream.Entries[0].StructuredMetadata[0].Value)
}

func trustedLabelsTestLogs(resources []map[string]string, logAttributes map[string]string) plog.Logs {
	logs := plog.NewLogs()
	for _, resourceAttributes := range resources {
		resourceLogs := logs.ResourceLogs().AppendEmpty()
		for name, value := range resourceAttributes {
			resourceLogs.Resource().Attributes().PutStr(name, value)
		}

		record := resourceLogs.ScopeLogs().AppendEmpty().LogRecords().AppendEmpty()
		record.Body().SetStr("test log")
		record.SetTimestamp(pcommon.Timestamp(time.Unix(0, 1570818238000000000).UnixNano()))
		for name, value := range logAttributes {
			record.Attributes().PutStr(name, value)
		}
	}
	return logs
}
