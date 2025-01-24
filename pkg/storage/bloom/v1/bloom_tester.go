package v1

import (
	"fmt"
	"unsafe"

	"github.com/go-kit/log/level"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/storage/bloom/v1/filter"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
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

	case KeyValueMatcher:
		return newKeyValueMatcherTest(matcher)

	case KeyMatcher:
		return newKeyMatcherTest(matcher)

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

type keyValueMatcherTest struct {
	matcher KeyValueMatcher
}

func newKeyValueMatcherTest(matcher KeyValueMatcher) keyValueMatcherTest {
	return keyValueMatcherTest{matcher: matcher}
}

func (kvm keyValueMatcherTest) Matches(series labels.Labels, bloom filter.Checker) bool {
	// TODO(rfratto): reintroduce the use of a shared tokenizer here to avoid
	// desyncing between how tokens are passed during building vs passed during
	// querying.
	//
	// For a shared tokenizer to be ergonomic:
	//
	// 1. A prefix shouldn't be required until MatchesWithPrefixBuf is called
	// 2. It should be possible to test for just the key

	var (
		combined    = fmt.Sprintf("%s=%s", kvm.matcher.Key, kvm.matcher.Value)
		rawCombined = unsafe.Slice(unsafe.StringData(combined), len(combined)) // #nosec G103 -- we know the string is not mutated
	)

	return kvm.match(series, bloom, rawCombined)
}

func (kvm keyValueMatcherTest) MatchesWithPrefixBuf(series labels.Labels, bloom filter.Checker, buf []byte, prefixLen int) bool {
	var (
		combined         = fmt.Sprintf("%s=%s", kvm.matcher.Key, kvm.matcher.Value)
		prefixedCombined = appendToBuf(buf, prefixLen, combined)
	)

	return kvm.match(series, bloom, prefixedCombined)
}

// match returns true if the series matches the matcher or is in the bloom filter.
func (kvm keyValueMatcherTest) match(series labels.Labels, bloom filter.Checker, combined []byte) bool {
	// If we don't have the series labels, we cannot disambiguate which labels come from the series in which case
	// we may filter out chunks for queries like `{env="prod"} | env="prod"` if env=prod is not structured metadata
	if len(series) == 0 {
		level.Warn(util_log.Logger).Log("msg", "series has no labels, cannot filter out chunks")
		return true
	}

	// It's in the series if the key is set and has the same value.
	// By checking val != "" we handle `{env="prod"} | user=""`.
	val := series.Get(kvm.matcher.Key)
	inSeries := val != "" && val == kvm.matcher.Value

	inBloom := bloom.Test(combined)
	return inSeries || inBloom
}

// appendToBuf is the equivalent of append(buf[:prefixLen], str). len(buf) must
// be greater than or equal to prefixLen+len(str) to avoid allocations.
func appendToBuf(buf []byte, prefixLen int, str string) []byte {
	rawString := unsafe.Slice(unsafe.StringData(str), len(str)) // #nosec G103 -- we know the string is not mutated
	return append(buf[:prefixLen], rawString...)
}

type keyMatcherTest struct {
	matcher KeyMatcher
}

func newKeyMatcherTest(matcher KeyMatcher) keyMatcherTest {
	return keyMatcherTest{matcher: matcher}
}

func (km keyMatcherTest) Matches(series labels.Labels, bloom filter.Checker) bool {
	// TODO(rfratto): reintroduce the use of a shared tokenizer here to avoid
	// desyncing between how tokens are passed during building vs passed during
	// querying.
	//
	// For a shared tokenizer to be ergonomic:
	//
	// 1. A prefix shouldn't be required until MatchesWithPrefixBuf is called
	// 2. It should be possible to test for just the key

	var (
		key    = km.matcher.Key
		rawKey = unsafe.Slice(unsafe.StringData(key), len(key))
	)

	return km.match(series, bloom, rawKey)
}

func (km keyMatcherTest) MatchesWithPrefixBuf(series labels.Labels, bloom filter.Checker, buf []byte, prefixLen int) bool {
	var (
		key         = km.matcher.Key
		prefixedKey = appendToBuf(buf, prefixLen, key)
	)

	return km.match(series, bloom, prefixedKey)
}

// match returns true if the series matches the matcher or is in the bloom
// filter.
func (km keyMatcherTest) match(series labels.Labels, bloom filter.Checker, key []byte) bool {
	// If we don't have the series labels, we cannot disambiguate which labels come from the series in which case
	// we may filter out chunks for queries like `{env="prod"} | env="prod"` if env=prod is not structured metadata
	if len(series) == 0 {
		level.Warn(util_log.Logger).Log("msg", "series has no labels, cannot filter out chunks")
		return true
	}

	inSeries := series.Get(km.matcher.Key) != ""
	inBloom := bloom.Test(key)
	return inSeries || inBloom
}
