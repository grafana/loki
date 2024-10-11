package v1

import (
	"fmt"
	"unsafe"

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

type matchAllTest struct{}

var MatchAll = matchAllTest{}

// Matches implements BloomTest
func (n matchAllTest) Matches(_ filter.Checker) bool {
	return true
}

// MatchesWithPrefixBuf implements BloomTest
func (n matchAllTest) MatchesWithPrefixBuf(_ filter.Checker, _ []byte, _ int) bool {
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
func (o orTest) Matches(bloom filter.Checker) bool {
	return o.left.Matches(bloom) || o.right.Matches(bloom)
}

// MatchesWithPrefixBuf implements BloomTest
func (o orTest) MatchesWithPrefixBuf(bloom filter.Checker, buf []byte, prefixLen int) bool {
	return o.left.MatchesWithPrefixBuf(bloom, buf, prefixLen) || o.right.MatchesWithPrefixBuf(bloom, buf, prefixLen)
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
func (a andTest) Matches(bloom filter.Checker) bool {
	return a.left.Matches(bloom) && a.right.Matches(bloom)
}

// MatchesWithPrefixBuf implements BloomTest
func (a andTest) MatchesWithPrefixBuf(bloom filter.Checker, buf []byte, prefixLen int) bool {
	return a.left.MatchesWithPrefixBuf(bloom, buf, prefixLen) && a.right.MatchesWithPrefixBuf(bloom, buf, prefixLen)
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

func (sm stringMatcherTest) Matches(bloom filter.Checker) bool {
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

	if !bloom.Test(rawKey) {
		// The structured metadata key wasn't indexed. However, sm.matcher might be
		// checking against a label which *does* exist, so we can't safely filter
		// out this chunk.
		//
		// TODO(rfratto): The negative test here is a bit confusing, and the key
		// presence test should likely be done higher up within FuseQuerier.
		return true
	}

	return bloom.Test(rawCombined)
}

func (sm stringMatcherTest) MatchesWithPrefixBuf(bloom filter.Checker, buf []byte, prefixLen int) bool {
	var (
		combined = fmt.Sprintf("%s=%s", sm.matcher.Key, sm.matcher.Value)

		prefixedKey      = appendToBuf(buf, prefixLen, sm.matcher.Key)
		prefixedCombined = appendToBuf(buf, prefixLen, combined)
	)

	if !bloom.Test(prefixedKey) {
		// The structured metadata key wasn't indexed for a prefix. However,
		// sm.matcher might be checking against a label which *does* exist, so we
		// can't safely filter out this chunk.
		//
		// TODO(rfratto): The negative test here is a bit confusing, and the key
		// presence test should likely be done higher up within FuseQuerier.
		return true
	}

	return bloom.Test(prefixedCombined)
}

// appendToBuf is the equivalent of append(buf[:prefixLen], str). len(buf) must
// be greater than or equal to prefixLen+len(str) to avoid allocations.
func appendToBuf(buf []byte, prefixLen int, str string) []byte {
	rawString := unsafe.Slice(unsafe.StringData(str), len(str))
	return append(buf[:prefixLen], rawString...)
}
