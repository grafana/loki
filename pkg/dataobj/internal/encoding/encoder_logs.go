package encoding

import (
	"bytes"
	"math"

	"github.com/gogo/protobuf/proto"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/dataset"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/datasetmd"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/logsmd"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/streamio"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/util/bufpool"
)

// LogsEncoder encodes an individual logs section in a data object.
//
// The zero value of LogsEncoder is ready for use.
type LogsEncoder struct {
	data *bytes.Buffer

	columns   []*logsmd.ColumnDesc // closed columns.
	curColumn *logsmd.ColumnDesc   // curColumn is the currently open column.
}

// OpenColumn opens a new column in the logs section. OpenColumn fails if there
// is another open column or if the LogsEncoder has been closed.
func (enc *LogsEncoder) OpenColumn(columnType logsmd.ColumnType, info *dataset.ColumnInfo) (*LogsColumnEncoder, error) {
	if enc.curColumn != nil {
		return nil, ErrElementExist
	}

	// MetadataOffset and MetadataSize aren't available until the column is
	// closed. We temporarily set these fields to the maximum values so they're
	// accounted for in the MetadataSize estimate.
	enc.curColumn = &logsmd.ColumnDesc{
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

	return newLogsColumnEncoder(enc, enc.size()), nil
}

// size returns the current number of buffered data bytes.
func (enc *LogsEncoder) size() int {
	if enc.data == nil {
		return 0
	}
	return enc.data.Len()
}

// MetadataSize returns an estimate of the current size of the metadata for the
// section. MetadataSize includes an estimate for the currently open element.
func (enc *LogsEncoder) MetadataSize() int { return ElementMetadataSize(enc) }

func (enc *LogsEncoder) Metadata() proto.Message {
	columns := enc.columns[:len(enc.columns):cap(enc.columns)]
	if enc.curColumn != nil {
		columns = append(columns, enc.curColumn)
	}
	return &logsmd.Metadata{Columns: columns}
}

// EncodeTo writes the section to the given [Encoder]. EncodeTo returns an
// error if there is an open column.
//
// EncodeTo returns 0, nil if there is no data to write.
//
// After EncodeTo is called successfully, the LogsEncoder is reset to a fresh
// state and can be reused.
func (enc *LogsEncoder) EncodeTo(dst *Encoder) (int64, error) {
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
	if err := streamio.WriteUvarint(metadataBuffer, logsFormatVersion); err != nil {
		return 0, err
	} else if err := ElementMetadataWrite(enc, metadataBuffer); err != nil {
		return 0, err
	}

	dst.AppendSection(SectionTypeLogs, enc.data.Bytes(), metadataBuffer.Bytes())
	return int64(len(enc.data.Bytes()) + len(metadataBuffer.Bytes())), nil
}

// Reset resets the LogsEncoder to a fresh state, discarding any in-progress
// columns.
func (enc *LogsEncoder) Reset() {
	bufpool.PutUnsized(enc.data)
	enc.data = nil
	enc.curColumn = nil
}

// append adds data and metadata to enc. append must only be called from child
// elements on Close and Discard. Discard calls must pass nil for both data and
// metadata to denote a discard.
func (enc *LogsEncoder) append(data, metadata []byte) error {
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

// LogsColumnEncoder encodes an individual column in a logs section.
// LogsColumnEncoder are created by [LogsEncoder].
type LogsColumnEncoder struct {
	parent *LogsEncoder

	startOffset int  // Byte offset in the section where the column starts.
	closed      bool // true if LogsColumnEncoder has been closed.

	data        *bytes.Buffer // All page data.
	pageHeaders []*logsmd.PageDesc

	memPages      []*dataset.MemPage // Pages to write.
	totalPageSize int                // Size of bytes across all pages.
}

func newLogsColumnEncoder(parent *LogsEncoder, offset int) *LogsColumnEncoder {
	return &LogsColumnEncoder{
		parent:      parent,
		startOffset: offset,

		data: bufpool.GetUnsized(),
	}
}

// AppendPage appens a new [dataset.MemPage] to the column. AppendPage fails if
// the column has been closed.
func (enc *LogsColumnEncoder) AppendPage(page *dataset.MemPage) error {
	if enc.closed {
		return ErrClosed
	}

	// It's possible the caller can pass an incorrect value for UncompressedSize
	// and CompressedSize, but those fields are purely for stats so we don't
	// check it.
	enc.pageHeaders = append(enc.pageHeaders, &logsmd.PageDesc{
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
func (enc *LogsColumnEncoder) MetadataSize() int { return ElementMetadataSize(enc) }

func (enc *LogsColumnEncoder) Metadata() proto.Message {
	return &logsmd.ColumnMetadata{Pages: enc.pageHeaders}
}

// Commit closes the column, flushing all data to the parent element. After
// Commit is called, the LogsColumnEncoder can no longer be modified.
func (enc *LogsColumnEncoder) Commit() error {
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

	if err := ElementMetadataWrite(enc, metadataBuffer); err != nil {
		return err
	}

	return enc.parent.append(enc.data.Bytes(), metadataBuffer.Bytes())
}

// Discard discards the column, discarding any data written to it. After
// Discard is called, the LogsColumnEncoder can no longer be modified.
func (enc *LogsColumnEncoder) Discard() error {
	if enc.closed {
		return ErrClosed
	}
	enc.closed = true

	defer bufpool.PutUnsized(enc.data)

	return enc.parent.append(nil, nil) // Notify parent of discard.
}
