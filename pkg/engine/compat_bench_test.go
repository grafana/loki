package engine

import (
	"fmt"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/memory"

	"github.com/grafana/loki/v3/pkg/engine/internal/semconv"
	"github.com/grafana/loki/v3/pkg/engine/internal/types"
	"github.com/grafana/loki/v3/pkg/util/arrowtest"
)

func BenchmarkStreamsResultBuilder(b *testing.B) {
	alloc := memory.NewGoAllocator()

	benchmarks := []struct {
		name      string
		numRows   int
		numLabels int
		numMeta   int
		numParsed int
	}{
		{
			name:      "small_simple",
			numRows:   10,
			numLabels: 2,
			numMeta:   1,
			numParsed: 0,
		},
		{
			name:      "medium_simple",
			numRows:   100,
			numLabels: 2,
			numMeta:   1,
			numParsed: 0,
		},
		{
			name:      "large_simple",
			numRows:   1000,
			numLabels: 2,
			numMeta:   1,
			numParsed: 0,
		},
		{
			name:      "xlarge_simple",
			numRows:   10000,
			numLabels: 2,
			numMeta:   1,
			numParsed: 0,
		},
		{
			name:      "medium_many_labels",
			numRows:   100,
			numLabels: 10,
			numMeta:   3,
			numParsed: 0,
		},
		{
			name:      "medium_with_parsed",
			numRows:   100,
			numLabels: 2,
			numMeta:   2,
			numParsed: 5,
		},
		{
			name:      "large_complex",
			numRows:   1000,
			numLabels: 10,
			numMeta:   5,
			numParsed: 8,
		},
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			// Build schema
			colTs := semconv.ColumnIdentTimestamp
			colMsg := semconv.ColumnIdentMessage
			fields := []arrow.Field{
				semconv.FieldFromIdent(colTs, false),
				semconv.FieldFromIdent(colMsg, false),
			}

			// Add label columns
			labelIdents := make([]*semconv.Identifier, bm.numLabels)
			for i := 0; i < bm.numLabels; i++ {
				ident := semconv.NewIdentifier(
					fmt.Sprintf("label_%d", i),
					types.ColumnTypeLabel,
					types.Loki.String,
				)
				labelIdents[i] = ident
				fields = append(fields, semconv.FieldFromIdent(ident, false))
			}

			// Add metadata columns
			metaIdents := make([]*semconv.Identifier, bm.numMeta)
			for i := 0; i < bm.numMeta; i++ {
				ident := semconv.NewIdentifier(
					fmt.Sprintf("meta_%d", i),
					types.ColumnTypeMetadata,
					types.Loki.String,
				)
				metaIdents[i] = ident
				fields = append(fields, semconv.FieldFromIdent(ident, false))
			}

			// Add parsed columns
			parsedIdents := make([]*semconv.Identifier, bm.numParsed)
			for i := 0; i < bm.numParsed; i++ {
				ident := semconv.NewIdentifier(
					fmt.Sprintf("parsed_%d", i),
					types.ColumnTypeParsed,
					types.Loki.String,
				)
				parsedIdents[i] = ident
				fields = append(fields, semconv.FieldFromIdent(ident, false))
			}

			schema := arrow.NewSchema(fields, nil)

			// Build rows
			rows := make(arrowtest.Rows, bm.numRows)
			baseTime := time.Unix(0, 1620000000000000000).UTC()
			for rowIdx := 0; rowIdx < bm.numRows; rowIdx++ {
				row := make(map[string]any)
				row[colTs.FQN()] = baseTime.Add(time.Duration(rowIdx) * time.Nanosecond)
				row[colMsg.FQN()] = fmt.Sprintf("log line %d with some additional text to make it more realistic", rowIdx)

				// Add label values
				for labelIdx, ident := range labelIdents {
					row[ident.FQN()] = fmt.Sprintf("label_%d_value_%d", labelIdx, rowIdx%10)
				}

				// Add metadata values
				for metaIdx, ident := range metaIdents {
					row[ident.FQN()] = fmt.Sprintf("meta_%d_value_%d", metaIdx, rowIdx%5)
				}

				// Add parsed values
				for parsedIdx, ident := range parsedIdents {
					row[ident.FQN()] = fmt.Sprintf("parsed_%d_value_%d", parsedIdx, rowIdx%3)
				}

				rows[rowIdx] = row
			}

			record := rows.Record(alloc, schema)
			defer record.Release()

			b.ResetTimer()
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				rb := newStreamsResultBuilder()
				// Collect records twice on purpose to see how efficient CollectRecord is when the builder already has
				// some data
				rb.CollectRecord(record)
				rb.CollectRecord(record)

				// Ensure the result is used to prevent compiler optimizations
				if rb.Len() != bm.numRows*2 {
					b.Fatalf("expected %d entries, got %d", bm.numRows*2, rb.Len())
				}
			}
		})
	}
}
