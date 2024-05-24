package logql

import "github.com/grafana/loki/v3/pkg/logql/syntax"

// optimizeSampleExpr Attempt to optimize the SampleExpr to another that will run faster but will produce the same result.
func optimizeSampleExpr(expr syntax.SampleExpr) (syntax.SampleExpr, error) {
	var skip bool
	// we skip sharding AST for now, it's not easy to clone them since they are not part of the language.
	expr.Walk(func(e syntax.Expr) {
		switch e.(type) {
		case *ConcatSampleExpr, DownstreamSampleExpr, *QuantileSketchEvalExpr, *QuantileSketchMergeExpr, *MergeFirstOverTimeExpr, *MergeLastOverTimeExpr:
			skip = true
			return
		}
	})
	if skip {
		return expr, nil
	}
	expr, err := syntax.Clone[syntax.SampleExpr](expr)
	if err != nil {
		return nil, err
	}
	removeLineformat(expr)
	return expr, nil
}

// removeLineformat removes unnecessary line_format within a SampleExpr.
func removeLineformat(expr syntax.SampleExpr) {
	expr.Walk(func(e syntax.Expr) {
		rangeExpr, ok := e.(*syntax.RangeAggregationExpr)
		if !ok {
			return
		}
		// bytes operation count bytes of the log line so line_format changes the result.
		if rangeExpr.Operation == syntax.OpRangeTypeBytes ||
			rangeExpr.Operation == syntax.OpRangeTypeBytesRate {
			return
		}
		pipelineExpr, ok := rangeExpr.Left.Left.(*syntax.PipelineExpr)
		if !ok {
			return
		}
		temp := pipelineExpr.MultiStages[:0]
		for i, s := range pipelineExpr.MultiStages {
			_, ok := s.(*syntax.LineFmtExpr)
			if !ok {
				temp = append(temp, s)
				continue
			}
			// we found a lineFmtExpr, we need to check if it's followed by a labelParser or lineFilter
			// in which case it could be useful for further processing.
			var found bool
			for j := i; j < len(pipelineExpr.MultiStages); j++ {
				if _, ok := pipelineExpr.MultiStages[j].(*syntax.LogfmtParserExpr); ok {
					found = true
					break
				}
				if _, ok := pipelineExpr.MultiStages[j].(*syntax.LabelParserExpr); ok {
					found = true
					break
				}
				if _, ok := pipelineExpr.MultiStages[j].(*syntax.LineFilterExpr); ok {
					found = true
					break
				}
				if _, ok := pipelineExpr.MultiStages[j].(*syntax.JSONExpressionParser); ok {
					found = true
					break
				}
				if _, ok := pipelineExpr.MultiStages[j].(*syntax.LogfmtExpressionParser); ok {
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
			rangeExpr.Left.Left = &syntax.MatchersExpr{Mts: rangeExpr.Left.Left.Matchers()}
		}
	})
}
