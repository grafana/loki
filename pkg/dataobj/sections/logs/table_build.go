package logs

import (
	"bytes"
	"cmp"
	"slices"

	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/dataset"
)

// buildTable builds a table from the set of provided records. The records are
// sorted with [sortRecords] prior to building the table.
func buildTable(buf *tableBuffer, pageSize, pageRowCount int, compressionOpts dataset.CompressionOptions, records []Record, sortOrder SortOrder) *table {
	sortRecords(records, sortOrder)

	buf.Reset()

	var (
		streamIDBuilder  = buf.StreamID(pageSize, pageRowCount)
		timestampBuilder = buf.Timestamp(pageSize, pageRowCount)
		messageBuilder   = buf.Message(pageSize, pageRowCount, compressionOpts)
	)

	var prev Record
	row := 0
	for _, record := range records {
		if equalRecords(prev, record) {
			// Skip equal records
			continue
		}
		prev = record

		// Append only fails if given out-of-order data, where the provided row
		// number is less than the previous row number. That can't happen here, so
		// to keep the code readable we ignore the error values.

		_ = streamIDBuilder.Append(row, dataset.Int64Value(record.StreamID))
		_ = timestampBuilder.Append(row, dataset.Int64Value(record.Timestamp.UnixNano()))
		_ = messageBuilder.Append(row, dataset.BinaryValue(record.Line))

		record.Metadata.Range(func(md labels.Label) {
			// Passing around md.Value as an unsafe slice is safe here: appending
			// values is always read-only and the byte slice will never be mutated.
			metadataBuilder := buf.Metadata(md.Name, pageSize, pageRowCount, compressionOpts)
			_ = metadataBuilder.Append(row, dataset.BinaryValue(unsafeSlice(md.Value, 0)))
		})
		row++
	}

	table, err := buf.Flush()
	if err != nil {
		// Unreachable; we always ensure every required column is created.
		panic(err)
	}
	return table
}

// sortRecords sorts the set of records according to the specified sort order.
func sortRecords(records []Record, sortOrder SortOrder) {
	slices.SortFunc(records, func(a, b Record) int {
		switch sortOrder {
		case SortStreamASC:
			// Sort by [streamID ASC, timestamp DESC]
			if res := cmp.Compare(a.StreamID, b.StreamID); res != 0 {
				return res
			}
			return b.Timestamp.Compare(a.Timestamp)
		case SortTimestampDESC:
			// Sort by [timestamp DESC, streamID ASC]
			if res := b.Timestamp.Compare(a.Timestamp); res != 0 {
				return res
			}
			return cmp.Compare(a.StreamID, b.StreamID)
		default:
			panic("invalid sort order")
		}
	})
}

func equalRecords(a, b Record) bool {
	if a.StreamID != b.StreamID {
		return false
	}
	if a.Timestamp != b.Timestamp {
		return false
	}
	if !labels.Equal(a.Metadata, b.Metadata) {
		return false
	}
	return bytes.Equal(a.Line, b.Line)
}
