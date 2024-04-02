package v1

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/grafana/loki/pkg/logql/syntax"
	"github.com/grafana/loki/pkg/storage/bloom/v1/filter"
)

func TestFiltersToBloomTests(t *testing.T) {
	for _, tc := range []struct {
		name        string
		query       string
		bloom       filter.Checker
		expectMatch bool
	}{
		{
			name:        "No filters",
			query:       `{app="fake"}`,
			bloom:       fakeBloom{"foo", "bar"},
			expectMatch: true,
		},
		{
			name:        "Single filter",
			query:       `{app="fake"} |= "foo"`,
			bloom:       fakeBloom{"foo", "bar"},
			expectMatch: true,
		},
		{
			name:        "Single filter no match",
			query:       `{app="fake"} |= "nope"`,
			bloom:       fakeBloom{"foo", "bar"},
			expectMatch: false,
		},
		{
			name:        "two filters",
			query:       `{app="fake"} |= "foo" |= "bar"`,
			bloom:       fakeBloom{"foo", "bar"},
			expectMatch: true,
		},
		{
			name:        "two filters no match",
			query:       `{app="fake"} |= "foo" |= "nope"`,
			bloom:       fakeBloom{"foo", "bar"},
			expectMatch: false,
		},
		{
			name:        "notEq doesnt exist",
			query:       `{app="fake"} != "nope"`,
			bloom:       fakeBloom{"foo", "bar"},
			expectMatch: true,
		},
		{
			name:        "notEq exists",
			query:       `{app="fake"} != "foo"`,
			bloom:       fakeBloom{"foo", "bar"},
			expectMatch: true, // Still should match because it's NotEQ
		},
		{
			name:        "or filter both match",
			query:       `{app="fake"} |= "foo" or "bar"`,
			bloom:       fakeBloom{"foo", "bar"},
			expectMatch: true,
		},
		{
			name:        "or filter one right match",
			query:       `{app="fake"} |= "nope" or "foo"`,
			bloom:       fakeBloom{"foo", "bar"},
			expectMatch: true,
		},
		{
			name:        "or filter one left match",
			query:       `{app="fake"} |= "foo" or "nope"`,
			bloom:       fakeBloom{"foo", "bar"},
			expectMatch: true,
		},
		{
			name:        "or filter no match",
			query:       `{app="fake"} |= "no" or "nope"`,
			bloom:       fakeBloom{"foo", "bar"},
			expectMatch: false,
		},
		{
			name:        "Not or filter match",
			query:       `{app="fake"} != "nope" or "no"`,
			bloom:       fakeBloom{"foo", "bar"},
			expectMatch: true,
		},
		{
			name:        "NotEq OR filter right exists",
			query:       `{app="fake"} != "nope" or "bar"`,
			bloom:       fakeBloom{"foo", "bar"},
			expectMatch: true, // Still should match because it's NotEQ
		},
		{
			name:        "Not OR filter left exists",
			query:       `{app="fake"} != "foo" or "nope"`,
			bloom:       fakeBloom{"foo", "bar"},
			expectMatch: true, // Still should match because it's NotEQ
		},
		{
			name:        "NotEq OR filter both exists",
			query:       `{app="fake"} != "foo" or "bar"`,
			bloom:       fakeBloom{"foo", "bar"},
			expectMatch: true, // Still should match because it's NotEQ
		},
		{
			name:        "complex filter match",
			query:       `{app="fake"} |= "foo" |= "bar" or "baz" |= "fuzz" or "not" != "nope" != "no" or "none"`,
			bloom:       fakeBloom{"foo", "bar", "baz", "fuzz"},
			expectMatch: true,
		},
		{
			name:        "regex match all star",
			query:       `{app="fake"} |~ ".*"`,
			bloom:       fakeBloom{"foo", "bar"},
			expectMatch: true,
		},
		{
			name:        "regex match all plus",
			query:       `{app="fake"} |~ ".+"`,
			bloom:       fakeBloom{"foo", "bar"},
			expectMatch: true,
		},
		{
			name:        "regex match all notEq",
			query:       `{app="fake"} !~ ".*"`,
			bloom:       fakeBloom{"foo", "bar"},
			expectMatch: true, // Still should match,
		},
		{
			name:        "regex match",
			query:       `{app="fake"} |~ "nope|.*foo.*"`,
			bloom:       fakeBloom{"foo", "bar"},
			expectMatch: true,
		},
		{
			name:        "regex no match",
			query:       `{app="fake"} |~ ".*not.*"`,
			bloom:       fakeBloom{"foo", "bar"},
			expectMatch: false,
		},
		{
			name:        "regex notEq right exists",
			query:       `{app="fake"} !~ "nope|.*foo.*"`,
			bloom:       fakeBloom{"foo", "bar"},
			expectMatch: true, // Still should match because it's NotEQ
		},
		{
			name:        "complex regex match",
			query:       `{app="fake"} |~ "(nope|.*not.*|.*foo.*)" or "(no|ba)" !~ "noz.*" or "(nope|not)"`,
			bloom:       fakeBloom{"foo", "bar", "baz", "fuzz"},
			expectMatch: true,
		},
		{
			name:        "complex regex with notEq exists",
			query:       `{app="fake"} |~ "(nope|.*not.*|.*foo.*)" or "(no|ba)" !~ "noz.*"`,
			bloom:       fakeBloom{"foo", "bar", "baz", "fuzz", "noz"},
			expectMatch: true, // Still should match because it's NotEQ
		},
		{
			name:        "line filter after line format",
			query:       `{app="fake"} |= "foo" | line_format "thisNewTextShouldMatch" |= "thisNewTextShouldMatch"`,
			bloom:       fakeBloom{"foo"},
			expectMatch: true,
		},
		{
			name:        "pattern match exists",
			query:       `{app="fake"} |> "<_>foo<bar>"`,
			bloom:       fakeBloom{"foo", "bar"},
			expectMatch: true,
		},
		{
			name:        "pattern match does not exist",
			query:       `{app="fake"} |> "<_>foo<bar>"`,
			bloom:       fakeBloom{"bar", "baz"},
			expectMatch: false,
		},
		{
			name:        "pattern not match exists",
			query:       `{app="fake"} !> "<_>foo<bar>"`,
			bloom:       fakeBloom{"foo", "bar"},
			expectMatch: true,
		},
		{
			name:        "pattern not match does not exist",
			query:       `{app="fake"} !> "<_>foo<bar>"`,
			bloom:       fakeBloom{"bar", "baz"},
			expectMatch: true,
		},
		{
			name:        "pattern all",
			query:       `{app="fake"} |> "<_>"`,
			bloom:       fakeBloom{"bar", "baz"},
			expectMatch: true,
		},
		{
			name:        "pattern empty",
			query:       `{app="fake"} |> ""`,
			bloom:       fakeBloom{"bar", "baz"},
			expectMatch: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			expr, err := syntax.ParseExpr(tc.query)
			assert.NoError(t, err)
			filters := ExtractTestableLineFilters(expr)

			bloomTests := FiltersToBloomTest(fakeNgramBuilder{}, filters...)
			assert.Equal(t, tc.expectMatch, bloomTests.Matches(tc.bloom))
		})
	}
}

type fakeNgramBuilder struct{}

func (f fakeNgramBuilder) Tokens(line string) Iterator[[]byte] {
	return NewSliceIter[[]byte]([][]byte{[]byte(line)})
}

type fakeBloom []string

func (f fakeBloom) Test(data []byte) bool {
	str := string(data)
	for _, match := range f {
		if str == match {
			return true
		}
	}
	return false
}
