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
	region     *xcap.Region

	tsEval    evalFunc // used to evaluate the timestamp column
	valueEval evalFunc // used to evaluate the value column

	identCache *semconv.IdentifierCache
}

var (
	vectorAggregationOperations = map[types.VectorAggregationType]aggregationOperation{
		types.VectorAggregationTypeSum:   aggregationOperationSum,
		types.VectorAggregationTypeCount: aggregationOperationCount,
		types.VectorAggregationTypeAvg:   aggregationOperationAvg,
		types.VectorAggregationTypeMax:   aggregationOperationMax,
		types.VectorAggregationTypeMin:   aggregationOperationMin,
	}
)

func newVectorAggregationPipeline(inputs []Pipeline, evaluator *expressionEvaluator, opts vectorAggregationOptions, region *xcap.Region) (*vectorAggregationPipeline, error) {
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
		region:     region,
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

// Read reads the next value into its state.
func (v *vectorAggregationPipeline) Read(ctx context.Context) (arrow.RecordBatch, error) {
	if v.inputsExhausted {
		return nil, EOF
	}
	return v.read(ctx)
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

			if record.NumRows() == 0 {
				// Nothing to process
				continue
			}

			inputsExhausted = false

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

			// extract all the columns that are used for grouping
			var arrays []*array.String
			var groupingFields []arrow.Field

			if v.grouping.Without {
				// Grouping without a lable set. Exclude lables from that set.
				schema := record.Schema()
				for i, field := range schema.Fields() {
					ident, err := v.identCache.ParseFQN(field.Name)
					if err != nil {
						return nil, err
					}

					if ident.ColumnType() == types.ColumnTypeLabel ||
						ident.ColumnType() == types.ColumnTypeMetadata ||
						ident.ColumnType() == types.ColumnTypeParsed {
						found := false
						for _, g := range v.grouping.Columns {
							colExpr, ok := g.(*physical.ColumnExpr)
							if !ok {
								return nil, fmt.Errorf("unknown column expression %v", g)
							}

							// Match ambiguous columns only by name
							if colExpr.Ref.Type == types.ColumnTypeAmbiguous && colExpr.Ref.Column == ident.ShortName() {
								found = true
								break
							}

							// Match all other columns by name and type
							if colExpr.Ref.Column == ident.ShortName() && colExpr.Ref.Type == ident.ColumnType() {
								found = true
								break
							}
						}
						if !found {
							arrays = append(arrays, record.Column(i).(*array.String))
							groupingFields = append(groupingFields, field)
						}
					}
				}
			} else {
				// Gouping by a label set. Take only labels from that set.
				for _, columnExpr := range v.grouping.Columns {
					vec, err := v.evaluator.eval(columnExpr, record)
					if err != nil {
						return nil, err
					}

					if vec.DataType().ID() != types.Arrow.String.ID() {
						return nil, fmt.Errorf("unsupported datatype for grouping %s", vec.DataType())
					}

					arr := vec.(*array.String)
					arrays = append(arrays, arr)

					colExpr, ok := columnExpr.(*physical.ColumnExpr)
					if !ok {
						return nil, fmt.Errorf("invalid column expression type %T", columnExpr)
					}
					ident := semconv.NewIdentifier(colExpr.Ref.Column, colExpr.Ref.Type, types.Loki.String)
					groupingFields = append(groupingFields, semconv.FieldFromIdent(ident, true))
				}
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

	if v.region != nil {
		computeTime := time.Since(startedAt) - inputReadTime
		v.region.Record(xcap.StatPipelineExecDuration.Observe(computeTime.Seconds()))
	}

	return v.aggregator.BuildRecord()
}

// Close closes the resources of the pipeline.
func (v *vectorAggregationPipeline) Close() {
	v.aggregator.Reset()
	if v.region != nil {
		v.region.End()
	}
	for _, input := range v.inputs {
		input.Close()
	}
}

// Region implements RegionProvider.
func (v *vectorAggregationPipeline) Region() *xcap.Region {
	return v.region
}
