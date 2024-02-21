package v1

import (
	"github.com/grafana/loki/pkg/logql/syntax"
	"github.com/grafana/loki/pkg/storage/bloom/v1/filter"
	"github.com/prometheus/prometheus/model/labels"
)

type BloomTest interface {
	Test(bloom filter.Checker) bool
	TestWithPrefixBuf(bloom filter.Checker, buf []byte, prefixLen int) bool
}

type BloomTests []BloomTest

func (b BloomTests) Test(bloom filter.Checker) bool {
	for _, test := range b {
		if !test.Test(bloom) {
			return false
		}
	}
	return true
}

func (b BloomTests) TestWithPrefixBuf(bloom filter.Checker, buf []byte, prefixLen int) bool {
	for _, test := range b {
		if !test.TestWithPrefixBuf(bloom, buf, prefixLen) {
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
		// TODO(salvacorts): Simplify regex similarly to how it's done at pkg/logql/log/filter.go (`simplify` function)
		// 					 Ideally we want to extract the simplify logic into pkg/util/regex.go
		return NoopTest
	default:
		return NoopTest
	}
}

type noopTest struct{}

var NoopTest = noopTest{}

func (n noopTest) Test(_ filter.Checker) bool {
	return true
}

func (n noopTest) TestWithPrefixBuf(_ filter.Checker, _ []byte, _ int) bool {
	return true
}

// NGramBuilder is an interface for tokenizing strings into ngrams
// Extracting this interface allows us to test the bloom filter without having to use the actual tokenizer
// TODO: This should be moved to tokenizer.go
type NGramBuilder interface {
	Build(input string) (ngrams [][]byte)
}

type nGramTokenizerWrapper struct {
	NGramTokenizer
}

func NewNGramBuilder(t NGramTokenizer) NGramBuilder {
	return nGramTokenizerWrapper{NGramTokenizer: t}
}

func (n nGramTokenizerWrapper) Build(input string) (ngrams [][]byte) {
	// TODO: preallocate ngrams
	it := n.Tokens(input)
	for it.Next() {
		ngram := make([]byte, n.N)
		copy(ngram, it.At())
		ngrams = append(ngrams, ngram)
	}
	return
}

type stringTest struct {
	ngrams [][]byte
}

func newStringTest(b NGramBuilder, search string) stringTest {
	return stringTest{ngrams: b.Build(search)}
}

func (b stringTest) Test(bloom filter.Checker) bool {
	for _, ngram := range b.ngrams {
		if !bloom.Test(ngram) {
			return false
		}
	}
	return true
}

func (b stringTest) TestWithPrefixBuf(bloom filter.Checker, buf []byte, prefixLen int) bool {
	for _, ngram := range b.ngrams {
		buf = append(buf[:prefixLen], ngram...)
		if !bloom.Test(buf) {
			return false
		}
	}
	return true
}

type notTest struct {
	BloomTest
}

func newNotTest(test BloomTest) BloomTest {
	return notTest{BloomTest: test}
}

func (b notTest) Test(bloom filter.Checker) bool {
	return !b.BloomTest.Test(bloom)
}

func (b notTest) TestWithPrefixBuf(bloom filter.Checker, buf []byte, prefixLen int) bool {
	return !b.BloomTest.TestWithPrefixBuf(bloom, buf, prefixLen)
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

func (o orTest) Test(bloom filter.Checker) bool {
	return o.left.Test(bloom) || o.right.Test(bloom)
}

func (o orTest) TestWithPrefixBuf(bloom filter.Checker, buf []byte, prefixLen int) bool {
	return o.left.TestWithPrefixBuf(bloom, buf, prefixLen) || o.right.TestWithPrefixBuf(bloom, buf, prefixLen)
}
