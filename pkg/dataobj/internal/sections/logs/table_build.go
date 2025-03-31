package logs

import (
	"cmp"
	"slices"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/dataset"
)

// buildTable builds a table from the set of provided records. The records are
// sorted with [sortRecords] prior to building the table.
func buildTable(buf *tableBuffer, pageSize int, compressionOpts dataset.CompressionOptions, records []Record) *table {
	sortRecords(records)

	buf.Reset()

	var (
		streamIDBuilder  = buf.StreamID(pageSize)
		timestampBuilder = buf.Timestamp(pageSize)
		messageBuilder   = buf.Message(pageSize, compressionOpts)
	)

	for i, record := range records {
		// Append only fails if given out-of-order data, where the provided row
		// number is less than the previous row number. That can't happen here, so
		// to keep the code readable we ignore the error values.

		_ = streamIDBuilder.Append(i, dataset.Int64Value(record.StreamID))
		_ = timestampBuilder.Append(i, dataset.Int64Value(record.Timestamp.UnixNano()))
		_ = messageBuilder.Append(i, dataset.ByteArrayValue(record.Line))

		for _, md := range record.Metadata {
			metadataBuilder := buf.Metadata(md.Name, pageSize, compressionOpts)
			_ = metadataBuilder.Append(i, dataset.StringValue(md.Value))
		}
	}

	table, err := buf.Flush()
	if err != nil {
		// Unreachable; we always ensure every required column is created.
		panic(err)
	}
	return table
}

// sortRecords sorts the set of records by stream ID and timestamp.
func sortRecords(records []Record) {
	slices.SortFunc(records, func(a, b Record) int {
		if res := cmp.Compare(a.StreamID, b.StreamID); res != 0 {
			return res
		}
		return a.Timestamp.Compare(b.Timestamp)
	})
}
