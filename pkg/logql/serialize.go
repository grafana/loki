package logql

import (
	"fmt"
	"io"
	"regexp"
	"time"

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

func EncodeJSON(e syntax.Expr, w io.Writer) error {
	s := jsoniter.ConfigFastest.BorrowStream(w)
	defer jsoniter.ConfigFastest.ReturnStream(s)
	v := NewJSONSerializer(s)
	err := syntax.Dispatch(e, v)
	s.Flush()

	return err
}

func DecodeJSON(raw string) (syntax.Expr, error) {
	iter := jsoniter.ParseString(jsoniter.ConfigFastest, raw)

	key := iter.ReadObject()
	switch key {
	case "bin":
		return decodeBinOp(iter)
	case "vector_agg":
		return decodeVectorAgg(iter)
	case "range_agg":
		return decodeRangeAgg(iter)
	case "literal":
		return decodeLiteral(iter)
	case "vector":
		return decodeVector(iter)
	case "label_replace":
		return decodeLabelReplace(iter)
	case "log_selector":
		return decodeLogSelector(iter)
	default:
		return nil, fmt.Errorf("unknown expression type: %s", key)
	}
}

var _ syntax.RootVisitor = &JSONSerializer{}

func (v *JSONSerializer) VisitBinOp(e *syntax.BinOpExpr) {
	v.WriteObjectStart()

	v.WriteObjectField("bin")
	v.WriteObjectStart()

	v.WriteMore()
	v.WriteObjectField("op")
	v.WriteString(e.Op)

	// TODO: encode options

	v.WriteMore()
	v.WriteObjectField("lhs")
	syntax.DispatchSampleExpr(e.SampleExpr, v)

	v.WriteMore()
	v.WriteObjectField("rhs")
	syntax.DispatchSampleExpr(e.RHS, v)

	v.WriteObjectEnd()
	v.WriteObjectEnd()
	v.Flush()
}

func (v *JSONSerializer) VisitVectorAggregation(e *syntax.VectorAggregationExpr) {
	v.WriteObjectStart()

	v.WriteObjectField("vector_agg")
	v.WriteObjectStart()

	v.WriteObjectField("params")
	v.WriteInt(e.Params)

	if e.Grouping != nil {
		v.WriteMore()
		v.WriteObjectField("grouping")
		encodeGrouping(v.Stream, e.Grouping)
	}

	v.WriteMore()
	v.WriteObjectField("inner")
	syntax.DispatchSampleExpr(e.Left, v)

	v.WriteObjectEnd()
	v.WriteObjectEnd()
	v.Flush()
}

func (v *JSONSerializer) VisitRangeAggregation(e *syntax.RangeAggregationExpr) {
	v.WriteObjectStart()

	v.WriteObjectField("range_agg")
	v.WriteObjectStart()

	v.WriteObjectField("op")
	v.WriteString(e.Operation)

	if e.Grouping != nil {
		v.WriteMore()
		v.WriteObjectField("grouping")
		encodeGrouping(v.Stream, e.Grouping)
	}

	if e.Params != nil {
		v.WriteMore()
		v.WriteObjectField("params")
		v.WriteFloat64(*e.Params)
	}

	v.WriteMore()
	v.WriteObjectField("range")
	v.VisitLogRange(e.Left)
	v.WriteObjectEnd()

	v.WriteObjectEnd()
	v.Flush()
}

func (v *JSONSerializer) VisitLogRange(e *syntax.LogRange) {
	v.WriteObjectStart()

	v.WriteObjectField("interval_nanos")
	v.WriteInt64(int64(e.Interval))
	v.WriteMore()
	v.WriteObjectField("offset_nanos")
	v.WriteInt64(int64(e.Offset))

	// Serialize log selector pipeline as string.
	v.WriteMore()
	v.WriteObjectField("log_selector")
	encodeLogSelector(v.Stream, e.Left)

	if e.Unwrap != nil {
		v.WriteMore()
		v.WriteObjectField("unwrap")
		encodeUnwrap(v.Stream, e.Unwrap)
	}

	v.WriteObjectEnd()
	v.Flush()
}

func (v *JSONSerializer) VisitLabelReplace(e *syntax.LabelReplaceExpr) {
	v.WriteObjectStart()

	v.WriteObjectField("label_replace")
	v.WriteObjectStart()

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
	v.WriteObjectEnd()
	v.Flush()
}

func (v *JSONSerializer) VisitLiteral(e *syntax.LiteralExpr) {
	v.WriteObjectStart()

	v.WriteObjectField("literal")
	v.WriteObjectStart()

	v.WriteMore()
	v.WriteObjectField("val")
	v.WriteFloat64(e.Val)

	v.WriteObjectEnd()
	v.WriteObjectEnd()
	v.Flush()
}

func (v *JSONSerializer) VisitVector(e *syntax.VectorExpr) {
	v.WriteObjectStart()

	v.WriteObjectField("vector")
	v.WriteObjectStart()

	v.WriteMore()
	v.WriteObjectField("val")
	v.WriteFloat64(e.Val)

	v.WriteObjectEnd()
	v.WriteObjectEnd()
	v.Flush()
}

func (v *JSONSerializer) VisitMatchers(e *syntax.MatchersExpr) {
	v.WriteObjectStart()

	v.WriteObjectField("log_selector")
	encodeLogSelector(v.Stream, e)
	v.WriteObjectEnd()
	v.Flush()
}

func (v *JSONSerializer) VisitPipeline(e *syntax.PipelineExpr) {
	v.WriteObjectStart()

	v.WriteObjectField("log_selector")
	encodeLogSelector(v.Stream, e)
	v.WriteObjectEnd()
	v.Flush()
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

func decodeGrouping(iter *jsoniter.Iterator) (*syntax.Grouping, error) {
	g := &syntax.Grouping{}
	for f := iter.ReadObject(); f != ""; f = iter.ReadObject() {
		switch f {
		case "without":
			g.Without = iter.ReadBool()
		case "groups":
			iter.ReadArrayCB(func(iter *jsoniter.Iterator) bool {
				g.Groups = append(g.Groups, iter.ReadString())
				return true
			})
		}
	}

	return g, nil
}

func encodeUnwrap(s *jsoniter.Stream, u *syntax.UnwrapExpr) {
	s.WriteObjectStart()
	s.WriteObjectField("identifier")
	s.WriteString(u.Identifier)

	s.WriteMore()
	s.WriteObjectField("operation")
	s.WriteString(u.Operation)

	/*
		s.WriteMore()
		s.WriteObjectField("post_filterers")
		s.WriteArrayStart()
		for i, group := range u.PostFilters{
			if i > 0 {
				s.WriteMore()
			}
			s.WriteString(group)
		}
		s.WriteArrayEnd()
	*/

	s.WriteObjectEnd()
}

func decodeUnwrap(iter *jsoniter.Iterator) *syntax.UnwrapExpr {
	e := &syntax.UnwrapExpr{}
	for f := iter.ReadObject(); f != ""; f = iter.ReadObject() {
		switch f {
		case "identifier":
			e.Identifier = iter.ReadString()
		case "operation":
			e.Operation = iter.ReadString()
		case "post_filterers":
			iter.Skip()
		}
	}

	return e
}

func encodeLogSelector(s *jsoniter.Stream, e syntax.LogSelectorExpr) {
	s.WriteObjectStart()
	s.WriteObjectField("raw")

	s.WriteString(e.String())

	s.WriteObjectEnd()
	s.Flush()
}

func decodeLogSelector(iter *jsoniter.Iterator) (syntax.LogSelectorExpr, error) {
	var e syntax.LogSelectorExpr
	var err error

	for f := iter.ReadObject(); f != ""; f = iter.ReadObject() {
		switch f {
		case "raw":
			raw := iter.ReadString()
			expr, err := syntax.ParseExpr(raw)
			if err != nil {
				return nil, err
			}

			var ok bool
			e, ok = expr.(syntax.LogSelectorExpr)

			if !ok {
				err = fmt.Errorf("unexpected expression type: want(LogSelectorExpr), got(%T)", expr)
			}
		}
	}

	return e, err
}

func decodeSample(iter *jsoniter.Iterator) (syntax.SampleExpr, error) {
	key := iter.ReadObject()
	switch key {
	case "bin":
		return decodeBinOp(iter)
	case "vector_agg":
		return decodeVectorAgg(iter)
	case "range_agg":
		return decodeRangeAgg(iter)
	case "literal":
		return decodeLiteral(iter)
	case "vector":
		return decodeVector(iter)
	case "label_replace":
		return decodeLabelReplace(iter)
	default:
		return nil, fmt.Errorf("unknown sample expression type: %s", key)
	}
}

func decodeBinOp(iter *jsoniter.Iterator) (*syntax.BinOpExpr, error) {
	expr := &syntax.BinOpExpr{}
	var err error

	for f := iter.ReadObject(); f != ""; f = iter.ReadObject() {
		switch f {
		case "op":
			expr.Op = iter.ReadString()
		case "rhs":
			expr.RHS, err = decodeSample(iter)
		case "lhs":
			expr.SampleExpr, err = decodeSample(iter)
		}
	}

	return expr, err
}

func decodeVectorAgg(iter *jsoniter.Iterator) (*syntax.VectorAggregationExpr, error) {
	expr := &syntax.VectorAggregationExpr{}
	var err error

	for f := iter.ReadObject(); f != ""; f = iter.ReadObject() {
		switch f {
		case "params":
			expr.Params = iter.ReadInt()
		case "grouping":
			expr.Grouping, err = decodeGrouping(iter)
		case "inner":
			expr.Left, err = decodeSample(iter)
		}
	}

	return expr, err
}

func decodeRangeAgg(iter *jsoniter.Iterator) (*syntax.RangeAggregationExpr, error) {
	expr := &syntax.RangeAggregationExpr{}
	var err error

	for f := iter.ReadObject(); f != ""; f = iter.ReadObject() {
		switch f {
		case "op":
			expr.Operation = iter.ReadString()
		case "params":
			tmp := iter.ReadFloat64()
			expr.Params = &tmp
		case "range":
			expr.Left, err = decodeLogRange(iter)
		case "grouping":
			expr.Grouping, err = decodeGrouping(iter)
		}
	}

	return expr, err
}

func decodeLogRange(iter *jsoniter.Iterator) (*syntax.LogRange, error) {
	expr := &syntax.LogRange{}
	var err error

	for f := iter.ReadObject(); f != ""; f = iter.ReadObject() {
		switch f {
		case "log_selector":
			expr.Left, err = decodeLogSelector(iter)
		case "interval_nanos":
			expr.Interval = time.Duration(iter.ReadInt64())
		case "offset_nanos":
			expr.Offset = time.Duration(iter.ReadInt64())
		case "unwrap":
			expr.Unwrap = decodeUnwrap(iter)
		}
	}

	return expr, err
}

func decodeLabelReplace(iter *jsoniter.Iterator) (*syntax.LabelReplaceExpr, error) {
	expr := &syntax.LabelReplaceExpr{}
	var err error

	for f := iter.ReadObject(); f != ""; f = iter.ReadObject() {
		switch f {
		case "inner":
			expr.Left, err = decodeSample(iter)
		case "dst":
			expr.Dst = iter.ReadString()
		case "src":
			expr.Src = iter.ReadString()
		case "replacement":
			expr.Replacement = iter.ReadString()
		case "regexp":
			expr.Regex = iter.ReadString()
			if expr.Regex != "" {
				expr.Re, err = regexp.Compile(expr.Regex)
			}
		}
	}

	return expr, err
}

func decodeLiteral(iter *jsoniter.Iterator) (*syntax.LiteralExpr, error) {
	expr := &syntax.LiteralExpr{}

	for f := iter.ReadObject(); f != ""; f = iter.ReadObject() {
		switch f {
		case "val":
			expr.Val = iter.ReadFloat64()
		}
	}

	return expr, nil
}

func decodeVector(iter *jsoniter.Iterator) (*syntax.VectorExpr, error) {
	expr := &syntax.VectorExpr{}

	for f := iter.ReadObject(); f != ""; f = iter.ReadObject() {
		switch f {
		case "val":
			expr.Val = iter.ReadFloat64()
		}
	}

	return expr, nil
}

func decodeMatchers(iter *jsoniter.Iterator) (syntax.LogSelectorExpr, error) {
	return decodeLogSelector(iter)
}

func decodePipeline(iter *jsoniter.Iterator) (syntax.LogSelectorExpr, error) {
	return decodeLogSelector(iter)
}
