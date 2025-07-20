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
			name:  "filter non-indexed key with empty value",
			query: `{app="fake"} | noexist=""`,
			match: false,
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
