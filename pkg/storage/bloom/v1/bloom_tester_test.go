package v1

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logql/syntax"

	"github.com/grafana/loki/pkg/push"
)

type fakeLineBloom []string

// fakeBloom is a fake bloom filter that matches tokens exactly.
// It uses a tokenizer to build the tokens for a line
func newFakeBloom(tokenizer *NGramTokenizer, line string) (res fakeLineBloom) {
	toks := tokenizer.Tokens(line)
	for toks.Next() {
		res = append(res, string(toks.At()))
	}
	return
}

func (f fakeLineBloom) Test(data []byte) bool {
	str := string(data)
	for _, match := range f {
		if str == match {
			return true
		}
	}
	return false
}

func TestBloomQueryingLogic(t *testing.T) {
	// All tested on 4skip1
	n := 4
	skip := 1
	tokenizer := NewNGramTokenizer(n, skip)

	for _, tc := range []struct {
		desc    string
		line    string
		query   string
		match   bool
		enabled bool
	}{
		{
			desc:  "filter too short always match",
			line:  "foobar",
			query: `{app="fake"} |= "z"`,
			match: true,
		},
		{
			desc:  "simple matcher",
			line:  "foobar",
			query: `{app="fake"} |= "oobar"`,
			match: true,
		},
		{
			desc:  "longer sequence",
			line:  "abcdefghijklmnopqrstuvwxyz",
			query: `{app="fake"} |= "nopqrstuvwxyz"`,
			match: true,
		},
		{
			desc:  "longer sequence nomatch",
			line:  "abcdefghijklmnopqrstuvwxyz",
			query: `{app="fake"} |= "nopqrstuvwxyzzz"`,
			match: false,
		},
		{
			desc:  "pattern simple",
			line:  "abcdefghijklmnopqrstuvwxyz",
			query: `{app="fake"} |> "<_>lmnopq<_>"`,
			match: true,
		},
		{
			desc:  "pattern too short matches",
			line:  "abcdefghijklmnopqrstuvwxyz",
			query: `{app="fake"} |> "<_>zzz<_>"`,
			match: true,
		},
		{
			desc:  "pattern mix long success and short",
			line:  "abcdefghijklmnopqrstuvwxyz",
			query: `{app="fake"} |> "<_>lmnop<_>zzz<_>"`,
			match: true,
		},
		{
			desc:  "pattern mix long fail and short",
			line:  "abcdefghijklmnopqrstuvwxyz",
			query: `{app="fake"} |> "<_>zzzzz<_>zzz<_>"`,
			match: false,
		},
		{
			desc:  "regexp disabled",
			line:  "foobarbaz",
			query: `{app="fake"} |~ "(aaaaa|bbbbb)bazz"`,
			match: true,
		},
	} {

		// shortcut to enable specific tests
		tc.enabled = true
		if !tc.enabled {
			continue
		}
		t.Run(tc.desc, func(t *testing.T) {
			bloom := newFakeBloom(tokenizer, tc.line)
			expr, err := syntax.ParseExpr(tc.query)
			require.NoError(t, err)
			filters := ExtractTestableLineFilters(expr)
			bloomTests := FiltersToBloomTest(tokenizer, filters...)
			matched := bloomTests.Matches(bloom)

			require.Equal(t, tc.match, matched)

		})
	}
}

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
		)
	)

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
			name:  "ignore non-indexed key",
			query: `{app="fake"} | noexist="noexist"`,
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
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			expr, err := syntax.ParseExpr(tc.query)
			require.NoError(t, err)

			matchers := ExtractTestableLabelMatchers(expr)
			bloomTest := LabelMatchersToBloomTest(matchers...)

			// .Matches and .MatchesWithPrefixBuf should both have the same result.
			require.Equal(t, tc.match, bloomTest.Matches(bloom))
			require.Equal(t, tc.match, bloomTest.MatchesWithPrefixBuf(bloom, []byte(prefix), len(prefix)))
		})
	}
}

type fakeMetadataBloom []string

// fakeBloom is a fake bloom filter that matches tokens exactly.
// It uses a tokenizer to build the tokens for a line
func newFakeMetadataBloom(tokenizer *StructuredMetadataTokenizer, kvs ...push.LabelAdapter) (res fakeLineBloom) {
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
