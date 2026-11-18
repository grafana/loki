package log

import (
	"slices"
)

// minContainsAnyLiterals is the smallest number of case-sensitive literals for which an OR of
// contains filters becomes a containsAnyFilter.
//
// Below it, one bytes.Contains call per literal is faster. bytes.Contains uses SIMD, and the
// Aho-Corasick matcher reads one byte at a time.
const minContainsAnyLiterals = 6

// minCaseInsensitiveContainsAnyLiterals is the smallest number of case-insensitive literals for
// which an OR of contains filters becomes a containsAnyFilter. It is lower than
// minContainsAnyLiterals, because containsLower reads one byte at a time too. Two literals cost
// about the same either way.
const minCaseInsensitiveContainsAnyLiterals = 3

// Limits on the literals of an Aho-Corasick containsAnyFilter. The query sets the literals, and
// the matcher costs grow fast with their size:
//
//   - Memory is about 4 KB per byte of the literals, so 2 KB of literals take about 8 MB.
//   - Build time grows with the cube of the longest literal in the worst case. Six literals of
//     128 bytes take about 3 ms, and six of 512 bytes take about 70 ms.
//
// An OR above either limit stays an OR of contains filters.
const (
	maxContainsAnyBytes      = 2048
	maxContainsAnyLiteralLen = 128
)

// Limits on the literals of a literalDFA containsAnyFilter. A literalDFA holds 4 bytes for each
// state and byte class, with one state per byte of the literals or their variants.
//
//   - maxLiteralDFABytes bounds the bytes of all literals or variants. 8 KB take at most 8 MB, and
//     less than 1 MB for typical literals.
//   - maxFoldedLetters bounds the letters of one literal that have a non-ASCII variant. The number
//     of variants doubles with each such letter.
const (
	maxLiteralDFABytes = 8192
	maxFoldedLetters   = 4
)

// literalMatcher reports whether a line contains any literal of a fixed set.
type literalMatcher interface {
	Contains(line []byte) bool
}

// containsAnyFilter keeps the lines that contain at least one of its literals.
//
// It tests all literals in one pass over the line, so its cost does not grow with the number of
// literals.
//
// It is safe for concurrent use: both matchers only read their tables.
type containsAnyFilter struct {
	// literals holds the literals as the contains filters held them. A case-insensitive literal
	// is lowercase.
	literals        [][]byte
	caseInsensitive bool
	matcher         literalMatcher
}

// newCaseInsensitiveContainsAnyFilter returns a filter that keeps the same lines as an OR of
// case-insensitive contains filters for literals. Each literal must be non-empty lowercase ASCII.
func newCaseInsensitiveContainsAnyFilter(literals [][]byte) containsAnyFilter {
	var variants [][]byte
	for _, literal := range literals {
		variants = appendContainsLowerVariants(variants, literal)
	}
	return containsAnyFilter{
		literals:        literals,
		caseInsensitive: true,
		matcher:         newLiteralDFA(variants, true),
	}
}

func (f containsAnyFilter) Filter(line []byte) bool {
	return f.matcher.Contains(line)
}

func (f containsAnyFilter) ToStage() Stage {
	return NewStageFunc(nil, lineFilterHints(), func(_ int64, line []byte, _ *LabelsBuilder) ([]byte, bool) {
		return line, f.Filter(line)
	})
}

// Matches implements Matcher.
func (f containsAnyFilter) Matches(test Checker) bool {
	for _, literal := range f.literals {
		if test.Test(literal, f.caseInsensitive, false) {
			return true
		}
	}
	return false
}

// appendContainsLowerVariants appends to dst the byte strings that containsLower matches for
// literal, apart from ASCII case. literal must be lowercase ASCII.
//
// containsLower lowers each rune of the line with unicode.ToLower. Only two non-ASCII runes lower
// to ASCII: U+0130 to i and U+212A to k. containsLower compares the first byte of a match without
// decoding it, so these runes match only after the first byte.
func appendContainsLowerVariants(dst [][]byte, literal []byte) [][]byte {
	variants := [][]byte{nil}
	for i, b := range literal {
		alternative := ""
		if i > 0 && b == 'i' {
			alternative = "\u0130"
		} else if i > 0 && b == 'k' {
			alternative = "\u212A"
		}

		next := variants[:0:0]
		for _, variant := range variants {
			next = append(next, append(variant[:len(variant):len(variant)], b))
			if alternative != "" {
				next = append(next, append(variant[:len(variant):len(variant)], alternative...))
			}
		}
		variants = next
	}
	return append(dst, variants...)
}

// foldedLetters returns the number of letters of literal for which appendContainsLowerVariants
// adds a variant.
func foldedLetters(literal []byte) int {
	n := 0
	for i, b := range literal {
		if i > 0 && (b == 'i' || b == 'k') {
			n++
		}
	}
	return n
}

// mergeContainsAny replaces contains filters of the OR tree f with containsAnyFilters, with the
// merger that SetMultiLiteralFilter set. It returns f unchanged when the multi-literal filter is
// disabled.
//
// The result keeps the same lines as f.
func mergeContainsAny(f MatcherFilterer) MatcherFilterer {
	merger := activeMerger.Load()
	if merger == nil {
		return f
	}
	return merger.merge(f)
}

// containsAnyMerger merges the contains filters of an OR tree into containsAnyFilters.
type containsAnyMerger struct {
	caseSensitive caseSensitiveMatcher
}

// merge merges the case-sensitive and the case-insensitive filters of f separately, and only a
// group that passes its threshold and limits. The other legs of the OR stay as they are. It
// returns f unchanged when no group merges.
func (m *containsAnyMerger) merge(f MatcherFilterer) MatcherFilterer {
	if _, ok := f.(orFilter); !ok {
		return f
	}

	var legs orLegs
	legs.collect(f)

	var merged, rest []MatcherFilterer
	if m.caseSensitive.fit(legs.caseSensitive) {
		merged = append(merged, m.caseSensitive.newFilter(matchesOf(legs.caseSensitive)))
		multiLiteralFiltersBuilt.WithLabelValues(m.caseSensitive.name(), "true").Inc()
	} else {
		rest = appendLegs(rest, legs.caseSensitive)
	}
	if fitCaseInsensitive(legs.caseInsensitive) {
		merged = append(merged, newCaseInsensitiveContainsAnyFilter(matchesOf(legs.caseInsensitive)))
		multiLiteralFiltersBuilt.WithLabelValues(CaseSensitiveMatcherDFA, "false").Inc()
	} else {
		rest = appendLegs(rest, legs.caseInsensitive)
	}
	if len(merged) == 0 {
		return f
	}

	// The containsAnyFilters go first. In batch mode the later legs then test only the lines that
	// they dropped.
	result := merged[0]
	for _, leg := range slices.Concat(merged[1:], rest, legs.others) {
		result = newOrFilter(result, leg)
	}
	return result
}

// orLegs holds the legs of an OR tree, split into contains filters that a containsAnyFilter can
// test and other filters.
type orLegs struct {
	caseSensitive []*containsFilter

	// caseInsensitive holds only ASCII literals. A non-ASCII literal needs Unicode case folding,
	// which literalDFA does not do.
	caseInsensitive []*containsFilter

	others []MatcherFilterer
}

func (l *orLegs) collect(f MatcherFilterer) {
	switch f := f.(type) {
	case orFilter:
		l.collect(f.left)
		l.collect(f.right)
	case wrapper:
		// An OR leg must be a MatcherFilterer, so keep the wrapper when its Filterer is not one.
		inner, ok := f.Filterer.(MatcherFilterer)
		if !ok {
			l.others = append(l.others, f)
			return
		}
		l.collect(inner)
	case *containsFilter:
		switch {
		case !f.caseInsensitive:
			l.caseSensitive = append(l.caseSensitive, f)
		case isASCII(f.match):
			l.caseInsensitive = append(l.caseInsensitive, f)
		default:
			l.others = append(l.others, f)
		}
	default:
		// A containsAnyFilter also stays a leg. Merging its literals again would build a second
		// matcher.
		l.others = append(l.others, f)
	}
}

// fitCaseInsensitive reports whether filters are enough to merge and within the size limits.
func fitCaseInsensitive(filters []*containsFilter) bool {
	if len(filters) < minCaseInsensitiveContainsAnyLiterals {
		return false
	}
	total := 0
	for _, f := range filters {
		folded := foldedLetters(f.match)
		if folded > maxFoldedLetters {
			return false
		}
		// A variant replaces a letter with at most 3 bytes.
		total += (len(f.match) + 2*folded) << folded
	}
	return total <= maxLiteralDFABytes
}

func matchesOf(filters []*containsFilter) [][]byte {
	out := make([][]byte, len(filters))
	for i, f := range filters {
		out[i] = f.match
	}
	return out
}

func appendLegs(dst []MatcherFilterer, filters []*containsFilter) []MatcherFilterer {
	for _, f := range filters {
		dst = append(dst, f)
	}
	return dst
}

func isASCII(b []byte) bool {
	for _, c := range b {
		if c >= 0x80 {
			return false
		}
	}
	return true
}

// NewOrFilters returns a filter that keeps the lines that pass any of filters. It returns nil when
// filters is empty.
//
// It tests many contains filters with one pass over the line instead of one pass per filter. As
// with an OR of two filters, a TrueFilter leg is dropped.
func NewOrFilters(filters []Filterer) Filterer {
	var or MatcherFilterer
	for _, f := range filters {
		or = ChainOrMatcherFilterer(or, WrapFilterer(f))
	}
	if or == nil {
		return nil
	}
	return mergeContainsAny(or)
}
