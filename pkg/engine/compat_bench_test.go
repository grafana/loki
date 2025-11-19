package engine

import (
	"fmt"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/memory"

	"github.com/grafana/loki/v3/pkg/engine/internal/semconv"
	"github.com/grafana/loki/v3/pkg/engine/internal/types"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/util/arrowtest"
)

func BenchmarkStreamsResultBuilder(b *testing.B) {
	alloc := memory.NewGoAllocator()

	benchmarks := []struct {
		name                string
		numRowsFirstRecord  int
		numRowsSecondRecord int
		numLabels           int
		numMeta             int
		numParsed           int
	}{
		{
			name:                "records_equal_size",
			numRowsFirstRecord:  1000,
			numRowsSecondRecord: 1000,
			numLabels:           10,
			numMeta:             5,
			numParsed:           8,
		},
		{
			name:                "record_two_bigger",
			numRowsFirstRecord:  1000,
			numRowsSecondRecord: 2000,
			numLabels:           10,
			numMeta:             5,
			numParsed:           8,
		},
		{
			name:                "record_two_smaller",
			numRowsFirstRecord:  1000,
			numRowsSecondRecord: 500,
			numLabels:           10,
			numMeta:             5,
			numParsed:           8,
		},
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {

			schema, labelIdents, metaIdents, parsedIdents := prepareSchema(bm.numLabels, bm.numMeta, bm.numParsed)
			baseTime := time.Unix(0, 1620000000000000000).UTC()

			rows1 := generateRows(labelIdents, metaIdents, parsedIdents, bm.numRowsFirstRecord, baseTime)
			record1 := rows1.Record(alloc, schema)
			defer record1.Release()

			rows2 := generateRows(labelIdents, metaIdents, parsedIdents, bm.numRowsSecondRecord, baseTime)
			record2 := rows2.Record(alloc, schema)
			defer record2.Release()

			b.ResetTimer()
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				rb := newStreamsResultBuilder(logproto.BACKWARD)
				// Collect records twice on purpose to see how efficient CollectRecord is when the builder already has
				// some data
				rb.CollectRecord(record1)
				rb.CollectRecord(record2)

				// Ensure the result is used to prevent compiler optimizations
				if rb.Len() != bm.numRowsFirstRecord+bm.numRowsSecondRecord {
					b.Fatalf("expected %d entries, got %d", bm.numRowsFirstRecord+bm.numRowsSecondRecord, rb.Len())
				}
			}
		})
	}
}

func prepareSchema(numLabels int, numMeta int, numParsed int) (*arrow.Schema, []*semconv.Identifier, []*semconv.Identifier, []*semconv.Identifier) {
	// Build schema
	colTs := semconv.ColumnIdentTimestamp
	colMsg := semconv.ColumnIdentMessage
	fields := []arrow.Field{
		semconv.FieldFromIdent(colTs, false),
		semconv.FieldFromIdent(colMsg, false),
	}

	// Add label columns
	labelIdents := make([]*semconv.Identifier, numLabels)
	for i := 0; i < numLabels; i++ {
		ident := semconv.NewIdentifier(
			fmt.Sprintf("label_%d", i),
			types.ColumnTypeLabel,
			types.Loki.String,
		)
		labelIdents[i] = ident
		fields = append(fields, semconv.FieldFromIdent(ident, false))
	}

	// Add metadata columns
	metaIdents := make([]*semconv.Identifier, numMeta)
	for i := 0; i < numMeta; i++ {
		ident := semconv.NewIdentifier(
			fmt.Sprintf("meta_%d", i),
			types.ColumnTypeMetadata,
			types.Loki.String,
		)
		metaIdents[i] = ident
		fields = append(fields, semconv.FieldFromIdent(ident, false))
	}

	// Add parsed columns
	parsedIdents := make([]*semconv.Identifier, numParsed)
	for i := 0; i < numParsed; i++ {
		ident := semconv.NewIdentifier(
			fmt.Sprintf("parsed_%d", i),
			types.ColumnTypeParsed,
			types.Loki.String,
		)
		parsedIdents[i] = ident
		fields = append(fields, semconv.FieldFromIdent(ident, false))
	}

	return arrow.NewSchema(fields, nil), labelIdents, metaIdents, parsedIdents
}

func generateRows(
	labelIdents []*semconv.Identifier,
	metaIdents []*semconv.Identifier,
	parsedIdents []*semconv.Identifier,
	numRows int,
	baseTime time.Time,
) arrowtest.Rows {
	rows := make(arrowtest.Rows, numRows)
	for rowIdx := 0; rowIdx < numRows; rowIdx++ {
		row := make(map[string]any)
		row[semconv.ColumnIdentTimestamp.FQN()] = baseTime.Add(time.Duration(rowIdx) * time.Nanosecond)
		row[semconv.ColumnIdentMessage.FQN()] = fmt.Sprintf("log line %d with some additional text to make it more realistic", rowIdx)

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
	return rows
}
