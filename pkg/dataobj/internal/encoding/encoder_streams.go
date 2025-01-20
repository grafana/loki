package encoding

import (
	"bytes"
	"math"

	"github.com/gogo/protobuf/proto"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/dataset"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/datasetmd"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/streamsmd"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/streamio"
)

// StreamsEncoder encodes an individual streams section in a data object.
// StreamsEncoder are created by [Encoder]s.
type StreamsEncoder struct {
	parent *Encoder

	startOffset int  // Byte offset in the file where the column starts.
	closed      bool // true if StreamsEncoder has been closed.

	data      *bytes.Buffer
	columns   []*streamsmd.ColumnDesc // closed columns.
	curColumn *streamsmd.ColumnDesc   // curColumn is the currently open column.
}

func newStreamsEncoder(parent *Encoder, offset int) *StreamsEncoder {
	buf := bytesBufferPool.Get().(*bytes.Buffer)
	buf.Reset()

	return &StreamsEncoder{
		parent:      parent,
		startOffset: offset,

		data: buf,
	}
}

// OpenColumn opens a new column in the streams section. OpenColumn fails if
// there is another open column or if the StreamsEncoder has been closed.
func (enc *StreamsEncoder) OpenColumn(columnType streamsmd.ColumnType, info *dataset.ColumnInfo) (*StreamsColumnEncoder, error) {
	if enc.curColumn != nil {
		return nil, ErrElementExist
	} else if enc.closed {
		return nil, ErrClosed
	}

	// MetadataOffset and MetadataSize aren't available until the column is
	// closed. We temporarily set these fields to the maximum values so they're
	// accounted for in the MetadataSize estimate.
	enc.curColumn = &streamsmd.ColumnDesc{
		Type: columnType,
		Info: &datasetmd.ColumnInfo{
			Name:             info.Name,
			ValueType:        info.Type,
			RowsCount:        uint32(info.RowsCount),
			ValuesCount:      uint32(info.ValuesCount),
			Compression:      info.Compression,
			UncompressedSize: uint32(info.UncompressedSize),
			CompressedSize:   uint32(info.CompressedSize),
			Statistics:       info.Statistics,

			MetadataOffset: math.MaxUint32,
			MetadataSize:   math.MaxUint32,
		},
	}

	return newStreamsColumnEncoder(
		enc,
		enc.startOffset+enc.data.Len(),
	), nil
}

// MetadataSize returns an estimate of the current size of the metadata for the
// stream. MetadataSize includes an estimate for the currently open element.
func (enc *StreamsEncoder) MetadataSize() int { return elementMetadataSize(enc) }

func (enc *StreamsEncoder) metadata() proto.Message {
	columns := enc.columns[:len(enc.columns):cap(enc.columns)]
	if enc.curColumn != nil {
		columns = append(columns, enc.curColumn)
	}
	return &streamsmd.Metadata{Columns: columns}
}

// Commit closes the section, flushing all data to the parent element. After
// Commit is called, the StreamsEncoder can no longer be modified.
//
// Commit fails if there is an open column.
func (enc *StreamsEncoder) Commit() error {
	if enc.closed {
		return ErrClosed
	} else if enc.curColumn != nil {
		return ErrElementExist
	}
	enc.closed = true

	defer bytesBufferPool.Put(enc.data)

	if len(enc.columns) == 0 {
		// No data was written; discard.
		return enc.parent.append(nil, nil)
	}

	metadataBuffer := bytesBufferPool.Get().(*bytes.Buffer)
	metadataBuffer.Reset()
	defer bytesBufferPool.Put(metadataBuffer)

	// The section metadata should start with its version.
	if err := streamio.WriteUvarint(metadataBuffer, streamsFormatVersion); err != nil {
		return err
	} else if err := elementMetadataWrite(enc, metadataBuffer); err != nil {
		return err
	}
	return enc.parent.append(enc.data.Bytes(), metadataBuffer.Bytes())
}

// Discard discards the section, discarding any data written to it. After
// Discard is called, the StreamsEncoder can no longer be modified.
//
// Discard fails if there is an open column.
func (enc *StreamsEncoder) Discard() error {
	if enc.closed {
		return ErrClosed
	} else if enc.curColumn != nil {
		return ErrElementExist
	}
	enc.closed = true

	defer bytesBufferPool.Put(enc.data)

	return enc.parent.append(nil, nil)
}

// append adds data and metadata to enc. append must only be called from child
// elements on Close and Discard. Discard calls must pass nil for both data and
// metadata to denote a discard.
func (enc *StreamsEncoder) append(data, metadata []byte) error {
	if enc.closed {
		return ErrClosed
	} else if enc.curColumn == nil {
		return errElementNoExist
	}

	if len(data) == 0 && len(metadata) == 0 {
		// Column was discarded.
		enc.curColumn = nil
		return nil
	}

	enc.curColumn.Info.MetadataOffset = uint32(enc.startOffset + enc.data.Len() + len(data))
	enc.curColumn.Info.MetadataSize = uint32(len(metadata))

	// bytes.Buffer.Write never fails.
	enc.data.Grow(len(data) + len(metadata))
	_, _ = enc.data.Write(data)
	_, _ = enc.data.Write(metadata)

	enc.columns = append(enc.columns, enc.curColumn)
	enc.curColumn = nil
	return nil
}

// StreamsColumnEncoder encodes an individual column in a streams section.
// StreamsColumnEncoder are created by [StreamsEncoder].
type StreamsColumnEncoder struct {
	parent *StreamsEncoder

	startOffset int  // Byte offset in the file where the column starts.
	closed      bool // true if StreamsColumnEncoder has been closed.

	data        *bytes.Buffer // All page data.
	pageHeaders []*streamsmd.PageDesc

	memPages      []*dataset.MemPage // Pages to write.
	totalPageSize int                // Total size of all pages.
}

func newStreamsColumnEncoder(parent *StreamsEncoder, offset int) *StreamsColumnEncoder {
	buf := bytesBufferPool.Get().(*bytes.Buffer)
	buf.Reset()

	return &StreamsColumnEncoder{
		parent:      parent,
		startOffset: offset,

		data: buf,
	}
}

// AppendPage appens a new [dataset.MemPage] to the column. AppendPage fails if
// the column has been closed.
func (enc *StreamsColumnEncoder) AppendPage(page *dataset.MemPage) error {
	if enc.closed {
		return ErrClosed
	}

	// It's possible the caller can pass an incorrect value for UncompressedSize
	// and CompressedSize, but those fields are purely for stats so we don't
	// check it.
	enc.pageHeaders = append(enc.pageHeaders, &streamsmd.PageDesc{
		Info: &datasetmd.PageInfo{
			UncompressedSize: uint32(page.Info.UncompressedSize),
			CompressedSize:   uint32(page.Info.CompressedSize),
			Crc32:            page.Info.CRC32,
			RowsCount:        uint32(page.Info.RowCount),
			ValuesCount:      uint32(page.Info.ValuesCount),
			Encoding:         page.Info.Encoding,

			DataOffset: uint32(enc.startOffset + enc.totalPageSize),
			DataSize:   uint32(len(page.Data)),

			Statistics: page.Info.Stats,
		},
	})

	enc.memPages = append(enc.memPages, page)
	enc.totalPageSize += len(page.Data)
	return nil
}

// MetadataSize returns an estimate of the current size of the metadata for the
// column. MetadataSize does not include the size of data appended.
func (enc *StreamsColumnEncoder) MetadataSize() int { return elementMetadataSize(enc) }

func (enc *StreamsColumnEncoder) metadata() proto.Message {
	return &streamsmd.ColumnMetadata{Pages: enc.pageHeaders}
}

// Commit closes the column, flushing all data to the parent element. After
// Commit is called, the StreamsColumnEncoder can no longer be modified.
func (enc *StreamsColumnEncoder) Commit() error {
	if enc.closed {
		return ErrClosed
	}
	enc.closed = true

	defer bytesBufferPool.Put(enc.data)

	if len(enc.pageHeaders) == 0 {
		// No data was written; discard.
		return enc.parent.append(nil, nil)
	}

	// Write all pages. To avoid costly reallocations, we grow our buffer to fit
	// all data first.
	enc.data.Grow(enc.totalPageSize)
	for _, p := range enc.memPages {
		_, _ = enc.data.Write(p.Data) // bytes.Buffer.Write never fails.
	}

	metadataBuffer := bytesBufferPool.Get().(*bytes.Buffer)
	metadataBuffer.Reset()
	defer bytesBufferPool.Put(metadataBuffer)

	if err := elementMetadataWrite(enc, metadataBuffer); err != nil {
		return err
	}
	return enc.parent.append(enc.data.Bytes(), metadataBuffer.Bytes())
}

// Discard discards the column, discarding any data written to it. After
// Discard is called, the StreamsColumnEncoder can no longer be modified.
func (enc *StreamsColumnEncoder) Discard() error {
	if enc.closed {
		return ErrClosed
	}
	enc.closed = true

	defer bytesBufferPool.Put(enc.data)

	return enc.parent.append(nil, nil) // Notify parent of discard.
}
