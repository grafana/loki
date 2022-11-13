package syntax

import (
	"fmt"
	"strings"

	"github.com/prometheus/common/model"
)

var (
	maxCharsPerLine = 100
)

func Prettify(e Expr) string {
	return e.Pretty(0)
}

// e.g: `{foo="bar"}`
func (e *MatchersExpr) Pretty(level int) string {
	return e.String()
}

// e.g: `{foo="bar"} | logfmt | level="error"`
// Here, left = `{foo="bar"}` and multistages would collection of each stage in pipeline, here logfmt and level="error"
func (e *PipelineExpr) Pretty(level int) string {
	if !needSplit(e) {
		return e.String()
	}
	s := fmt.Sprintf("%s\n", e.Left.Pretty(level+1))
	for _, ms := range e.MultiStages {
		s += ms.Pretty(level + 1)
	}
	return s
}

// e.g: `|= "error" != "memcache"`
func (e *LineFilterExpr) Pretty(level int) string {
	return ""
}

// e.g:
// `| logfmt`
// `| json`
// `| regexp`
// `| pattern`
// `| unpack`
func (e *LabelParserExpr) Pretty(level int) string {
	s := indent(level)
	return s + e.String()
}

// e.g: | level!="error"
func (e *LabelFilterExpr) Pretty(level int) string {
	return ""
}

// e.g: | line_format "{{ .label }}"
func (e *LineFmtExpr) Pretty(level int) string {
	return ""
}

// TODO: I have no idea what it does.
func (e *DecolorizeExpr) Pretty(level int) string {
	return ""
}

// e.g: | label_format dst="{{ .src }}"
func (e *LabelFmtExpr) Pretty(level int) string {
	return ""
}

// e.g:
// 1. | json label="expression", another="expression" (with parameters)
// 2. | json (without parameters)
func (e *JSONExpressionParser) Pretty(level int) string {
	return ""
}

// e.g: sum_over_time({foo="bar"} | logfmt | unwrap bytes_processed [5m])
func (e *UnwrapExpr) Pretty(level int) string {
	return e.String() // TODO: fix it later
}

// e.g: `{foo="bar"}|logfmt[5m]`
// TODO: Change it to LogRangeExpr (to be consistent with other expressions)
func (e *LogRange) Pretty(level int) string {
	s := indent(level)

	if !needSplit(e) {
		return s + e.String()
	}

	s += e.Left.Pretty(level + 1)

	if e.Unwrap != nil {
		s += e.Unwrap.Pretty(level + 1)
	}

	s += fmt.Sprintf("%s [%s]", s, model.Duration(e.Interval))

	if e.Offset != 0 {
		oe := OffsetExpr{Offset: e.Offset}
		s += oe.Pretty(level + 1)
	}

	return s
}

// e.g: count_over_time({foo="bar"}[5m offset 3h)
// NOTE: why does offset doesn't work on just stream selector? e.g: `{foo="bar"}| offset 1h`? is it bug? or anything else?
func (e *OffsetExpr) Pretty(level int) string {
	return e.String() // TODO: fix it later
}

// NOTE: Should I care about SampleExpr or LogSelectorExpr? (those are just abstract types)

// e.g: count_over_time({foo="bar"}[5m])
func (e *RangeAggregationExpr) Pretty(level int) string {
	s := indent(level)
	if !needSplit(e) {
		return s + e.String()
	}

	s += e.Operation

	s += "(\n"

	s += e.Left.Pretty(level + 1)

	if e.Grouping != nil {
		// TODO
	}

	s += "\n)"

	return s
}

// e.g: sum by (container)(count_over_time({foo="bar"}[5m]))
func (e *VectorAggregationExpr) Pretty(level int) string {
	return ""
}

// e.g: Any operations involving
// "or", "and" and "unless" (logical/set)
// "+", "-", "*", "/", "%", "^" (arithmetic)
// "==", "!=", ">", ">=", "<", "<=" (comparison)
func (e *BinOpExpr) Pretty(level int) string {
	return ""
}

// e.g: 4.6
func (e *LiteralExpr) Pretty(level int) string {
	return ""
}

// e.g: label_replace(up{job="api-server",service="a:c"}, "foo", "$1", "service", "(.*):.*")
func (e *LabelReplaceExpr) Pretty(level int) string {
	return ""
}

// e.g: vector(5)
func (e *VectorExpr) Pretty(level int) string {
	return ""
}

// Helpers

func needSplit(e Expr) bool {
	if e == nil {
		return false
	}
	return len(e.String()) > maxCharsPerLine
}

func indent(level int) string {
	return strings.Repeat(" ", level)
}
