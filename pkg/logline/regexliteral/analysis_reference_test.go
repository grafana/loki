// Frozen P2 walker from 072f4aa2ff: a test-only oracle for exact compiled-output parity.
// Keep its recursive exact attempts independent of the production analysis.
package regexliteral

import (
	"slices"

	"github.com/grafana/regexp/syntax"

	"github.com/grafana/loki/v3/pkg/logline/needle"
)

// referenceWalkConfig is shared by a referenceWalker and the referenceWalkers it starts for the
// branches of an alternation.
type referenceWalkConfig struct {
	// maxExact and maxClass bound enumeration, independently of the final
	// DNF cap in Compile. With an enumeration cap of one, every run is a
	// single literal. Flushing a run never compiles or collapses it.
	maxExact int
	maxClass int
}

// referenceWalker collects contiguous literal runs. A run is text that appears in
// every match, held as the set of exact strings it can be. Anything that can
// match more strings than the caps allow ends it.
type referenceWalker struct {
	*referenceWalkConfig
	// run is the set of exact alternatives for the current literal run. nil
	// means there is no run.
	run textSet
	acc needle.Expr
}

func newReferenceWalker(maxAlternatives int) *referenceWalker {
	maxAlternatives = max(maxAlternatives, 1)
	return &referenceWalker{referenceWalkConfig: &referenceWalkConfig{
		maxExact: min(maxExact, maxAlternatives),
		maxClass: min(maxClass, maxAlternatives),
	}}
}

// expr walks re and returns its requirement.
func (w *referenceWalker) expr(re *syntax.Regexp) needle.Expr {
	w.walk(re)
	w.flush()
	return w.acc
}

func (w *referenceWalker) walk(re *syntax.Regexp) {
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
			b := referenceWalker{referenceWalkConfig: w.referenceWalkConfig}
			branches[i] = b.expr(sub)
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
func (w *referenceWalker) join(set textSet) {
	switch {
	case w.run == nil:
		w.run = set
	case len(w.run)*len(set) > w.maxExact:
		w.flush()
		w.run = set
	default:
		w.run = crossText(w.run, set)
	}
}

// flush requires one member of the run. A run that can be empty requires
// nothing.
func (w *referenceWalker) flush() {
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

// exact returns the deferred, deduplicated set of strings re matches when
// it has at most maxExact members. Zero-width assertions match the empty
// string. The set is never empty.
func (w *referenceWalker) exact(re *syntax.Regexp) (textSet, bool) {
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
		return textSet{textLeaf(string(re.Rune))}, true

	case syntax.OpEmptyMatch,
		syntax.OpBeginLine, syntax.OpEndLine,
		syntax.OpBeginText, syntax.OpEndText,
		syntax.OpWordBoundary, syntax.OpNoWordBoundary:
		// Zero-width, so the text on either side is adjacent in the match and
		// the run continues. Prometheus only strips ^ and $ at the ends
		// because its matchers are anchored. Unanchored, they can appear
		// anywhere and still consume nothing.
		return textSet{nil}, true

	case syntax.OpCapture:
		return w.exact(re.Sub[0])

	case syntax.OpConcat:
		set := textSet{nil}
		for _, sub := range re.Sub {
			s, ok := w.exact(sub)
			if !ok || len(set)*len(s) > w.maxExact {
				return nil, false
			}
			set = crossText(set, s)
		}
		return set, true

	case syntax.OpAlternate:
		var set textSet
		for _, sub := range re.Sub {
			s, ok := w.exact(sub)
			if !ok {
				return nil, false
			}
			set = unionText(set, s)
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
		set := unionText(s, textSet{nil})
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
		if len(s) == 1 {
			// The old loop checks the accumulated byte length before each
			// copy, so the last copy may cross maxRepeatBytes. Share repeated
			// subtrees without changing that boundary (or truncating text).
			if n := textBytes(s[0]); re.Min > 0 && n > 0 && re.Min-1 > maxRepeatBytes/n {
				return nil, false
			}
			return textSet{repeatText(s[0], re.Min)}, true
		}
		set := textSet{nil}
		for range re.Min {
			if len(set)*len(s) > w.maxExact || set.firstBytes() > maxRepeatBytes {
				return nil, false
			}
			set = crossText(set, s)
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
func (w *referenceWalker) classSet(re *syntax.Regexp) (textSet, bool) {
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

	set := make(textSet, 0, n)
	for i := 0; i < len(re.Rune); i += 2 {
		for r := re.Rune[i]; r <= re.Rune[i+1]; r++ {
			set = set.with(textLeaf(string(upperASCII(r))))
		}
	}
	if len(set) > w.maxClass {
		return nil, false
	}
	return set, true
}

// repeat walks x{min,max}. max is -1 when there is no upper bound. A repeat
// that can match zero times requires nothing. Otherwise the first min copies
// join the left like a concatenation, up to maxRepeatBytes. When the count
// can vary or copies were left out, the last copy is also one member of the
// set and joins the right, the same as OpPlus.
func (w *referenceWalker) repeat(re *syntax.Regexp) {
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
