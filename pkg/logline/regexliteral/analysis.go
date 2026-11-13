package regexliteral

import "github.com/grafana/regexp/syntax"

const (
	basicMode = iota
	richMode
)

// regexAnalysis stores exact sets, not materialized requirements. nil means
// non-exact; {nil} is the exact set containing the empty string. Keeping both
// modes on the same AST shares demand-driven bottom-up analysis. Each node's
// pair is computed once, including failures, and emission reuses cached results.
type regexAnalysis struct {
	exact  map[*syntax.Regexp][2]textSet
	limits [2]int
}

// richLimit is normally maxExact; smaller limits are useful for testing the
// product-before-dedup boundary independently of the public DNF cap.
func analyze(re *syntax.Regexp, richLimit int) *regexAnalysis {
	a := &regexAnalysis{
		exact:  make(map[*syntax.Regexp][2]textSet),
		limits: [2]int{1, min(maxExact, max(1, richLimit))},
	}
	a.summary(re)
	return a
}

func (a *regexAnalysis) summary(re *syntax.Regexp) [2]textSet {
	if sets, ok := a.exact[re]; ok {
		return sets
	}
	basic := a.exactNode(re, basicMode)
	rich := basic
	if basic == nil {
		rich = a.exactNode(re, richMode)
	}
	// A successful cap-one exact set denotes the same singleton at any
	// larger cap. Share its immutable rope and slice, but never derive a
	// basic success from a rich singleton: an intermediate basic step may
	// have failed (even when a later zero-count repeat matches only "").
	sets := [2]textSet{basic, rich}
	a.exact[re] = sets
	return sets
}

// exactNode requests child summaries only when needed, preserving the old
// short-circuit traversal. This matters for an optional body whose first child
// is non-exact: no fallback emission needs the rest of that body either.
// Each mode retains its own intermediate checks and sticky failures.
func (a *regexAnalysis) exactNode(re *syntax.Regexp, mode int) textSet {
	limit := a.limits[mode]
	switch re.Op {
	case syntax.OpLiteral:
		// Preserve the parser's case-fold canonical form. As before, a few
		// non-ASCII spellings of ASCII fold orbits are deliberately missed;
		// see AGENTS.md and the existing no-false-negatives test exclusions.
		return textSet{textLeaf(string(re.Rune))}

	case syntax.OpEmptyMatch,
		syntax.OpBeginLine, syntax.OpEndLine,
		syntax.OpBeginText, syntax.OpEndText,
		syntax.OpWordBoundary, syntax.OpNoWordBoundary:
		return textSet{nil}

	case syntax.OpCapture:
		return a.summary(re.Sub[0])[mode]

	case syntax.OpConcat:
		set := textSet{nil}
		for _, sub := range re.Sub {
			s := a.summary(sub)[mode]
			if s == nil || len(set)*len(s) > limit {
				return nil
			}
			set = crossText(set, s)
		}
		return set

	case syntax.OpAlternate:
		var set textSet
		for _, sub := range re.Sub {
			s := a.summary(sub)[mode]
			if s == nil {
				return nil
			}
			set = unionText(set, s)
			if len(set) > limit {
				return nil
			}
		}
		return set

	case syntax.OpQuest:
		return optionalExact(a.summary(re.Sub[0])[mode], limit)

	case syntax.OpRepeat:
		if re.Min == 0 && re.Max == 1 {
			return optionalExact(a.summary(re.Sub[0])[mode], limit)
		}
		if re.Min != re.Max {
			return nil
		}
		s := a.summary(re.Sub[0])[mode]
		if s == nil {
			return nil
		}
		if len(s) == 1 {
			// Check before the final copy, as in P2. A zero count still
			// requires an exact child; do not bypass an earlier failure.
			if n := textBytes(s[0]); re.Min > 0 && n > 0 && re.Min-1 > maxRepeatBytes/n {
				return nil
			}
			return textSet{repeatText(s[0], re.Min)}
		}
		set := textSet{nil}
		for range re.Min {
			if len(set)*len(s) > limit || set.firstBytes() > maxRepeatBytes {
				return nil
			}
			set = crossText(set, s)
		}
		return set

	case syntax.OpCharClass:
		return classExact(re, min(maxClass, limit))
	}
	return nil
}

func optionalExact(s textSet, limit int) textSet {
	if s == nil {
		return nil
	}
	set := unionText(s, textSet{nil})
	if len(set) > limit {
		return nil
	}
	return set
}

// classExact keeps P2's raw-rune check before ASCII uppercasing and dedup.
// The basic and rich class caps remain independent of the final DNF cap.
func classExact(re *syntax.Regexp, limit int) textSet {
	n := 0
	for i := 0; i < len(re.Rune); i += 2 {
		n += int(re.Rune[i+1]-re.Rune[i]) + 1
		if n > 2*limit {
			return nil
		}
	}
	if n == 0 {
		return nil
	}
	set := make(textSet, 0, n)
	for i := 0; i < len(re.Rune); i += 2 {
		for r := re.Rune[i]; r <= re.Rune[i+1]; r++ {
			set = set.with(textLeaf(string(upperASCII(r))))
		}
	}
	if len(set) > limit {
		return nil
	}
	return set
}
