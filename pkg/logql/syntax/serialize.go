package syntax

import (
	"fmt"
	"io"
	"time"

	jsoniter "github.com/json-iterator/go"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/pkg/logql/log"
)

type JSONSerializer struct {
	*jsoniter.Stream
}

func NewJSONSerializer(s *jsoniter.Stream) *JSONSerializer {
	return &JSONSerializer{
		Stream: s,
	}
}

func EncodeJSON(e Expr, w io.Writer) error {
	s := jsoniter.ConfigFastest.BorrowStream(w)
	defer jsoniter.ConfigFastest.ReturnStream(s)
	v := NewJSONSerializer(s)
	e.Accept(v)
	return s.Flush()
}

func DecodeJSON(raw string) (Expr, error) {
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

var _ RootVisitor = &JSONSerializer{}

func (v *JSONSerializer) VisitBinOp(e *BinOpExpr) {
	v.WriteObjectStart()

	v.WriteObjectField("bin")
	v.WriteObjectStart()

	v.WriteObjectField("op")
	v.WriteString(e.Op)

	v.WriteMore()
	v.WriteObjectField("lhs")
	e.SampleExpr.Accept(v)

	v.WriteMore()
	v.WriteObjectField("rhs")
	e.RHS.Accept(v)

	if e.Opts != nil {
		v.WriteMore()
		v.WriteObjectField("options")
		v.WriteObjectStart()

		v.WriteObjectField("return_bool")
		v.WriteBool(e.Opts.ReturnBool)

		if e.Opts.VectorMatching != nil {
			v.WriteMore()
			v.WriteObjectField("vector_matching")
			encodeVectorMatching(v.Stream, e.Opts.VectorMatching)
		}

		v.WriteObjectEnd()
		v.Flush()

	}

	v.WriteObjectEnd()
	v.WriteObjectEnd()
	v.Flush()
}

func (v *JSONSerializer) VisitVectorAggregation(e *VectorAggregationExpr) {
	v.WriteObjectStart()

	v.WriteObjectField("vector_agg")
	v.WriteObjectStart()

	v.WriteObjectField("params")
	v.WriteInt(e.Params)

	v.WriteMore()
	v.WriteObjectField("operation")
	v.WriteString(e.Operation)

	if e.Grouping != nil {
		v.WriteMore()
		v.WriteObjectField("grouping")
		encodeGrouping(v.Stream, e.Grouping)
	}

	v.WriteMore()
	v.WriteObjectField("inner")
	e.Left.Accept(v)

	v.WriteObjectEnd()
	v.WriteObjectEnd()
	v.Flush()
}

func (v *JSONSerializer) VisitRangeAggregation(e *RangeAggregationExpr) {
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

func (v *JSONSerializer) VisitLogRange(e *LogRange) {
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

func (v *JSONSerializer) VisitLabelReplace(e *LabelReplaceExpr) {
	v.WriteObjectStart()

	v.WriteObjectField("label_replace")
	v.WriteObjectStart()

	v.WriteObjectField("inner")
	e.Left.Accept(v)

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

func (v *JSONSerializer) VisitLiteral(e *LiteralExpr) {
	v.WriteObjectStart()

	v.WriteObjectField("literal")
	v.WriteObjectStart()

	v.WriteObjectField("val")
	v.WriteFloat64(e.Val)

	v.WriteObjectEnd()
	v.WriteObjectEnd()
	v.Flush()
}

func (v *JSONSerializer) VisitVector(e *VectorExpr) {
	v.WriteObjectStart()

	v.WriteObjectField("vector")
	v.WriteObjectStart()

	v.WriteObjectField("val")
	v.WriteFloat64(e.Val)

	v.WriteObjectEnd()
	v.WriteObjectEnd()
	v.Flush()
}

func (v *JSONSerializer) VisitMatchers(e *MatchersExpr) {
	v.WriteObjectStart()

	v.WriteObjectField("log_selector")
	encodeLogSelector(v.Stream, e)
	v.WriteObjectEnd()
	v.Flush()
}

func (v *JSONSerializer) VisitPipeline(e *PipelineExpr) {
	v.WriteObjectStart()

	v.WriteObjectField("log_selector")
	encodeLogSelector(v.Stream, e)
	v.WriteObjectEnd()
	v.Flush()
}

// Below are StageExpr visitors that we are skipping since a pipeline is
// serialized as a string.
func (*JSONSerializer) VisitDecolorize(*DecolorizeExpr)                     {}
func (*JSONSerializer) VisitDropLabels(*DropLabelsExpr)                     {}
func (*JSONSerializer) VisitJSONExpressionParser(*JSONExpressionParser)     {}
func (*JSONSerializer) VisitKeepLabel(*KeepLabelsExpr)                      {}
func (*JSONSerializer) VisitLabelFilter(*LabelFilterExpr)                   {}
func (*JSONSerializer) VisitLabelFmt(*LabelFmtExpr)                         {}
func (*JSONSerializer) VisitLabelParser(*LabelParserExpr)                   {}
func (*JSONSerializer) VisitLineFilter(*LineFilterExpr)                     {}
func (*JSONSerializer) VisitLineFmt(*LineFmtExpr)                           {}
func (*JSONSerializer) VisitLogfmtExpressionParser(*LogfmtExpressionParser) {}
func (*JSONSerializer) VisitLogfmtParser(*LogfmtParserExpr)                 {}

func encodeGrouping(s *jsoniter.Stream, g *Grouping) {
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

func decodeGrouping(iter *jsoniter.Iterator) (*Grouping, error) {
	g := &Grouping{}
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

func encodeUnwrap(s *jsoniter.Stream, u *UnwrapExpr) {
	s.WriteObjectStart()
	s.WriteObjectField("identifier")
	s.WriteString(u.Identifier)

	s.WriteMore()
	s.WriteObjectField("operation")
	s.WriteString(u.Operation)

	s.WriteMore()
	s.WriteObjectField("post_filterers")
	s.WriteArrayStart()
	for i, filter := range u.PostFilters {
		if i > 0 {
			s.WriteMore()
		}
		encodeLabelFilter(s, filter)
	}
	s.WriteArrayEnd()

	s.WriteObjectEnd()
}

func decodeUnwrap(iter *jsoniter.Iterator) *UnwrapExpr {
	e := &UnwrapExpr{}
	for f := iter.ReadObject(); f != ""; f = iter.ReadObject() {
		switch f {
		case "identifier":
			e.Identifier = iter.ReadString()
		case "operation":
			e.Operation = iter.ReadString()
		case "post_filterers":
			iter.ReadArrayCB(func(i *jsoniter.Iterator) bool {
				e.PostFilters = append(e.PostFilters, decodeLabelFilter(i))
				return true
			})
		}
	}

	return e
}

const (
	Name  = "name"
	Value = "value"
	Type  = "type"
)

func encodeLabelFilter(s *jsoniter.Stream, filter log.LabelFilterer) {
	switch concrete := filter.(type) {
	case *log.BinaryLabelFilter:
		s.WriteObjectStart()
		s.WriteObjectField("binary")

		s.WriteObjectStart()
		s.WriteObjectField("left")
		encodeLabelFilter(s, concrete.Left)

		s.WriteMore()
		s.WriteObjectField("right")
		encodeLabelFilter(s, concrete.Right)
		s.WriteObjectEnd()

		s.WriteMore()
		s.WriteObjectField("and")
		s.WriteBool(concrete.And)

		s.WriteObjectEnd()
	case log.NoopLabelFilter:
		return
	case *log.BytesLabelFilter:
		s.WriteObjectStart()
		s.WriteObjectField("bytes")

		s.WriteObjectStart()
		s.WriteObjectField(Name)
		s.WriteString(concrete.Name)

		s.WriteMore()
		s.WriteObjectField(Value)
		s.WriteUint64(concrete.Value)

		s.WriteMore()
		s.WriteObjectField(Type)
		s.WriteInt(int(concrete.Type))
		s.WriteObjectEnd()

		s.WriteObjectEnd()
	case *log.DurationLabelFilter:
		s.WriteObjectStart()
		s.WriteObjectField("duration")

		s.WriteObjectStart()
		s.WriteObjectField(Name)
		s.WriteString(concrete.Name)

		s.WriteMore()
		s.WriteObjectField(Value)
		s.WriteInt64(int64(concrete.Value))

		s.WriteMore()
		s.WriteObjectField(Type)
		s.WriteInt(int(concrete.Type))
		s.WriteObjectEnd()

		s.WriteObjectEnd()
	case *log.NumericLabelFilter:
		s.WriteObjectStart()
		s.WriteObjectField("numeric")

		s.WriteObjectStart()
		s.WriteObjectField(Name)
		s.WriteString(concrete.Name)

		s.WriteMore()
		s.WriteObjectField(Value)
		s.WriteFloat64(concrete.Value)

		s.WriteMore()
		s.WriteObjectField(Type)
		s.WriteInt(int(concrete.Type))
		s.WriteObjectEnd()

		s.WriteObjectEnd()
	case *log.StringLabelFilter:
		s.WriteObjectStart()
		s.WriteObjectField("string")

		s.WriteObjectStart()
		if concrete.Matcher != nil {
			s.WriteObjectField(Name)
			s.WriteString(concrete.Name)

			s.WriteMore()
			s.WriteObjectField(Value)
			s.WriteString(concrete.Value)

			s.WriteMore()
			s.WriteObjectField(Type)
			s.WriteInt(int(concrete.Type))
		}
		s.WriteObjectEnd()

		s.WriteObjectEnd()
	case *log.LineFilterLabelFilter:
		// Line filter label filter are encoded as string filters as
		// well. See log.NewStringLabelFilter.
		s.WriteObjectStart()
		s.WriteObjectField("string")

		s.WriteObjectStart()
		if concrete.Matcher != nil {
			s.WriteObjectField(Name)
			s.WriteString(concrete.Name)

			s.WriteMore()
			s.WriteObjectField(Value)
			s.WriteString(concrete.Value)

			s.WriteMore()
			s.WriteObjectField(Type)
			s.WriteInt(int(concrete.Type))
		}
		s.WriteObjectEnd()

		s.WriteObjectEnd()
	case *log.IPLabelFilter:
		s.WriteObjectStart()
		s.WriteObjectField("ip")

		s.WriteObjectStart()
		s.WriteObjectField(Type)
		s.WriteInt(int(concrete.Ty))

		s.WriteMore()
		s.WriteObjectField("label")
		s.WriteString(concrete.Label)

		s.WriteMore()
		s.WriteObjectField("pattern")
		s.WriteString(concrete.Pattern)

		s.WriteObjectEnd()

		s.WriteObjectEnd()
	}
}

func decodeLabelFilter(iter *jsoniter.Iterator) log.LabelFilterer {
	for f := iter.ReadObject(); f != ""; f = iter.ReadObject() {
		switch f {
		case "binary":
			var left, right log.LabelFilterer
			var and bool
			for k := iter.ReadObject(); k != ""; k = iter.ReadObject() {
				switch k {
				case "and":
					and = iter.ReadBool()
				case "left":
					left = decodeLabelFilter(iter)
				case "right":
					right = decodeLabelFilter(iter)
				}
			}

			return &log.BinaryLabelFilter{
				And:   and,
				Left:  left,
				Right: right,
			}

		case "bytes":
			var name string
			var b uint64
			var t log.LabelFilterType
			for k := iter.ReadObject(); k != ""; k = iter.ReadObject() {
				switch k {
				case Name:
					name = iter.ReadString()
				case Value:
					b = iter.ReadUint64()
				case Type:
					t = log.LabelFilterType(iter.ReadInt())
				}
			}
			return log.NewBytesLabelFilter(t, name, b)
		case "duration":
			var name string
			var duration time.Duration
			var t log.LabelFilterType
			for k := iter.ReadObject(); k != ""; k = iter.ReadObject() {
				switch k {
				case Name:
					name = iter.ReadString()
				case Value:
					duration = time.Duration(iter.ReadInt64())
				case Type:
					t = log.LabelFilterType(iter.ReadInt())
				}
			}

			return log.NewDurationLabelFilter(t, name, duration)
		case "numeric":
			var name string
			var value float64
			var t log.LabelFilterType
			for k := iter.ReadObject(); k != ""; k = iter.ReadObject() {
				switch k {
				case Name:
					name = iter.ReadString()
				case Value:
					value = iter.ReadFloat64()
				case Type:
					t = log.LabelFilterType(iter.ReadInt())
				}
			}

			return log.NewNumericLabelFilter(t, name, value)
		case "string":

			var name string
			var value string
			var t labels.MatchType
			for k := iter.ReadObject(); k != ""; k = iter.ReadObject() {
				switch k {
				case Name:
					name = iter.ReadString()
				case Value:
					value = iter.ReadString()
				case Type:
					t = labels.MatchType(iter.ReadInt())
				}
			}

			var matcher *labels.Matcher
			if name != "" && value != "" {
				matcher = labels.MustNewMatcher(t, name, value)
			}

			return log.NewStringLabelFilter(matcher)

		case "ip":
			var label string
			var pattern string
			var t log.LabelFilterType
			for k := iter.ReadObject(); k != ""; k = iter.ReadObject() {
				switch k {
				case "pattern":
					label = iter.ReadString()
				case "label":
					pattern = iter.ReadString()
				case Type:
					t = log.LabelFilterType(iter.ReadInt())
				}
			}
			return log.NewIPLabelFilter(pattern, label, t)
		}
	}

	return nil
}

func encodeLogSelector(s *jsoniter.Stream, e LogSelectorExpr) {
	s.WriteObjectStart()
	s.WriteObjectField("raw")

	s.WriteString(e.String())

	s.WriteObjectEnd()
	s.Flush()
}

func decodeLogSelector(iter *jsoniter.Iterator) (LogSelectorExpr, error) {
	var e LogSelectorExpr

	for f := iter.ReadObject(); f != ""; f = iter.ReadObject() {
		switch f {
		case "raw":
			raw := iter.ReadString()
			expr, err := ParseExpr(raw)
			if err != nil {
				return nil, err
			}

			var ok bool
			e, ok = expr.(LogSelectorExpr)

			if !ok {
				return nil, fmt.Errorf("unexpected expression type: want(LogSelectorExpr), got(%T)", expr)
			}
		}
	}

	return e, nil
}

func decodeSample(iter *jsoniter.Iterator) (SampleExpr, error) {
	var expr SampleExpr
	var err error
	for key := iter.ReadObject(); key != ""; key = iter.ReadObject() {
		switch key {
		case "bin":
			expr, err = decodeBinOp(iter)
		case "vector_agg":
			expr, err = decodeVectorAgg(iter)
		case "range_agg":
			expr, err = decodeRangeAgg(iter)
		case "literal":
			expr, err = decodeLiteral(iter)
		case "vector":
			expr, err = decodeVector(iter)
		case "label_replace":
			expr, err = decodeLabelReplace(iter)
		default:
			return nil, fmt.Errorf("unknown sample expression type: %s", key)
		}
	}
	return expr, err
}

func decodeBinOp(iter *jsoniter.Iterator) (*BinOpExpr, error) {
	expr := &BinOpExpr{}
	var err error

	for f := iter.ReadObject(); f != ""; f = iter.ReadObject() {
		switch f {
		case "op":
			expr.Op = iter.ReadString()
		case "rhs":
			expr.RHS, err = decodeSample(iter)
		case "lhs":
			expr.SampleExpr, err = decodeSample(iter)
		case "options":
			expr.Opts = decodeBinOpOptions(iter)
		}
	}

	return expr, err
}
func decodeBinOpOptions(iter *jsoniter.Iterator) *BinOpOptions {
	opts := &BinOpOptions{}

	for f := iter.ReadObject(); f != ""; f = iter.ReadObject() {
		switch f {
		case "return_bool":
			opts.ReturnBool = iter.ReadBool()
		case "vector_matching":
			opts.VectorMatching = decodeVectorMatching(iter)
		}
	}

	return opts
}

func encodeVectorMatching(s *jsoniter.Stream, vm *VectorMatching) {
	s.WriteObjectStart()

	s.WriteObjectField("include")
	s.WriteArrayStart()
	for i, l := range vm.Include {
		if i > 0 {
			s.WriteMore()
		}
		s.WriteString(l)
	}
	s.WriteArrayEnd()

	s.WriteMore()
	s.WriteObjectField("on")
	s.WriteBool(vm.On)

	s.WriteMore()
	s.WriteObjectField("card")
	s.WriteInt(int(vm.Card))

	s.WriteMore()
	s.WriteObjectField("matching_labels")
	s.WriteArrayStart()
	for i, l := range vm.MatchingLabels {
		if i > 0 {
			s.WriteMore()
		}
		s.WriteString(l)
	}
	s.WriteArrayEnd()

	s.WriteObjectEnd()
}

func decodeVectorMatching(iter *jsoniter.Iterator) *VectorMatching {
	vm := &VectorMatching{}

	for f := iter.ReadObject(); f != ""; f = iter.ReadObject() {
		switch f {
		case "include":
			iter.ReadArrayCB(func(i *jsoniter.Iterator) bool {
				vm.Include = append(vm.Include, i.ReadString())
				return true
			})
		case "on":
			vm.On = iter.ReadBool()
		case "card":
			vm.Card = VectorMatchCardinality(iter.ReadInt())
		case "matching_labels":
			iter.ReadArrayCB(func(i *jsoniter.Iterator) bool {
				vm.MatchingLabels = append(vm.MatchingLabels, i.ReadString())
				return true
			})
		}
	}
	return vm
}

func decodeVectorAgg(iter *jsoniter.Iterator) (*VectorAggregationExpr, error) {
	expr := &VectorAggregationExpr{}
	var err error

	for f := iter.ReadObject(); f != ""; f = iter.ReadObject() {
		switch f {
		case "operation":
			expr.Operation = iter.ReadString()
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

func decodeRangeAgg(iter *jsoniter.Iterator) (*RangeAggregationExpr, error) {
	expr := &RangeAggregationExpr{}
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

func decodeLogRange(iter *jsoniter.Iterator) (*LogRange, error) {
	expr := &LogRange{}
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

func decodeLabelReplace(iter *jsoniter.Iterator) (*LabelReplaceExpr, error) {
	var err error
	var left SampleExpr
	var dst, src, replacement, regex string

	for f := iter.ReadObject(); f != ""; f = iter.ReadObject() {
		switch f {
		case "inner":
			left, err = decodeSample(iter)
			if err != nil {
				return nil, err
			}
		case "dst":
			dst = iter.ReadString()
		case "src":
			src = iter.ReadString()
		case "replacement":
			replacement = iter.ReadString()
		case "regex":
			regex = iter.ReadString()
		}
	}

	return mustNewLabelReplaceExpr(left, dst, replacement, src, regex), nil
}

func decodeLiteral(iter *jsoniter.Iterator) (*LiteralExpr, error) {
	expr := &LiteralExpr{}

	for f := iter.ReadObject(); f != ""; f = iter.ReadObject() {
		switch f {
		case "val":
			expr.Val = iter.ReadFloat64()
		}
	}

	return expr, nil
}

func decodeVector(iter *jsoniter.Iterator) (*VectorExpr, error) {
	expr := &VectorExpr{}

	for f := iter.ReadObject(); f != ""; f = iter.ReadObject() {
		switch f {
		case "val":
			expr.Val = iter.ReadFloat64()
		}
	}

	return expr, nil
}

func decodeMatchers(iter *jsoniter.Iterator) (LogSelectorExpr, error) {
	return decodeLogSelector(iter)
}

func decodePipeline(iter *jsoniter.Iterator) (LogSelectorExpr, error) {
	return decodeLogSelector(iter)
}
