package hintprovider

import (
	"sort"

	"github.com/grafana/loki/v3/pkg/logline"
)

// ExtractQueryNgrams converts query text into sorted unique n-gram terms using
// the extraction algorithm paired with indexVersion.
//
// A text n-gram is sliced to ngramLength, unchanged. A packed key (v4 numeric
// grams) is sliced to the index's full term key width instead, because it carries
// value bytes where the text path leaves zeros: slicing one of those to a shorter
// ngramLength would drop low value bytes, and both FindTerm and the shard filter
// zero-pad what they are given, so the term would resolve to a different
// dictionary key and to the wrong shard.
//
// Returns (nil, nil) when the query is too short to produce ngrams; returns a
// non-nil error only when indexVersion is not a recognised version.
func ExtractQueryNgrams(query string, ngramLength int, indexVersion string) ([]string, error) {
	fn, err := logline.ExtractorForVersion(indexVersion)
	if err != nil {
		return nil, err
	}
	keyLength, err := logline.TermKeyLengthForVersion(indexVersion)
	if err != nil {
		return nil, err
	}

	if ngramLength <= 0 || ngramLength > 8 || len(query) < ngramLength {
		return nil, nil
	}

	ngrams := fn(ngramLength, query, nil, nil, nil)
	if len(ngrams) == 0 {
		return nil, nil
	}

	terms := make([]string, 0, len(ngrams))
	for _, key := range ngrams {
		width := ngramLength
		if logline.IsPackedTermKey(key) {
			width = keyLength
		}
		terms = append(terms, string(key[:width]))
	}
	sort.Strings(terms)

	deduped := terms[:1]
	for _, term := range terms[1:] {
		if term != deduped[len(deduped)-1] {
			deduped = append(deduped, term)
		}
	}
	return deduped, nil
}

// orderUncorrelated spreads adjacent terms apart to reduce local correlation.
// The input terms are not modified.
func orderUncorrelated(terms []string) []string {
	if len(terms) <= 1 {
		return terms
	}

	ordered := make([]string, 0, len(terms))
	stride := max(len(terms)/2, 1)

	for i := range stride {
		ordered = append(ordered, terms[i])
		j := i + stride
		if j < len(terms) {
			ordered = append(ordered, terms[j])
		}
	}

	if len(ordered) < len(terms) {
		ordered = append(ordered, terms[len(ordered):]...)
	}
	return ordered
}
