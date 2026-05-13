package worker

import (
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/cespare/xxhash/v2"

	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
	"github.com/grafana/loki/v3/pkg/engine/internal/semconv"
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
		if err := computeTimeShards(rec, routing.TimeRange, numShards, shardIndices); err != nil {
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
// This mimics the grouping logic used in aggregator.go.
func computeLabelHashShards(rec arrow.RecordBatch, grouping physical.Grouping, numShards int, shardIndices []int) error {
	if len(grouping.Columns) == 0 {
		// No grouping columns - all rows go to shard 0
		return nil
	}

	// Find the grouping columns in the record
	groupingCols := make([]arrow.Field, 0, len(grouping.Columns))
	groupingColIndices := make([]int, 0, len(grouping.Columns))

	schema := rec.Schema()
	for _, groupExpr := range grouping.Columns {
		colExpr, ok := groupExpr.(*physical.ColumnExpr)
		if !ok {
			continue
		}
		colName := colExpr.Ref.Column

		// Find the column index in the schema
		for i := 0; i < int(rec.NumCols()); i++ {
			field := schema.Field(i)
			if field.Name == colName {
				groupingCols = append(groupingCols, field)
				groupingColIndices = append(groupingColIndices, i)
				break
			}
		}
	}

	if len(groupingCols) == 0 {
		// No grouping columns found - all rows go to shard 0
		return nil
	}

	// Hash each row's grouping labels to determine shard
	digest := xxhash.New()
	for rowIdx := 0; rowIdx < int(rec.NumRows()); rowIdx++ {
		digest.Reset()

		for i, colIdx := range groupingColIndices {
			if i > 0 {
				_, _ = digest.Write([]byte{0}) // separator
			}

			col := rec.Column(colIdx)
			field := groupingCols[i]

			// Write field name
			_, _ = digest.WriteString(field.Name)
			_, _ = digest.Write([]byte("="))

			// Write field value
			if col.IsNull(rowIdx) {
				_, _ = digest.WriteString("")
			} else {
				switch arr := col.(type) {
				case *array.String:
					_, _ = digest.WriteString(arr.Value(rowIdx))
				case *array.Binary:
					_, _ = digest.Write(arr.Value(rowIdx))
				default:
					// For other types, use string representation
					_, _ = digest.WriteString(col.ValueStr(rowIdx))
				}
			}
		}

		hash := digest.Sum64()
		shardIndices[rowIdx] = int(hash % uint64(numShards))
	}

	return nil
}

// computeTimeShards computes the shard index for each row based on timestamp.
// The time range is divided evenly among shards.
func computeTimeShards(rec arrow.RecordBatch, timeRange physical.TimeRange, numShards int, shardIndices []int) error {
	// Find the timestamp column
	timestampCol, err := findTimestampColumn(rec)
	if err != nil {
		// No timestamp column - all rows go to shard 0
		return nil
	}

	if timeRange.IsZero() || timeRange.Start.Equal(timeRange.End) {
		// Invalid time range - all rows go to shard 0
		return nil
	}

	// Calculate shard boundaries
	totalDuration := timeRange.End.Sub(timeRange.Start)
	shardDuration := totalDuration / time.Duration(numShards)

	for rowIdx := 0; rowIdx < int(rec.NumRows()); rowIdx++ {
		if timestampCol.IsNull(rowIdx) {
			shardIndices[rowIdx] = 0
			continue
		}

		ts := timestampCol.Value(rowIdx).ToTime(arrow.Nanosecond)

		// Calculate which shard this timestamp belongs to
		if ts.Before(timeRange.Start) {
			shardIndices[rowIdx] = 0
		} else if ts.After(timeRange.End) || ts.Equal(timeRange.End) {
			shardIndices[rowIdx] = numShards - 1
		} else {
			offset := ts.Sub(timeRange.Start)
			shardIdx := int(offset / shardDuration)
			if shardIdx >= numShards {
				shardIdx = numShards - 1
			}
			shardIndices[rowIdx] = shardIdx
		}
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
	return nil, nil
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
