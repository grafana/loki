package logql

import (
	jsoniter "github.com/json-iterator/go"

	"github.com/grafana/loki/pkg/logql/syntax"
)

type JSONSerializer struct {
	*jsoniter.Stream
}

func NewJSONSerializer(s *jsoniter.Stream) *JSONSerializer {
	return &JSONSerializer{
		Stream: s,
	}
}

var _ syntax.RootVisitor = &JSONSerializer{}

func (v *JSONSerializer) VisitBinOp(e *syntax.BinOpExpr) {
	v.WriteObjectStart()

	v.WriteObjectField("type")
	v.WriteString("bin")

	v.WriteMore()
	v.WriteObjectField("op")
	v.WriteString(e.Op)

	v.WriteMore()
	v.WriteObjectField("lhs")
	syntax.DispatchSampleExpr(e.SampleExpr, v)

	v.WriteMore()
	v.WriteObjectField("rhs")
	syntax.DispatchSampleExpr(e.RHS, v)

	v.WriteObjectEnd()
	v.Flush()
}

func (v *JSONSerializer) VisitVectorAggregation(e *syntax.VectorAggregationExpr) {
	v.WriteObjectStart()

	v.WriteObjectField("type")
	v.WriteString("vector_aggr")

	v.WriteMore()
	v.WriteObjectField("params")
	v.WriteInt(e.Params)

	v.WriteMore()
	v.WriteObjectField("grouping")
	encodeGrouping(v.Stream, e.Grouping)

	v.WriteMore()
	v.WriteObjectField("inner")
	syntax.DispatchSampleExpr(e.Left, v)

	v.WriteObjectEnd()
	v.Flush()
}

func (v *JSONSerializer) VisitRangeAggregation(e *syntax.RangeAggregationExpr) {
	v.WriteObjectStart()

	v.WriteObjectField("type")
	v.WriteString("range_aggr")

	v.WriteMore()
	v.WriteObjectField("op")
	v.WriteString(e.Operation)

	v.WriteMore()
	v.WriteObjectField("grouping")
	encodeGrouping(v.Stream, e.Grouping)

	v.WriteMore()
	v.WriteObjectField("params")
	v.WriteFloat64(*e.Params)

	v.WriteMore()
	v.WriteObjectField("range")
	// TODO: decide whether LogRange should have a visitor method
	v.WriteObjectStart()
	v.WriteObjectField("interval_nanos")
	v.WriteInt64(int64(e.Left.Interval))
	v.WriteMore()
	v.WriteObjectField("offset_nanos")
	v.WriteInt64(int64(e.Left.Offset))

	// Serialize log selector pipeline as string.
	v.WriteMore()
	v.WriteObjectField("log_selector")
	encodeLogSelector(v.Stream, e.Left.Left)

	v.WriteMore()
	v.WriteObjectField("unwrap")
	v.WriteString(e.Left.Unwrap.String())
	v.WriteObjectEnd()

	v.WriteObjectEnd()
	v.Flush()
}

func (v *JSONSerializer) VisitLabelReplace(e *syntax.LabelReplaceExpr) {
	v.WriteObjectStart()

	v.WriteObjectField("type")
	v.WriteString("label_replace")

	v.WriteMore()
	v.WriteObjectField("inner")
	syntax.DispatchSampleExpr(e.Left, v)

	v.WriteMore()
	v.WriteObjectField("dst")
	v.WriteString(e.Dst)

	v.WriteMore()
	v.WriteObjectField("src")
	v.WriteString(e.Src)

	v.WriteMore()
	v.WriteObjectField("replacement")
	v.WriteString(e.Replacement)

	v.WriteMore()
	v.WriteObjectField("regex")
	v.WriteString(e.Regex)

	v.WriteObjectEnd()
	v.Flush()
}

func (v *JSONSerializer) VisitLiteral(e *syntax.LiteralExpr) {
	v.WriteObjectStart()

	v.WriteObjectField("type")
	v.WriteString("literal")

	v.WriteMore()
	v.WriteObjectField("val")
	v.WriteFloat64(e.Val)

	v.WriteObjectEnd()
	v.Flush()
}

func (v *JSONSerializer) VisitVector(e *syntax.VectorExpr) {
	v.WriteObjectStart()

	v.WriteObjectField("type")
	v.WriteString("vector")

	v.WriteMore()
	v.WriteObjectField("val")
	v.WriteFloat64(e.Val)

	v.WriteObjectEnd()
	v.Flush()
}

func (v *JSONSerializer) VisitMatchers(e *syntax.MatchersExpr) {
	encodeLogSelector(v.Stream, e)
}

func (v *JSONSerializer) VisitPipeline(e *syntax.PipelineExpr) {
	encodeLogSelector(v.Stream, e)
}

// Below are StageExpr visitors that we are skipping since a pipeline is
// serialized as a string.
func (*JSONSerializer) VisitDecolorize(*syntax.DecolorizeExpr)                     {}
func (*JSONSerializer) VisitDropLabels(*syntax.DropLabelsExpr)                     {}
func (*JSONSerializer) VisitJSONExpressionParser(*syntax.JSONExpressionParser)     {}
func (*JSONSerializer) VisitKeekLabel(*syntax.KeepLabelsExpr)                      {}
func (*JSONSerializer) VisitLabelFilter(*syntax.LabelFilterExpr)                   {}
func (*JSONSerializer) VisitLabelFmt(*syntax.LabelFmtExpr)                         {}
func (*JSONSerializer) VisitLabelParser(*syntax.LabelParserExpr)                   {}
func (*JSONSerializer) VisitLineFilter(*syntax.LineFilterExpr)                     {}
func (*JSONSerializer) VisitLineFmt(*syntax.LineFmtExpr)                           {}
func (*JSONSerializer) VisitLogfmtExpressionParser(*syntax.LogfmtExpressionParser) {}
func (*JSONSerializer) VisitLogfmtParser(*syntax.LogfmtParserExpr)                 {}

func encodeGrouping(s *jsoniter.Stream, g *syntax.Grouping) {
	s.WriteObjectStart()
	s.WriteObjectField("without")
	s.WriteBool(g.Without)
	s.WriteMore()
	s.WriteObjectField("groups")
	s.WriteArrayStart()
	for i, group := range g.Groups {
		if i > 0 {
			s.WriteMore()
		}
		s.WriteString(group)
	}
	s.WriteArrayEnd()
	s.WriteObjectEnd()
}

func encodeLogSelector(s *jsoniter.Stream, e syntax.LogSelectorExpr) {
	s.WriteObjectStart()
	s.WriteObjectField("type")
	s.WriteString("log_selector")

	s.WriteMore()
	s.WriteObjectField("raw")
	s.WriteString(e.String())

	s.WriteObjectEnd()
	s.Flush()
}
