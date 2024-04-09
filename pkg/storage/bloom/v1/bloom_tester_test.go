package v1

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logql/syntax"
)

type fakeBloom []string

// fakeBloom is a fake bloom filter that matches tokens exactly.
// It uses a tokenizer to build the tokens for a line
func newFakeBloom(tokenizer *NGramTokenizer, line string) (res fakeBloom) {
	toks := tokenizer.Tokens(line)
	for toks.Next() {
		res = append(res, string(toks.At()))
	}
	return
}

func (f fakeBloom) Test(data []byte) bool {
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
