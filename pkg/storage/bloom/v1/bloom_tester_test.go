package v1

import (
	"testing"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logql/syntax"

	"github.com/grafana/loki/pkg/push"
)

func TestLabelMatchersToBloomTest(t *testing.T) {
	// All test cases below have access to a fake bloom filter with
	// trace_id=exists_1 and trace_id=exists_2
	var (
		prefix    = "fakeprefix"
		tokenizer = NewStructuredMetadataTokenizer(prefix)
		bloom     = newFakeMetadataBloom(
			tokenizer,
			push.LabelAdapter{Name: "trace_id", Value: "exists_1"},
			push.LabelAdapter{Name: "trace_id", Value: "exists_2"},
			push.LabelAdapter{Name: "app", Value: "other"},
		)
	)

	series := labels.FromStrings("env", "prod", "app", "fake")
	tt := []struct {
		name  string
		query string
		match bool
	}{
		{
			name:  "no matchers",
			query: `{app="fake"}`,
			match: true,
		},
		{
			name:  "basic matcher pass",
			query: `{app="fake"} | trace_id="exists_1"`,
			match: true,
		},
		{
			name:  "basic matcher fail",
			query: `{app="fake"} | trace_id="noexist"`,
			match: false,
		},
		{
			name:  "multiple matcher pass",
			query: `{app="fake"} | trace_id="exists_1" | trace_id="exists_2"`,
			match: true,
		},
		{
			name:  "multiple matcher fail",
			query: `{app="fake"} | trace_id="exists_1" | trace_id="noexist"`,
			match: false,
		},
		{
			name:  "filter non-indexed key",
			query: `{app="fake"} | noexist="noexist"`,
			match: false,
		},
		{
			// LogQL: `field=""` matches rows where the label is absent OR
			// empty. Blooms can't positively assert "label is absent", so the
			// bloom test must pass (no chunk pruning); the per-row label
			// filter decides each row downstream.
			name:  "non-indexed key with empty value passes (bloom can't prove absence)",
			query: `{app="fake"} | noexist=""`,
			match: true,
		},
		{
			name:  "ignore label from series",
			query: `{app="fake"} | env="prod"`,
			match: true,
		},
		{
			name:  "filter label from series",
			query: `{app="fake"} | env="dev"`, // env is set to prod in the series
			match: false,
		},
		{
			name:  "ignore label from series and structured metadata",
			query: `{app="fake"} | app="other"`,
			match: true,
		},
		{
			name:  "filter series label with non-existing value",
			query: `{app="fake"} | app="noexist"`,
			match: false,
		},
		{
			name:  "ignore label from series with empty value",
			query: `{app="fake"} | app=""`,
			match: false,
		},
		{
			// Trace_id is a structured-metadata key (recorded per row in the
			// bloom for some rows). Some rows in the chunk may have it absent;
			// LogQL says those match `trace_id=""`. The bloom can't tell us
			// "no row had trace_id" so the chunk must pass; the per-row
			// filter handles each row.
			name:  "structured-metadata key with empty-value filter passes the bloom",
			query: `{app="fake"} | trace_id=""`,
			match: true,
		},
		{
			name:  "ignore unsupported operator",
			query: `{app="fake"} | trace_id=~".*noexist.*"`,
			match: true,
		},
		{
			name:  "or test pass",
			query: `{app="fake"} | trace_id="noexist" or trace_id="exists_1"`,
			match: true,
		},
		{
			name:  "or test fail",
			query: `{app="fake"} | trace_id="noexist" or trace_id="noexist"`,
			match: false,
		},
		{
			name:  "and test pass",
			query: `{app="fake"} | trace_id="exists_1" or trace_id="exists_2"`,
			match: true,
		},
		{
			name:  "and test fail",
			query: `{app="fake"} | trace_id="exists_1" and trace_id="noexist"`,
			match: false,
		},
		{
			name:  "presence test pass",
			query: `{app="fake"} | trace_id=~".+"`,
			match: true,
		},
		{
			name:  "presence test pass",
			query: `{app="fake"} | noexist=~".+"`,
			match: false,
		},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			expr, err := syntax.ParseExpr(tc.query)
			require.NoError(t, err)

			matchers := ExtractTestableLabelMatchers(expr)
			bloomTest := LabelMatchersToBloomTest(matchers...)

			// .Matches and .MatchesWithPrefixBuf should both have the same result.
			require.Equal(t, tc.match, bloomTest.Matches(series, bloom))
			require.Equal(t, tc.match, bloomTest.MatchesWithPrefixBuf(series, bloom, []byte(prefix), len(prefix)))
		})
	}
}

type fakeMetadataBloom []string

// fakeBloom is a fake bloom filter that matches tokens exactly.
// It uses a tokenizer to build the tokens for a line
func newFakeMetadataBloom(tokenizer *StructuredMetadataTokenizer, kvs ...push.LabelAdapter) (res fakeMetadataBloom) {
	for _, kv := range kvs {
		it := tokenizer.Tokens(kv)
		for it.Next() {
			res = append(res, it.At())
		}
	}
	return res
}

func (f fakeMetadataBloom) Test(data []byte) bool {
	str := string(data)
	for _, match := range f {
		if str == match {
			return true
		}
	}
	return false
}
