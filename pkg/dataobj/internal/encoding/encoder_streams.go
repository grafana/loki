package encoding

import (
	"bytes"
	"math"

	"github.com/gogo/protobuf/proto"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/dataset"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/datasetmd"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/streamsmd"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/streamio"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/util/bufpool"
)

// StreamsEncoder encodes an individual streams section in a data object.
// StreamsEncoder are created by [Encoder]s.
//
// The zero value of StreamsEncoder is ready for use.
type StreamsEncoder struct {
	data *bytes.Buffer

	columns   []*streamsmd.ColumnDesc // closed columns.
	curColumn *streamsmd.ColumnDesc   // curColumn is the currently open column.
}

// NewStreamsEncoder creates a new StreamsEncoder.
func NewStreamsEncoder() *StreamsEncoder {
	return &StreamsEncoder{}
}

// OpenColumn opens a new column in the streams section. OpenColumn fails if
// there is another open column.
func (enc *StreamsEncoder) OpenColumn(columnType streamsmd.ColumnType, info *dataset.ColumnInfo) (*StreamsColumnEncoder, error) {
	if enc.curColumn != nil {
		return nil, ErrElementExist
	}

	// MetadataOffset and MetadataSize aren't available until the column is
	// closed. We temporarily set these fields to the maximum values so they're
	// accounted for in the MetadataSize estimate.
	enc.curColumn = &streamsmd.ColumnDesc{
		Type: columnType,
		Info: &datasetmd.ColumnInfo{
			Name:             info.Name,
			ValueType:        info.Type,
			RowsCount:        uint64(info.RowsCount),
			ValuesCount:      uint64(info.ValuesCount),
			Compression:      info.Compression,
			UncompressedSize: uint64(info.UncompressedSize),
			CompressedSize:   uint64(info.CompressedSize),
			Statistics:       info.Statistics,

			MetadataOffset: math.MaxUint32,
			MetadataSize:   math.MaxUint32,
		},
	}

	return newStreamsColumnEncoder(enc, enc.size()), nil
}

// size returns the current number of buffered data bytes.
func (enc *StreamsEncoder) size() int {
	if enc.data == nil {
		return 0
	}
	return enc.data.Len()
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

// EncodeTo writes the section to the given [Encoder]. EncodeTo returns an
// error if there is an open column.
//
// EncodeTo returns 0, nil if there is no data to write.
//
// After EncodeTo is called successfully, the StreamsEncoder is reset to a
// fresh state and can be reused.
func (enc *StreamsEncoder) EncodeTo(dst *Encoder) (int64, error) {
	if enc.curColumn != nil {
		return 0, ErrElementExist
	}
	defer enc.Reset()

	if len(enc.columns) == 0 {
		return 0, nil
	}

	metadataBuffer := bufpool.GetUnsized()
	defer bufpool.PutUnsized(metadataBuffer)

	// The section metadata should start with its version.
	if err := streamio.WriteUvarint(metadataBuffer, streamsFormatVersion); err != nil {
		return 0, err
	} else if err := elementMetadataWrite(enc, metadataBuffer); err != nil {
		return 0, err
	}

	dst.AppendSection(SectionTypeStreams, enc.data.Bytes(), metadataBuffer.Bytes())
	return int64(len(enc.data.Bytes()) + len(metadataBuffer.Bytes())), nil
}

// Reset resets the StreamsEncoder to a fresh state, discarding any in-progress
// columns.
func (enc *StreamsEncoder) Reset() {
	bufpool.PutUnsized(enc.data)
	enc.data = nil
	enc.curColumn = nil
}

// append adds data and metadata to enc. append must only be called from child
// elements on Close and Discard. Discard calls must pass nil for both data and
// metadata to denote a discard.
func (enc *StreamsEncoder) append(data, metadata []byte) error {
	if enc.curColumn == nil {
		return errElementNoExist
	}

	if len(data) == 0 && len(metadata) == 0 {
		// Column was discarded.
		enc.curColumn = nil
		return nil
	}

	if enc.data == nil {
		enc.data = bufpool.GetUnsized()
	}

	enc.curColumn.Info.MetadataOffset = uint64(enc.data.Len() + len(data))
	enc.curColumn.Info.MetadataSize = uint64(len(metadata))

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

	startOffset int  // Byte offset in the section where the column starts.
	closed      bool // true if StreamsColumnEncoder has been closed.

	data        *bytes.Buffer // All page data.
	pageHeaders []*streamsmd.PageDesc

	memPages      []*dataset.MemPage // Pages to write.
	totalPageSize int                // Total size of all pages.
}

func newStreamsColumnEncoder(parent *StreamsEncoder, offset int) *StreamsColumnEncoder {
	return &StreamsColumnEncoder{
		parent:      parent,
		startOffset: offset,

		data: bufpool.GetUnsized(),
	}
}

// AppendPage appends a new [dataset.MemPage] to the column. AppendPage fails if
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
			UncompressedSize: uint64(page.Info.UncompressedSize),
			CompressedSize:   uint64(page.Info.CompressedSize),
			Crc32:            page.Info.CRC32,
			RowsCount:        uint64(page.Info.RowCount),
			ValuesCount:      uint64(page.Info.ValuesCount),
			Encoding:         page.Info.Encoding,

			DataOffset: uint64(enc.startOffset + enc.totalPageSize),
			DataSize:   uint64(len(page.Data)),

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

	defer bufpool.PutUnsized(enc.data)

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

	metadataBuffer := bufpool.GetUnsized()
	defer bufpool.PutUnsized(metadataBuffer)

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

	defer bufpool.PutUnsized(enc.data)

	return enc.parent.append(nil, nil) // Notify parent of discard.
}
