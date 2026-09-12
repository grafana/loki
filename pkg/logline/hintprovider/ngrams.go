package hintprovider

import (
	"sort"

	"github.com/grafana/loki/v3/pkg/logline"
)

// ExtractQueryNgrams converts query text into sorted unique n-gram terms using
// the extraction algorithm paired with indexVersion.
//
// Returns (nil, nil) when the query is too short to produce ngrams; returns a
// non-nil error only when indexVersion is not a recognised version.
func ExtractQueryNgrams(query string, ngramLength int, indexVersion string) ([]string, error) {
	fn, err := logline.ExtractorForVersion(indexVersion)
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
		terms = append(terms, string(key[:ngramLength]))
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
