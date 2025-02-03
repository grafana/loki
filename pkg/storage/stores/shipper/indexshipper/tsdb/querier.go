// Copyright 2017 The Prometheus Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package tsdb

import (
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/pkg/errors"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/storage"

	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/index"
)

// Bitmap used by func isRegexMetaCharacter to check whether a character needs to be escaped.
var regexMetaCharacterBytes [16]byte

// isRegexMetaCharacter reports whether byte b needs to be escaped.
func isRegexMetaCharacter(b byte) bool {
	return b < utf8.RuneSelf && regexMetaCharacterBytes[b%16]&(1<<(b/16)) != 0
}

func init() {
	for _, b := range []byte(`.+*?()|[]{}^$`) {
		regexMetaCharacterBytes[b%16] |= 1 << (b / 16)
	}
}

// IndexReader provides reading access of serialized index data.
type IndexReader interface {
	// Bounds returns the earliest and latest samples in the index
	Bounds() (int64, int64)

	Checksum() uint32

	// Symbols return an iterator over sorted string symbols that may occur in
	// series' labels and indices. It is not safe to use the returned strings
	// beyond the lifetime of the index reader.
	Symbols() index.StringIter

	// SortedLabelValues returns sorted possible label values.
	SortedLabelValues(name string, matchers ...*labels.Matcher) ([]string, error)

	// LabelValues returns possible label values which may not be sorted.
	LabelValues(name string, matchers ...*labels.Matcher) ([]string, error)

	// Postings returns the postings list iterator for the label pairs.
	// The Postings here contain the offsets to the series inside the index.
	// Found IDs are not strictly required to point to a valid Series, e.g.
	// during background garbage collections. Input values must be sorted.
	Postings(name string, fpFilter index.FingerprintFilter, values ...string) (index.Postings, error)

	// Series populates the given labels and chunk metas for the series identified
	// by the reference.
	// Returns storage.ErrNotFound if the ref does not resolve to a known series.
	Series(ref storage.SeriesRef, from int64, through int64, lset *labels.Labels, chks *[]index.ChunkMeta) (uint64, error)

	// ChunkStats returns the stats for the chunks in the given series.
	ChunkStats(ref storage.SeriesRef, from, through int64, lset *labels.Labels, by map[string]struct{}) (uint64, index.ChunkStats, error)

	// LabelNames returns all the unique label names present in the index in sorted order.
	LabelNames(matchers ...*labels.Matcher) ([]string, error)

	// LabelValueFor returns label value for the given label name in the series referred to by ID.
	// If the series couldn't be found or the series doesn't have the requested label a
	// storage.ErrNotFound is returned as error.
	LabelValueFor(id storage.SeriesRef, label string) (string, error)

	// LabelNamesFor returns all the label names for the series referred to by IDs.
	// The names returned are sorted.
	LabelNamesFor(ids ...storage.SeriesRef) ([]string, error)

	// Close releases the underlying resources of the reader.
	Close() error
}

// PostingsForMatchers assembles a single postings iterator against the index reader
// based on the given matchers. The resulting postings are not ordered by series.
func PostingsForMatchers(ix IndexReader, fpFilter index.FingerprintFilter, ms ...*labels.Matcher) (index.Postings, error) {
	var its, notIts []index.Postings
	// See which label must be non-empty.
	// Optimization for case like {l=~".", l!="1"}.
	labelMustBeSet := make(map[string]bool, len(ms))
	for _, m := range ms {
		if !m.Matches("") {
			labelMustBeSet[m.Name] = true
		}
	}

	for _, m := range ms {
		if labelMustBeSet[m.Name] {
			// If this matcher must be non-empty, we can be smarter.
			matchesEmpty := m.Matches("")
			isNot := m.Type == labels.MatchNotEqual || m.Type == labels.MatchNotRegexp
			if isNot && matchesEmpty { // l!="foo"
				// If the label can't be empty and is a Not and the inner matcher
				// doesn't match empty, then subtract it out at the end.
				inverse, err := m.Inverse()
				if err != nil {
					return nil, err
				}

				it, err := postingsForMatcher(ix, fpFilter, inverse)
				if err != nil {
					return nil, err
				}
				notIts = append(notIts, it)
			} else if isNot && !matchesEmpty { // l!=""
				// If the label can't be empty and is a Not, but the inner matcher can
				// be empty we need to use inversePostingsForMatcher.
				inverse, err := m.Inverse()
				if err != nil {
					return nil, err
				}

				it, err := inversePostingsForMatcher(ix, fpFilter, inverse)
				if err != nil {
					return nil, err
				}
				its = append(its, it)
			} else { // l="a"
				// Non-Not matcher, use normal postingsForMatcher.
				it, err := postingsForMatcher(ix, fpFilter, m)
				if err != nil {
					return nil, err
				}
				its = append(its, it)
			}
		} else { // l=""
			// If the matchers for a labelname selects an empty value, it selects all
			// the series which don't have the label name set too. See:
			// https://github.com/prometheus/prometheus/issues/3575 and
			// https://github.com/prometheus/prometheus/pull/3578#issuecomment-351653555
			it, err := inversePostingsForMatcher(ix, fpFilter, m)
			if err != nil {
				return nil, err
			}
			notIts = append(notIts, it)
		}
	}

	// If there's nothing to subtract from, add in everything and remove the notIts later.
	if len(its) == 0 && len(notIts) != 0 {
		k, v := index.AllPostingsKey()
		allPostings, err := ix.Postings(k, fpFilter, v)
		if err != nil {
			return nil, err
		}
		its = append(its, allPostings)
	}

	it := index.Intersect(its...)

	for _, n := range notIts {
		it = index.Without(it, n)
	}

	return it, nil
}

func postingsForMatcher(ix IndexReader, fpFilter index.FingerprintFilter, m *labels.Matcher) (index.Postings, error) {
	// This method will not return postings for missing labels.

	// Fast-path for equal matching.
	if m.Type == labels.MatchEqual {
		return ix.Postings(m.Name, fpFilter, m.Value)
	}

	// Fast-path for set matching.
	if m.Type == labels.MatchRegexp {
		setMatches := findSetMatches(m.GetRegexString())
		if len(setMatches) > 0 {
			sort.Strings(setMatches)
			return ix.Postings(m.Name, fpFilter, setMatches...)
		}
	}

	vals, err := ix.LabelValues(m.Name)
	if err != nil {
		return nil, err
	}

	var res []string
	lastVal, isSorted := "", true
	for _, val := range vals {
		if m.Matches(val) {
			res = append(res, val)
			if isSorted && val < lastVal {
				isSorted = false
			}
			lastVal = val
		}
	}

	if len(res) == 0 {
		return index.EmptyPostings(), nil
	}

	if !isSorted {
		sort.Strings(res)
	}
	return ix.Postings(m.Name, fpFilter, res...)
}

// inversePostingsForMatcher returns the postings for the series with the label name set but not matching the matcher.
func inversePostingsForMatcher(ix IndexReader, fpFilter index.FingerprintFilter, m *labels.Matcher) (index.Postings, error) {
	// Fast-path for MatchNotRegexp matching.
	// Inverse of a MatchNotRegexp is MatchRegexp (double negation).
	// Fast-path for set matching.
	if m.Type == labels.MatchNotRegexp {
		setMatches := findSetMatches(m.GetRegexString())
		if len(setMatches) > 0 {
			return ix.Postings(m.Name, fpFilter, setMatches...)
		}
	}

	// Fast-path for MatchNotEqual matching.
	// Inverse of a MatchNotEqual is MatchEqual (double negation).
	if m.Type == labels.MatchNotEqual {
		return ix.Postings(m.Name, fpFilter, m.Value)
	}

	vals, err := ix.LabelValues(m.Name)
	if err != nil {
		return nil, err
	}

	var res []string
	lastVal, isSorted := "", true
	for _, val := range vals {
		if !m.Matches(val) {
			res = append(res, val)
			if isSorted && val < lastVal {
				isSorted = false
			}
			lastVal = val
		}
	}

	if !isSorted {
		sort.Strings(res)
	}
	return ix.Postings(m.Name, fpFilter, res...)
}

func findSetMatches(pattern string) []string {
	// Return empty matches if the wrapper from Prometheus is missing.
	if len(pattern) < 6 || pattern[:4] != "^(?:" || pattern[len(pattern)-2:] != ")$" {
		return nil
	}
	escaped := false
	sets := []*strings.Builder{{}}
	for i := 4; i < len(pattern)-2; i++ {
		if escaped {
			switch {
			case isRegexMetaCharacter(pattern[i]):
				sets[len(sets)-1].WriteByte(pattern[i])
			case pattern[i] == '\\':
				sets[len(sets)-1].WriteByte('\\')
			default:
				return nil
			}
			escaped = false
		} else {
			switch {
			case isRegexMetaCharacter(pattern[i]):
				if pattern[i] == '|' {
					sets = append(sets, &strings.Builder{})
				} else {
					return nil
				}
			case pattern[i] == '\\':
				escaped = true
			default:
				sets[len(sets)-1].WriteByte(pattern[i])
			}
		}
	}
	matches := make([]string, 0, len(sets))
	for _, s := range sets {
		if s.Len() > 0 {
			matches = append(matches, s.String())
		}
	}
	return matches
}

func labelValuesWithMatchers(r IndexReader, name string, matchers ...*labels.Matcher) ([]string, error) {
	// We're only interested in metrics which have the label <name>.
	requireLabel, err := labels.NewMatcher(labels.MatchNotEqual, name, "")
	if err != nil {
		return nil, errors.Wrapf(err, "Failed to instantiate label matcher")
	}

	var p index.Postings
	p, err = PostingsForMatchers(r, nil, append(matchers, requireLabel)...)
	if err != nil {
		return nil, err
	}

	dedupe := map[string]interface{}{}
	for p.Next() {
		v, err := r.LabelValueFor(p.At(), name)
		if err != nil {
			if err == storage.ErrNotFound {
				continue
			}

			return nil, err
		}
		dedupe[v] = nil
	}

	if err = p.Err(); err != nil {
		return nil, err
	}

	values := make([]string, 0, len(dedupe))
	for value := range dedupe {
		values = append(values, value)
	}

	return values, nil
}

func labelNamesWithMatchers(r IndexReader, matchers ...*labels.Matcher) ([]string, error) {
	p, err := PostingsForMatchers(r, nil, matchers...)
	if err != nil {
		return nil, err
	}

	var postings []storage.SeriesRef
	for p.Next() {
		postings = append(postings, p.At())
	}
	if p.Err() != nil {
		return nil, errors.Wrapf(p.Err(), "postings for label names with matchers")
	}

	return r.LabelNamesFor(postings...)
}
