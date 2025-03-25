package engine

import "github.com/grafana/loki/v3/pkg/logql/syntax"

// canExecuteWithNewEngine determines whether a query can be executed by the new execution engine.
func canExecuteWithNewEngine(expr syntax.Expr) bool {
	switch expr := expr.(type) {
	case syntax.SampleExpr:
		return false
	case syntax.LogSelectorExpr:
		ret := true
		expr.Walk(func(e syntax.Expr) {
			switch e.(type) {
			case *syntax.LineParserExpr, *syntax.LogfmtParserExpr, *syntax.LogfmtExpressionParserExpr, *syntax.JSONExpressionParserExpr:
				ret = false
			case *syntax.LineFmtExpr, *syntax.LabelFmtExpr:
				ret = false
			case *syntax.KeepLabelsExpr, *syntax.DropLabelsExpr:
				ret = false
			}
		})
		return ret
	}
	return false
}
