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
//
// Needles keeps alternation and returns a needle.Expr, an OR of literal
// sets. Literals is Needles with one conjunction, returned as plain literals.
package regexliteral

import (
	"slices"

	"github.com/grafana/regexp/syntax"

	"github.com/grafana/loki/v3/pkg/logline/needle"
)

const (
	// maxExact bounds the exact sets the walk builds. A larger set is
	// treated as not exact, and a run that would cross past it is split.
	maxExact = 16
	// maxClass bounds the runes of a character class that is used as an
	// exact set, after ASCII letters are uppercased and deduplicated.
	maxClass = 4
	// maxRepeatBytes bounds the text a counted repeat adds to a run. Past it
	// the remaining copies are left out, which only drops precision. The
	// parser allows x{1000}, and needles that long do not narrow more.
	maxRepeatBytes = 256
)

// Literals returns the literals that every string matched by pattern
// contains, sorted. It is Needles with maxConjunctions 1, pruned, so duplicates and
// literals contained in another one are removed. Nothing is returned for a
// pattern that does not parse or has no required literal.
//
// Literals keep the case the regexp parser stores. Case-sensitive text is
// returned as written. A case-insensitive literal comes back in the parser's
// canonical form, which is uppercase for ASCII letters.
//
// Callers still decide whether a literal is long enough to look up.
func Literals(pattern string) []string {
	conjs := Needles(pattern, 1).Prune().Conjunctions()
	if len(conjs) == 0 {
		return nil
	}
	return conjs[0]
}

// Needles returns a requirement that every string matched by pattern
// satisfies. It keeps alternation. `foo|bar` gives {foo} or {bar}, and small
// finite alternations are joined to the literals next to them, so
// `(GET|POST) /api` gives {GET /api} or {POST /api}.
//
// The result has at most maxConjunctions conjunctions. A pattern that does
// not parse gives True. With maxConjunctions 1 the result is one conjunction,
// the literals of Literals before pruning.
//
// Case follows the same rules as Literals, with one addition: runes of a
// small character class are used with ASCII letters uppercased. Callers must
// uppercase ASCII before they look literals up, as every extractor does.
//
// The result is not pruned. Call Prune on the final requirement.
func Needles(pattern string, maxConjunctions int) needle.Expr {
	re, err := parse(pattern)
	if err != nil {
		return needle.True()
	}
	return newWalker(maxConjunctions).expr(re)
}

// parse runs the same parse as Loki's parseRegexpFilter. It does not call
// Simplify. Simplify expands counted repeats into large trees, which costs
// time and gives the walker nothing it cannot handle itself (see repeat).
// Prometheus dropped it for the same reason (prometheus/prometheus c743b2f).
func parse(pattern string) (*syntax.Regexp, error) {
	re, err := syntax.Parse(pattern, syntax.Perl)
	if err != nil {
		return nil, err
	}
	return re, nil
}

// walkConfig is shared by a walker and the walkers it starts for the
// branches of an alternation.
type walkConfig struct {
	maxConjunctions int
	// maxExact and maxClass are the caps, lowered to maxConjunctions. A run
	// never has more strings than maxConjunctions, so flushing it never
	// collapses, and with maxConjunctions 1 every run is a single literal.
	maxExact int
	maxClass int
}

// walker collects contiguous literal runs. A run is text that appears in
// every match, held as the set of exact strings it can be. Anything that can
// match more strings than the caps allow ends it.
type walker struct {
	*walkConfig
	// run is the set of exact alternatives for the current literal run. nil
	// means there is no run.
	run []string
	acc needle.Expr
}

func newWalker(maxConjunctions int) *walker {
	maxConjunctions = max(maxConjunctions, 1)
	return &walker{walkConfig: &walkConfig{
		maxConjunctions: maxConjunctions,
		maxExact:        min(maxExact, maxConjunctions),
		maxClass:        min(maxClass, maxConjunctions),
	}}
}

// expr walks re and returns its requirement.
func (w *walker) expr(re *syntax.Regexp) needle.Expr {
	w.walk(re)
	w.flush()
	return w.acc
}

func (w *walker) walk(re *syntax.Regexp) {
	if set, ok := w.exact(re); ok {
		w.join(set)
		return
	}

	switch re.Op {
	case syntax.OpConcat:
		for _, sub := range re.Sub {
			w.walk(sub)
		}

	case syntax.OpCapture:
		w.walk(re.Sub[0])

	case syntax.OpPlus:
		if set, ok := w.exact(re.Sub[0]); ok {
			// The first and the last repetition are each one member of the
			// set, so the set joins the run on the left, and a new run
			// starting with the set joins the right.
			w.join(set)
			w.flush()
			w.join(set)
			return
		}
		// x contains at least one match, so its own literals are required,
		// but they cannot join the neighbours.
		w.flush()
		w.walk(re.Sub[0])
		w.flush()

	case syntax.OpRepeat:
		w.repeat(re)

	case syntax.OpAlternate:
		// Too many strings to join to the neighbours. Each branch is walked
		// on its own, and a match satisfies at least one of them.
		w.flush()
		branches := make([]needle.Expr, len(re.Sub))
		for i, sub := range re.Sub {
			b := walker{walkConfig: w.walkConfig}
			branches[i] = b.expr(sub)
		}
		w.acc = needle.And(w.maxConjunctions, w.acc, needle.Or(w.maxConjunctions, branches...))

	default:
		// ?, * and . over anything that is not exact, large character
		// classes and no-match. They can match more strings than the caps
		// allow, or none, so nothing is required.
		w.flush()
	}
}

// join appends an exact set to the run. When the cross product would be
// larger than maxExact, the run is flushed and a new one starts with set.
func (w *walker) join(set []string) {
	switch {
	case w.run == nil:
		w.run = set
	case len(w.run)*len(set) > w.maxExact:
		w.flush()
		w.run = set
	default:
		w.run = cross(w.run, set)
	}
}

// flush requires one member of the run. A run that can be empty requires
// nothing.
func (w *walker) flush() {
	if w.run == nil {
		return
	}
	lits := make([]needle.Expr, len(w.run))
	for i, s := range w.run {
		lits[i] = needle.Lit(s)
	}
	w.acc = needle.And(w.maxConjunctions, w.acc, needle.Or(w.maxConjunctions, lits...))
	w.run = nil
}

// exact returns the set of strings re matches, sorted and deduplicated, when
// it has at most maxExact members. Zero-width assertions match the empty
// string. The set is never empty.
func (w *walker) exact(re *syntax.Regexp) ([]string, bool) {
	switch re.Op {
	case syntax.OpLiteral:
		// For a case-insensitive literal the parser stores the smallest rune
		// of each fold orbit. That is the uppercase letter for ASCII, and it
		// never turns a non-ASCII-only orbit into ASCII, so the runes can be
		// used as they are.
		//
		// A few non-ASCII runes also match an ASCII letter under (?i), for
		// example U+017F for s and U+212A for k. The parser stores those
		// orbits as ASCII S and K, so lines that spell a letter that way are
		// missed. We accept that to keep needles whole.
		return []string{string(re.Rune)}, true

	case syntax.OpEmptyMatch,
		syntax.OpBeginLine, syntax.OpEndLine,
		syntax.OpBeginText, syntax.OpEndText,
		syntax.OpWordBoundary, syntax.OpNoWordBoundary:
		// Zero-width, so the text on either side is adjacent in the match and
		// the run continues. Prometheus only strips ^ and $ at the ends
		// because its matchers are anchored. Unanchored, they can appear
		// anywhere and still consume nothing.
		return []string{""}, true

	case syntax.OpCapture:
		return w.exact(re.Sub[0])

	case syntax.OpConcat:
		set := []string{""}
		for _, sub := range re.Sub {
			s, ok := w.exact(sub)
			if !ok || len(set)*len(s) > w.maxExact {
				return nil, false
			}
			set = cross(set, s)
		}
		return set, true

	case syntax.OpAlternate:
		var set []string
		for _, sub := range re.Sub {
			s, ok := w.exact(sub)
			if !ok {
				return nil, false
			}
			set = sortedSet(append(set, s...))
			if len(set) > w.maxExact {
				return nil, false
			}
		}
		return set, true

	case syntax.OpQuest:
		s, ok := w.exact(re.Sub[0])
		if !ok {
			return nil, false
		}
		set := sortedSet(append(slices.Clone(s), ""))
		if len(set) > w.maxExact {
			return nil, false
		}
		return set, true

	case syntax.OpRepeat:
		// Only a fixed count is a finite set. x{0,1} is x?.
		if re.Min == 0 && re.Max == 1 {
			return w.exact(&syntax.Regexp{Op: syntax.OpQuest, Sub: re.Sub})
		}
		if re.Min != re.Max {
			return nil, false
		}
		s, ok := w.exact(re.Sub[0])
		if !ok {
			return nil, false
		}
		set := []string{""}
		for range re.Min {
			if len(set)*len(s) > w.maxExact || len(set[0]) > maxRepeatBytes {
				return nil, false
			}
			set = cross(set, s)
		}
		return set, true

	case syntax.OpCharClass:
		return w.classSet(re)
	}
	return nil, false
}

// classSet returns the runes of a small character class as strings, with
// ASCII letters uppercased. Parse turns `500|503` into `50[03]`, and a case
// insensitive letter into a class of its fold orbit, so small classes are
// common.
func (w *walker) classSet(re *syntax.Regexp) ([]string, bool) {
	// Uppercasing can at most halve the count, so a class with more than
	// twice maxClass runes cannot fit.
	n := 0
	for i := 0; i < len(re.Rune); i += 2 {
		n += int(re.Rune[i+1]-re.Rune[i]) + 1
		if n > 2*w.maxClass {
			return nil, false
		}
	}
	if n == 0 {
		return nil, false
	}

	set := make([]string, 0, n)
	for i := 0; i < len(re.Rune); i += 2 {
		for r := re.Rune[i]; r <= re.Rune[i+1]; r++ {
			set = append(set, string(upperASCII(r)))
		}
	}
	set = sortedSet(set)
	if len(set) > w.maxClass {
		return nil, false
	}
	return set, true
}

func upperASCII(r rune) rune {
	if 'a' <= r && r <= 'z' {
		return r - ('a' - 'A')
	}
	return r
}

// cross returns every concatenation of a member of a and a member of b.
func cross(a, b []string) []string {
	out := make([]string, 0, len(a)*len(b))
	for _, x := range a {
		for _, y := range b {
			out = append(out, x+y)
		}
	}
	return sortedSet(out)
}

func sortedSet(s []string) []string {
	slices.Sort(s)
	return slices.Compact(s)
}

// repeat walks x{min,max}. max is -1 when there is no upper bound. A repeat
// that can match zero times requires nothing. Otherwise the first min copies
// join the left like a concatenation, up to maxRepeatBytes. When the count
// can vary or copies were left out, the last copy is also one member of the
// set and joins the right, the same as OpPlus.
func (w *walker) repeat(re *syntax.Regexp) {
	if re.Min < 1 {
		w.flush()
		return
	}
	set, ok := w.exact(re.Sub[0])
	if !ok {
		w.flush()
		w.walk(re.Sub[0])
		w.flush()
		return
	}
	copies, added := 0, 0
	for copies < re.Min && added < maxRepeatBytes {
		w.join(set)
		copies++
		added += len(set[0])
	}
	// When copies were left out or the count can vary, the run's end is not
	// next to the text that follows, so the last copy starts a new run.
	if copies < re.Min || re.Max != re.Min {
		w.flush()
		w.join(set)
	}
}
