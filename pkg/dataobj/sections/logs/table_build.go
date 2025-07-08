package logs

import (
	"cmp"
	"fmt"
	"slices"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/dataset"
)

// buildTable builds a table from the set of provided records. The records are
// sorted with [sortRecords] prior to building the table.
func buildTable(buf *tableBuffer, pageSize int, compressionOpts dataset.CompressionOptions, records []Record) *table {
	fmt.Printf("buildTable: %d records\n", len(records))
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

		err := streamIDBuilder.Append(i, dataset.Int64Value(record.StreamID))
		if err != nil {
			fmt.Printf("streamIDBuilder.Append: %v\n", err)
			panic(err)
		}
		err = timestampBuilder.Append(i, dataset.Int64Value(record.Timestamp.UnixNano()))
		if err != nil {
			fmt.Printf("timestampBuilder.Append: %v\n", err)
			panic(err)
		}
		err = messageBuilder.Append(i, dataset.ByteArrayValue(record.Line))
		if err != nil {
			fmt.Printf("messageBuilder.Append: %v\n", err)
			panic(err)
		}

		for _, md := range record.Metadata {
			// Passing around md.Value as an unsafe slice is safe here: appending
			// values is always read-only and the byte slice will never be mutated.
			metadataBuilder := buf.Metadata(md.Name, pageSize, compressionOpts)
			err := metadataBuilder.Append(i, dataset.ByteArrayValue(unsafeSlice(md.Value, 0)))
			if err != nil {
				fmt.Printf("metadataBuilder.Append: %v\n", err)
				panic(err)
			}
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
