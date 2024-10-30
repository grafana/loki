package v1

import (
	"fmt"
	"unsafe"

	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/storage/bloom/v1/filter"
)

type BloomTest interface {
	Matches(series labels.Labels, bloom filter.Checker) bool
	MatchesWithPrefixBuf(series labels.Labels, bloom filter.Checker, buf []byte, prefixLen int) bool
}

type BloomTests []BloomTest

func (b BloomTests) Matches(series labels.Labels, bloom filter.Checker) bool {
	for _, test := range b {
		if !test.Matches(series, bloom) {
			return false
		}
	}
	return true
}

func (b BloomTests) MatchesWithPrefixBuf(series labels.Labels, bloom filter.Checker, buf []byte, prefixLen int) bool {
	for _, test := range b {
		if !test.MatchesWithPrefixBuf(series, bloom, buf, prefixLen) {
			return false
		}
	}
	return true
}

type matchAllTest struct{}

var MatchAll = matchAllTest{}

// Matches implements BloomTest
func (n matchAllTest) Matches(_ labels.Labels, _ filter.Checker) bool {
	return true
}

// MatchesWithPrefixBuf implements BloomTest
func (n matchAllTest) MatchesWithPrefixBuf(_ labels.Labels, _ filter.Checker, _ []byte, _ int) bool {
	return true
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

// Matches implements BloomTest
func (o orTest) Matches(series labels.Labels, bloom filter.Checker) bool {
	return o.left.Matches(series, bloom) || o.right.Matches(series, bloom)
}

// MatchesWithPrefixBuf implements BloomTest
func (o orTest) MatchesWithPrefixBuf(series labels.Labels, bloom filter.Checker, buf []byte, prefixLen int) bool {
	return o.left.MatchesWithPrefixBuf(series, bloom, buf, prefixLen) || o.right.MatchesWithPrefixBuf(series, bloom, buf, prefixLen)
}

type andTest struct {
	left, right BloomTest
}

func newAndTest(left, right BloomTest) andTest {
	return andTest{
		left:  left,
		right: right,
	}
}

// Matches implements BloomTest
func (a andTest) Matches(series labels.Labels, bloom filter.Checker) bool {
	return a.left.Matches(series, bloom) && a.right.Matches(series, bloom)
}

// MatchesWithPrefixBuf implements BloomTest
func (a andTest) MatchesWithPrefixBuf(series labels.Labels, bloom filter.Checker, buf []byte, prefixLen int) bool {
	return a.left.MatchesWithPrefixBuf(series, bloom, buf, prefixLen) && a.right.MatchesWithPrefixBuf(series, bloom, buf, prefixLen)
}

func LabelMatchersToBloomTest(matchers ...LabelMatcher) BloomTest {
	tests := make(BloomTests, 0, len(matchers))
	for _, matcher := range matchers {
		tests = append(tests, matcherToBloomTest(matcher))
	}
	return tests
}

func matcherToBloomTest(matcher LabelMatcher) BloomTest {
	switch matcher := matcher.(type) {
	case UnsupportedLabelMatcher:
		return matchAllTest{}

	case PlainLabelMatcher:
		return newStringMatcherTest(matcher)

	case OrLabelMatcher:
		return newOrTest(
			matcherToBloomTest(matcher.Left),
			matcherToBloomTest(matcher.Right),
		)

	case AndLabelMatcher:
		return newAndTest(
			matcherToBloomTest(matcher.Left),
			matcherToBloomTest(matcher.Right),
		)

	default:
		// Unhandled cases pass bloom tests by default.
		return matchAllTest{}
	}
}

type stringMatcherTest struct {
	matcher PlainLabelMatcher
}

func newStringMatcherTest(matcher PlainLabelMatcher) stringMatcherTest {
	return stringMatcherTest{matcher: matcher}
}

func (sm stringMatcherTest) Matches(series labels.Labels, bloom filter.Checker) bool {
	// TODO(rfratto): reintroduce the use of a shared tokenizer here to avoid
	// desyncing between how tokens are passed during building vs passed during
	// querying.
	//
	// For a shared tokenizer to be ergonomic:
	//
	// 1. A prefix shouldn't be required until MatchesWithPrefixBuf is called
	// 2. It should be possible to test for just the key

	var (
		combined = fmt.Sprintf("%s=%s", sm.matcher.Key, sm.matcher.Value)

		rawKey      = unsafe.Slice(unsafe.StringData(sm.matcher.Key), len(sm.matcher.Key))
		rawCombined = unsafe.Slice(unsafe.StringData(combined), len(combined))
	)

	return sm.match(series, bloom, rawKey, rawCombined)
}

func (sm stringMatcherTest) MatchesWithPrefixBuf(series labels.Labels, bloom filter.Checker, buf []byte, prefixLen int) bool {
	var (
		combined = fmt.Sprintf("%s=%s", sm.matcher.Key, sm.matcher.Value)

		prefixedKey      = appendToBuf(buf, prefixLen, sm.matcher.Key)
		prefixedCombined = appendToBuf(buf, prefixLen, combined)
	)

	return sm.match(series, bloom, prefixedKey, prefixedCombined)
}

func (sm stringMatcherTest) match(series labels.Labels, bloom filter.Checker, key []byte, combined []byte) bool {
	// If the label is part of the series, we cannot check the bloom since
	// the label is not structured metadata
	if value := series.Get(sm.matcher.Key); value != "" {
		// If the series label value is the same as the matcher value, we cannot filter out this chunk.
		// Otherwise, we can filter out this chunk.
		// E.g. `{env="prod"} | env="prod"` should not filter out the chunk.
		// E.g. `{env="prod"} | env="dev"` should filter out the chunk.
		// E.g. `{env="prod"} | env=""` should filter out the chunk.
		return value == sm.matcher.Value
	}

	// To this point we know the label is structured metadata so if the label name is not
	// in the bloom, we can filter out the chunk.
	if !bloom.Test(key) {
		return false
	}

	return bloom.Test(combined)
}

// appendToBuf is the equivalent of append(buf[:prefixLen], str). len(buf) must
// be greater than or equal to prefixLen+len(str) to avoid allocations.
func appendToBuf(buf []byte, prefixLen int, str string) []byte {
	rawString := unsafe.Slice(unsafe.StringData(str), len(str))
	return append(buf[:prefixLen], rawString...)
}
