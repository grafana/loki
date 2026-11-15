// Package regexliteral finds the literal substrings that every match of a
// regular expression must contain, so they can be looked up in a logline
// index.
//
// The walk is adapted from optimizeConcatRegex in Prometheus
// (model/labels/regexp.go): the literals of a concatenation are required text.
// Prometheus matchers are anchored at both ends, so there the first and last
// literals are a prefix and a suffix. Loki line filters are not anchored, and
// a label filter is anchored to a value that is itself a substring of the
// indexed text. Either way the only property that holds for the indexed text
// is "contains", so every literal found is returned as a plain substring and
// no position is kept.
package regexliteral

import (
	"slices"
	"strings"

	"github.com/grafana/regexp/syntax"
)

// Required returns the literals that every string matched by pattern
// contains. Order follows the pattern. Duplicates and literals contained in
// another one are removed. Nothing is returned for a pattern that does not
// parse or has no required literal.
//
// Literals keep the case the regexp parser stores. Case-sensitive text is
// returned as written. A case-insensitive literal comes back in the parser's
// canonical form, which is uppercase for ASCII letters.
//
// Callers still decide whether a literal is long enough to look up.
func Required(pattern string) []string {
	// Same parse as Loki's parseRegexpFilter, so we see the AST Loki runs.
	re, err := syntax.Parse(pattern, syntax.Perl)
	if err != nil {
		return nil
	}

	var w walker
	w.walk(re.Simplify())
	w.flush()
	if len(w.literals) < 2 {
		return w.literals
	}
	return dropContained(w.literals)
}

// walker collects contiguous literal runs. A run is text that appears verbatim
// in every match. Anything that can match more than one string ends it.
type walker struct {
	run      strings.Builder
	literals []string
}

func (w *walker) walk(re *syntax.Regexp) {
	switch re.Op {
	case syntax.OpLiteral:
		w.literal(re)

	case syntax.OpConcat:
		for _, sub := range re.Sub {
			w.walk(sub)
		}

	case syntax.OpCapture:
		w.walk(re.Sub[0])

	case syntax.OpEmptyMatch,
		syntax.OpBeginLine, syntax.OpEndLine,
		syntax.OpBeginText, syntax.OpEndText,
		syntax.OpWordBoundary, syntax.OpNoWordBoundary:
		// Zero-width, so the text on either side is adjacent in the match and
		// the run continues. Prometheus only strips ^ and $ at the ends
		// because its matchers are anchored. Unanchored, they can appear
		// anywhere and still consume nothing.

	case syntax.OpPlus:
		sub := re.Sub[0]
		for sub.Op == syntax.OpCapture {
			sub = sub.Sub[0]
		}
		if sub.Op == syntax.OpLiteral {
			// x+ starts with one x and ends with one x, so x joins the run on
			// the left, and a new run starting with x joins the right.
			w.literal(sub)
			w.flush()
			w.literal(sub)
			return
		}
		// x contains at least one match, so its own literals are required,
		// but they cannot join the neighbours.
		w.flush()
		w.walk(sub)
		w.flush()

	case syntax.OpRepeat:
		// Simplify rewrites counted repeats, so this only guards against a
		// form it left in place.
		w.flush()
		if re.Min >= 1 {
			w.walk(re.Sub[0])
			w.flush()
		}

	default:
		// Alternation, ?, *, ., character classes and no-match. Any of them
		// can match more than one string, or none, so nothing is required.
		w.flush()
	}
}

func (w *walker) literal(re *syntax.Regexp) {
	// For a case-insensitive literal the parser stores the smallest rune of
	// each fold orbit. That is the uppercase letter for ASCII, and it never
	// turns a non-ASCII-only orbit into ASCII, so the runes can be used as
	// they are.
	//
	// A few non-ASCII runes also match an ASCII letter under (?i), for example
	// U+017F for s and U+212A for k. The parser stores those orbits as ASCII
	// S and K, so lines that spell a letter that way are missed. We accept
	// that to keep needles whole.
	for _, r := range re.Rune {
		w.run.WriteRune(r)
	}
}

func (w *walker) flush() {
	if w.run.Len() == 0 {
		return
	}
	lit := w.run.String()
	w.run.Reset()
	if !slices.Contains(w.literals, lit) {
		w.literals = append(w.literals, lit)
	}
}

// dropContained removes literals contained in a longer one. Every n-gram of
// the shorter literal is an n-gram of the longer, so it narrows nothing.
func dropContained(literals []string) []string {
	out := make([]string, 0, len(literals))
	for i, lit := range literals {
		contained := false
		for j, other := range literals {
			if i != j && len(other) > len(lit) && strings.Contains(other, lit) {
				contained = true
				break
			}
		}
		if !contained {
			out = append(out, lit)
		}
	}
	return out
}
