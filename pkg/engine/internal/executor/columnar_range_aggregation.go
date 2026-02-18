package executor

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"

	"github.com/grafana/loki/v3/pkg/engine/internal/semconv"

	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
	"github.com/grafana/loki/v3/pkg/engine/internal/types"
	"github.com/grafana/loki/v3/pkg/xcap"
)

// columnarRangeAggregationPipeline is the columnar variant of rangeAggregationPipeline.
// It uses the columnarAggregator for batch processing and requires that the
// grouping labels are known upfront (grouping.Without == false).
type columnarRangeAggregationPipeline struct {
	inputs          []Pipeline
	inputsExhausted bool

	aggregator *columnarAggregator
	evaluator  *expressionEvaluator
	opts       rangeAggregationOptions
}

func newColumnarRangeAggregationPipeline(inputs []Pipeline, evaluator *expressionEvaluator, opts rangeAggregationOptions) (*columnarRangeAggregationPipeline, error) {
	r := &columnarRangeAggregationPipeline{
		inputs:    inputs,
		evaluator: evaluator,
		opts:      opts,
	}
	r.init()
	return r, nil
}

func (r *columnarRangeAggregationPipeline) init() {
	windows := []window{}
	cur := r.opts.startTs
	for cur.Compare(r.opts.endTs) <= 0 {
		windows = append(windows, window{start: cur.Add(-r.opts.rangeInterval), end: cur})

		if r.opts.step == 0 {
			break
		}

		cur = cur.Add(r.opts.step)
	}

	f := newMatcherFactoryFromOpts(r.opts)
	matchFunc := f.createColumnarMatchFunc(windows)

	op, ok := rangeAggregationOperations[r.opts.operation]
	if !ok {
		panic(fmt.Sprintf("unknown range aggregation operation: %v", r.opts.operation))
	}

	labels := groupByLabelsFromColumns(r.opts.grouping.Columns)
	r.aggregator = newColumnarAggregator(len(windows), op, labels, matchFunc)
	r.aggregator.SetMaxSeries(r.opts.maxQuerySeries)
}

func (r *columnarRangeAggregationPipeline) Open(ctx context.Context) error {
	for _, input := range r.inputs {
		if err := input.Open(ctx); err != nil {
			return err
		}
	}
	return nil
}

func (r *columnarRangeAggregationPipeline) Read(ctx context.Context) (arrow.RecordBatch, error) {
	if r.inputsExhausted {
		return nil, EOF
	}
	return r.read(ctx)
}

func (r *columnarRangeAggregationPipeline) read(ctx context.Context) (arrow.RecordBatch, error) {
	var (
		tsColumnExpr = &physical.ColumnExpr{
			Ref: types.ColumnRef{
				Column: types.ColumnNameBuiltinTimestamp,
				Type:   types.ColumnTypeBuiltin,
			},
		}

		valColumnExpr = &physical.ColumnExpr{
			Ref: types.ColumnRef{
				Column: types.ColumnNameGeneratedValue,
				Type:   types.ColumnTypeGenerated,
			},
		}

		startedAt     = time.Now()
		inputReadTime time.Duration
	)

	r.aggregator.Reset()
	inputsExhausted := false
	for !inputsExhausted {
		inputsExhausted = true

		for _, input := range r.inputs {
			inputStart := time.Now()
			record, err := input.Read(ctx)
			inputReadTime += time.Since(inputStart)

			if err != nil {
				if errors.Is(err, EOF) {
					continue
				}
				return nil, err
			}

			inputsExhausted = false

			if record.NumRows() == 0 {
				continue
			}

			arrays, err := evalGroupByColumns(r.evaluator, r.opts.grouping.Columns, record)
			if err != nil {
				return nil, err
			}

			tsVec, err := r.evaluator.eval(tsColumnExpr, record)
			if err != nil {
				return nil, err
			}
			tsCol := tsVec.(*array.Timestamp)

			var valArr *array.Float64
			if r.opts.operation != types.RangeAggregationTypeCount {
				valVec, err := r.evaluator.eval(valColumnExpr, record)
				if err != nil {
					return nil, err
				}
				valArr = valVec.(*array.Float64)
			}

			if err := r.aggregator.AddBatch(tsCol, valArr, arrays); err != nil {
				return nil, err
			}
		}
	}

	r.inputsExhausted = true

	if region := xcap.RegionFromContext(ctx); region != nil {
		computeTime := time.Since(startedAt) - inputReadTime
		region.Record(xcap.StatPipelineExecDuration.Observe(computeTime.Seconds()))
	}

	return r.aggregator.BuildRecord()
}

func (r *columnarRangeAggregationPipeline) Close() {
	r.aggregator.Reset()
	for _, input := range r.inputs {
		input.Close()
	}
}

// createColumnarMatchFunc builds a columnarWindowMatchFunc that processes an
// entire batch of timestamps using nanos-native integer arithmetic. Only the
// aligned case (step == interval) is currently supported.
func (f *matcherFactory) createColumnarMatchFunc(windows []window) columnarWindowMatchFunc {
	if f.step != f.interval {
		panic(fmt.Sprintf("columnar window matching only supports aligned windows (step == interval), got step=%v interval=%v", f.step, f.interval))
	}
	return f.createColumnarAlignedMatcher(windows)
}

// createColumnarAlignedMatcher builds a columnar matcher for the aligned case
// where step == interval. Each in-bounds timestamp maps to exactly one window
// via integer division. All constants are pre-computed as nanos to avoid
// time.Time construction in the hot loop.
func (f *matcherFactory) createColumnarAlignedMatcher(windows []window) columnarWindowMatchFunc {
	startNs := f.start.UnixNano()
	stepNs := f.step.Nanoseconds()
	boundsStartNs := f.bounds.start.UnixNano()
	boundsEndNs := f.bounds.end.UnixNano()

	return func(inputTs, outTs []int64, validMask []bool, numRows int) {
		for i := range numRows {
			if !validMask[i] {
				continue
			}

			ts := inputTs[i]
			if ts <= boundsStartNs || ts > boundsEndNs {
				validMask[i] = false
				continue
			}

			idx := (ts - startNs + stepNs - 1) / stepNs
			outTs[i] = windows[idx].end.UnixNano()
		}
	}
}

// groupByLabelsFromColumns precomputes arrow.Field descriptors from
// physical plan column expressions. These don't depend on record data.
func groupByLabelsFromColumns(columns []physical.ColumnExpression) []arrow.Field {
	fields := make([]arrow.Field, 0, len(columns))
	for _, columnExpr := range columns {
		colExpr, ok := columnExpr.(*physical.ColumnExpr)
		if !ok {
			continue
		}
		ident := semconv.NewIdentifier(colExpr.Ref.Column, colExpr.Ref.Type, types.Loki.String)
		fields = append(fields, semconv.FieldFromIdent(ident, true))
	}
	return fields
}

// evalGroupByColumns evaluates the known group-by column expressions against
// a record and returns the data arrays.
func evalGroupByColumns(evaluator *expressionEvaluator, columns []physical.ColumnExpression, record arrow.RecordBatch) ([]*array.String, error) {
	arrays := make([]*array.String, 0, len(columns))
	for _, columnExpr := range columns {
		vec, err := evaluator.eval(columnExpr, record)
		if err != nil {
			return nil, err
		}
		if vec.DataType().ID() != types.Arrow.String.ID() {
			return nil, fmt.Errorf("unsupported datatype for grouping %s", vec.DataType())
		}
		arrays = append(arrays, vec.(*array.String))
	}
	return arrays, nil
}
