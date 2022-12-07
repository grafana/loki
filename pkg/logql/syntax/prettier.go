package syntax

import (
	"fmt"
	"strings"

	"github.com/prometheus/common/model"
)

// Doc:
// 1. Differs with promql prettifer, by linefilter and pipelineexpr. Because even if we add \n, we indent to same level for these two so far.

var (
	maxCharsPerLine = 20
)

func Prettify(e Expr) string {
	return e.Pretty(0)
}

// e.g: `{foo="bar"}`
func (e *MatchersExpr) Pretty(level int) string {
	return commonPrefixIndent(level, e)
}

// e.g: `{foo="bar"} | logfmt | level="error"`
// Here, left = `{foo="bar"}` and multistages would collection of each stage in pipeline, here `logfmt` and `level="error"`
func (e *PipelineExpr) Pretty(level int) string {
	if !needSplit(e) {
		return indent(level) + e.String()
	}

	s := fmt.Sprintf("%s\n", e.Left.Pretty(level))
	for i, ms := range e.MultiStages {
		s += ms.Pretty(level + 1)
		//NOTE: Needed because, we tend to format multiple stage in pipeline as each stage in single line
		// e.g:
		// | logfmt
		// | level = "error"
		// But all the stages will have same indent level. So here we don't increase level.
		if i < len(e.MultiStages)-1 {
			s += "\n"
		}
	}
	return s
}

// e.g: `|= "error" != "memcache" |= ip("192.168.0.1")`
// NOTE: here `ip` is Op in this expression.
func (e *LineFilterExpr) Pretty(level int) string {
	if !needSplit(e) {
		return indent(level) + e.String()
	}

	var s string

	if e.Left != nil {
		// s += indent(level)
		s += e.Left.Pretty(level)
		// NOTE: Similar to PiplelinExpr, we also have to format every LineFilterExpr in new line. But with same indendation level.
		// e.g:
		// |= "error"
		// != "memcached"
		// |= ip("192.168.0.1")
		s += "\n"
	}

	s += indent(level)

	// We re-use LineFilterExpr's String() implementation to avoid duplication.
	// We create new LineFilterExpr without `Left`.
	ne := newLineFilterExpr(e.Ty, e.Op, e.Match)
	s += ne.String()

	return s
}

// e.g:
// `| logfmt`
// `| json`
// `| regexp`
// `| pattern`
// `| unpack`
func (e *LabelParserExpr) Pretty(level int) string {
	return commonPrefixIndent(level, e)
}

// e.g: | level!="error"
func (e *LabelFilterExpr) Pretty(level int) string {
	return commonPrefixIndent(level, e)
}

// e.g: | line_format "{{ .label }}"
func (e *LineFmtExpr) Pretty(level int) string {
	return commonPrefixIndent(level, e)
}

// TODO: I have no idea yet about what it does. Later!
func (e *DecolorizeExpr) Pretty(level int) string {
	return ""
}

// e.g: | label_format dst="{{ .src }}"
func (e *LabelFmtExpr) Pretty(level int) string {
	return commonPrefixIndent(level, e)
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
	if !needSplit(e) {
		return indent(level) + e.String()
	}

	s := e.Left.Pretty(level)

	if e.Unwrap != nil {
		s += e.Unwrap.Pretty(level)
	}

	// TODO: this will put [1m] on the same line, not in new line as people used to now.
	s = fmt.Sprintf("%s [%s]", s, model.Duration(e.Interval))

	if e.Offset != 0 {
		oe := OffsetExpr{Offset: e.Offset}
		s += oe.Pretty(level)
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

func commonPrefixIndent(level int, current Expr) string {
	return fmt.Sprintf("%s%s", indent(level), current.String())
}

func needSplit(e Expr) bool {
	if e == nil {
		return false
	}
	return len(e.String()) > maxCharsPerLine
}

const indentString = "  "

func indent(level int) string {
	return strings.Repeat(indentString, level)
}
