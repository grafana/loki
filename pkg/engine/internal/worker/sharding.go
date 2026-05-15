package worker

import (
	"errors"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/cespare/xxhash/v2"

	"github.com/grafana/loki/v3/pkg/engine/internal/executor"
	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
	"github.com/grafana/loki/v3/pkg/engine/internal/semconv"
	"github.com/grafana/loki/v3/pkg/engine/internal/types"
	"github.com/grafana/loki/v3/pkg/engine/internal/workflow"
)

// partitionRecordBatch splits a record batch into multiple batches based on the sink routing strategy.
// Returns a slice of record batches, one per shard. The number of returned batches equals numShards.
// Some batches may be nil if no rows belong to that shard.
func partitionRecordBatch(rec arrow.RecordBatch, routing *workflow.SinkRouting, numShards int) ([]arrow.RecordBatch, error) {
	if numShards <= 1 {
		// No partitioning needed
		return []arrow.RecordBatch{rec}, nil
	}

	// Compute shard index for each row
	shardIndices := make([]int, rec.NumRows())

	switch routing.Strategy {
	case workflow.SinkRoutingStrategyLabelHash:
		if err := computeLabelHashShards(rec, routing.Grouping, numShards, shardIndices); err != nil {
			return nil, err
		}

	case workflow.SinkRoutingStrategyTimeShard:
		if err := computeTimeShards(rec, routing.TimeRanges, shardIndices); err != nil {
			return nil, err
		}

	default:
		// Broadcast or unknown - return single batch
		return []arrow.RecordBatch{rec}, nil
	}

	// Split the record batch by shard
	return splitRecordByShards(rec, shardIndices, numShards), nil
}

// computeLabelHashShards computes the shard index for each row based on grouping labels.
func computeLabelHashShards(rec arrow.RecordBatch, grouping physical.Grouping, numShards int, shardIndices []int) error {
	if len(grouping.Columns) == 0 {
		// No grouping columns - all rows go to shard 0
		return nil
	}

	// Use the shared grouping column collection logic
	// We need an evaluator for collectByGroupingColumns, but for sharding we can use a simple one
	// that just looks up column references
	evaluator := &simpleEvaluatorForSharding{rec: rec}
	arrays, fields, err := executor.CollectByGroupingColumns(rec, grouping, evaluator)
	if err != nil {
		// If we can't collect grouping columns, fall back to shard 0 for all rows
		return nil
	}

	if len(arrays) == 0 {
		// No grouping columns found - all rows go to shard 0
		return nil
	}

	// Hash each row's grouping labels to determine shard using shared hash function
	digest := xxhash.New()
	for rowIdx := 0; rowIdx < int(rec.NumRows()); rowIdx++ {
		hash := executor.ComputeGroupingHash(arrays, fields, rowIdx, digest)
		shardIndices[rowIdx] = int(hash % uint64(numShards))
	}

	return nil
}

// simpleEvaluatorForSharding is a minimal expression evaluator that only supports
// column reference lookups for use in sharding.
type simpleEvaluatorForSharding struct {
	rec arrow.RecordBatch
}

func (e *simpleEvaluatorForSharding) EvalForGrouping(expr physical.Expression, rec arrow.RecordBatch) (arrow.Array, error) {
	colExpr, ok := expr.(*physical.ColumnExpr)
	if !ok {
		return nil, errors.New("unsupported expression type")
	}

	// Find the column by name (FQN)
	ident := semconv.NewIdentifier(colExpr.Ref.Column, colExpr.Ref.Type, types.Loki.String)
	fqn := ident.FQN()

	schema := rec.Schema()
	for i := 0; i < int(rec.NumCols()); i++ {
		field := schema.Field(i)
		if field.Name == fqn {
			return rec.Column(i), nil
		}
	}

	return nil, errors.New("column not found")
}

// computeTimeShards computes the shard index for each row based on timestamp.
// Each row is assigned to a shard based on which time range it falls into.
func computeTimeShards(rec arrow.RecordBatch, timeRanges []physical.TimeRange, shardIndices []int) error {
	// Find the timestamp column
	timestampCol, err := findTimestampColumn(rec)
	if err != nil {
		// No timestamp column - all rows go to shard 0
		return nil
	}

	if len(timeRanges) == 0 {
		// No time ranges - all rows go to shard 0
		return nil
	}

	for rowIdx := 0; rowIdx < int(rec.NumRows()); rowIdx++ {
		if timestampCol.IsNull(rowIdx) {
			shardIndices[rowIdx] = 0
			continue
		}

		ts := timestampCol.Value(rowIdx).ToTime(arrow.Nanosecond)

		// Find which time range this timestamp belongs to
		shardIdx := 0
		for i, tr := range timeRanges {
			if (ts.Equal(tr.Start) || ts.After(tr.Start)) && ts.Before(tr.End) {
				shardIdx = i
				break
			}
		}

		shardIndices[rowIdx] = shardIdx
	}

	return nil
}

// findTimestampColumn finds the timestamp column in the record batch.
func findTimestampColumn(rec arrow.RecordBatch) (*array.Timestamp, error) {
	timestampFQN := semconv.ColumnIdentTimestamp.FQN()
	schema := rec.Schema()
	for i := 0; i < int(rec.NumCols()); i++ {
		field := schema.Field(i)
		if field.Name == timestampFQN {
			if tsCol, ok := rec.Column(i).(*array.Timestamp); ok {
				return tsCol, nil
			}
		}
	}
	return nil, errors.New("no timestamp column")
}

// splitRecordByShards splits a record batch into multiple batches based on shard indices.
// Returns a slice of record batches, one per shard.
func splitRecordByShards(rec arrow.RecordBatch, shardIndices []int, numShards int) []arrow.RecordBatch {
	// Count rows per shard
	rowsPerShard := make([]int, numShards)
	for _, shardIdx := range shardIndices {
		rowsPerShard[shardIdx]++
	}

	// Create builders for each shard
	schema := rec.Schema()
	alloc := memory.NewGoAllocator()
	builders := make([]*array.RecordBuilder, numShards)
	for i := 0; i < numShards; i++ {
		builders[i] = array.NewRecordBuilder(alloc, schema)
		builders[i].Reserve(rowsPerShard[i])
	}

	// Distribute rows to shards
	for rowIdx := 0; rowIdx < int(rec.NumRows()); rowIdx++ {
		shardIdx := shardIndices[rowIdx]
		builder := builders[shardIdx]

		for colIdx := 0; colIdx < int(rec.NumCols()); colIdx++ {
			col := rec.Column(colIdx)
			fieldBuilder := builder.Field(colIdx)

			if col.IsNull(rowIdx) {
				fieldBuilder.AppendNull()
				continue
			}

			// Append the value based on type
			switch arr := col.(type) {
			case *array.Timestamp:
				fieldBuilder.(*array.TimestampBuilder).Append(arr.Value(rowIdx))
			case *array.Float64:
				fieldBuilder.(*array.Float64Builder).Append(arr.Value(rowIdx))
			case *array.String:
				fieldBuilder.(*array.StringBuilder).Append(arr.Value(rowIdx))
			case *array.Binary:
				fieldBuilder.(*array.BinaryBuilder).Append(arr.Value(rowIdx))
			case *array.Int64:
				fieldBuilder.(*array.Int64Builder).Append(arr.Value(rowIdx))
			case *array.Boolean:
				fieldBuilder.(*array.BooleanBuilder).Append(arr.Value(rowIdx))
			default:
				// For other types, append null as fallback
				fieldBuilder.AppendNull()
			}
		}
	}

	// Build the result batches
	results := make([]arrow.RecordBatch, numShards)
	for i := 0; i < numShards; i++ {
		results[i] = builders[i].NewRecordBatch()
	}

	return results
}
