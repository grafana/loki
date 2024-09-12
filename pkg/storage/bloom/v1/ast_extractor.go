package v1

import (
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/logql/log"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
)

// LabelMatcher represents bloom tests for key-value pairs, mapped from
// LabelFilterExprs from the AST.
type LabelMatcher interface{ isLabelMatcher() }

// UnsupportedLabelMatcher represents a label matcher which could not be
// mapped. Bloom tests for UnsupportedLabelMatchers must always pass.
type UnsupportedLabelMatcher struct{}

// PlainLabelMatcher represents a direct key-value matcher. Bloom tests
// must only pass if the key-value pair exists in the bloom.
type PlainLabelMatcher struct{ Key, Value string }

// OrLabelMatcher represents a logical OR test. Bloom tests must only pass if
// one of the Left or Right label matcher bloom tests pass.
type OrLabelMatcher struct{ Left, Right LabelMatcher }

// AndLabelMatcher represents a logical AND test. Bloom tests must only pass
// if both of the Left and Right label matcher bloom tests pass.
type AndLabelMatcher struct{ Left, Right LabelMatcher }

// ExtractTestableLabelMatchers extracts label matchers from the label filters
// in an expression. The resulting label matchers can then be used for testing
// against bloom filters. Only label matchers before the first parse stage are
// included.
//
// Unsupported LabelFilterExprs map to an UnsupportedLabelMatcher, for which
// bloom tests should always pass.
func ExtractTestableLabelMatchers(expr syntax.Expr) []LabelMatcher {
	var (
		exprs           []*syntax.LabelFilterExpr
		foundParseStage bool
	)

	visitor := &syntax.DepthFirstTraversal{
		VisitLabelFilterFn: func(v syntax.RootVisitor, e *syntax.LabelFilterExpr) {
			if !foundParseStage {
				exprs = append(exprs, e)
			}
		},

		// TODO(rfratto): Find a way to generically represent or test for an
		// expression that modifies extracted labels (parsers, keep, drop, etc.).
		//
		// As the AST is now, we can't prove at compile time that the list of
		// visitors below is complete. For example, if a new parser stage
		// expression is added without updating this list, blooms can silently
		// misbehave.

		VisitLogfmtParserFn:           func(v syntax.RootVisitor, e *syntax.LogfmtParserExpr) { foundParseStage = true },
		VisitLabelParserFn:            func(v syntax.RootVisitor, e *syntax.LabelParserExpr) { foundParseStage = true },
		VisitJSONExpressionParserFn:   func(v syntax.RootVisitor, e *syntax.JSONExpressionParser) { foundParseStage = true },
		VisitLogfmtExpressionParserFn: func(v syntax.RootVisitor, e *syntax.LogfmtExpressionParser) { foundParseStage = true },
		VisitLabelFmtFn:               func(v syntax.RootVisitor, e *syntax.LabelFmtExpr) { foundParseStage = true },
		VisitKeepLabelFn:              func(v syntax.RootVisitor, e *syntax.KeepLabelsExpr) { foundParseStage = true },
		VisitDropLabelsFn:             func(v syntax.RootVisitor, e *syntax.DropLabelsExpr) { foundParseStage = true },
	}
	expr.Accept(visitor)

	return buildLabelMatchers(exprs)
}

func buildLabelMatchers(exprs []*syntax.LabelFilterExpr) []LabelMatcher {
	matchers := make([]LabelMatcher, 0, len(exprs))
	for _, expr := range exprs {
		matchers = append(matchers, buildLabelMatcher(expr.LabelFilterer))
	}
	return matchers
}

func buildLabelMatcher(filter log.LabelFilterer) LabelMatcher {
	switch filter := filter.(type) {

	case *log.LineFilterLabelFilter:
		if filter.Type != labels.MatchEqual {
			return UnsupportedLabelMatcher{}
		}

		return PlainLabelMatcher{
			Key:   filter.Name,
			Value: filter.Value,
		}

	case *log.StringLabelFilter:
		if filter.Type != labels.MatchEqual {
			return UnsupportedLabelMatcher{}
		}

		return PlainLabelMatcher{
			Key:   filter.Name,
			Value: filter.Value,
		}

	case *log.BinaryLabelFilter:
		var (
			left  = buildLabelMatcher(filter.Left)
			right = buildLabelMatcher(filter.Right)
		)

		if filter.And {
			return AndLabelMatcher{Left: left, Right: right}
		}
		return OrLabelMatcher{Left: left, Right: right}

	default:
		return UnsupportedLabelMatcher{}
	}
}

//
// Implement marker types:
//

func (UnsupportedLabelMatcher) isLabelMatcher() {}
func (PlainLabelMatcher) isLabelMatcher()       {}
func (OrLabelMatcher) isLabelMatcher()          {}
func (AndLabelMatcher) isLabelMatcher()         {}
