package v1

import (
	regexpsyntax "github.com/grafana/regexp/syntax"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/pkg/logql/log"
	"github.com/grafana/loki/pkg/logql/syntax"
	"github.com/grafana/loki/pkg/storage/bloom/v1/filter"
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
	case labels.MatchEqual, labels.MatchNotEqual:
		var test BloomTest = newStringTest(b, filter.Match)
		if filter.Ty == labels.MatchNotEqual {
			test = newNotTest(test)
		}
		return test
	case labels.MatchRegexp, labels.MatchNotRegexp:
		reg, err := regexpsyntax.Parse(filter.Match, regexpsyntax.Perl)
		if err != nil {
			// TODO: log error
			return MatchAll
		}
		reg = reg.Simplify()

		simplifier := log.NewRegexSimplifier(newStringFilterFunc(b), newStringFilterFunc(b))
		matcherFilter, ok := simplifier.Simplify(reg, false)
		if !ok {
			// If the regex simplifier fails, we default to MatchAll
			return MatchAll
		}

		var test BloomTest = matcherFilterWrapper{filter: matcherFilter}
		if filter.Ty == labels.MatchNotRegexp {
			test = newNotTest(test)
		}
		return test
	default:
		return MatchAll
	}
}

type matcherFilterWrapper struct {
	filter log.MatcherFilter
}

// Matches implements the BloomTest interface
func (m matcherFilterWrapper) Matches(bloom filter.Checker) bool {
	return m.filter.Matches(bloom)
}

// MatchesWithPrefixBuf implements the BloomTest interface
func (m matcherFilterWrapper) MatchesWithPrefixBuf(bloom filter.Checker, buf []byte, prefixLen int) bool {
	return m.filter.Matches(prefixedChecker{
		checker:   bloom,
		buf:       buf,
		prefixLen: prefixLen,
	})
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

// Matches implements the BloomTest interface
func (n matchAllTest) Matches(_ filter.Checker) bool {
	return true
}

// MatchesWithPrefixBuf implements the BloomTest interface
func (n matchAllTest) MatchesWithPrefixBuf(_ filter.Checker, _ []byte, _ int) bool {
	return true
}

// NGramBuilder is an interface for tokenizing strings into ngrams
// Extracting this interface allows us to test the bloom filter without having to use the actual tokenizer
// TODO: This should be moved to tokenizer.go
type NGramBuilder interface {
	Tokens(line string) Iterator[[]byte]
}

type stringTest struct {
	ngrams [][]byte
}

func newStringTest(b NGramBuilder, search string) stringTest {
	var test stringTest
	it := b.Tokens(search)
	for it.Next() {
		ngram := make([]byte, len(it.At()))
		copy(ngram, it.At())
		test.ngrams = append(test.ngrams, ngram)
	}
	return test
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
	test stringTest
}

// Filter implements the log.Filterer interface
func (b stringMatcherFilter) Filter(line []byte) bool {
	panic("implement me")
}

// ToStage implements the log.Filterer interface
func (b stringMatcherFilter) ToStage() log.Stage {
	panic("implement me")
}

// Matches implements the log.MatcherFilter interface
func (b stringMatcherFilter) Matches(test log.Checker) bool {
	return b.test.Matches(test.(filter.Checker))
}

func newStringFilterFunc(b NGramBuilder) log.NewMatcherFilterFunc {
	return func(match []byte, caseInsensitive bool) log.MatcherFilter {
		return stringMatcherFilter{
			test: newStringTest(b, string(match)),
		}
	}
}

type notTest struct {
	BloomTest
}

func newNotTest(test BloomTest) BloomTest {
	return notTest{BloomTest: test}
}

func (b notTest) Matches(bloom filter.Checker) bool {
	return !b.BloomTest.Matches(bloom)
}

func (b notTest) MatchesWithPrefixBuf(bloom filter.Checker, buf []byte, prefixLen int) bool {
	return !b.BloomTest.MatchesWithPrefixBuf(bloom, buf, prefixLen)
}

type orTest struct {
	left, right BloomTest
}

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
