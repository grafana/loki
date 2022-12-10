package syntax

import (
	"fmt"
	"strconv"
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

// e.g: | decolorize
func (e *DecolorizeExpr) Pretty(level int) string {
	return e.String()
}

// e.g: | label_format dst="{{ .src }}"
func (e *LabelFmtExpr) Pretty(level int) string {
	return commonPrefixIndent(level, e)
}

// e.g: | json label="expression", another="expression"
func (e *JSONExpressionParser) Pretty(level int) string {
	return commonPrefixIndent(level, e)
}

// e.g: sum_over_time({foo="bar"} | logfmt | unwrap bytes_processed [5m])
func (e *UnwrapExpr) Pretty(level int) string {
	var sb strings.Builder
	sb.WriteString(indent(level))

	if e.Operation != "" {
		sb.WriteString(fmt.Sprintf("%s %s %s(%s)", OpPipe, OpUnwrap, e.Operation, e.Identifier))
	} else {
		sb.WriteString(fmt.Sprintf("%s %s %s", OpPipe, OpUnwrap, e.Identifier))
	}
	for _, f := range e.PostFilters {
		sb.WriteString(fmt.Sprintf("\n%s%s %s", indent(level), OpPipe, f))
	}
	return sb.String() //
}

// e.g: `{foo="bar"}|logfmt[5m]`
// TODO: Change it to LogRangeExpr (to be consistent with other expressions)
func (e *LogRange) Pretty(level int) string {
	if !needSplit(e) {
		return indent(level) + e.String()
	}

	s := e.Left.Pretty(level)

	if e.Unwrap != nil {
		// NOTE: | unwrap should go to newline
		s += "\n"
		s += e.Unwrap.Pretty(level + 1)
	}

	// TODO: this will put [1m] on the same line, not in new line as people used to now.
	s = fmt.Sprintf("%s [%s]", s, model.Duration(e.Interval))

	if e.Offset != 0 {
		oe := OffsetExpr{Offset: e.Offset}
		s += oe.Pretty(level)
	}

	return s
}

// e.g: count_over_time({foo="bar"}[5m] offset 3h)
// NOTE: why does offset doesn't work on just stream selector? e.g: `{foo="bar"}| offset 1h`? is it bug? or anything else?
// Also offset expression never to be indended. It always goes with it's parent expr (usually RangeExpr).
func (e *OffsetExpr) Pretty(level int) string {
	// using `model.Duration` as it can format ignoring zero units.
	// e.g: time.Duration(2 * Hour) -> "2h0m0s"
	// but model.Duration(2 * Hour) -> "2h"
	return fmt.Sprintf(" %s %s", OpOffset, model.Duration(e.Offset))
}

// NOTE: Should I care about SampleExpr or LogSelectorExpr? (those are just abstract types)

// e.g: count_over_time({foo="bar"}[5m])
func (e *RangeAggregationExpr) Pretty(level int) string {
	s := indent(level)
	if !needSplit(e) {
		return s + e.String()
	}

	s += e.Operation // e.g: quantile_over_time

	s += "(\n"

	// print args to the function.
	if e.Params != nil {
		s = fmt.Sprintf("%s%s%s,", s, indent(level+1), fmt.Sprint(*e.Params))
		s += "\n"
	}

	s += e.Left.Pretty(level + 1)

	s += "\n" + indent(level) + ")"

	if e.Grouping != nil {
		s += e.Grouping.Pretty(level)
	}

	return s
}

// e.g:
// sum(count_over_time({foo="bar"}[5m])) by (container)
// topk(10, count_over_time({foo="bar"}[5m])) by (container)

// Syntax: <aggr-op>([parameter,] <vector expression>) [without|by (<label list>)]
// <aggr-op> - sum, avg, bottomk, topk, etc.
// [parameters,] - optional params, used only by bottomk and topk for now.
// <vector expression> - vector on which aggregation is done.
// [without|by (<label list)] - optional labels to aggregate either with `by` or `without` clause.
func (e *VectorAggregationExpr) Pretty(level int) string {
	s := indent(level)

	if !needSplit(e) {
		return s + e.String()
	}

	var params []string

	// level + 1 because arguments to function will be in newline.
	left := e.Left.Pretty(level + 1)
	switch e.Operation {
	// e.Params default value (0) can mean a legit param for topk and bottomk
	case OpTypeBottomK, OpTypeTopK:
		params = []string{fmt.Sprintf("%s%d", indent(level+1), e.Params), left}

	default:
		if e.Params != 0 {
			params = []string{fmt.Sprintf("%s%d", indent(level+1), e.Params), left}
		} else {
			params = []string{left}
		}
	}

	s += e.Operation
	if e.Grouping != nil {
		s += e.Grouping.Pretty(level)
	}

	// (\n [params,\n])
	s += "(\n"
	for i, v := range params {
		s += v
		// LogQL doesn't allow `,` at the end of last argument.
		if i < len(params)-1 {
			s += ","
		}
		s += "\n"
	}
	s += indent(level) + ")"

	return s
}

// e.g: Any operations involving
// "or", "and" and "unless" (logical/set)
// "+", "-", "*", "/", "%", "^" (arithmetic)
// "==", "!=", ">", ">=", "<", "<=" (comparison)
func (e *BinOpExpr) Pretty(level int) string {
	// TODO: handle e.Opts

	s := indent(level)
	if !needSplit(e) {
		return s + e.String()
	}

	s = e.SampleExpr.Pretty(level+1) + "\n"
	s += indent(level) + e.Op + "\n"
	s += e.RHS.Pretty(level + 1)

	return s
}

// e.g: 4.6
func (e *LiteralExpr) Pretty(level int) string {
	return commonPrefixIndent(level, e)
}

// e.g: label_replace(rate({job="api-server",service="a:c"}[5m]), "foo", "$1", "service", "(.*):.*")
func (e *LabelReplaceExpr) Pretty(level int) string {
	s := indent(level)

	if !needSplit(e) {
		return s + e.String()
	}

	s += OpLabelReplace

	s += "(\n"

	params := []string{
		e.Left.Pretty(level + 1),
		indent(level+1) + strconv.Quote(e.Dst),
		indent(level+1) + strconv.Quote(e.Replacement),
		indent(level+1) + strconv.Quote(e.Src),
		indent(level+1) + strconv.Quote(e.Regex),
	}

	for i, v := range params {
		s += v
		// LogQL doesn't allow `,` at the end of last argument.
		if i < len(params)-1 {
			s += ","
		}
		s += "\n"
	}

	s += indent(level) + ")"

	return s
}

// e.g: vector(5)
func (e *VectorExpr) Pretty(level int) string {
	return commonPrefixIndent(level, e)
}

// Grouping is techincally not expression type. But used in both range and vector aggregation (`by` and `without` clause)
// So by implenting `Pretty` for Grouping, we can re use it for both.
// NOTE: indent is ignored for `Grouping`, because grouping always stays in the same line of it's parent expression.

// e.g:
// by(container,namespace) -> by (container, namespace)
func (g *Grouping) Pretty(_ int) string {
	var s string

	if g.Without {
		s += " without"
	} else if len(g.Groups) > 0 {
		s += " by"
	}

	if len(g.Groups) > 0 {
		s += " ("
		s += strings.Join(g.Groups, ", ")
		s += ")"
	}
	return s
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
