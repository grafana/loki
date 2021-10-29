package logql

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/prometheus/prometheus/promql"

	"github.com/grafana/loki/pkg/iter"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logql/log"
	"github.com/grafana/loki/pkg/logqlmodel"
)

// Expr is the root expression which can be a SampleExpr or LogSelectorExpr
type Expr interface {
	logQLExpr()      // ensure it's not implemented accidentally
	Shardable() bool // A recursive check on the AST to see if it's shardable.
	Walkable
	fmt.Stringer
}

type QueryParams interface {
	LogSelector() (LogSelectorExpr, error)
	GetStart() time.Time
	GetEnd() time.Time
	GetShards() []string
}

// implicit holds default implementations
type implicit struct{}

func (implicit) logQLExpr() {}

// SelectParams specifies parameters passed to data selections.
type SelectLogParams struct {
	*logproto.QueryRequest
}

func (s SelectLogParams) String() string {
	if s.QueryRequest != nil {
		return fmt.Sprintf("selector=%s, direction=%s, start=%s, end=%s, limit=%d, shards=%s",
			s.Selector, logproto.Direction_name[int32(s.Direction)], s.Start, s.End, s.Limit, strings.Join(s.Shards, ","))
	}
	return ""
}

// LogSelector returns the LogSelectorExpr from the SelectParams.
// The `LogSelectorExpr` can then returns all matchers and filters to use for that request.
func (s SelectLogParams) LogSelector() (LogSelectorExpr, error) {
	return ParseLogSelector(s.Selector, true)
}

type SelectSampleParams struct {
	*logproto.SampleQueryRequest
}

// Expr returns the SampleExpr from the SelectSampleParams.
// The `LogSelectorExpr` can then returns all matchers and filters to use for that request.
func (s SelectSampleParams) Expr() (SampleExpr, error) {
	return ParseSampleExpr(s.Selector)
}

// LogSelector returns the LogSelectorExpr from the SelectParams.
// The `LogSelectorExpr` can then returns all matchers and filters to use for that request.
func (s SelectSampleParams) LogSelector() (LogSelectorExpr, error) {
	expr, err := ParseSampleExpr(s.Selector)
	if err != nil {
		return nil, err
	}
	return expr.Selector(), nil
}

// Querier allows a LogQL expression to fetch an EntryIterator for a
// set of matchers and filters
type Querier interface {
	SelectLogs(context.Context, SelectLogParams) (iter.EntryIterator, error)
	SelectSamples(context.Context, SelectSampleParams) (iter.SampleIterator, error)
}

// LogSelectorExpr is a LogQL expression filtering and returning logs.
type LogSelectorExpr interface {
	Matchers() []*labels.Matcher
	LogPipelineExpr
	HasFilter() bool
	Expr
}

// Type alias for backward compatibility
type (
	Pipeline        = log.Pipeline
	SampleExtractor = log.SampleExtractor
)

// LogPipelineExpr is an expression defining a log pipeline.
type LogPipelineExpr interface {
	Pipeline() (Pipeline, error)
	Expr
}

// StageExpr is an expression defining a single step into a log pipeline
type StageExpr interface {
	Stage() (log.Stage, error)
	Expr
}

// MultiStageExpr is multiple stages which implement a PipelineExpr.
type MultiStageExpr []StageExpr

func (m MultiStageExpr) Pipeline() (log.Pipeline, error) {
	stages, err := m.stages()
	if err != nil {
		return nil, err
	}
	return log.NewPipeline(stages), nil
}

func (m MultiStageExpr) stages() ([]log.Stage, error) {
	c := make([]log.Stage, 0, len(m))
	for _, e := range m {
		p, err := e.Stage()
		if err != nil {
			return nil, logqlmodel.NewStageError(e.String(), err)
		}
		if p == log.NoopStage {
			continue
		}
		c = append(c, p)
	}
	return c, nil
}

func (m MultiStageExpr) String() string {
	var sb strings.Builder
	for i, e := range m {
		sb.WriteString(e.String())
		if i+1 != len(m) {
			sb.WriteString(" ")
		}
	}
	return sb.String()
}

func (MultiStageExpr) logQLExpr() {} // nolint:unused

type MatchersExpr struct {
	matchers []*labels.Matcher
	implicit
}

func newMatcherExpr(matchers []*labels.Matcher) *MatchersExpr {
	return &MatchersExpr{matchers: matchers}
}

func (e *MatchersExpr) Matchers() []*labels.Matcher {
	return e.matchers
}

func (e *MatchersExpr) AppendMatchers(m []*labels.Matcher) {
	e.matchers = append(e.matchers, m...)
}

func (e *MatchersExpr) Shardable() bool { return true }

func (e *MatchersExpr) Walk(f WalkFn) { f(e) }

func (e *MatchersExpr) String() string {
	var sb strings.Builder
	sb.WriteString("{")
	for i, m := range e.matchers {
		sb.WriteString(m.String())
		if i+1 != len(e.matchers) {
			sb.WriteString(", ")
		}
	}
	sb.WriteString("}")
	return sb.String()
}

func (e *MatchersExpr) Pipeline() (log.Pipeline, error) {
	return log.NewNoopPipeline(), nil
}

func (e *MatchersExpr) HasFilter() bool {
	return false
}

type PipelineExpr struct {
	MultiStages MultiStageExpr
	Left        *MatchersExpr
	implicit
}

func newPipelineExpr(left *MatchersExpr, pipeline MultiStageExpr) LogSelectorExpr {
	return &PipelineExpr{
		Left:        left,
		MultiStages: pipeline,
	}
}

func (e *PipelineExpr) Shardable() bool {
	for _, p := range e.MultiStages {
		if !p.Shardable() {
			return false
		}
	}
	return true
}

func (e *PipelineExpr) Walk(f WalkFn) {
	f(e)

	if e.Left == nil {
		return
	}

	xs := make([]Walkable, 0, len(e.MultiStages)+1)
	xs = append(xs, e.Left)
	for _, p := range e.MultiStages {
		xs = append(xs, p)
	}
	walkAll(f, xs...)
}

func (e *PipelineExpr) Matchers() []*labels.Matcher {
	return e.Left.Matchers()
}

func (e *PipelineExpr) String() string {
	var sb strings.Builder
	sb.WriteString(e.Left.String())
	sb.WriteString(" ")
	sb.WriteString(e.MultiStages.String())
	return sb.String()
}

func (e *PipelineExpr) Pipeline() (log.Pipeline, error) {
	return e.MultiStages.Pipeline()
}

// HasFilter returns true if the pipeline contains stage that can filter out lines.
func (e *PipelineExpr) HasFilter() bool {
	for _, p := range e.MultiStages {
		switch p.(type) {
		case *LineFilterExpr, *LabelFilterExpr:
			return true
		default:
			continue
		}
	}
	return false
}

type LineFilterExpr struct {
	Left  *LineFilterExpr
	Ty    labels.MatchType
	Match string
	Op    string
	implicit
}

func newLineFilterExpr(ty labels.MatchType, op, match string) *LineFilterExpr {
	return &LineFilterExpr{
		Ty:    ty,
		Match: match,
		Op:    op,
	}
}

func newNestedLineFilterExpr(left *LineFilterExpr, right *LineFilterExpr) *LineFilterExpr {
	return &LineFilterExpr{
		Left:  left,
		Ty:    right.Ty,
		Match: right.Match,
		Op:    right.Op,
	}
}

func (e *LineFilterExpr) Walk(f WalkFn) {
	f(e)
	if e.Left == nil {
		return
	}
	e.Left.Walk(f)
}

// AddFilterExpr adds a filter expression to a logselector expression.
func AddFilterExpr(expr LogSelectorExpr, ty labels.MatchType, op, match string) (LogSelectorExpr, error) {
	filter := newLineFilterExpr(ty, op, match)
	switch e := expr.(type) {
	case *MatchersExpr:
		return newPipelineExpr(e, MultiStageExpr{filter}), nil
	case *PipelineExpr:
		e.MultiStages = append(e.MultiStages, filter)
		return e, nil
	default:
		return nil, fmt.Errorf("unknown LogSelector: %v+", expr)
	}
}

func (e *LineFilterExpr) Shardable() bool { return true }

func (e *LineFilterExpr) String() string {
	var sb strings.Builder
	if e.Left != nil {
		sb.WriteString(e.Left.String())
		sb.WriteString(" ")
	}
	switch e.Ty {
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
	if e.Op == "" {
		sb.WriteString(strconv.Quote(e.Match))
		return sb.String()
	}
	sb.WriteString(e.Op)
	sb.WriteString("(")
	sb.WriteString(strconv.Quote(e.Match))
	sb.WriteString(")")
	return sb.String()
}

func (e *LineFilterExpr) Filter() (log.Filterer, error) {
	var f log.Filterer

	switch e.Op {
	case OpFilterIP:
		var err error
		f, err = log.NewIPLineFilter(e.Match, e.Ty)
		if err != nil {
			return nil, err
		}
	default:
		var err error // to avoid `f` being shadowed.
		f, err = log.NewFilter(e.Match, e.Ty)
		if err != nil {
			return nil, err
		}
	}

	if e.Left != nil {
		nextFilter, err := e.Left.Filter()
		if err != nil {
			return nil, err
		}
		if nextFilter != nil {
			f = log.NewAndFilter(nextFilter, f)
		}
	}

	return f, nil
}

func (e *LineFilterExpr) Stage() (log.Stage, error) {
	f, err := e.Filter()
	if err != nil {
		return nil, err
	}
	return f.ToStage(), nil
}

type LabelParserExpr struct {
	Op    string
	Param string
	implicit
}

func newLabelParserExpr(op, param string) *LabelParserExpr {
	return &LabelParserExpr{
		Op:    op,
		Param: param,
	}
}

func (e *LabelParserExpr) Shardable() bool { return true }

func (e *LabelParserExpr) Walk(f WalkFn) { f(e) }

func (e *LabelParserExpr) Stage() (log.Stage, error) {
	switch e.Op {
	case OpParserTypeJSON:
		return log.NewJSONParser(), nil
	case OpParserTypeLogfmt:
		return log.NewLogfmtParser(), nil
	case OpParserTypeRegexp:
		return log.NewRegexpParser(e.Param)
	case OpParserTypeUnpack:
		return log.NewUnpackParser(), nil
	case OpParserTypePattern:
		return log.NewPatternParser(e.Param)
	default:
		return nil, fmt.Errorf("unknown parser operator: %s", e.Op)
	}
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
	implicit
}

func newLabelFilterExpr(filterer log.LabelFilterer) *LabelFilterExpr {
	return &LabelFilterExpr{
		LabelFilterer: filterer,
	}
}

func (e *LabelFilterExpr) Shardable() bool { return true }

func (e *LabelFilterExpr) Walk(f WalkFn) { f(e) }

func (e *LabelFilterExpr) Stage() (log.Stage, error) {
	switch ip := e.LabelFilterer.(type) {
	case *log.IPLabelFilter:
		return ip, ip.PatternError()
	}
	return e.LabelFilterer, nil
}

func (e *LabelFilterExpr) String() string {
	return fmt.Sprintf("%s %s", OpPipe, e.LabelFilterer.String())
}

type LineFmtExpr struct {
	Value string
	implicit
}

func newLineFmtExpr(value string) *LineFmtExpr {
	return &LineFmtExpr{
		Value: value,
	}
}

func (e *LineFmtExpr) Shardable() bool { return true }

func (e *LineFmtExpr) Walk(f WalkFn) { f(e) }

func (e *LineFmtExpr) Stage() (log.Stage, error) {
	return log.NewFormatter(e.Value)
}

func (e *LineFmtExpr) String() string {
	return fmt.Sprintf("%s %s %s", OpPipe, OpFmtLine, strconv.Quote(e.Value))
}

type LabelFmtExpr struct {
	Formats []log.LabelFmt

	implicit
}

func newLabelFmtExpr(fmts []log.LabelFmt) *LabelFmtExpr {
	return &LabelFmtExpr{
		Formats: fmts,
	}
}

func (e *LabelFmtExpr) Shardable() bool { return false }

func (e *LabelFmtExpr) Walk(f WalkFn) { f(e) }

func (e *LabelFmtExpr) Stage() (log.Stage, error) {
	return log.NewLabelsFormatter(e.Formats)
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

type JSONExpressionParser struct {
	Expressions []log.JSONExpression

	implicit
}

func newJSONExpressionParser(expressions []log.JSONExpression) *JSONExpressionParser {
	return &JSONExpressionParser{
		Expressions: expressions,
	}
}

func (j *JSONExpressionParser) Shardable() bool { return true }

func (j *JSONExpressionParser) Walk(f WalkFn) { f(j) }

func (j *JSONExpressionParser) Stage() (log.Stage, error) {
	return log.NewJSONExpressionParser(j.Expressions)
}

func (j *JSONExpressionParser) String() string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("%s %s ", OpPipe, OpParserTypeJSON))
	for i, exp := range j.Expressions {
		sb.WriteString(exp.Identifier)
		sb.WriteString("=")
		sb.WriteString(strconv.Quote(exp.Expression))

		if i+1 != len(j.Expressions) {
			sb.WriteString(",")
		}
	}
	return sb.String()
}

func mustNewMatcher(t labels.MatchType, n, v string) *labels.Matcher {
	m, err := labels.NewMatcher(t, n, v)
	if err != nil {
		panic(logqlmodel.NewParseError(err.Error(), 0, 0))
	}
	return m
}

func mustNewFloat(s string) float64 {
	n, err := strconv.ParseFloat(s, 64)
	if err != nil {
		panic(logqlmodel.NewParseError(fmt.Sprintf("unable to parse float: %s", err.Error()), 0, 0))
	}
	return n
}

type UnwrapExpr struct {
	Identifier string
	Operation  string

	PostFilters []log.LabelFilterer
}

func (u UnwrapExpr) String() string {
	var sb strings.Builder
	if u.Operation != "" {
		sb.WriteString(fmt.Sprintf(" %s %s %s(%s)", OpPipe, OpUnwrap, u.Operation, u.Identifier))
	} else {
		sb.WriteString(fmt.Sprintf(" %s %s %s", OpPipe, OpUnwrap, u.Identifier))
	}
	for _, f := range u.PostFilters {
		sb.WriteString(fmt.Sprintf(" %s %s", OpPipe, f))
	}
	return sb.String()
}

func (u *UnwrapExpr) addPostFilter(f log.LabelFilterer) *UnwrapExpr {
	u.PostFilters = append(u.PostFilters, f)
	return u
}

func newUnwrapExpr(id string, operation string) *UnwrapExpr {
	return &UnwrapExpr{Identifier: id, Operation: operation}
}

type LogRange struct {
	Left     LogSelectorExpr
	Interval time.Duration
	Offset   time.Duration

	Unwrap *UnwrapExpr

	implicit
}

// impls Stringer
func (r LogRange) String() string {
	var sb strings.Builder
	sb.WriteString(r.Left.String())
	if r.Unwrap != nil {
		sb.WriteString(r.Unwrap.String())
	}
	sb.WriteString(fmt.Sprintf("[%v]", model.Duration(r.Interval)))
	if r.Offset != 0 {
		offsetExpr := OffsetExpr{Offset: r.Offset}
		sb.WriteString(offsetExpr.String())
	}
	return sb.String()
}

func (r *LogRange) Shardable() bool { return r.Left.Shardable() }

func (r *LogRange) Walk(f WalkFn) {
	f(r)
	if r.Left == nil {
		return
	}
	r.Left.Walk(f)
}

func newLogRange(left LogSelectorExpr, interval time.Duration, u *UnwrapExpr, o *OffsetExpr) *LogRange {
	var offset time.Duration
	if o != nil {
		offset = o.Offset
	}
	return &LogRange{
		Left:     left,
		Interval: interval,
		Unwrap:   u,
		Offset:   offset,
	}
}

type OffsetExpr struct {
	Offset time.Duration
}

func (o *OffsetExpr) String() string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf(" %s %s", OpOffset, o.Offset.String()))
	return sb.String()
}

func newOffsetExpr(offset time.Duration) *OffsetExpr {
	return &OffsetExpr{
		Offset: offset,
	}
}

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
	OpRangeTypeFirst     = "first_over_time"
	OpRangeTypeLast      = "last_over_time"
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
	OpParserTypeJSON    = "json"
	OpParserTypeLogfmt  = "logfmt"
	OpParserTypeRegexp  = "regexp"
	OpParserTypeUnpack  = "unpack"
	OpParserTypePattern = "pattern"

	OpFmtLine  = "line_format"
	OpFmtLabel = "label_format"

	OpPipe   = "|"
	OpUnwrap = "unwrap"
	OpOffset = "offset"

	OpOn       = "on"
	OpIgnoring = "ignoring"

	OpGroupLeft  = "group_left"
	OpGroupRight = "group_right"

	// conversion Op
	OpConvBytes           = "bytes"
	OpConvDuration        = "duration"
	OpConvDurationSeconds = "duration_seconds"

	OpLabelReplace = "label_replace"

	// function filters
	OpFilterIP = "ip"
)

func IsComparisonOperator(op string) bool {
	switch op {
	case OpTypeCmpEQ, OpTypeNEQ, OpTypeGT, OpTypeGTE, OpTypeLT, OpTypeLTE:
		return true
	default:
		return false
	}
}

// IsLogicalBinOp tests whether an operation is a logical/set binary operation
func IsLogicalBinOp(op string) bool {
	switch op {
	case OpTypeOr, OpTypeAnd, OpTypeUnless:
		return true
	default:
		return false
	}
}

// SampleExpr is a LogQL expression filtering logs and returning metric samples.
type SampleExpr interface {
	// Selector is the LogQL selector to apply when retrieving logs.
	Selector() LogSelectorExpr
	Extractor() (SampleExtractor, error)
	Expr
}

type RangeAggregationExpr struct {
	Left      *LogRange
	Operation string

	Params   *float64
	Grouping *Grouping
	implicit
}

func newRangeAggregationExpr(left *LogRange, operation string, gr *Grouping, stringParams *string) SampleExpr {
	var params *float64
	if stringParams != nil {
		if operation != OpRangeTypeQuantile {
			panic(logqlmodel.NewParseError(fmt.Sprintf("parameter %s not supported for operation %s", *stringParams, operation), 0, 0))
		}
		var err error
		params = new(float64)
		*params, err = strconv.ParseFloat(*stringParams, 64)
		if err != nil {
			panic(logqlmodel.NewParseError(fmt.Sprintf("invalid parameter for operation %s: %s", operation, err), 0, 0))
		}

	} else {
		if operation == OpRangeTypeQuantile {
			panic(logqlmodel.NewParseError(fmt.Sprintf("parameter required for operation %s", operation), 0, 0))
		}
	}
	e := &RangeAggregationExpr{
		Left:      left,
		Operation: operation,
		Grouping:  gr,
		Params:    params,
	}
	if err := e.validate(); err != nil {
		panic(logqlmodel.NewParseError(err.Error(), 0, 0))
	}
	return e
}

func (e *RangeAggregationExpr) Selector() LogSelectorExpr {
	return e.Left.Left
}

func (e RangeAggregationExpr) validate() error {
	if e.Grouping != nil {
		switch e.Operation {
		case OpRangeTypeAvg, OpRangeTypeStddev, OpRangeTypeStdvar, OpRangeTypeQuantile, OpRangeTypeMax, OpRangeTypeMin, OpRangeTypeFirst, OpRangeTypeLast:
		default:
			return fmt.Errorf("grouping not allowed for %s aggregation", e.Operation)
		}
	}
	if e.Left.Unwrap != nil {
		switch e.Operation {
		case OpRangeTypeAvg, OpRangeTypeSum, OpRangeTypeMax, OpRangeTypeMin, OpRangeTypeStddev, OpRangeTypeStdvar, OpRangeTypeQuantile, OpRangeTypeRate, OpRangeTypeAbsent, OpRangeTypeFirst, OpRangeTypeLast:
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

// impls Stringer
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

// impl SampleExpr
func (e *RangeAggregationExpr) Shardable() bool {
	return shardableOps[e.Operation] && e.Left.Shardable()
}

func (e *RangeAggregationExpr) Walk(f WalkFn) {
	f(e)
	if e.Left == nil {
		return
	}
	e.Left.Walk(f)
}

type Grouping struct {
	Groups  []string
	Without bool
}

// impls Stringer
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

type VectorAggregationExpr struct {
	Left SampleExpr

	Grouping  *Grouping
	Params    int
	Operation string
	implicit
}

func mustNewVectorAggregationExpr(left SampleExpr, operation string, gr *Grouping, params *string) SampleExpr {
	var p int
	var err error
	switch operation {
	case OpTypeBottomK, OpTypeTopK:
		if params == nil {
			panic(logqlmodel.NewParseError(fmt.Sprintf("parameter required for operation %s", operation), 0, 0))
		}
		if p, err = strconv.Atoi(*params); err != nil {
			panic(logqlmodel.NewParseError(fmt.Sprintf("invalid parameter %s(%s,", operation, *params), 0, 0))
		}

	default:
		if params != nil {
			panic(logqlmodel.NewParseError(fmt.Sprintf("unsupported parameter for operation %s(%s,", operation, *params), 0, 0))
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

func (e *VectorAggregationExpr) Selector() LogSelectorExpr {
	return e.Left.Selector()
}

func (e *VectorAggregationExpr) Extractor() (log.SampleExtractor, error) {
	// inject in the range vector extractor the outer groups to improve performance.
	// This is only possible if the operation is a sum. Anything else needs all labels.
	if r, ok := e.Left.(*RangeAggregationExpr); ok && canInjectVectorGrouping(e.Operation, r.Operation) {
		// if the range vec operation has no grouping we can push down the vec one.
		if r.Grouping == nil {
			return r.extractor(e.Grouping)
		}
	}
	return e.Left.Extractor()
}

// canInjectVectorGrouping tells if a vector operation can inject grouping into the nested range vector.
func canInjectVectorGrouping(vecOp, rangeOp string) bool {
	if vecOp != OpTypeSum {
		return false
	}
	switch rangeOp {
	case OpRangeTypeBytes, OpRangeTypeBytesRate, OpRangeTypeSum, OpRangeTypeRate, OpRangeTypeCount:
		return true
	default:
		return false
	}
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

// impl SampleExpr
func (e *VectorAggregationExpr) Shardable() bool {
	return shardableOps[e.Operation] && e.Left.Shardable()
}

func (e *VectorAggregationExpr) Walk(f WalkFn) {
	f(e)
	if e.Left == nil {
		return
	}
	e.Left.Walk(f)
}

// VectorMatchCardinality describes the cardinality relationship
// of two Vectors in a binary operation.
type VectorMatchCardinality int

const (
	CardOneToOne VectorMatchCardinality = iota
	CardManyToOne
	CardOneToMany
)

func (vmc VectorMatchCardinality) String() string {
	switch vmc {
	case CardOneToOne:
		return "one-to-one"
	case CardManyToOne:
		return "many-to-one"
	case CardOneToMany:
		return "one-to-many"
	}
	panic("promql.VectorMatchCardinality.String: unknown match cardinality")
}

// VectorMatching describes how elements from two Vectors in a binary
// operation are supposed to be matched.
type VectorMatching struct {
	// The cardinality of the two Vectors.
	Card VectorMatchCardinality
	// MatchingLabels contains the labels which define equality of a pair of
	// elements from the Vectors.
	MatchingLabels []string
	// On includes the given label names from matching,
	// rather than excluding them.
	On bool
	// Include contains additional labels that should be included in
	// the result from the side with the lower cardinality.
	Include []string
}

type BinOpOptions struct {
	ReturnBool     bool
	VectorMatching *VectorMatching
}

type BinOpExpr struct {
	SampleExpr
	RHS  SampleExpr
	Op   string
	Opts *BinOpOptions
}

func (e *BinOpExpr) String() string {
	op := e.Op
	if e.Opts != nil {
		if e.Opts.ReturnBool {
			op = fmt.Sprintf("%s bool", op)
		}
		if e.Opts.VectorMatching != nil {
			group := ""
			if e.Opts.VectorMatching.Card == CardManyToOne {
				group = OpGroupLeft
			} else if e.Opts.VectorMatching.Card == CardOneToMany {
				group = OpGroupRight
			}
			if e.Opts.VectorMatching.Include != nil {
				group = fmt.Sprintf("%s (%s)", group, strings.Join(e.Opts.VectorMatching.Include, ","))
			}

			if e.Opts.VectorMatching.On || e.Opts.VectorMatching.MatchingLabels != nil {
				on := OpOn
				if !e.Opts.VectorMatching.On {
					on = OpIgnoring
				}
				op = fmt.Sprintf("%s %s (%s) %s", op, on, strings.Join(e.Opts.VectorMatching.MatchingLabels, ","), group)
			}
		}
	}
	return fmt.Sprintf("(%s %s %s)", e.SampleExpr.String(), op, e.RHS.String())
}

// impl SampleExpr
func (e *BinOpExpr) Shardable() bool {
	if e.Opts != nil && e.Opts.VectorMatching != nil {
		// prohibit sharding when we're changing the label groupings, such as on or ignoring
		return false
	}
	return shardableOps[e.Op] && e.SampleExpr.Shardable() && e.RHS.Shardable()
}

func (e *BinOpExpr) Walk(f WalkFn) {
	walkAll(f, e.SampleExpr, e.RHS)
}

func mustNewBinOpExpr(op string, opts *BinOpOptions, lhs, rhs Expr) SampleExpr {
	left, ok := lhs.(SampleExpr)
	if !ok {
		panic(logqlmodel.NewParseError(fmt.Sprintf(
			"unexpected type for left leg of binary operation (%s): %T",
			op,
			lhs,
		), 0, 0))
	}

	right, ok := rhs.(SampleExpr)
	if !ok {
		panic(logqlmodel.NewParseError(fmt.Sprintf(
			"unexpected type for right leg of binary operation (%s): %T",
			op,
			rhs,
		), 0, 0))
	}

	leftLit, lOk := left.(*LiteralExpr)
	rightLit, rOk := right.(*LiteralExpr)

	if IsLogicalBinOp(op) {
		if lOk {
			panic(logqlmodel.NewParseError(fmt.Sprintf(
				"unexpected literal for left leg of logical/set binary operation (%s): %f",
				op,
				leftLit.value,
			), 0, 0))
		}

		if rOk {
			panic(logqlmodel.NewParseError(fmt.Sprintf(
				"unexpected literal for right leg of logical/set binary operation (%s): %f",
				op,
				rightLit.value,
			), 0, 0))
		}
	}

	// map expr like (1+1) -> 2
	if lOk && rOk {
		return reduceBinOp(op, leftLit, rightLit)
	}

	return &BinOpExpr{
		SampleExpr: left,
		RHS:        right,
		Op:         op,
		Opts:       opts,
	}
}

// Reduces a binary operation expression. A binop is reducible if both of its legs are literal expressions.
// This is because literals need match all labels, which is currently difficult to encode into StepEvaluators.
// Therefore, we ensure a binop can be reduced/simplified, maintaining the invariant that it does not have two literal legs.
func reduceBinOp(op string, left, right *LiteralExpr) *LiteralExpr {
	merged := mergeBinOp(
		op,
		&promql.Sample{Point: promql.Point{V: left.value}},
		&promql.Sample{Point: promql.Point{V: right.value}},
		false,
		false,
	)
	return &LiteralExpr{value: merged.V}
}

type LiteralExpr struct {
	value float64
	implicit
}

func mustNewLiteralExpr(s string, invert bool) *LiteralExpr {
	n, err := strconv.ParseFloat(s, 64)
	if err != nil {
		panic(logqlmodel.NewParseError(fmt.Sprintf("unable to parse literal as a float: %s", err.Error()), 0, 0))
	}

	if invert {
		n = -n
	}

	return &LiteralExpr{
		value: n,
	}
}

func (e *LiteralExpr) String() string {
	return fmt.Sprint(e.value)
}

// literlExpr impls SampleExpr & LogSelectorExpr mainly to reduce the need for more complicated typings
// to facilitate sum types. We'll be type switching when evaluating them anyways
// and they will only be present in binary operation legs.
func (e *LiteralExpr) Selector() LogSelectorExpr               { return e }
func (e *LiteralExpr) HasFilter() bool                         { return false }
func (e *LiteralExpr) Shardable() bool                         { return true }
func (e *LiteralExpr) Walk(f WalkFn)                           { f(e) }
func (e *LiteralExpr) Pipeline() (log.Pipeline, error)         { return log.NewNoopPipeline(), nil }
func (e *LiteralExpr) Matchers() []*labels.Matcher             { return nil }
func (e *LiteralExpr) Extractor() (log.SampleExtractor, error) { return nil, nil }
func (e *LiteralExpr) Value() float64                          { return e.value }

// helper used to impl Stringer for vector and range aggregations
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

type LabelReplaceExpr struct {
	Left        SampleExpr
	Dst         string
	Replacement string
	Src         string
	Regex       string
	Re          *regexp.Regexp

	implicit
}

func mustNewLabelReplaceExpr(left SampleExpr, dst, replacement, src, regex string) *LabelReplaceExpr {
	re, err := regexp.Compile("^(?:" + regex + ")$")
	if err != nil {
		panic(logqlmodel.NewParseError(fmt.Sprintf("invalid regex in label_replace: %s", err.Error()), 0, 0))
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

func (e *LabelReplaceExpr) Selector() LogSelectorExpr {
	return e.Left.Selector()
}

func (e *LabelReplaceExpr) Extractor() (SampleExtractor, error) {
	return e.Left.Extractor()
}

func (e *LabelReplaceExpr) Shardable() bool {
	return false
}

func (e *LabelReplaceExpr) Walk(f WalkFn) {
	f(e)
	if e.Left == nil {
		return
	}
	e.Left.Walk(f)
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
