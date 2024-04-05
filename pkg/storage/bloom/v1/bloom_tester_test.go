package v1

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logql/syntax"
)

// func TestFiltersToBloomTests(t *testing.T) {
// 	for _, tc := range []struct {
// 		name        string
// 		query       string
// 		bloom       filter.Checker
// 		expectMatch bool
// 	}{
// 		{
// 			name:        "No filters",
// 			query:       `{app="fake"}`,
// 			bloom:       fakeBloom{"foo", "bar"},
// 			expectMatch: true,
// 		},
// 		{
// 			name:        "Single filter",
// 			query:       `{app="fake"} |= "foo"`,
// 			bloom:       fakeBloom{"foo", "bar"},
// 			expectMatch: true,
// 		},
// 		{
// 			name:        "Single filter no match",
// 			query:       `{app="fake"} |= "nope"`,
// 			bloom:       fakeBloom{"foo", "bar"},
// 			expectMatch: false,
// 		},
// 		{
// 			name:        "two filters",
// 			query:       `{app="fake"} |= "foo" |= "bar"`,
// 			bloom:       fakeBloom{"foo", "bar"},
// 			expectMatch: true,
// 		},
// 		{
// 			name:        "two filters no match",
// 			query:       `{app="fake"} |= "foo" |= "nope"`,
// 			bloom:       fakeBloom{"foo", "bar"},
// 			expectMatch: false,
// 		},
// 		{
// 			name:        "notEq doesnt exist",
// 			query:       `{app="fake"} != "nope"`,
// 			bloom:       fakeBloom{"foo", "bar"},
// 			expectMatch: true,
// 		},
// 		{
// 			name:        "notEq exists",
// 			query:       `{app="fake"} != "foo"`,
// 			bloom:       fakeBloom{"foo", "bar"},
// 			expectMatch: true, // Still should match because it's NotEQ
// 		},
// 		{
// 			name:        "or filter both match",
// 			query:       `{app="fake"} |= "foo" or "bar"`,
// 			bloom:       fakeBloom{"foo", "bar"},
// 			expectMatch: true,
// 		},
// 		{
// 			name:        "or filter one right match",
// 			query:       `{app="fake"} |= "nope" or "foo"`,
// 			bloom:       fakeBloom{"foo", "bar"},
// 			expectMatch: true,
// 		},
// 		{
// 			name:        "or filter one left match",
// 			query:       `{app="fake"} |= "foo" or "nope"`,
// 			bloom:       fakeBloom{"foo", "bar"},
// 			expectMatch: true,
// 		},
// 		{
// 			name:        "or filter no match",
// 			query:       `{app="fake"} |= "no" or "nope"`,
// 			bloom:       fakeBloom{"foo", "bar"},
// 			expectMatch: false,
// 		},
// 		{
// 			name:        "Not or filter match",
// 			query:       `{app="fake"} != "nope" or "no"`,
// 			bloom:       fakeBloom{"foo", "bar"},
// 			expectMatch: true,
// 		},
// 		{
// 			name:        "NotEq OR filter right exists",
// 			query:       `{app="fake"} != "nope" or "bar"`,
// 			bloom:       fakeBloom{"foo", "bar"},
// 			expectMatch: true, // Still should match because it's NotEQ
// 		},
// 		{
// 			name:        "Not OR filter left exists",
// 			query:       `{app="fake"} != "foo" or "nope"`,
// 			bloom:       fakeBloom{"foo", "bar"},
// 			expectMatch: true, // Still should match because it's NotEQ
// 		},
// 		{
// 			name:        "NotEq OR filter both exists",
// 			query:       `{app="fake"} != "foo" or "bar"`,
// 			bloom:       fakeBloom{"foo", "bar"},
// 			expectMatch: true, // Still should match because it's NotEQ
// 		},
// 		{
// 			name:        "complex filter match",
// 			query:       `{app="fake"} |= "foo" |= "bar" or "baz" |= "fuzz" or "not" != "nope" != "no" or "none"`,
// 			bloom:       fakeBloom{"foo", "bar", "baz", "fuzz"},
// 			expectMatch: true,
// 		},
// 		{
// 			name:        "regex match all star",
// 			query:       `{app="fake"} |~ ".*"`,
// 			bloom:       fakeBloom{"foo", "bar"},
// 			expectMatch: true,
// 		},
// 		{
// 			name:        "regex match all plus",
// 			query:       `{app="fake"} |~ ".+"`,
// 			bloom:       fakeBloom{"foo", "bar"},
// 			expectMatch: true,
// 		},
// 		{
// 			name:        "regex match all notEq",
// 			query:       `{app="fake"} !~ ".*"`,
// 			bloom:       fakeBloom{"foo", "bar"},
// 			expectMatch: true, // Still should match,
// 		},
// 		{
// 			name:        "regex match",
// 			query:       `{app="fake"} |~ "nope|.*foo.*"`,
// 			bloom:       fakeBloom{"foo", "bar"},
// 			expectMatch: true,
// 		},
// 		{
// 			name:        "regex no match",
// 			query:       `{app="fake"} |~ ".*not.*"`,
// 			bloom:       fakeBloom{"foo", "bar"},
// 			expectMatch: false,
// 		},
// 		{
// 			name:        "regex notEq right exists",
// 			query:       `{app="fake"} !~ "nope|.*foo.*"`,
// 			bloom:       fakeBloom{"foo", "bar"},
// 			expectMatch: true, // Still should match because it's NotEQ
// 		},
// 		{
// 			name:        "complex regex match",
// 			query:       `{app="fake"} |~ "(nope|.*not.*|.*foo.*)" or "(no|ba)" !~ "noz.*" or "(nope|not)"`,
// 			bloom:       fakeBloom{"foo", "bar", "baz", "fuzz"},
// 			expectMatch: true,
// 		},
// 		{
// 			name:        "complex regex with notEq exists",
// 			query:       `{app="fake"} |~ "(nope|.*not.*|.*foo.*)" or "(no|ba)" !~ "noz.*"`,
// 			bloom:       fakeBloom{"foo", "bar", "baz", "fuzz", "noz"},
// 			expectMatch: true, // Still should match because it's NotEQ
// 		},
// 		{
// 			name:        "line filter after line format",
// 			query:       `{app="fake"} |= "foo" | line_format "thisNewTextShouldMatch" |= "thisNewTextShouldMatch"`,
// 			bloom:       fakeBloom{"foo"},
// 			expectMatch: true,
// 		},
// 		{
// 			name:        "pattern match exists",
// 			query:       `{app="fake"} |> "<_>foo<bar>"`,
// 			bloom:       fakeBloom{"foo", "bar"},
// 			expectMatch: true,
// 		},
// 		{
// 			name:        "pattern match does not exist",
// 			query:       `{app="fake"} |> "<_>foo<bar>"`,
// 			bloom:       fakeBloom{"bar", "baz"},
// 			expectMatch: false,
// 		},
// 		{
// 			name:        "pattern not match exists",
// 			query:       `{app="fake"} !> "<_>foo<bar>"`,
// 			bloom:       fakeBloom{"foo", "bar"},
// 			expectMatch: true,
// 		},
// 		{
// 			name:        "pattern not match does not exist",
// 			query:       `{app="fake"} !> "<_>foo<bar>"`,
// 			bloom:       fakeBloom{"bar", "baz"},
// 			expectMatch: true,
// 		},
// 		{
// 			name:        "pattern all",
// 			query:       `{app="fake"} |> "<_>"`,
// 			bloom:       fakeBloom{"bar", "baz"},
// 			expectMatch: true,
// 		},
// 		{
// 			name:        "pattern empty",
// 			query:       `{app="fake"} |> ""`,
// 			bloom:       fakeBloom{"bar", "baz"},
// 			expectMatch: true,
// 		},
// 	} {
// 		t.Run(tc.name, func(t *testing.T) {
// 			expr, err := syntax.ParseExpr(tc.query)
// 			assert.NoError(t, err)
// 			filters := ExtractTestableLineFilters(expr)

// 			bloomTests := FiltersToBloomTest(fakeNgramBuilder{}, filters...)
// 			assert.Equal(t, tc.expectMatch, bloomTests.Matches(tc.bloom))
// 		})
// 	}
// }

// type fakeNgramBuilder struct{}

// func (f fakeNgramBuilder) Tokens(line string) Iterator[[]byte] {
// 	return NewSliceIter[[]byte]([][]byte{[]byte(line)})
// }

// type fakeBloom []string

// func (f fakeBloom) Test(data []byte) bool {
// 	str := string(data)
// 	for _, match := range f {
// 		if str == match {
// 			return true
// 		}
// 	}
// 	return false
// }

type fakeBloom2 []string

func newFakeBloom2(tokenizer *NGramTokenizer, line string) (res fakeBloom2) {
	toks := tokenizer.Tokens(line)
	for toks.Next() {
		res = append(res, string(toks.At()))
	}
	return
}

func (f fakeBloom2) Test(data []byte) bool {
	str := string(data)
	for _, match := range f {
		if str == match {
			return true
		}
	}
	return false
}

func TestIdk(t *testing.T) {
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
			desc:  "regexp simple",
			line:  "foobarbaz",
			query: `{app="fake"} |~ "(foo|bar)bazz"`,
			match: true,
		},
		{
			desc:  "regexp simple",
			line:  "foobarbaz",
			query: `{app="fake"} |~ "(foo|bar)baz"`,
			match: true,
		},
		{
			desc:  "regexp nomatch",
			line:  "foobarbaz",
			query: `{app="fake"} |~ "(foo|bar)bazzz"`,
			match: false,
		},
	} {

		// shortcut to enable specific tests
		tc.enabled = true
		if !tc.enabled {
			continue
		}
		t.Run(tc.desc, func(t *testing.T) {
			bloom := newFakeBloom2(tokenizer, tc.line)
			expr, err := syntax.ParseExpr(tc.query)
			require.NoError(t, err)
			filters := ExtractTestableLineFilters(expr)
			bloomTests := FiltersToBloomTest(tokenizer, filters...)
			matched := bloomTests.Matches(bloom)

			require.Equal(t, tc.match, matched)

		})
	}
}
