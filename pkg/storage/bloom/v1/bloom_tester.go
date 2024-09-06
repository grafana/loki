package v1

import (
	"unicode/utf8"

	"github.com/grafana/regexp"

	iter "github.com/grafana/loki/v3/pkg/iter/v2"
	"github.com/grafana/loki/v3/pkg/logql/log"
	"github.com/grafana/loki/v3/pkg/logql/log/pattern"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/storage/bloom/v1/filter"
)

type BloomTest interface {
	Matches(bloom filter.Checker) bool
	MatchesWithPrefixBuf(bloom filter.Checker, buf []byte, prefixLen int) bool
}

type BloomTests []BloomTest

func (b BloomTests) Matches(bloom filter.Checker) bool {
	for _, test := range b {
		if !test.Matches(bloom) {
			return false
		}
	}
	return true
}

func (b BloomTests) MatchesWithPrefixBuf(bloom filter.Checker, buf []byte, prefixLen int) bool {
	for _, test := range b {
		if !test.MatchesWithPrefixBuf(bloom, buf, prefixLen) {
			return false
		}
	}
	return true
}

// ExtractTestableLineFilters extracts all line filters from an expression
// that can be tested against a bloom filter. This will skip any line filters
// after a line format expression. A line format expression might add content
// that the query later matches against, which can't be tested with a bloom filter.
// E.g. For {app="fake"} |= "foo" | line_format "thisNewTextShouldMatch" |= "thisNewTextShouldMatch"
// this function will return only the line filter for "foo" since the line filter for "thisNewTextShouldMatch"
// wouldn't match against the bloom filter but should match against the query.
func ExtractTestableLineFilters(expr syntax.Expr) []syntax.LineFilterExpr {
	if expr == nil {
		return nil
	}

	var filters []syntax.LineFilterExpr
	var lineFmtFound bool
	visitor := &syntax.DepthFirstTraversal{
		VisitLineFilterFn: func(_ syntax.RootVisitor, e *syntax.LineFilterExpr) {
			if e != nil && !lineFmtFound {
				filters = append(filters, *e)
			}
		},
		VisitLineFmtFn: func(_ syntax.RootVisitor, e *syntax.LineFmtExpr) {
			if e != nil {
				lineFmtFound = true
			}
		},
	}
	expr.Accept(visitor)
	return filters
}

// FiltersToBloomTest converts a list of line filters to a BloomTest.
// Note that all the line filters should be testable against a bloom filter.
// Use ExtractTestableLineFilters to extract testable line filters from an expression.
// TODO(owen-d): limits the number of bloom lookups run.
// An arbitrarily high number can overconsume cpu and is a DoS vector.
// TODO(owen-d): use for loop not recursion to protect callstack
func FiltersToBloomTest(b NGramBuilder, filters ...syntax.LineFilterExpr) BloomTest {
	tests := make(BloomTests, 0, len(filters))
	for _, f := range filters {
		if f.Left != nil {
			tests = append(tests, FiltersToBloomTest(b, *f.Left))
		}
		if f.Or != nil {
			left := FiltersToBloomTest(b, *f.Or)
			right := simpleFilterToBloomTest(b, f.LineFilter)
			tests = append(tests, newOrTest(left, right))
			continue
		}

		tests = append(tests, simpleFilterToBloomTest(b, f.LineFilter))
	}
	return tests
}

func simpleFilterToBloomTest(b NGramBuilder, filter syntax.LineFilter) BloomTest {
	switch filter.Ty {
	case log.LineMatchNotEqual, log.LineMatchNotRegexp, log.LineMatchNotPattern:
		// We cannot test _negated_ filters with a bloom filter since blooms are probabilistic
		// filters that can only tell us if a string _might_ exist.
		// For example, for `!= "foo"`, the bloom filter might tell us that the string "foo" might exist
		// but because we are not sure, we cannot discard that chunk because it might actually not be there.
		// Therefore, we return a test that always returns true.
		return MatchAll
	case log.LineMatchEqual:
		return newStringTest(b, filter.Match)
	case log.LineMatchRegexp:
		return MatchAll
	case log.LineMatchPattern:
		return newPatternTest(b, filter.Match)
	default:
		return MatchAll
	}
}

type bloomCheckerWrapper struct {
	bloom filter.Checker
}

// Test implements the log.Checker interface
func (b bloomCheckerWrapper) Test(line []byte, _ bool, _ bool) bool {
	return b.bloom.Test(line)
}

// TestRegex implements the log.Checker interface
func (b bloomCheckerWrapper) TestRegex(_ *regexp.Regexp) bool {
	// We won't support regexes in bloom filters so we just return true
	return true
}

type logCheckerWrapper struct {
	checker log.Checker
}

// Test implements the filter.Checker interface
func (l logCheckerWrapper) Test(data []byte) bool {
	return l.checker.Test(data, true, false)
}

type matcherFilterWrapper struct {
	filter log.Matcher
}

func (m matcherFilterWrapper) Matches(bloom filter.Checker) bool {
	return m.filter.Matches(bloomCheckerWrapper{bloom})
}

func (m matcherFilterWrapper) MatchesWithPrefixBuf(bloom filter.Checker, buf []byte, prefixLen int) bool {
	return m.filter.Matches(bloomCheckerWrapper{prefixedChecker{
		checker:   bloom,
		buf:       buf,
		prefixLen: prefixLen,
	}})
}

type prefixedChecker struct {
	checker   filter.Checker
	buf       []byte
	prefixLen int
}

func (p prefixedChecker) Test(data []byte) bool {
	return p.checker.Test(append(p.buf[:p.prefixLen], data...))
}

type matchAllTest struct{}

var MatchAll = matchAllTest{}

func (n matchAllTest) Matches(_ filter.Checker) bool {
	return true
}

func (n matchAllTest) MatchesWithPrefixBuf(_ filter.Checker, _ []byte, _ int) bool {
	return true
}

// NGramBuilder is an interface for tokenizing strings into ngrams
// Extracting this interface allows us to test the bloom filter without having to use the actual tokenizer
// TODO: This should be moved to tokenizer.go
type NGramBuilder interface {
	Tokens(line string) iter.Iterator[[]byte]
	N() int
	SkipFactor() int
}

type stringTest struct {
	ngrams [][]byte
}

func newStringTest(b NGramBuilder, search string) (res BloomTest) {
	// search string must be longer than the combined ngram length and skip factor
	// in order for all possible skip offsets to have at least 1 ngram
	skip := b.SkipFactor()
	if ct := utf8.RuneCountInString(search); ct < b.N()+skip {
		return MatchAll
	}

	tests := make([]stringTest, 0, skip)

	for i := 0; i < skip+1; i++ {
		searchWithOffset := search
		for j := 0; j < i; j++ {
			_, size := utf8.DecodeRuneInString(searchWithOffset)
			// NB(owen-d): small bounds check for invalid utf8
			searchWithOffset = searchWithOffset[min(size, len(searchWithOffset)):]
		}

		var test stringTest
		it := b.Tokens(searchWithOffset)
		for it.Next() {
			ngram := make([]byte, len(it.At()))
			copy(ngram, it.At())
			test.ngrams = append(test.ngrams, ngram)
		}
		tests = append(tests, test)
	}

	res = tests[0]
	for _, t := range tests[1:] {
		res = newOrTest(res, t)
	}
	return res
}

// Matches implements the BloomTest interface
func (b stringTest) Matches(bloom filter.Checker) bool {
	for _, ngram := range b.ngrams {
		if !bloom.Test(ngram) {
			return false
		}
	}
	return true
}

// MatchesWithPrefixBuf implements the BloomTest interface
func (b stringTest) MatchesWithPrefixBuf(bloom filter.Checker, buf []byte, prefixLen int) bool {
	for _, ngram := range b.ngrams {
		buf = append(buf[:prefixLen], ngram...)
		if !bloom.Test(buf) {
			return false
		}
	}
	return true
}

type stringMatcherFilter struct {
	test BloomTest
}

// Matches implements the log.Filterer interface
func (b stringMatcherFilter) Matches(test log.Checker) bool {
	return b.test.Matches(logCheckerWrapper{test})
}

func newStringFilterFunc(b NGramBuilder) log.NewMatcherFiltererFunc {
	return func(match []byte, _ bool) log.MatcherFilterer {
		return log.WrapMatcher(stringMatcherFilter{
			test: newStringTest(b, string(match)),
		})
	}
}

type orTest struct {
	left, right BloomTest
}

// In addition to common `|= "foo" or "bar"`,
// orTest is particularly useful when testing skip-factors>0, which
// can result in different "sequences" of ngrams for a particular line
// and if either sequence matches the filter, the chunk is considered a match.
// For instance, with n=3,skip=1, the line "foobar" generates ngrams:
// ["foo", "oob", "oba", "bar"]
// Now let's say we want to search for the same "foobar".
// Note: we don't know which offset in the line this match may be,
// so we check every possible offset. The filter will match the ngrams:
// offset_0 creates ["foo", "oba"]
// offset_1 creates ["oob", "bar"]
// If either sequences are found in the bloom filter, the chunk is considered a match.
// Expanded, this is
// match == (("foo" && "oba") || ("oob" && "bar"))
func newOrTest(left, right BloomTest) orTest {
	return orTest{
		left:  left,
		right: right,
	}
}

func (o orTest) Matches(bloom filter.Checker) bool {
	return o.left.Matches(bloom) || o.right.Matches(bloom)
}

func (o orTest) MatchesWithPrefixBuf(bloom filter.Checker, buf []byte, prefixLen int) bool {
	return o.left.MatchesWithPrefixBuf(bloom, buf, prefixLen) || o.right.MatchesWithPrefixBuf(bloom, buf, prefixLen)
}

func newPatternTest(b NGramBuilder, match string) BloomTest {
	lit, err := pattern.ParseLiterals(match)
	if err != nil {
		return MatchAll
	}

	var res BloomTests

	for _, l := range lit {
		res = append(res, newStringTest(b, string(l)))
	}
	return res
}
