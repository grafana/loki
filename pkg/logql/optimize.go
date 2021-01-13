package logql

// optimizeSampleExpr Attempt to optimize the SampleExpr to another that will run faster but will produce the same result.
func optimizeSampleExpr(expr SampleExpr) (SampleExpr, error) {
	var skip bool
	// we skip sharding AST for now, it's not easy to clone them since they are not part of the language.
	walkSampleExpr(expr, func(e SampleExpr) {
		switch e.(type) {
		case *ConcatSampleExpr, *DownstreamSampleExpr:
			skip = true
		}
	})
	if skip {
		return expr, nil
	}
	// clone the expr.
	q := expr.String()
	expr, err := ParseSampleExpr(q)
	if err != nil {
		return nil, err
	}
	removeLineformat(expr)
	return expr, nil
}

// removeLineformat removes unnecessary line_format within a SampleExpr.
func removeLineformat(expr SampleExpr) {
	walkSampleExpr(expr, func(e SampleExpr) {
		rangeExpr, ok := e.(*rangeAggregationExpr)
		if !ok {
			return
		}
		// bytes operation count bytes of the log line so line_format changes the result.
		if rangeExpr.operation == OpRangeTypeBytes ||
			rangeExpr.operation == OpRangeTypeBytesRate {
			return
		}
		pipelineExpr, ok := rangeExpr.left.left.(*pipelineExpr)
		if !ok {
			return
		}
		temp := pipelineExpr.pipeline[:0]
		for i, s := range pipelineExpr.pipeline {
			_, ok := s.(*lineFmtExpr)
			if !ok {
				temp = append(temp, s)
				continue
			}
			// we found a lineFmtExpr, we need to check if it's followed by a labelParser or lineFilter
			// in which case it could be useful for further processing.
			var found bool
			for j := i; j < len(pipelineExpr.pipeline); j++ {
				if _, ok := pipelineExpr.pipeline[j].(*labelParserExpr); ok {
					found = true
					break
				}
				if _, ok := pipelineExpr.pipeline[j].(*lineFilterExpr); ok {
					found = true
					break
				}
			}
			if found {
				// we cannot remove safely the linefmtExpr.
				temp = append(temp, s)
			}
		}
		pipelineExpr.pipeline = temp
		// transform into a matcherExpr if there's no more pipeline.
		if len(pipelineExpr.pipeline) == 0 {
			rangeExpr.left.left = &matchersExpr{
				matchers: rangeExpr.left.left.Matchers(),
			}
		}
	})
}

// walkSampleExpr traverses in depth-first order a SampleExpr.
func walkSampleExpr(expr SampleExpr, visitor func(e SampleExpr)) {
	visitor(expr)
	for _, c := range children(expr) {
		walkSampleExpr(c, visitor)
	}
}

// children returns children node of a SampleExpr.
func children(expr SampleExpr) []SampleExpr {
	switch e := expr.(type) {
	case *rangeAggregationExpr:
		return []SampleExpr{}
	case *vectorAggregationExpr:
		return []SampleExpr{e.left}
	case *binOpExpr:
		return []SampleExpr{e.SampleExpr, e.RHS}
	case *literalExpr:
		return []SampleExpr{}
	case *labelReplaceExpr:
		return []SampleExpr{e.left}
	case *DownstreamSampleExpr:
		return []SampleExpr{e.SampleExpr}
	case *ConcatSampleExpr:
		if e.next != nil {
			return []SampleExpr{e.next}
		}
		return []SampleExpr{}
	default:
		panic("unknown sample expression")
	}
}
