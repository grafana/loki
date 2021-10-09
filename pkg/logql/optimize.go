package logql

// optimizeSampleExpr Attempt to optimize the SampleExpr to another that will run faster but will produce the same result.
func optimizeSampleExpr(expr SampleExpr) (SampleExpr, error) {
	var skip bool
	// we skip sharding AST for now, it's not easy to clone them since they are not part of the language.
	expr.Walk(func(e interface{}) {
		switch e.(type) {
		case *ConcatSampleExpr, *DownstreamSampleExpr:
			skip = true
			return
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
	expr.Walk(func(e interface{}) {
		rangeExpr, ok := e.(*RangeAggregationExpr)
		if !ok {
			return
		}
		// bytes operation count bytes of the log line so line_format changes the result.
		if rangeExpr.Operation == OpRangeTypeBytes ||
			rangeExpr.Operation == OpRangeTypeBytesRate {
			return
		}
		pipelineExpr, ok := rangeExpr.Left.Left.(*PipelineExpr)
		if !ok {
			return
		}
		temp := pipelineExpr.MultiStages[:0]
		for i, s := range pipelineExpr.MultiStages {
			_, ok := s.(*LineFmtExpr)
			if !ok {
				temp = append(temp, s)
				continue
			}
			// we found a lineFmtExpr, we need to check if it's followed by a labelParser or lineFilter
			// in which case it could be useful for further processing.
			var found bool
			for j := i; j < len(pipelineExpr.MultiStages); j++ {
				if _, ok := pipelineExpr.MultiStages[j].(*LabelParserExpr); ok {
					found = true
					break
				}
				if _, ok := pipelineExpr.MultiStages[j].(*LineFilterExpr); ok {
					found = true
					break
				}
			}
			if found {
				// we cannot remove safely the linefmtExpr.
				temp = append(temp, s)
			}
		}
		pipelineExpr.MultiStages = temp
		// transform into a matcherExpr if there's no more pipeline.
		if len(pipelineExpr.MultiStages) == 0 {
			rangeExpr.Left.Left = &MatchersExpr{
				matchers: rangeExpr.Left.Left.Matchers(),
			}
		}
	})
}
