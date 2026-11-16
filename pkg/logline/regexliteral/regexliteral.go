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
// Needles keeps alternation in an uncompiled needle.Expr. Callers map and
// combine expressions before compiling the final requirement to a bounded
// plan. Literals compiles Needles with a cap of one and returns plain literals.
package regexliteral

import (
	"slices"

	"github.com/grafana/regexp/syntax"

	"github.com/grafana/loki/v3/pkg/logline/needle"
)

const (
	// maxExact bounds the exact sets the analysis builds. A larger set is
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
// contains, sorted. It compiles Needles(pattern) with a DNF cap of one, so
// duplicates and literals contained in another one are removed. Nothing is
// returned for a pattern that does not parse or has no required literal.
//
// Literals keep the case the regexp parser stores. Case-sensitive text is
// returned as written. A case-insensitive literal comes back in the parser's
// canonical form, which is uppercase for ASCII letters.
//
// Callers still decide whether a literal is long enough to look up.
func Literals(pattern string) []string {
	conjs := Needles(pattern).Compile(1)
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
// Exact-set and literal-run enumeration use fixed internal limits, independent
// of the final DNF cap passed to Compile. A basic walk also keeps required
// text that the richer walk joins into alternatives, so it survives a low cap.
// A pattern that does not parse gives True.
//
// Case follows the same rules as Literals, with one addition: runes of a
// small character class are used with ASCII letters uppercased. Callers must
// uppercase ASCII before they look literals up, as every extractor does.
// The result is uncompiled and unpruned. Map and combine it with other
// requirements before calling Compile on the final expression; Compile applies
// the DNF cap and final pruning.
func Needles(pattern string) needle.Expr {
	re, err := parse(pattern)
	if err != nil {
		return needle.True()
	}

	// Analyze both enumeration limits on demand. Emission keeps basic
	// fallback literals without rebuilding exact summaries in either walk.
	a := analyze(re, maxExact)
	basic := a.expr(re, basicMode)
	// A successful basic summary is the same singleton in both modes.
	// Emit it once rather than materializing the same whole literal twice.
	if a.summary(re)[basicMode] != nil {
		return basic
	}
	return needle.And(basic, a.expr(re, richMode))
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

// walker emits requirements from cached analysis. It descends only when a
// node is non-exact in its mode, so exact ancestors never materialize their
// children's standalone requirements. nil run means no pending literal run.
type walker struct {
	analysis *regexAnalysis
	mode     int
	run      textSet
	acc      needle.Expr
}

func (a *regexAnalysis) expr(re *syntax.Regexp, mode int) needle.Expr {
	w := walker{analysis: a, mode: mode}
	w.walk(re)
	w.flush()
	return w.acc
}

func (w *walker) walk(re *syntax.Regexp) {
	if set := w.analysis.summary(re)[w.mode]; set != nil {
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
		if set := w.analysis.summary(re.Sub[0])[w.mode]; set != nil {
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
			branches[i] = w.analysis.expr(sub, w.mode)
		}
		w.acc = needle.And(w.acc, needle.Or(branches...))

	default:
		// ?, * and . over anything that is not exact, large character
		// classes and no-match. They can match more strings than the caps
		// allow, or none, so nothing is required.
		w.flush()
	}
}

// join appends an exact set to the run. When the cross product would be
// larger than maxExact, the run is flushed and a new one starts with set.
func (w *walker) join(set textSet) {
	switch {
	case w.run == nil:
		w.run = set
	case len(w.run)*len(set) > w.analysis.limits[w.mode]:
		w.flush()
		w.run = set
	default:
		w.run = crossText(w.run, set)
	}
}

// flush requires one member of the run. A run that can be empty requires
// nothing.
func (w *walker) flush() {
	if w.run == nil {
		return
	}
	texts := make([]string, len(w.run))
	for i, s := range w.run {
		texts[i] = materializeText(s)
	}
	// Bounded OR absorption depends on input order. Restore lexical order
	// once, after materialization, without sorting growing rope prefixes.
	slices.Sort(texts)
	lits := make([]needle.Expr, len(texts))
	for i, s := range texts {
		lits[i] = needle.Lit(s)
	}
	w.acc = needle.And(w.acc, needle.Or(lits...))
	w.run = nil
}

func upperASCII(r rune) rune {
	if 'a' <= r && r <= 'z' {
		return r - ('a' - 'A')
	}
	return r
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
	set := w.analysis.summary(re.Sub[0])[w.mode]
	if set == nil {
		w.flush()
		w.walk(re.Sub[0])
		w.flush()
		return
	}
	copies, added := 0, 0
	firstBytes := set.firstBytes()
	for copies < re.Min && added < maxRepeatBytes {
		w.join(set)
		copies++
		added += firstBytes
	}
	// When copies were left out or the count can vary, the run's end is not
	// next to the text that follows, so the last copy starts a new run.
	if copies < re.Min || re.Max != re.Min {
		w.flush()
		w.join(set)
	}
}
