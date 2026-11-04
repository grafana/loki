package syntax

import (
	"errors"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/prometheus/common/model"

	logqlerr "github.com/grafana/loki/v3/pkg/logql/errors"
)

// Histogram reader functions. They take a histogram vector, produced by
// histogram_over_time, and return a float vector.
const (
	OpHistogramQuantile  = "histogram_quantile"
	OpHistogramQuantiles = "histogram_quantiles"
	OpHistogramFraction  = "histogram_fraction"
	OpHistogramCount     = "histogram_count"
	OpHistogramSum       = "histogram_sum"
	OpHistogramAvg       = "histogram_avg"

	// OpBuckets introduces an explicit bucket list in histogram_over_time.
	OpBuckets = "buckets"
)

const (
	// MaxBucketBounds is the most explicit bounds buckets(...) accepts. The
	// implicit +Inf bucket makes the histogram one bucket larger.
	MaxBucketBounds = 32
	// MaxHistogramQuantiles is the most quantiles histogram_quantiles accepts,
	// the same as in PromQL.
	MaxHistogramQuantiles = 10
)

// BucketSpec defines the buckets of a histogram_over_time histogram. A nil
// spec selects the default exponential buckets.
//
// It is a node of its own, rather than a plain list of numbers, so other bucket
// layouts can be added later without changing the grammar.
type BucketSpec struct {
	// Bounds are the inclusive upper bounds of the custom buckets. An implicit
	// +Inf bucket collects everything above the last one.
	Bounds []float64
}

func newBucketSpec(bounds []float64) *BucketSpec {
	return &BucketSpec{Bounds: bounds}
}

// Validate checks that the bounds are usable as custom bucket bounds: at least
// one and at most MaxBucketBounds, finite and strictly increasing.
func (b *BucketSpec) Validate() error {
	if len(b.Bounds) == 0 {
		return errors.New("buckets requires at least one bound")
	}
	if len(b.Bounds) > MaxBucketBounds {
		return fmt.Errorf("buckets accepts at most %d bounds, got %d", MaxBucketBounds, len(b.Bounds))
	}
	for i, v := range b.Bounds {
		switch {
		case math.IsNaN(v):
			return errors.New("bucket bounds must not be NaN")
		case math.IsInf(v, 0):
			return fmt.Errorf("bucket bound %s must be finite, the +Inf bucket is implicit", formatHistogramParam(v))
		case i > 0 && v <= b.Bounds[i-1]:
			return fmt.Errorf("bucket bounds must be strictly increasing, %s is not greater than %s",
				formatHistogramParam(v), formatHistogramParam(b.Bounds[i-1]))
		}
	}
	return nil
}

func (b *BucketSpec) clone() *BucketSpec {
	return &BucketSpec{Bounds: slices.Clone(b.Bounds)}
}

func (b *BucketSpec) String() string {
	bounds := make([]string, 0, len(b.Bounds))
	for _, v := range b.Bounds {
		bounds = append(bounds, formatHistogramParam(v))
	}
	return OpBuckets + "(" + strings.Join(bounds, ",") + ")"
}

// HistogramFuncExpr reads a histogram vector and returns a float vector, for
// example histogram_quantile(0.99, sum by (path) (histogram_over_time(...))).
type HistogramFuncExpr struct {
	Left      SampleExpr
	Operation string
	// Params holds the quantile for histogram_quantile, the quantiles for
	// histogram_quantiles, and the lower and upper bound for histogram_fraction.
	Params []float64
	// Label is the name of the label that carries the quantile in the result of
	// histogram_quantiles.
	Label string
	err   error
}

func (HistogramFuncExpr) isExpr()       {}
func (HistogramFuncExpr) isSampleExpr() {}

func mustNewHistogramFuncExpr(left SampleExpr, operation, label string, params []float64) *HistogramFuncExpr {
	e := &HistogramFuncExpr{
		Left:      left,
		Operation: operation,
		Params:    params,
		Label:     label,
	}
	if err := e.validate(); err != nil {
		return &HistogramFuncExpr{err: logqlerr.NewParseError(err.Error(), 0, 0)}
	}
	return e
}

// validate checks the arguments of the function. The grammar already fixes the
// number of arguments of most functions, but a serialized expression does not
// go through the grammar.
func (e *HistogramFuncExpr) validate() error {
	if e.Left == nil {
		return fmt.Errorf("%s requires a histogram argument", e.Operation)
	}
	for _, p := range e.Params {
		if math.IsNaN(p) {
			return fmt.Errorf("%s arguments must not be NaN", e.Operation)
		}
	}
	switch e.Operation {
	case OpHistogramQuantile:
		if len(e.Params) != 1 {
			return fmt.Errorf("%s requires exactly 1 quantile, got %d", e.Operation, len(e.Params))
		}
	case OpHistogramQuantiles:
		if e.Label == "" {
			return fmt.Errorf("%s requires a label name for the quantile", e.Operation)
		}
		if err := validateQuantileLabel(e.Label); err != nil {
			return fmt.Errorf("%s: %w", e.Operation, err)
		}
		if n := len(e.Params); n < 1 || n > MaxHistogramQuantiles {
			return fmt.Errorf("%s requires between 1 and %d quantiles, got %d", e.Operation, MaxHistogramQuantiles, n)
		}
	case OpHistogramFraction:
		if len(e.Params) != 2 {
			return fmt.Errorf("%s requires a lower and an upper bound, got %d arguments", e.Operation, len(e.Params))
		}
	case OpHistogramCount, OpHistogramSum, OpHistogramAvg:
		if len(e.Params) != 0 || e.Label != "" {
			return fmt.Errorf("%s takes no arguments besides the histogram", e.Operation)
		}
	default:
		return fmt.Errorf(UnsupportedErr, e.Operation)
	}
	return nil
}

// validateQuantileLabel checks the name of the label that carries the quantile.
// It ends up on the result series, where it must be usable in a grouping, so it
// has to be a plain label name. Names that start with two underscores are
// reserved, and __error__ among them would mark the series as failed.
func validateQuantileLabel(name string) error {
	switch {
	case !model.LegacyValidation.IsValidLabelName(name):
		return fmt.Errorf("%q is not a valid label name", name)
	case strings.HasPrefix(name, "__"):
		return fmt.Errorf("label name %q is reserved, names starting with __ are", name)
	}
	return nil
}

func (e *HistogramFuncExpr) Selector() (LogSelectorExpr, error) {
	if e.err != nil {
		return nil, e.err
	}
	return e.Left.Selector()
}

func (e *HistogramFuncExpr) MatcherGroups() ([]MatcherRange, error) {
	if e.err != nil {
		return nil, e.err
	}
	return e.Left.MatcherGroups()
}

func (e *HistogramFuncExpr) Extractor() (SampleExtractor, error) {
	if e.err != nil {
		return nil, e.err
	}
	return e.Left.Extractor()
}

// Shardable is false because a quantile of partial histograms cannot be merged.
// The shard mapper shards the histogram inside and evaluates this function once,
// over the merged result.
func (e *HistogramFuncExpr) Shardable(_ bool) bool {
	return false
}

func (e *HistogramFuncExpr) Walk(f WalkFn) {
	if !f(e) {
		return
	}
	if e.Left != nil {
		e.Left.Walk(f)
	}
}

func (e *HistogramFuncExpr) Accept(v RootVisitor) { v.VisitHistogramFunc(e) }

// args returns the arguments that are written before and after the histogram.
func (e *HistogramFuncExpr) args() (before, after []string) {
	params := make([]string, 0, len(e.Params))
	for _, p := range e.Params {
		params = append(params, formatHistogramParam(p))
	}
	switch e.Operation {
	case OpHistogramQuantile, OpHistogramFraction:
		return params, nil
	case OpHistogramQuantiles:
		return nil, append([]string{strconv.Quote(e.Label)}, params...)
	default:
		return nil, nil
	}
}

func (e *HistogramFuncExpr) String() string {
	before, after := e.args()
	parts := append(before, e.Left.String())
	parts = append(parts, after...)
	return e.Operation + "(" + strings.Join(parts, ",") + ")"
}

// formatHistogramParam formats a number so that the parser reads it back. The
// infinities have no number syntax, so they use the +Inf and -Inf spellings
// that mustParseInf accepts.
func formatHistogramParam(v float64) string {
	switch {
	case math.IsInf(v, 1):
		return "+Inf"
	case math.IsInf(v, -1):
		return "-Inf"
	default:
		return strconv.FormatFloat(v, 'f', -1, 64)
	}
}

// mustParseNumber parses a NUMBER token of a histogram function argument.
func mustParseNumber(sign, text string) float64 {
	v, err := strconv.ParseFloat(text, 64)
	if err != nil {
		panic(logqlerr.NewParseError(fmt.Sprintf("invalid number %q", text), 0, 0))
	}
	return applySign(sign, v)
}

// mustParseInf parses an IDENTIFIER token of a histogram function argument.
// Inf is the only identifier that is a number, because LogQL has no Inf literal
// and the identifiers NaN and the like must stay errors.
func mustParseInf(sign, ident string) float64 {
	if !strings.EqualFold(ident, "inf") {
		panic(logqlerr.NewParseError(fmt.Sprintf("invalid number %q, the only name accepted as a number is Inf", ident), 0, 0))
	}
	return applySign(sign, math.Inf(1))
}

func applySign(sign string, v float64) float64 {
	if sign == OpTypeSub {
		return -v
	}
	return v
}

// isHistogramValued reports whether expr evaluates to a histogram vector, which
// only histogram_over_time and the sum of a histogram vector do.
//
// A histogram vector can only be consumed by sum and by the histogram reader
// functions. It cannot be the result of a query, because the API returns floats.
func isHistogramValued(expr SampleExpr) bool {
	switch e := expr.(type) {
	case *RangeAggregationExpr:
		return e.Operation == OpRangeTypeHistogram
	case *VectorAggregationExpr:
		return e.Operation == OpTypeSum && isHistogramValued(e.Left)
	default:
		return false
	}
}

// validateHistogramFunc checks that a histogram reader is given a histogram.
func validateHistogramFunc(e *HistogramFuncExpr) error {
	if !isHistogramValued(e.Left) {
		return logqlerr.NewParseError(
			fmt.Sprintf("%s requires a histogram_over_time expression, or a sum of one, as its histogram argument", e.Operation), 0, 0)
	}
	return nil
}

// validateHistogramUse rejects the use of a histogram vector where only floats
// are valid. It looks at the direct children of expr only, validateSampleExpr
// walks the tree.
func validateHistogramUse(expr SampleExpr) error {
	switch e := expr.(type) {
	case *BinOpExpr:
		if isHistogramValued(e.SampleExpr) || isHistogramValued(e.RHS) {
			return logqlerr.NewParseError(
				"a histogram_over_time expression cannot be used in a binary operation, read it with a histogram_* function first", 0, 0)
		}
	case *VectorAggregationExpr:
		if e.Operation != OpTypeSum && isHistogramValued(e.Left) {
			return logqlerr.NewParseError(
				fmt.Sprintf("%s is not supported on a histogram_over_time expression, only sum is", e.Operation), 0, 0)
		}
	case *LabelReplaceExpr:
		if isHistogramValued(e.Left) {
			return logqlerr.NewParseError(
				"label_replace is not supported on a histogram_over_time expression", 0, 0)
		}
	}
	return nil
}
