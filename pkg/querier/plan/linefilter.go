package plan

import (
	"github.com/grafana/loki/pkg/logql/syntax"
)

type LineFilterExtractor struct {
	Filters []syntax.LineFilter
}

// VisitBinOp implements syntax.RootVisitor.
func (e *LineFilterExtractor) VisitBinOp(v *syntax.BinOpExpr) {
	v.SampleExpr.Accept(e)
	v.RHS.Accept(e)
}

// VisitDecolorize implements syntax.RootVisitor.
func (*LineFilterExtractor) VisitDecolorize(*syntax.DecolorizeExpr) {
}

// VisitDropLabels implements syntax.RootVisitor.
func (*LineFilterExtractor) VisitDropLabels(*syntax.DropLabelsExpr) {
}

// VisitJSONExpressionParser implements syntax.RootVisitor.
func (*LineFilterExtractor) VisitJSONExpressionParser(*syntax.JSONExpressionParser) {
}

// VisitKeepLabel implements syntax.RootVisitor.
func (*LineFilterExtractor) VisitKeepLabel(*syntax.KeepLabelsExpr) {
}

// VisitLabelFilter implements syntax.RootVisitor.
func (*LineFilterExtractor) VisitLabelFilter(*syntax.LabelFilterExpr) {
}

// VisitLabelFmt implements syntax.RootVisitor.
func (*LineFilterExtractor) VisitLabelFmt(*syntax.LabelFmtExpr) {
}

// VisitLabelParser implements syntax.RootVisitor.
func (*LineFilterExtractor) VisitLabelParser(*syntax.LabelParserExpr) {
}

// VisitLabelReplace implements syntax.RootVisitor.
func (*LineFilterExtractor) VisitLabelReplace(*syntax.LabelReplaceExpr) {
}

// VisitLineFilter implements syntax.RootVisitor.
func (e *LineFilterExtractor) VisitLineFilter(v *syntax.LineFilterExpr) {
	if v.Or != nil {
		v.Or.Accept(e)
	}
	if v.Left != nil {
		v.Left.Accept(e)
	}
	e.Filters = append(e.Filters, v.LineFilter)
}

// VisitLineFmt implements syntax.RootVisitor.
func (*LineFilterExtractor) VisitLineFmt(*syntax.LineFmtExpr) {
}

// VisitLiteral implements syntax.RootVisitor.
func (*LineFilterExtractor) VisitLiteral(*syntax.LiteralExpr) {
}

// VisitLogRange implements syntax.RootVisitor.
func (e *LineFilterExtractor) VisitLogRange(v *syntax.LogRange) {
	v.Left.Accept(e)
}

// VisitLogfmtExpressionParser implements syntax.RootVisitor.
func (*LineFilterExtractor) VisitLogfmtExpressionParser(*syntax.LogfmtExpressionParser) {
}

// VisitLogfmtParser implements syntax.RootVisitor.
func (*LineFilterExtractor) VisitLogfmtParser(*syntax.LogfmtParserExpr) {
}

// VisitMatchers implements syntax.RootVisitor.
func (*LineFilterExtractor) VisitMatchers(*syntax.MatchersExpr) {
}

// VisitPipeline implements syntax.RootVisitor.
func (e *LineFilterExtractor) VisitPipeline(v *syntax.PipelineExpr) {
	v.Left.Accept(e)
	for _, m := range v.MultiStages {
		m.Accept(e)
	}
}

// VisitRangeAggregation implements syntax.RootVisitor.
func (e *LineFilterExtractor) VisitRangeAggregation(v *syntax.RangeAggregationExpr) {
	v.Left.Accept(e)
}

// VisitVector implements syntax.RootVisitor.
func (*LineFilterExtractor) VisitVector(*syntax.VectorExpr) {
}

// VisitVectorAggregation implements syntax.RootVisitor.
func (e *LineFilterExtractor) VisitVectorAggregation(v *syntax.VectorAggregationExpr) {
	v.Left.Accept(e)
}
