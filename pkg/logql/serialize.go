package logql

import (
	jsoniter "github.com/json-iterator/go"

	"github.com/grafana/loki/pkg/logql/syntax"
)

type JSONSerailizer struct {
	stream *jsoniter.Stream
}

var _ syntax.RootVisitor = &JSONSerailizer{}

func (v *JSONSerailizer) VisitBinOp(e *syntax.BinOpExpr)
func (v *JSONSerailizer) VisitVectorAggregation(e *syntax.VectorAggregationExpr)
func (v *JSONSerailizer) VisitRangeAggregation(e *syntax.RangeAggregationExpr)
func (v *JSONSerailizer) VisitLabelReplace(e *syntax.LabelReplaceExpr)
func (v *JSONSerailizer) VisitLiteral(e *syntax.LiteralExpr)
func (v *JSONSerailizer) VisitVector(e *syntax.VectorExpr)

func (v *JSONSerailizer) VisitMatchers(e *syntax.MatchersExpr)
func (v *JSONSerailizer) VisitPipeline(e *syntax.PipelineExpr)

func (v *JSONSerailizer) VisitDecolorize(e *syntax.DecolorizeExpr)
func (v *JSONSerailizer) VisitDropLabels(e *syntax.DropLabelsExpr)
func (v *JSONSerailizer) VisitJSONExpressionParser(e *syntax.JSONExpressionParser)
func (v *JSONSerailizer) VisitKeekLabel(e *syntax.KeepLabelsExpr)
func (v *JSONSerailizer) VisitLabelFilter(e *syntax.LabelFilterExpr)
func (v *JSONSerailizer) VisitLabelFmt(e *syntax.LabelFmtExpr)
func (v *JSONSerailizer) VisitLabelParser(e *syntax.LabelParserExpr)
func (v *JSONSerailizer) VisitLineFilter(e *syntax.LineFilterExpr)
func (v *JSONSerailizer) VisitLineFmt(e *syntax.LineFmtExpr)
func (v *JSONSerailizer) VisitLogfmtExpressionParser(e *syntax.LogfmtExpressionParser)
func (v *JSONSerailizer) VisitLogfmtParser(e *syntax.LogfmtParserExpr)
