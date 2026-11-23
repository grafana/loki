package columnar

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"math"

	"github.com/gogo/protobuf/proto"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/dataset"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/format"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/datasetmd"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/streamio"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/util/bufpool"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/util/protocodec"
)

// These are the encoding format versions of dataset-derived sections.
//
// Version 1 was the format of the original section implementations. Version 2
// merged them into one format.
//
// Version 3 is not a format version. Older writers used it as the section
// version of logs sections in schema sort order, which have format version 2.
const (
	// FormatVersionDescriptorChecksum keeps the CRC32C checksum of each page in
	// the page descriptor.
	FormatVersionDescriptorChecksum = format.ColumnarVersionDescriptorChecksum

	// FormatVersionPageChecksum keeps the CRC32C checksum of each page in the
	// last four bytes of the page, and leaves it out of the page descriptor.
	// The data size of a page includes the four bytes.
	FormatVersionPageChecksum = format.ColumnarVersionPageChecksum

	// DefaultFormatVersion is the version that an [Encoder] writes unless the
	// caller calls [Encoder.SetFormatVersion]. Section builders also use it as
	// the version of their section type.
	DefaultFormatVersion = FormatVersionPageChecksum
)

// IsSupportedVersion returns true if [NewDecoder] accepts the given format
// version. It returns false for 3, because 3 is a section version, not a format
// version. The logs Open function maps it to 2.
func IsSupportedVersion(version uint32) bool {
	return version == FormatVersionDescriptorChecksum || version == FormatVersionPageChecksum
}

var (
	// errElementNoExist is used when a child element tries to notify its parent
	// of it closing but the parent doesn't have a child open. This would
	// indicate a bug in the encoder so it's not exposed to callers.
	errElementNoExist = errors.New("open element does not exist")
	errElementExist   = errors.New("open element already exists")
	errClosed         = errors.New("element is closed")

	errChecksumInPage     = errors.New("page has its checksum in its data")
	errVersionAfterColumn = errors.New("format version set after opening a column")
)

// Encoder encodes an individual dataset-based section in a data object.
//
// Create an Encoder with [NewEncoder]. The zero value is not valid.
type Encoder struct {
	tenant   string
	sortInfo *datasetmd.SortInfo
	version  uint32

	data     *bytes.Buffer
	metadata *bytes.Buffer

	dictionary       []string
	dictionaryLookup map[string]uint32

	columns   []*datasetmd.ColumnDesc
	curColumn *datasetmd.ColumnDesc
}

// NewEncoder returns an [Encoder] that writes [DefaultFormatVersion].
func NewEncoder() *Encoder {
	return &Encoder{version: DefaultFormatVersion}
}

// SetTenant sets the tenant ID for the section. This must be called before
// calling [Encoder.Flush]. The set tenant is reset after flushing or calling
// [Encoder.Reset].
func (enc *Encoder) SetTenant(tenant string) { enc.tenant = tenant }

// SetSortInfo sets the sort order information for the section. This must be
// called before calling [Encoder.Flush]. The sort order information is reset
// after flushing or calling [Encoder.Reset].
func (enc *Encoder) SetSortInfo(info *datasetmd.SortInfo) { enc.sortInfo = info }

// SetFormatVersion sets the format version of the section. It returns an error
// if version is not supported. It also returns an error after the first call
// to [Encoder.OpenColumn]. Flush and [Encoder.Reset] set the version back to
// [DefaultFormatVersion].
//
// Readers take the format version from the section type. The section type must
// carry the same version. Logs sections of version 3 are the exception: their
// format version is 2.
func (enc *Encoder) SetFormatVersion(version uint32) error {
	if !IsSupportedVersion(version) {
		return fmt.Errorf("unsupported format version %d", version)
	}
	if enc.curColumn != nil || len(enc.columns) > 0 {
		return errVersionAfterColumn
	}
	enc.version = version
	return nil
}

// NumColumns returns the number of columns committed to the section.
func (enc *Encoder) NumColumns() int { return len(enc.columns) }

// OpenColumn opens a new column in the section. OpenColumn fails if there is
// another open column.
func (enc *Encoder) OpenColumn(desc *dataset.ColumnDesc) (*ColumnEncoder, error) {
	if enc.curColumn != nil {
		return nil, errElementExist
	}

	enc.curColumn = &datasetmd.ColumnDesc{
		Type: &datasetmd.ColumnType{
			Physical:   desc.Type.Physical,
			LogicalRef: enc.getDictionaryRef(desc.Type.Logical),
		},
		TagRef:           enc.getDictionaryRef(desc.Tag),
		RowsCount:        uint64(desc.RowsCount),
		ValuesCount:      uint64(desc.ValuesCount),
		Compression:      desc.Compression,
		UncompressedSize: uint64(desc.UncompressedSize),
		CompressedSize:   uint64(desc.CompressedSize),
		Statistics:       desc.Statistics,

		// These fields aren't available until the column is closed. We
		// temporarily set them to the maximum values so they're accounted for
		// when estimating metadata size.

		PagesCount:           math.MaxUint32,
		ColumnMetadataOffset: math.MaxUint32,
		ColumnMetadataLength: math.MaxUint32,
	}

	return newColumnEncoder(enc, enc.dataSize()), nil
}

func (enc *Encoder) getDictionaryRef(text string) uint32 {
	enc.initDictionary() // Make sure the dictionary is initialized.

	if idx, ok := enc.dictionaryLookup[text]; ok {
		return idx
	}

	idx := uint32(len(enc.dictionary))
	enc.dictionary = append(enc.dictionary, text)
	enc.dictionaryLookup[text] = idx

	return idx
}

func (enc *Encoder) initDictionary() {
	if len(enc.dictionary) > 0 && len(enc.dictionaryLookup) > 0 {
		return // Already initialized.
	}

	// Initialize the dictionary with index zero being reserved for the
	// empty string.
	enc.dictionary = []string{""}
	enc.dictionaryLookup = map[string]uint32{"": 0}
}

// dataSize returns the current number of buffered data bytes.
func (enc *Encoder) dataSize() int {
	if enc.data == nil {
		return 0
	}
	return enc.data.Len()
}

// appendColumn adds column data and metadata to enc. appendColumn must only be
// called from the current child [ColumnEncoder] on Close or Discard. Discard
// calls must pass nil for both data and metadata to denote a discard.
//
// numPages specifies how many pages were encoded in the column.
//
// enc *must never* retain data or metadata beyond the call to appendColumn.
func (enc *Encoder) appendColumn(numPages int, data, metadata []byte) error {
	if enc.curColumn == nil {
		return errElementNoExist
	}

	if len(data) == 0 && len(metadata) == 0 {
		// Column was discarded.
		enc.curColumn = nil
		return nil
	}

	// Initialize enc.data and enc.metadata.
	enc.initBuffers()

	// Update deferred fields now that we know their values.
	enc.curColumn.PagesCount = uint64(numPages)
	enc.curColumn.ColumnMetadataOffset = uint64(enc.metadata.Len())
	enc.curColumn.ColumnMetadataLength = uint64(len(metadata))

	// [bytes.Buffer.Write] never fails.
	_, _ = enc.data.Write(data)
	_, _ = enc.metadata.Write(metadata)

	enc.columns = append(enc.columns, enc.curColumn)
	enc.curColumn = nil
	return nil
}

func (enc *Encoder) initBuffers() {
	if enc.data != nil && enc.metadata != nil {
		return
	}

	enc.data = bufpool.GetUnsized()
	enc.metadata = bufpool.GetUnsized()

	// Format version 2 starts the metadata region with the format version. No
	// current reader reads it: they take the version from the section type. It
	// stays so that a reader of the first data object format, which kept the
	// version in the metadata, sees a newer version and stops.
	//
	// Format version 4 does not write it. The section type carries the version.
	if enc.version == FormatVersionDescriptorChecksum {
		_ = streamio.WriteUvarint(enc.metadata, uint64(FormatVersionDescriptorChecksum)) // [bytes.Buffer.WriteByte] never returns an error.
	}
}

// Flush writes the section to the given [dataobj.SectionWriter]. Flush returns
// an error if there is a currently open column.
//
// After Flush is called successfully, the encoder is reset to a fresh state and
// can be reused.
func (enc *Encoder) Flush(w dataobj.SectionWriter) (int64, error) {
	if enc.curColumn != nil {
		return 0, errElementExist
	}
	defer enc.Reset()

	// Perform last-minute initialization for any unused data.
	enc.initBuffers()
	enc.initDictionary()

	// Encode SectionMetadata into our buffer, but record the offset before and
	// after so we can store its offset/length into our info extension.
	startMetadataOffset := enc.metadata.Len()
	_ = protocodec.Encode(enc.metadata, enc.buildSectionMetadata()) // Writes to [bytes.Buffer] never fail
	endMetadataOffset := enc.metadata.Len()

	sectionInfoExtension := enc.buildSectionInfoExtension(
		startMetadataOffset,
		endMetadataOffset-startMetadataOffset,
	)

	extensionBuffer := bufpool.Get(protocodec.Size(sectionInfoExtension))
	defer bufpool.Put(extensionBuffer)

	_ = protocodec.Encode(extensionBuffer, sectionInfoExtension)

	opts := &dataobj.WriteSectionOptions{
		Tenant:        enc.tenant,
		ExtensionData: extensionBuffer.Bytes(),
	}
	return w.WriteSection(opts, enc.data.Bytes(), enc.metadata.Bytes())
}

func (enc *Encoder) buildSectionMetadata() *datasetmd.SectionMetadata {
	return &datasetmd.SectionMetadata{
		Columns:    enc.columns,
		Dictionary: enc.dictionary,
		SortInfo:   enc.sortInfo,
	}
}

func (enc *Encoder) buildSectionInfoExtension(offset, length int) *datasetmd.SectionInfoExtension {
	return &datasetmd.SectionInfoExtension{
		SectionMetadataOffset: uint64(offset),
		SectionMetadataLength: uint64(length),
	}
}

// Reset resets the encoder to a fresh state, discarding any in-progress columns.
func (enc *Encoder) Reset() {
	enc.tenant = ""
	enc.sortInfo = nil
	enc.version = DefaultFormatVersion

	bufpool.PutUnsized(enc.data)
	bufpool.PutUnsized(enc.metadata)
	enc.data = nil
	enc.metadata = nil

	enc.dictionary = nil
	enc.dictionaryLookup = nil

	enc.columns = nil
	enc.curColumn = nil
}

// The ColumnEncoder encodes data for a single column in a section.
// ColumnEncoders must be created by an [Encoder].
type ColumnEncoder struct {
	parent *Encoder

	dataOffset    int    // Byte offset in the data region where column starts.
	formatVersion uint32 // Format version of the section when the column opened.
	closed        bool   // True if ColumnEncoder has been closed.

	memPages      []*dataset.MemPage    // Pages to write.
	pageDescs     []*datasetmd.PageDesc // Page descriptions.
	totalPageSize int                   // Total size of all pages.
}

func newColumnEncoder(parent *Encoder, dataOffset int) *ColumnEncoder {
	return &ColumnEncoder{
		parent: parent,

		dataOffset:    dataOffset,
		formatVersion: parent.version,
	}
}

// AppendPage appends a new page to enc. The page must not be modified after
// passing to AppendPage.
//
// The page must hold its checksum in page.Desc.CRC32, not in its data. In
// format version 4, Commit writes the checksum after the data of the page.
//
// AppendPage fails if enc is closed or if the page has its checksum in its
// data.
func (enc *ColumnEncoder) AppendPage(page *dataset.MemPage) error {
	if enc.closed {
		return errClosed
	}
	// A page read from a section of format version 4 already ends with its
	// checksum, and its CRC32 is zero. Commit would write the zero as a second
	// checksum, or put zero in the descriptor. Either way, the page would fail
	// its checksum check on read.
	if format.ColumnarPageHasChecksum(page.Desc.FormatVersion) {
		return errChecksumInPage
	}

	checksumInPage := format.ColumnarPageHasChecksum(enc.formatVersion)
	pageSize := len(page.Data)
	if checksumInPage {
		pageSize += crc32.Size
	}

	// AppendPage does not validate the other fields of page.Desc.
	desc := &datasetmd.PageDesc{
		UncompressedSize: uint64(page.Desc.UncompressedSize),
		RowsCount:        uint64(page.Desc.RowCount),
		ValuesCount:      uint64(page.Desc.ValuesCount),
		Encoding:         page.Desc.Encoding,

		DataOffset: uint64(enc.dataOffset + enc.totalPageSize),
		DataSize:   uint64(pageSize),

		Statistics: page.Desc.Stats,
	}
	if !checksumInPage {
		desc.DeprecatedCrc32 = page.Desc.CRC32
	}

	enc.pageDescs = append(enc.pageDescs, desc)
	enc.memPages = append(enc.memPages, page)
	enc.totalPageSize += pageSize
	return nil
}

// Commit completes the column, appending it to the section. After Commit is
// called, enc can no longer be used.
//
// If no pages have been appnded, Commit appends an empty column to the section.
func (enc *ColumnEncoder) Commit() error {
	if enc.closed {
		return errClosed
	}
	enc.closed = true

	columnData := bufpool.Get(enc.totalPageSize)
	defer bufpool.PutUnsized(columnData)

	var checksum [crc32.Size]byte
	for _, p := range enc.memPages {
		_, _ = columnData.Write(p.Data) // [bytes.Buffer.Write] never fails.
		if format.ColumnarPageHasChecksum(enc.formatVersion) {
			binary.LittleEndian.PutUint32(checksum[:], p.Desc.CRC32)
			_, _ = columnData.Write(checksum[:])
		}
	}

	metadata := enc.buildMetadata()

	columnMetadata := bufpool.Get(protocodec.Size(metadata))
	defer bufpool.Put(columnMetadata)

	if err := protocodec.Encode(columnMetadata, metadata); err != nil {
		return err
	}

	return enc.parent.appendColumn(len(enc.memPages), columnData.Bytes(), columnMetadata.Bytes())
}

// Discard discards the column, discarding any data written to it. After Discard
// is called, enc can no longer be used.
func (enc *ColumnEncoder) Discard() error {
	if enc.closed {
		return errClosed
	}
	enc.closed = true

	return enc.parent.appendColumn(0, nil, nil) // Notify parent of discard.
}

// buildMetadata builds the metadata message for the column.
func (enc *ColumnEncoder) buildMetadata() proto.Message {
	return &datasetmd.ColumnMetadata{Pages: enc.pageDescs}
}
