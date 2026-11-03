package hintprovider

import (
	"github.com/grafana/loki/v3/pkg/logql/syntax"
)

func collectLineFilters(expr syntax.Expr) []*syntax.LineFilterExpr {
	if expr == nil {
		return nil
	}

	var out []*syntax.LineFilterExpr
	visitor := &syntax.DepthFirstTraversal{
		VisitPipelineFn: func(_ syntax.RootVisitor, pipe *syntax.PipelineExpr) {
			out = append(out, lineFiltersFromOriginalLine(pipe.MultiStages)...)
		},
	}
	expr.Accept(visitor)
	return out
}

// lineFiltersFromOriginalLine collects |= / |~ stages that still run on the
// ingested line. line_format, decolorize, and unpack rewrite it; filters after
// them are not safe n-gram lookups.
func lineFiltersFromOriginalLine(stages syntax.MultiStageExpr) []*syntax.LineFilterExpr {
	original := true
	var out []*syntax.LineFilterExpr
	appendFilter := func(expr *syntax.LineFilterExpr) {
		if expr == nil {
			return
		}
		expr.Walk(func(node syntax.Expr) bool {
			if f, ok := node.(*syntax.LineFilterExpr); ok {
				out = append(out, f)
			}
			return true
		})
	}

	for _, stage := range stages {
		switch s := stage.(type) {
		case *syntax.LineFilterExpr:
			if original {
				appendFilter(s)
			}

		case *syntax.LineFmtExpr, *syntax.DecolorizeExpr:
			original = false

		case *syntax.LineParserExpr:
			if s != nil && s.Op == syntax.OpParserTypeUnpack {
				original = false
			}

		case *syntax.LabelFilterExpr, *syntax.LogfmtParserExpr, *syntax.JSONExpressionParserExpr,
			*syntax.LogfmtExpressionParserExpr, *syntax.LabelFmtExpr, *syntax.KeepLabelsExpr, *syntax.DropLabelsExpr:
			// Labels change; the ingested line does not.

		default:
			original = false
		}
	}
	return out
}
