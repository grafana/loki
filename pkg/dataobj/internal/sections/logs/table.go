package logs

import (
	"cmp"
	"context"
	"fmt"
	"slices"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/dataset"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/datasetmd"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/logsmd"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/result"
)

// A table is a collection of columns that form a logs section.
type table struct {
	StreamID  *tableColumn
	Timestamp *tableColumn
	Metadatas []*tableColumn
	Message   *tableColumn
}

type tableColumn struct {
	*dataset.MemColumn

	Type logsmd.ColumnType
}

var _ dataset.Dataset = (*table)(nil)

// ListColumns implements [dataset.Dataset].
func (t *table) ListColumns(_ context.Context) result.Seq[dataset.Column] {
	return result.Iter(func(yield func(dataset.Column) bool) error {
		if !yield(t.StreamID) {
			return nil
		}
		if !yield(t.Timestamp) {
			return nil
		}
		for _, metadata := range t.Metadatas {
			if !yield(metadata) {
				return nil
			}
		}
		if !yield(t.Message) {
			return nil
		}

		return nil
	})
}

// ListPages implements [dataset.Dataset].
func (t *table) ListPages(ctx context.Context, columns []dataset.Column) result.Seq[dataset.Pages] {
	return result.Iter(func(yield func(dataset.Pages) bool) error {
		for _, c := range columns {
			pages, err := result.Collect(c.ListPages(ctx))
			if err != nil {
				return err
			} else if !yield(dataset.Pages(pages)) {
				return nil
			}
		}

		return nil
	})
}

// ReadPages implements [dataset.Dataset].
func (t *table) ReadPages(ctx context.Context, pages []dataset.Page) result.Seq[dataset.PageData] {
	return result.Iter(func(yield func(dataset.PageData) bool) error {
		for _, p := range pages {
			data, err := p.ReadPage(ctx)
			if err != nil {
				return err
			} else if !yield(data) {
				return nil
			}
		}

		return nil
	})
}

// Size returns the total size of the table in bytes.
func (t *table) Size() int {
	var size int

	size += t.StreamID.ColumnInfo().CompressedSize
	size += t.Timestamp.ColumnInfo().CompressedSize
	for _, metadata := range t.Metadatas {
		size += metadata.ColumnInfo().CompressedSize
	}
	size += t.Message.ColumnInfo().CompressedSize

	return size
}

// A tableBuffer holds a set of column builders used for constructing tables.
// The zero value is ready for use.
type tableBuffer struct {
	streamID  *dataset.ColumnBuilder
	timestamp *dataset.ColumnBuilder

	metadatas      []*dataset.ColumnBuilder
	metadataLookup map[string]int                    // map of metadata key to index in metadatas
	usedMetadatas  map[*dataset.ColumnBuilder]string // metadata with its name.

	message *dataset.ColumnBuilder
}

// StreamID gets or creates a stream ID column for the buffer.
func (b *tableBuffer) StreamID(pageSize int) *dataset.ColumnBuilder {
	if b.streamID != nil {
		return b.streamID
	}

	col, err := dataset.NewColumnBuilder("", dataset.BuilderOptions{
		PageSizeHint: pageSize,
		Value:        datasetmd.VALUE_TYPE_INT64,
		Encoding:     datasetmd.ENCODING_TYPE_DELTA,
		Compression:  datasetmd.COMPRESSION_TYPE_NONE,
		Statistics: dataset.StatisticsOptions{
			StoreRangeStats: true,
		},
	})
	if err != nil {
		// We control the Value/Encoding tuple so this can't fail; if it does,
		// we're left in an unrecoverable state where nothing can be encoded
		// properly so we panic.
		panic(fmt.Sprintf("creating stream ID column: %v", err))
	}

	b.streamID = col
	return col
}

// Timestamp gets or creates a timestamp column for the buffer.
func (b *tableBuffer) Timestamp(pageSize int) *dataset.ColumnBuilder {
	if b.timestamp != nil {
		return b.timestamp
	}

	col, err := dataset.NewColumnBuilder("", dataset.BuilderOptions{
		PageSizeHint: pageSize,
		Value:        datasetmd.VALUE_TYPE_INT64,
		Encoding:     datasetmd.ENCODING_TYPE_DELTA,
		Compression:  datasetmd.COMPRESSION_TYPE_NONE,
		Statistics: dataset.StatisticsOptions{
			StoreRangeStats: true,
		},
	})
	if err != nil {
		// We control the Value/Encoding tuple so this can't fail; if it does,
		// we're left in an unrecoverable state where nothing can be encoded
		// properly so we panic.
		panic(fmt.Sprintf("creating timestamp column: %v", err))
	}

	b.timestamp = col
	return col
}

// Metadata gets or creates a metadata column for the buffer. To remove created
// metadata columns, call [tableBuffer.CleanupMetadatas].
func (b *tableBuffer) Metadata(key string, pageSize int, compressionOpts dataset.CompressionOptions) *dataset.ColumnBuilder {
	if b.usedMetadatas == nil {
		b.usedMetadatas = make(map[*dataset.ColumnBuilder]string)
	}

	index, ok := b.metadataLookup[key]
	if ok {
		builder := b.metadatas[index]
		b.usedMetadatas[builder] = key
		return builder
	}

	col, err := dataset.NewColumnBuilder(key, dataset.BuilderOptions{
		PageSizeHint:       pageSize,
		Value:              datasetmd.VALUE_TYPE_STRING,
		Encoding:           datasetmd.ENCODING_TYPE_PLAIN,
		Compression:        datasetmd.COMPRESSION_TYPE_ZSTD,
		CompressionOptions: compressionOpts,
		Statistics: dataset.StatisticsOptions{
			StoreRangeStats:       true,
			StoreCardinalityStats: true,
		},
	})
	if err != nil {
		// We control the Value/Encoding tuple so this can't fail; if it does,
		// we're left in an unrecoverable state where nothing can be encoded
		// properly so we panic.
		panic(fmt.Sprintf("creating metadata column: %v", err))
	}

	b.metadatas = append(b.metadatas, col)

	if b.metadataLookup == nil {
		b.metadataLookup = make(map[string]int)
	}
	b.metadataLookup[key] = len(b.metadatas) - 1
	b.usedMetadatas[col] = key
	return col
}

// Message gets or creates a message column for the buffer.
func (b *tableBuffer) Message(pageSize int, compressionOpts dataset.CompressionOptions) *dataset.ColumnBuilder {
	if b.message != nil {
		return b.message
	}

	col, err := dataset.NewColumnBuilder("", dataset.BuilderOptions{
		PageSizeHint:       pageSize,
		Value:              datasetmd.VALUE_TYPE_BYTE_ARRAY,
		Encoding:           datasetmd.ENCODING_TYPE_PLAIN,
		Compression:        datasetmd.COMPRESSION_TYPE_ZSTD,
		CompressionOptions: compressionOpts,

		// We explicitly don't have range stats for the message column:
		//
		// A "min log line" and "max log line" isn't very valuable, and since log
		// lines can be quite long, it would consume a fair amount of metadata.
		Statistics: dataset.StatisticsOptions{
			StoreRangeStats: false,
		},
	})
	if err != nil {
		// We control the Value/Encoding tuple so this can't fail; if it does,
		// we're left in an unrecoverable state where nothing can be encoded
		// properly so we panic.
		panic(fmt.Sprintf("creating messages column: %v", err))
	}

	b.message = col
	return col
}

// Reset resets the buffer to its initial state.
func (b *tableBuffer) Reset() {
	if b.streamID != nil {
		b.streamID.Reset()
	}
	if b.timestamp != nil {
		b.timestamp.Reset()
	}
	if b.message != nil {
		b.message.Reset()
	}
	for _, md := range b.metadatas {
		md.Reset()
	}

	// We don't want to keep all metadata columns around forever, so we only
	// retain the columns that were used in the last Flush.
	var (
		newMetadatas      = make([]*dataset.ColumnBuilder, 0, len(b.metadatas))
		newMetadataLookup = make(map[string]int, len(b.metadatas))
	)
	for _, md := range b.metadatas {
		if b.usedMetadatas == nil {
			break // Nothing was used.
		}

		key, used := b.usedMetadatas[md]
		if !used {
			continue
		}

		newMetadatas = append(newMetadatas, md)
		newMetadataLookup[key] = len(newMetadatas) - 1
	}
	b.metadatas = newMetadatas
	b.metadataLookup = newMetadataLookup
	clear(b.usedMetadatas) // Reset the used cache for next time.
}

// Flush flushes the buffer into a table. Flush returns an error if the stream,
// timestamp, or messages column was never appended to.
//
// Only metadata columns that were appended to since the last flush are included in the table.
func (b *tableBuffer) Flush() (*table, error) {
	defer b.Reset()

	if b.streamID == nil {
		return nil, fmt.Errorf("no stream column")
	} else if b.timestamp == nil {
		return nil, fmt.Errorf("no timestamp column")
	} else if b.message == nil {
		return nil, fmt.Errorf("no message column")
	}

	var (
		// Flush never returns an error so we ignore it here to keep the code simple.
		//
		// TODO(rfratto): remove error return from Flush to clean up code.

		streamID, _  = b.streamID.Flush()
		timestamp, _ = b.timestamp.Flush()
		messages, _  = b.message.Flush()

		metadatas = make([]*tableColumn, 0, len(b.metadatas))
	)

	for _, metadataBuilder := range b.metadatas {
		if b.usedMetadatas == nil {
			continue
		} else if _, ok := b.usedMetadatas[metadataBuilder]; !ok {
			continue
		}

		// Each metadata column may have a different number of rows compared to
		// other columns. Since adding NULLs isn't free, we don't call Backfill
		// here.
		metadata, _ := metadataBuilder.Flush()
		metadatas = append(metadatas, &tableColumn{metadata, logsmd.COLUMN_TYPE_METADATA})
	}

	// Sort metadata columns by name for consistency.
	slices.SortFunc(metadatas, func(a, b *tableColumn) int {
		return cmp.Compare(a.ColumnInfo().Name, b.ColumnInfo().Name)
	})

	return &table{
		StreamID:  &tableColumn{streamID, logsmd.COLUMN_TYPE_STREAM_ID},
		Timestamp: &tableColumn{timestamp, logsmd.COLUMN_TYPE_TIMESTAMP},
		Metadatas: metadatas,
		Message:   &tableColumn{messages, logsmd.COLUMN_TYPE_MESSAGE},
	}, nil
}
