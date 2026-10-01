package log

// literalStage is a line filter stage that only keeps lines containing literal.
type literalStage struct {
	StageFunc
	literal []byte
}

// withRequiredLiteral tags s with the longest case-sensitive literal every line
// kept by f contains, so chunk readers can search for it across many lines at once.
func withRequiredLiteral(f Filterer, s StageFunc) Stage {
	if lit := requiredLiteral(f); len(lit) > 0 {
		return literalStage{StageFunc: s, literal: lit}
	}
	return s
}

// requiredLiteral returns the longest case-sensitive literal every line kept by
// f contains, or nil.
func requiredLiteral(f Filterer) []byte {
	var lit []byte
	longer := func(l []byte) {
		if len(l) > len(lit) {
			lit = l
		}
	}
	switch f := f.(type) {
	case *containsFilter:
		if !f.caseInsensitive {
			lit = f.match
		}
	case containsAllFilter:
		for _, m := range f.matches {
			if !m.caseInsensitive {
				longer(m.match)
			}
		}
	case *containsAllFilter:
		for _, m := range f.matches {
			if !m.caseInsensitive {
				longer(m.match)
			}
		}
	case andFilters:
		for _, sub := range f.filters {
			longer(requiredLiteral(sub))
		}
	case andFilter:
		longer(requiredLiteral(f.left))
		longer(requiredLiteral(f.right))
	}
	return lit
}

// RequiredLiteral returns a case-sensitive literal that every line kept by p
// contains, or nil if p does not start with a line filter that implies one.
// The first stage sees the line as stored, so a reader may skip lines without
// the literal before calling Process.
func RequiredLiteral(p StreamPipeline) []byte {
	sp, ok := p.(*streamPipeline)
	if !ok || len(sp.stages) == 0 {
		return nil
	}
	if s, ok := sp.stages[0].(literalStage); ok {
		return s.literal
	}
	return nil
}

// stagesLiteral returns the literal of stages[0] if it is a literal line filter.
func stagesLiteral(stages []Stage) []byte {
	if len(stages) == 0 {
		return nil
	}
	if s, ok := stages[0].(literalStage); ok {
		return s.literal
	}
	return nil
}

// RequiredSampleLiteral is RequiredLiteral for a sample extractor.
func RequiredSampleLiteral(e StreamSampleExtractor) []byte {
	if l, ok := e.(interface{ requiredLiteral() []byte }); ok {
		return l.requiredLiteral()
	}
	return nil
}
