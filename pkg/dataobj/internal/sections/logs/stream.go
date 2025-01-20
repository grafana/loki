package logs

import (
	"context"
	"fmt"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/dataset"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/datasetmd"
)

// A stream is an accmulation of [Record]s within an individual stream. Streams
// are initially accumulated separately to more efficiently sort the data.
type stream struct {
	id       int64
	pageSize int

	timestamps *dataset.ColumnBuilder

	metadatas      []*dataset.ColumnBuilder
	metadataLookup map[string]int // map of metadata key to index in metadatas

	messages *dataset.ColumnBuilder

	closers []release // Release functions for returning pooled column builders.

	rows int
}

func newStream(id int64, pageSize int) *stream {
	// We control the Value/Encoding tuple so this can't fail; if it does,
	// we're left in an unrecoverable state where nothing can be encoded
	// properly so we panic.

	// TODO(rfratto): streams disappear from memory upon flush, so we should pool
	// these.

	var closers []release

	timestamps, release, err := getColumnBuilder("", dataset.BuilderOptions{
		PageSizeHint: pageSize,
		Value:        datasetmd.VALUE_TYPE_INT64,
		Encoding:     datasetmd.ENCODING_TYPE_DELTA,
		Compression:  datasetmd.COMPRESSION_TYPE_NONE,
	})
	if err != nil {
		panic(fmt.Sprintf("creating timestamp column: %v", err))
	}
	closers = append(closers, release)

	messages, release, err := getColumnBuilder("", dataset.BuilderOptions{
		PageSizeHint: pageSize,
		Value:        datasetmd.VALUE_TYPE_STRING,
		Encoding:     datasetmd.ENCODING_TYPE_PLAIN,
		Compression:  datasetmd.COMPRESSION_TYPE_ZSTD,
	})
	if err != nil {
		panic(fmt.Sprintf("creating message column: %v", err))
	}
	closers = append(closers, release)

	return &stream{
		id:       id,
		pageSize: pageSize,

		timestamps: timestamps,

		metadataLookup: make(map[string]int),

		messages: messages,

		closers: closers,
	}
}

func (s *stream) Append(entry Record) {
	if entry.StreamID != s.id {
		panic(fmt.Sprintf("logs.stream.Append: record with stream ID %d appended to stream %d", entry.StreamID, s.id))
	}

	// We ignore the errors below; they only fail if given out-of-order data
	// (where the row number is less than the previous row number), which can't
	// ever happen here.

	_ = s.timestamps.Append(s.rows, dataset.Int64Value(entry.Timestamp.UnixNano()))
	_ = s.messages.Append(s.rows, dataset.StringValue(entry.Line))

	for _, m := range entry.Metadata {
		col := s.getMetadataColumn(m.Name)
		_ = col.Append(s.rows, dataset.StringValue(m.Value))
	}

	s.rows++
}

func (s *stream) getMetadataColumn(key string) *dataset.ColumnBuilder {
	idx, ok := s.metadataLookup[key]
	if !ok {
		col, release, err := getColumnBuilder(key, dataset.BuilderOptions{
			PageSizeHint: s.pageSize,
			Value:        datasetmd.VALUE_TYPE_STRING,
			Encoding:     datasetmd.ENCODING_TYPE_PLAIN,
			Compression:  datasetmd.COMPRESSION_TYPE_ZSTD,
		})
		if err != nil {
			// We control the Value/Encoding tuple so this can't fail; if it does,
			// we're left in an unrecoverable state where nothing can be encoded
			// properly so we panic.
			panic(fmt.Sprintf("creating metadata column: %v", err))
		}
		s.closers = append(s.closers, release)

		s.metadatas = append(s.metadatas, col)
		s.metadataLookup[key] = len(s.metadatas) - 1
		return col
	}
	return s.metadatas[idx]
}

// EstimatedSize returns the estimated size of the stream in bytes.
func (s *stream) EstimatedSize() int {
	var size int

	size += s.rows // 1 byte per ID
	size += s.timestamps.EstimatedSize()
	size += s.messages.EstimatedSize()

	for _, md := range s.metadatas {
		size += md.EstimatedSize()
	}

	return size
}

func (s *stream) Build() (dataset.Dataset, error) {
	// Our columns are ordered as follows:
	//
	// 1. Timestamp
	// 2. Metadata columns
	// 3. Message
	//
	// Do *not* change this order without updating [Logs.buildDataset]!
	//
	// TODO(rfratto): find a clean way to decorate columns with additional
	// metadata so we don't have to rely on order.
	columns := make([]*dataset.MemColumn, 0, 2+len(s.metadatas))

	// Flush never returns an error so we ignore it here to keep the code simple.
	//
	// TODO(rfratto): remove error return from Flush to clean up code.
	timestamp, _ := s.timestamps.Flush()
	columns = append(columns, timestamp)

	for _, mdBuilder := range s.metadatas {
		mdBuilder.Backfill(s.rows)

		mdColumn, _ := mdBuilder.Flush()
		columns = append(columns, mdColumn)
	}

	messages, _ := s.messages.Flush()
	columns = append(columns, messages)

	// Sort the columns by timestamp. We don't need to pass a "real" context here
	// because dset is in memory and will be able to immediately fetch pages.
	dset := dataset.FromMemory(columns)
	return dataset.Sort(context.Background(), dset, []dataset.Column{timestamp}, s.pageSize)
}

// Close releases column builders associated with the stream.
func (s *stream) Close() {
	for _, c := range s.closers {
		c()
	}
}
