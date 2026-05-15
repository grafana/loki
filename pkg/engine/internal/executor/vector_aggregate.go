package executor

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"

	"github.com/grafana/loki/v3/pkg/engine/internal/assertions"
	"github.com/grafana/loki/v3/pkg/engine/internal/semconv"

	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
	"github.com/grafana/loki/v3/pkg/engine/internal/types"
	"github.com/grafana/loki/v3/pkg/xcap"
)

type vectorAggregationOptions struct {
	grouping       physical.Grouping
	operation      types.VectorAggregationType
	maxQuerySeries int // maximum number of unique series allowed (0 means no limit)
}

// vectorAggregationPipeline is a pipeline that performs vector aggregations.
//
// It reads from the input pipeline, groups the data by specified columns,
// and applies the aggregation function on each group.
type vectorAggregationPipeline struct {
	inputs          []Pipeline
	inputsExhausted bool // indicates if all inputs are exhausted

	aggregator *aggregator
	evaluator  *expressionEvaluator
	grouping   physical.Grouping
	opts       vectorAggregationOptions

	tsEval    evalFunc // used to evaluate the timestamp column
	valueEval evalFunc // used to evaluate the value column

	identCache *semconv.IdentifierCache
}

var vectorAggregationOperations = map[types.VectorAggregationType]aggregationOperation{
	types.VectorAggregationTypeSum:   aggregationOperationSum,
	types.VectorAggregationTypeCount: aggregationOperationCount,
	types.VectorAggregationTypeAvg:   aggregationOperationAvg,
	types.VectorAggregationTypeMax:   aggregationOperationMax,
	types.VectorAggregationTypeMin:   aggregationOperationMin,
}

func newVectorAggregationPipeline(inputs []Pipeline, evaluator *expressionEvaluator, opts vectorAggregationOptions) (*vectorAggregationPipeline, error) {
	if len(inputs) == 0 {
		return nil, fmt.Errorf("vector aggregation expects at least one input")
	}

	op, ok := vectorAggregationOperations[opts.operation]
	if !ok {
		panic(fmt.Sprintf("unknown vector aggregation operation: %v", opts.operation))
	}

	agg := newAggregator(0, op)
	agg.SetMaxSeries(opts.maxQuerySeries)

	return &vectorAggregationPipeline{
		inputs:     inputs,
		evaluator:  evaluator,
		grouping:   opts.grouping,
		opts:       opts,
		aggregator: agg,
		tsEval: evaluator.newFunc(&physical.ColumnExpr{
			Ref: types.ColumnRef{
				Column: types.ColumnNameBuiltinTimestamp,
				Type:   types.ColumnTypeBuiltin,
			},
		}),
		valueEval: evaluator.newFunc(&physical.ColumnExpr{
			Ref: types.ColumnRef{
				Column: types.ColumnNameGeneratedValue,
				Type:   types.ColumnTypeGenerated,
			},
		}),
		identCache: semconv.NewIdentifierCache(),
	}, nil
}

// Open opens all input pipelines.
func (v *vectorAggregationPipeline) Open(ctx context.Context) error {
	return openInputsConcurrently(ctx, v.inputs)
}

// Read reads the next value into its state.
func (v *vectorAggregationPipeline) Read(ctx context.Context) (arrow.RecordBatch, error) {
	if v.inputsExhausted {
		return nil, EOF
	}
	rec, err := v.read(ctx)

	assertions.CheckColumnDuplicates(rec)
	assertions.CheckLabelValuesDuplicates(rec)

	return rec, err
}

func (v *vectorAggregationPipeline) read(ctx context.Context) (arrow.RecordBatch, error) {
	var (
		inputReadTime time.Duration
		startedAt     = time.Now()

		labelValuesCache = newLabelValuesCache()
		fieldsCache      = newFieldsCache()
	)

	v.aggregator.Reset() // reset before reading new inputs
	inputsExhausted := false
	for !inputsExhausted {
		inputsExhausted = true

		for _, input := range v.inputs {
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
				// Nothing to process
				continue
			}

			assertions.CheckLabelValuesDuplicates(record)

			// extract timestamp column
			tsVec, err := v.tsEval(record)
			if err != nil {
				return nil, err
			}
			tsCol := tsVec.(*array.Timestamp)

			// extract value column
			valueVec, err := v.valueEval(record)
			if err != nil {
				return nil, err
			}
			valueArr := valueVec.(*array.Float64)

			arrays, groupingFields, err := collectGroupingColumns(record, v.grouping, v.evaluator, v.identCache)
			if err != nil {
				return nil, err
			}

			v.aggregator.AddLabels(groupingFields)

			for row := range int(record.NumRows()) {
				if valueArr.IsNull(row) {
					continue
				}

				labelValues := labelValuesCache.getLabelValues(arrays, row)
				labels := fieldsCache.getFields(arrays, groupingFields, row)

				if err := v.aggregator.Add(tsCol.Value(row).ToTime(arrow.Nanosecond), valueArr.Value(row), labels, labelValues); err != nil {
					return nil, err
				}
			}
		}
	}

	v.inputsExhausted = true

	rec, err := v.aggregator.BuildRecord()

	if region := xcap.RegionFromContext(ctx); region != nil {
		computeTime := time.Since(startedAt) - inputReadTime
		region.Record(xcap.StatPipelineExecDuration.Observe(computeTime.Seconds()))
	}

	return rec, err
}

// Close closes the resources of the pipeline.
func (v *vectorAggregationPipeline) Close() {
	v.aggregator.Reset()
	for _, input := range v.inputs {
		input.Close()
	}
}
