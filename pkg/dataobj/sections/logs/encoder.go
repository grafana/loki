package logs

import (
	"bytes"
	"errors"
	"math"

	"github.com/gogo/protobuf/proto"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/dataset"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/datasetmd"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/logsmd"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/streamio"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/util/bufpool"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/util/protocodec"
)

const (
	logsFormatVersion = 0x1
)

var (
	// errElementNoExist is used when a child element tries to notify its parent
	// of it closing but the parent doesn't have a child open. This would
	// indicate a bug in the encoder so it's not exposed to callers.
	errElementNoExist = errors.New("open element does not exist")
	errElementExist   = errors.New("open element already exists")
	errClosed         = errors.New("element is closed")
)

// encoder encodes an individual logs section in a data object.
//
// The zero value of encoder is ready for use.
type encoder struct {
	tenant string
	data   *bytes.Buffer

	columns   []*logsmd.ColumnDesc // closed columns.
	curColumn *logsmd.ColumnDesc   // curColumn is the currently open column.
	sortInfo  *datasetmd.SectionSortInfo
}

// OpenColumn opens a new column in the logs section. OpenColumn fails if there
// is another open column or if the encoder has been closed.
func (enc *encoder) OpenColumn(columnType logsmd.ColumnType, info *dataset.ColumnInfo) (*columnEncoder, error) {
	if enc.curColumn != nil {
		return nil, errElementExist
	}

	// MetadataOffset and MetadataSize aren't available until the column is
	// closed. We temporarily set these fields to the maximum values so they're
	// accounted for in the MetadataSize estimate.
	enc.curColumn = &logsmd.ColumnDesc{
		Type: columnType,
		Info: &datasetmd.ColumnInfo{
			Name:             info.Name,
			ValueType:        info.Type,
			PagesCount:       uint64(info.PagesCount),
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

	return newColumnEncoder(enc, enc.size()), nil
}

// size returns the current number of buffered data bytes.
func (enc *encoder) size() int {
	if enc.data == nil {
		return 0
	}
	return enc.data.Len()
}

// SetSortInfo sets the sort order information for the logs section.
// This should be called before committing the encoder.
func (enc *encoder) SetSortInfo(info *datasetmd.SectionSortInfo) {
	enc.sortInfo = info
}

// MetadataSize returns an estimate of the current size of the metadata for the
// section. MetadataSize includes an estimate for the currently open element.
func (enc *encoder) MetadataSize() int { return proto.Size(enc.Metadata()) }

func (enc *encoder) Metadata() proto.Message {
	columns := enc.columns[:len(enc.columns):cap(enc.columns)]
	if enc.curColumn != nil {
		columns = append(columns, enc.curColumn)
	}
	return &logsmd.Metadata{
		Columns:  columns,
		SortInfo: enc.sortInfo,
	}
}

// Flush writes the section to the given [dataobj.SectionWriter]. Flush
// returns an error if there is an open column.
//
// Flush returns 0, nil if there is no data to write.
//
// After Flush is called successfully, the encoder is reset to a fresh state
// and can be reused.
func (enc *encoder) Flush(w dataobj.SectionWriter) (int64, error) {
	if enc.curColumn != nil {
		return 0, errElementExist
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
	} else if err := protocodec.Encode(metadataBuffer, enc.Metadata()); err != nil {
		return 0, err
	}

	opts := &dataobj.WriteSectionOptions{Tenant: enc.tenant}
	n, err := w.WriteSection(opts, enc.data.Bytes(), metadataBuffer.Bytes())
	if err == nil {
		enc.Reset()
	}
	return n, err
}

// Reset resets the encoder to a fresh state, discarding any in-progress
// columns.
func (enc *encoder) Reset() {
	bufpool.PutUnsized(enc.data)
	enc.tenant = ""
	enc.data = nil
	enc.curColumn = nil
	enc.sortInfo = nil
}

// SetTenant sets the tenant who owns the streams section.
func (enc *encoder) SetTenant(tenant string) {
	enc.tenant = tenant
}

// append adds data and metadata to enc. append must only be called from child
// elements on Close and Discard. Discard calls must pass nil for both data and
// metadata to denote a discard.
func (enc *encoder) append(data, metadata []byte) error {
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

// columnEncoder encodes an individual column in a logs section.
// columnEncoder are created by [encoder].
type columnEncoder struct {
	parent *encoder

	startOffset int  // Byte offset in the section where the column starts.
	closed      bool // true if columnEncoder has been closed.

	data        *bytes.Buffer // All page data.
	pageHeaders []*logsmd.PageDesc

	memPages      []*dataset.MemPage // Pages to write.
	totalPageSize int                // Size of bytes across all pages.
}

func newColumnEncoder(parent *encoder, offset int) *columnEncoder {
	return &columnEncoder{
		parent:      parent,
		startOffset: offset,

		data: bufpool.GetUnsized(),
	}
}

// AppendPage appens a new [dataset.MemPage] to the column. AppendPage fails if
// the column has been closed.
func (enc *columnEncoder) AppendPage(page *dataset.MemPage) error {
	if enc.closed {
		return errClosed
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
func (enc *columnEncoder) MetadataSize() int { return proto.Size(enc.Metadata()) }

func (enc *columnEncoder) Metadata() proto.Message {
	return &logsmd.ColumnMetadata{Pages: enc.pageHeaders}
}

// Commit closes the column, flushing all data to the parent element. After
// Commit is called, the columnEncoder can no longer be modified.
func (enc *columnEncoder) Commit() error {
	if enc.closed {
		return errClosed
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

	if err := protocodec.Encode(metadataBuffer, enc.Metadata()); err != nil {
		return err
	}

	return enc.parent.append(enc.data.Bytes(), metadataBuffer.Bytes())
}

// Discard discards the column, discarding any data written to it. After
// Discard is called, the LogsColumnEncoder can no longer be modified.
func (enc *columnEncoder) Discard() error {
	if enc.closed {
		return errClosed
	}
	enc.closed = true

	defer bufpool.PutUnsized(enc.data)

	return enc.parent.append(nil, nil) // Notify parent of discard.
}
