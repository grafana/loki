package syntax

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/pkg/labels"

	"github.com/grafana/loki/pkg/logql/log"
)

const (
	// vector ops
	OpTypeSum     = "sum"
	OpTypeAvg     = "avg"
	OpTypeMax     = "max"
	OpTypeMin     = "min"
	OpTypeCount   = "count"
	OpTypeStddev  = "stddev"
	OpTypeStdvar  = "stdvar"
	OpTypeBottomK = "bottomk"
	OpTypeTopK    = "topk"

	// range vector ops
	OpRangeTypeCount     = "count_over_time"
	OpRangeTypeRate      = "rate"
	OpRangeTypeBytes     = "bytes_over_time"
	OpRangeTypeBytesRate = "bytes_rate"
	OpRangeTypeAvg       = "avg_over_time"
	OpRangeTypeSum       = "sum_over_time"
	OpRangeTypeMin       = "min_over_time"
	OpRangeTypeMax       = "max_over_time"
	OpRangeTypeStdvar    = "stdvar_over_time"
	OpRangeTypeStddev    = "stddev_over_time"
	OpRangeTypeQuantile  = "quantile_over_time"
	OpRangeTypeAbsent    = "absent_over_time"

	// binops - logical/set
	OpTypeOr     = "or"
	OpTypeAnd    = "and"
	OpTypeUnless = "unless"

	// binops - operations
	OpTypeAdd = "+"
	OpTypeSub = "-"
	OpTypeMul = "*"
	OpTypeDiv = "/"
	OpTypeMod = "%"
	OpTypePow = "^"

	// binops - comparison
	OpTypeCmpEQ = "=="
	OpTypeNEQ   = "!="
	OpTypeGT    = ">"
	OpTypeGTE   = ">="
	OpTypeLT    = "<"
	OpTypeLTE   = "<="

	// parsers
	OpParserTypeJSON   = "json"
	OpParserTypeLogfmt = "logfmt"
	OpParserTypeRegexp = "regexp"

	OpFmtLine  = "line_format"
	OpFmtLabel = "label_format"

	OpPipe   = "|"
	OpUnwrap = "unwrap"

	// conversion Op
	OpConvBytes           = "bytes"
	OpConvDuration        = "duration"
	OpConvDurationSeconds = "duration_seconds"

	OpLabelReplace = "label_replace"
)

type Expr interface {
	fmt.Stringer
	Sub() []Expr
}

type LogSelectorExpr interface {
	Expr
}

type LogExpr interface {
	Expr
}

type SampleExpr interface {
	Expr
}

type MetricExpr interface {
	Expr
}

type StageExpr interface {
	Expr
}

type MatcherExpr struct {
	Matchers []*labels.Matcher
}

func newMatcherExpr(matchers []*labels.Matcher) *MatcherExpr {
	return &MatcherExpr{Matchers: matchers}
}

func (e *MatcherExpr) String() string {
	var sb strings.Builder
	sb.WriteString("{")
	for i, m := range e.Matchers {
		sb.WriteString(m.String())
		if i+1 != len(e.Matchers) {
			sb.WriteString(", ")
		}
	}
	sb.WriteString("}")
	return sb.String()
}

func (e *MatcherExpr) Sub() []Expr {
	return []Expr{}
}

type PipelineExpr struct {
	Matcher  *MatcherExpr
	Pipeline *MultiStageExpr
}

func newPipelineExpr(matcher *MatcherExpr, pipeline *MultiStageExpr) *PipelineExpr {
	return &PipelineExpr{
		Matcher:  matcher,
		Pipeline: pipeline,
	}
}

func (e *PipelineExpr) String() string {
	var sb strings.Builder
	sb.WriteString(e.Matcher.String())
	sb.WriteString(" ")
	sb.WriteString(e.Pipeline.String())
	return sb.String()
}

func (e *PipelineExpr) Sub() []Expr {
	exprs := make([]Expr, 0, 2)
	if e.Matcher != nil {
		exprs = append(exprs, e.Matcher)
	}
	if e.Pipeline != nil {
		exprs = append(exprs, e.Pipeline)
	}

	return exprs
}

type Grouping struct {
	Without bool
	Groups  []string
}

func (g Grouping) String() string {
	var sb strings.Builder
	if g.Without {
		sb.WriteString(" without")
	} else if len(g.Groups) > 0 {
		sb.WriteString(" by")
	}

	if len(g.Groups) > 0 {
		sb.WriteString("(")
		sb.WriteString(strings.Join(g.Groups, ","))
		sb.WriteString(")")
	}

	return sb.String()
}

type LogRange struct {
	Left     Expr
	Interval time.Duration

	Unwrap *UnwrapExpr
}

func (e *LogRange) Sub() []Expr {
	if e.Left != nil {
		return []Expr{e.Left}
	}
	return []Expr{}
}

// impls Stringer
func (e LogRange) String() string {
	var sb strings.Builder
	sb.WriteString(e.Left.String())
	if e.Unwrap != nil {
		sb.WriteString(e.Unwrap.String())
	}
	sb.WriteString(fmt.Sprintf("[%v]", model.Duration(e.Interval)))
	return sb.String()
}

func newLogRange(left Expr, interval time.Duration, u *UnwrapExpr) *LogRange {
	return &LogRange{
		Left:     left,
		Interval: interval,
		Unwrap:   u,
	}
}

type LiteralExpr struct {
	Value float64
}

func mustNewLiteralExpr(s string, invert bool) *LiteralExpr {
	n, err := strconv.ParseFloat(s, 64)
	if err != nil {
		panic(newParseError(fmt.Sprintf("unable to parse literal as a float: %s", err.Error()), 0, 0))
	}

	if invert {
		n = -n
	}

	return &LiteralExpr{
		Value: n,
	}
}

func (e *LiteralExpr) String() string {
	return fmt.Sprint(e.Value)
}

func (e *LiteralExpr) Sub() []Expr {
	return []Expr{}
}

type LabelParserExpr struct {
	Op    string
	Param string
}

func newLabelParserExpr(op, param string) *LabelParserExpr {
	return &LabelParserExpr{
		Op:    op,
		Param: param,
	}
}

func (e *LabelParserExpr) Sub() []Expr {
	return []Expr{}
}

func (e *LabelParserExpr) String() string {
	var sb strings.Builder
	sb.WriteString(OpPipe)
	sb.WriteString(" ")
	sb.WriteString(e.Op)
	if e.Param != "" {
		sb.WriteString(" ")
		sb.WriteString(strconv.Quote(e.Param))
	}
	return sb.String()
}

type LabelFilterExpr struct {
	log.LabelFilterer
}

func (e *LabelFilterExpr) String() string {
	return fmt.Sprintf("%s %s", OpPipe, e.LabelFilterer.String())
}

func (e LabelFilterExpr) Sub() []Expr {
	return []Expr{}
}

type MultiStageExpr struct {
	Stages []Expr
}

func (m MultiStageExpr) Sub() []Expr {
	return m.Stages
}

func (m MultiStageExpr) String() string {
	var sb strings.Builder
	for i, e := range m.Stages {
		sb.WriteString(e.String())
		if i+1 != len(m.Stages) {
			sb.WriteString(" ")
		}
	}
	return sb.String()
}

type LineFmtExpr struct {
	Value string
}

func (e *LineFmtExpr) String() string {
	return fmt.Sprintf("%s %s %s", OpPipe, OpFmtLine, strconv.Quote(e.Value))
}

func (e *LineFmtExpr) Sub() []Expr {
	return []Expr{}
}

func newLineFmtExpr(value string) *LineFmtExpr {
	return &LineFmtExpr{
		Value: value,
	}
}

type LabelFmtExpr struct {
	Formats []log.LabelFmt
}

func (e *LabelFmtExpr) String() string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("%s %s ", OpPipe, OpFmtLabel))
	for i, f := range e.Formats {
		sb.WriteString(f.Name)
		sb.WriteString("=")
		if f.Rename {
			sb.WriteString(f.Value)
		} else {
			sb.WriteString(strconv.Quote(f.Value))
		}
		if i+1 != len(e.Formats) {
			sb.WriteString(",")
		}
	}
	return sb.String()
}

func (e *LabelFmtExpr) Sub() []Expr {
	return []Expr{}
}

func newLabelFmtExpr(fmts []log.LabelFmt) *LabelFmtExpr {
	return &LabelFmtExpr{
		Formats: fmts,
	}
}

type UnwrapExpr struct {
	Identifier string
	Operation  string

	PostFilters []log.LabelFilterer
}

func newUnwrapExpr(id string, operation string) *UnwrapExpr {
	return &UnwrapExpr{Identifier: id, Operation: operation}
}

func (e *UnwrapExpr) addPostFilter(f log.LabelFilterer) *UnwrapExpr {
	e.PostFilters = append(e.PostFilters, f)
	return e
}

func (e *UnwrapExpr) String() string {
	var sb strings.Builder
	if e.Operation != "" {
		sb.WriteString(fmt.Sprintf(" %s %s %s(%s)", OpPipe, OpUnwrap, e.Operation, e.Identifier))
	} else {
		sb.WriteString(fmt.Sprintf(" %s %s %s", OpPipe, OpUnwrap, e.Identifier))
	}
	for _, f := range e.PostFilters {
		sb.WriteString(fmt.Sprintf(" %s %s", OpPipe, f))
	}
	return sb.String()
}

func (e *UnwrapExpr) Sub() []Expr {
	return []Expr{}
}

type BinOpOptions struct {
	ReturnBool bool
}

type BinOpExpr struct {
	Left, Right Expr
	Op          string
	Opts        BinOpOptions
}

func mustNewBinOpExpr(op string, opts BinOpOptions, left, right Expr) *BinOpExpr {
	return &BinOpExpr{
		Left:  left,
		Right: right,
		Op:    op,
		Opts:  opts,
	}
}

func (e *BinOpExpr) String() string {
	if e.Opts.ReturnBool {
		return fmt.Sprintf("%s %s bool %s", e.Left.String(), e.Op, e.Right.String())
	}
	return fmt.Sprintf("%s %s %s", e.Left.String(), e.Op, e.Right.String())
}

func (e *BinOpExpr) Sub() []Expr {
	exprs := make([]Expr, 0, 2)
	if e.Left != nil {
		exprs = append(exprs, e.Left)
	}
	if e.Right != nil {
		exprs = append(exprs, e.Right)
	}

	return exprs
}

func mustNewFloat(s string) float64 {
	n, err := strconv.ParseFloat(s, 64)
	if err != nil {
		panic(newParseError(fmt.Sprintf("unable to parse float: %s", err.Error()), 0, 0))
	}
	return n
}

func mustNewMatcher(t labels.MatchType, n, v string) *labels.Matcher {
	m, err := labels.NewMatcher(t, n, v)
	if err != nil {
		panic(newParseError(err.Error(), 0, 0))
	}
	return m
}

// nolint:interfacer
func formatOperation(op string, grouping *Grouping, params ...string) string {
	nonEmptyParams := make([]string, 0, len(params))
	for _, p := range params {
		if p != "" {
			nonEmptyParams = append(nonEmptyParams, p)
		}
	}

	var sb strings.Builder
	sb.WriteString(op)
	if grouping != nil {
		sb.WriteString(grouping.String())
	}
	sb.WriteString("(")
	sb.WriteString(strings.Join(nonEmptyParams, ","))
	sb.WriteString(")")
	return sb.String()
}

type VectorAggregationExpr struct {
	Left SampleExpr

	Grouping  *Grouping
	Params    int
	Operation string
}

func (e *VectorAggregationExpr) String() string {
	var params []string
	if e.Params != 0 {
		params = []string{fmt.Sprintf("%d", e.Params), e.Left.String()}
	} else {
		params = []string{e.Left.String()}
	}
	return formatOperation(e.Operation, e.Grouping, params...)
}

func (e *VectorAggregationExpr) Sub() []Expr {
	if e.Left != nil {
		return []Expr{e.Left}
	}
	return []Expr{}
}

func mustNewVectorAggregationExpr(left SampleExpr, operation string, gr *Grouping, params *string) *VectorAggregationExpr {
	var p int
	var err error
	switch operation {
	case OpTypeBottomK, OpTypeTopK:
		if params == nil {
			panic(newParseError(fmt.Sprintf("parameter required for operation %s", operation), 0, 0))
		}
		if p, err = strconv.Atoi(*params); err != nil {
			panic(newParseError(fmt.Sprintf("invalid parameter %s(%s,", operation, *params), 0, 0))
		}

	default:
		if params != nil {
			panic(newParseError(fmt.Sprintf("unsupported parameter for operation %s(%s,", operation, *params), 0, 0))
		}
	}
	if gr == nil {
		gr = &Grouping{}
	}
	return &VectorAggregationExpr{
		Left:      left,
		Operation: operation,
		Grouping:  gr,
		Params:    p,
	}
}

type LabelReplaceExpr struct {
	Left        SampleExpr
	Dst         string
	Replacement string
	Src         string
	Regex       string
	Re          *regexp.Regexp
}

func mustNewLabelReplaceExpr(left SampleExpr, dst, replacement, src, regex string) *LabelReplaceExpr {
	re, err := regexp.Compile("^(?:" + regex + ")$")
	if err != nil {
		panic(newParseError(fmt.Sprintf("invalid regex in label_replace: %s", err.Error()), 0, 0))
	}
	return &LabelReplaceExpr{
		Left:        left,
		Dst:         dst,
		Replacement: replacement,
		Src:         src,
		Re:          re,
		Regex:       regex,
	}
}

func (e *LabelReplaceExpr) Sub() []Expr {
	if e.Left != nil {
		return []Expr{e.Left}
	}
	return []Expr{}
}

func (e *LabelReplaceExpr) String() string {
	var sb strings.Builder
	sb.WriteString(OpLabelReplace)
	sb.WriteString("(")
	sb.WriteString(e.Left.String())
	sb.WriteString(",")
	sb.WriteString(strconv.Quote(e.Dst))
	sb.WriteString(",")
	sb.WriteString(strconv.Quote(e.Replacement))
	sb.WriteString(",")
	sb.WriteString(strconv.Quote(e.Src))
	sb.WriteString(",")
	sb.WriteString(strconv.Quote(e.Regex))
	sb.WriteString(")")
	return sb.String()
}

type RangeAggregationExpr struct {
	Left      *LogRange
	Operation string

	Params   *float64
	Grouping *Grouping
}

func newRangeAggregationExpr(left *LogRange, operation string, gr *Grouping, stringParams *string) *RangeAggregationExpr {
	var params *float64
	if stringParams != nil {
		if operation != OpRangeTypeQuantile {
			panic(newParseError(fmt.Sprintf("parameter %s not supported for operation %s", *stringParams, operation), 0, 0))
		}
		var err error
		params = new(float64)
		*params, err = strconv.ParseFloat(*stringParams, 64)
		if err != nil {
			panic(newParseError(fmt.Sprintf("invalid parameter for operation %s: %s", operation, err), 0, 0))
		}

	} else {
		if operation == OpRangeTypeQuantile {
			panic(newParseError(fmt.Sprintf("parameter required for operation %s", operation), 0, 0))
		}
	}
	e := &RangeAggregationExpr{
		Left:      left,
		Operation: operation,
		Grouping:  gr,
		Params:    params,
	}
	if err := e.validate(); err != nil {
		panic(newParseError(err.Error(), 0, 0))
	}
	return e
}

func (e RangeAggregationExpr) validate() error {
	if e.Grouping != nil {
		switch e.Operation {
		case OpRangeTypeAvg, OpRangeTypeStddev, OpRangeTypeStdvar, OpRangeTypeQuantile, OpRangeTypeMax, OpRangeTypeMin:
		default:
			return fmt.Errorf("grouping not allowed for %s aggregation", e.Operation)
		}
	}
	if e.Left.Unwrap != nil {
		switch e.Operation {
		case OpRangeTypeRate, OpRangeTypeAvg, OpRangeTypeSum, OpRangeTypeMax, OpRangeTypeMin, OpRangeTypeStddev, OpRangeTypeStdvar, OpRangeTypeQuantile, OpRangeTypeAbsent:
			return nil
		default:
			return fmt.Errorf("invalid aggregation %s with unwrap", e.Operation)
		}
	}
	switch e.Operation {
	case OpRangeTypeBytes, OpRangeTypeBytesRate, OpRangeTypeCount, OpRangeTypeRate, OpRangeTypeAbsent:
		return nil
	default:
		return fmt.Errorf("invalid aggregation %s without unwrap", e.Operation)
	}
}

func (e *RangeAggregationExpr) Sub() []Expr {
	if e.Left != nil {
		return []Expr{e.Left}
	}
	return []Expr{}
}

func (e *RangeAggregationExpr) String() string {
	var sb strings.Builder
	sb.WriteString(e.Operation)
	sb.WriteString("(")
	if e.Params != nil {
		sb.WriteString(strconv.FormatFloat(*e.Params, 'f', -1, 64))
		sb.WriteString(",")
	}
	sb.WriteString(e.Left.String())
	sb.WriteString(")")
	if e.Grouping != nil {
		sb.WriteString(e.Grouping.String())
	}
	return sb.String()
}

type LineFilterExpr struct {
	Left  *LineFilterExpr
	Type  labels.MatchType
	Match string
}

func newLineFilterExpr(left *LineFilterExpr, ty labels.MatchType, match string) *LineFilterExpr {
	return &LineFilterExpr{
		Left:  left,
		Type:  ty,
		Match: match,
	}
}

func (e *LineFilterExpr) Sub() []Expr {
	if e.Left != nil {
		return []Expr{e.Left}
	}
	return []Expr{}
}

func (e *LineFilterExpr) String() string {
	var sb strings.Builder
	if e.Left != nil {
		sb.WriteString(e.Left.String())
		sb.WriteString(" ")
	}
	switch e.Type {
	case labels.MatchRegexp:
		sb.WriteString("|~")
	case labels.MatchNotRegexp:
		sb.WriteString("!~")
	case labels.MatchEqual:
		sb.WriteString("|=")
	case labels.MatchNotEqual:
		sb.WriteString("!=")
	}
	sb.WriteString(" ")
	sb.WriteString(strconv.Quote(e.Match))
	return sb.String()
}
