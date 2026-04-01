package logs

import (
	"bytes"

	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/dataset"
	"github.com/grafana/loki/v3/pkg/util/loser"
)

// recordBatchSequence wraps a sorted []Record slice for use with the loser tree.
type recordBatchSequence struct {
	records []Record
	idx     int
}

func (s *recordBatchSequence) Next() bool {
	s.idx++
	return s.idx < len(s.records)
}

func recordBatchAt(s *recordBatchSequence) Record {
	return s.records[s.idx]
}

func recordBatchClose(s *recordBatchSequence) {}

// mergeRecordBatches merges sorted record batches into a single table using a
// k-way merge via loser tree. This avoids the encode-decode cycle of
// intermediate stripes.
func mergeRecordBatches(buf *tableBuffer, pageSize, pageRowCount int, compressionOpts *dataset.CompressionOptions, batches [][]Record, sortOrder SortOrder) *table {
	if len(batches) == 0 {
		return nil
	}

	// Single batch: just build directly.
	if len(batches) == 1 {
		return buildTableFromSorted(buf, pageSize, pageRowCount, compressionOpts, batches[0])
	}

	sequences := make([]*recordBatchSequence, 0, len(batches))
	for _, batch := range batches {
		if len(batch) == 0 {
			continue
		}
		sequences = append(sequences, &recordBatchSequence{
			records: batch,
			idx:     -1, // loser tree calls Next() to initialize
		})
	}

	if len(sequences) == 0 {
		return nil
	}

	maxRecord := Record{
		StreamID:  1<<63 - 1,
	}

	var lessFunc func(Record, Record) bool
	switch sortOrder {
	case SortStreamASC:
		lessFunc = func(a, b Record) bool {
			if a.StreamID != b.StreamID {
				return a.StreamID < b.StreamID
			}
			return b.TimestampNano < a.TimestampNano
		}
	case SortTimestampDESC:
		lessFunc = func(a, b Record) bool {
			if a.TimestampNano != b.TimestampNano {
				return b.TimestampNano < a.TimestampNano
			}
			return a.StreamID < b.StreamID
		}
	default:
		panic("invalid sort order")
	}

	tree := loser.New(sequences, maxRecord, recordBatchAt, lessFunc, recordBatchClose)
	defer tree.Close()

	buf.Reset()

	var (
		streamIDBuilder  = buf.StreamID(pageSize, pageRowCount)
		timestampBuilder = buf.Timestamp(pageSize, pageRowCount)
		messageBuilder   = buf.Message(pageSize, pageRowCount, compressionOpts)
	)

	metadataCache := make(map[string]*dataset.ColumnBuilder, 8)

	var prev Record
	row := 0
	for tree.Next() {
		seq := tree.Winner()
		record := seq.records[seq.idx]

		if row > 0 && equalRecordsMerge(prev, record) {
			continue
		}
		prev = record

		_ = streamIDBuilder.Append(row, dataset.Int64Value(record.StreamID))
		_ = timestampBuilder.Append(row, dataset.Int64Value(record.TimestampNano))
		_ = messageBuilder.Append(row, dataset.BinaryValue(record.Line))

		record.Metadata.Range(func(md labels.Label) {
			metadataBuilder, ok := metadataCache[md.Name]
			if !ok {
				metadataBuilder = buf.Metadata(md.Name, pageSize, pageRowCount, compressionOpts)
				metadataCache[md.Name] = metadataBuilder
			}
			_ = metadataBuilder.Append(row, dataset.BinaryValue(unsafeSlice(md.Value, 0)))
		})
		row++
	}

	table, err := buf.Flush()
	if err != nil {
		panic(err)
	}
	return table
}

// buildTableFromSorted builds a table from already-sorted records (no sort step).
func buildTableFromSorted(buf *tableBuffer, pageSize, pageRowCount int, compressionOpts *dataset.CompressionOptions, records []Record) *table {
	buf.Reset()

	var (
		streamIDBuilder  = buf.StreamID(pageSize, pageRowCount)
		timestampBuilder = buf.Timestamp(pageSize, pageRowCount)
		messageBuilder   = buf.Message(pageSize, pageRowCount, compressionOpts)
	)

	metadataCache := make(map[string]*dataset.ColumnBuilder, 8)

	var prev Record
	row := 0
	for _, record := range records {
		if equalRecords(prev, record) {
			continue
		}
		prev = record

		_ = streamIDBuilder.Append(row, dataset.Int64Value(record.StreamID))
		_ = timestampBuilder.Append(row, dataset.Int64Value(record.TimestampNano))
		_ = messageBuilder.Append(row, dataset.BinaryValue(record.Line))

		record.Metadata.Range(func(md labels.Label) {
			metadataBuilder, ok := metadataCache[md.Name]
			if !ok {
				metadataBuilder = buf.Metadata(md.Name, pageSize, pageRowCount, compressionOpts)
				metadataCache[md.Name] = metadataBuilder
			}
			_ = metadataBuilder.Append(row, dataset.BinaryValue(unsafeSlice(md.Value, 0)))
		})
		row++
	}

	table, err := buf.Flush()
	if err != nil {
		panic(err)
	}
	return table
}

// equalRecordsMerge checks record equality for deduplication during merge.
// Fast path: StreamID and Timestamp comparison short-circuits for 99%+ of rows.
func equalRecordsMerge(a, b Record) bool {
	// These two comparisons will short-circuit for the vast majority of rows,
	// avoiding the expensive metadata and line comparisons.
	if a.StreamID != b.StreamID || a.TimestampNano != b.TimestampNano {
		return false
	}
	if !bytes.Equal(a.Line, b.Line) {
		return false
	}
	return labels.Equal(a.Metadata, b.Metadata)
}

