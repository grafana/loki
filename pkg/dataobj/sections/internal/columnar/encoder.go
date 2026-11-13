package columnar

import (
	"bytes"
	"errors"
	"math"

	"github.com/gogo/protobuf/proto"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/dataset"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/datasetmd"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/streamio"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/util/bufpool"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/util/protocodec"
)

// FormatVersion is the current version the encoding format for dataset-derived
// sections.
//
// FormatVersion started at 2 to help consolidate all the duplicated encoding
// code across the original section implementations (which were all collectively
// at v1).
const FormatVersion = 2

var (
	// errElementNoExist is used when a child element tries to notify its parent
	// of it closing but the parent doesn't have a child open. This would
	// indicate a bug in the encoder so it's not exposed to callers.
	errElementNoExist = errors.New("open element does not exist")
	errElementExist   = errors.New("open element already exists")
	errClosed         = errors.New("element is closed")
)

// Encoder encodes an individual dataset-based section in a data object.
//
// The zero value of Encoder is ready for use.
type Encoder struct {
	tenant   string
	sortInfo *datasetmd.SortInfo

	// pageData holds the data of every committed page in section order. Flush
	// copies it into one buffer of exactly dataLen bytes. A growing buffer
	// would double in size and hold the old copy while it grows.
	pageData [][]byte
	dataLen  int
	metadata *bytes.Buffer

	dictionary       []string
	dictionaryLookup map[string]uint32

	columns   []*datasetmd.ColumnDesc
	curColumn *datasetmd.ColumnDesc
}

// SetTenant sets the tenant ID for the section. This must be called before
// calling [Encoder.Flush]. The set tenant is reset after flushing or calling
// [Encoder.Reset].
func (enc *Encoder) SetTenant(tenant string) { enc.tenant = tenant }

// SetSortInfo sets the sort order information for the section. This must be
// called before calling [Encoder.Flush]. The sort order information is reset
// after flushing or calling [Encoder.Reset].
func (enc *Encoder) SetSortInfo(info *datasetmd.SortInfo) { enc.sortInfo = info }

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
func (enc *Encoder) dataSize() int { return enc.dataLen }

// appendColumn adds the data of pages and the column metadata to enc.
// appendColumn must only be called from the current child [ColumnEncoder] on
// Close or Discard. Discard calls must pass nil for both pages and metadata to
// denote a discard.
//
// dataSize is the total length of the data of pages, in bytes.
//
// enc keeps references to the page data until the next Flush or Reset, so the
// caller must not modify it before then. enc does not retain metadata beyond
// the call to appendColumn.
func (enc *Encoder) appendColumn(pages []*dataset.MemPage, dataSize int, metadata []byte) error {
	if enc.curColumn == nil {
		return errElementNoExist
	}

	if len(pages) == 0 && len(metadata) == 0 {
		// Column was discarded.
		enc.curColumn = nil
		return nil
	}

	// Initialize enc.metadata.
	enc.initBuffers()

	// Update deferred fields now that we know their values.
	enc.curColumn.PagesCount = uint64(len(pages))
	enc.curColumn.ColumnMetadataOffset = uint64(enc.metadata.Len())
	enc.curColumn.ColumnMetadataLength = uint64(len(metadata))

	for _, p := range pages {
		enc.pageData = append(enc.pageData, p.Data)
	}
	enc.dataLen += dataSize
	_, _ = enc.metadata.Write(metadata) // [bytes.Buffer.Write] never fails.

	enc.columns = append(enc.columns, enc.curColumn)
	enc.curColumn = nil
	return nil
}

func (enc *Encoder) initBuffers() {
	if enc.metadata != nil {
		return
	}

	enc.metadata = bufpool.GetUnsized()

	// Initialize the metadata buffer with the format version. Section
	// implementations will use the version stored in the SectionType, but we
	// write the format version to the metadata anyway as the first byte so that
	// older versions of dataobj readers (where the version was encoded in
	// metadata) recognizes that it's a newer format and abort.
	//
	// TODO(rfratto): remove this once we've fully rolled out the new format,
	// and there's no more instances of Loki which are reading the v1 format.
	// Since this byte is written but never read, it can be removed without
	// needing a new format version or a breaking change.
	_ = streamio.WriteUvarint(enc.metadata, FormatVersion) // [bytes.Buffer.WriteByte] never returns an error.
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
	data := make([]byte, 0, enc.dataLen)
	for _, p := range enc.pageData {
		data = append(data, p...)
	}
	return w.WriteSection(opts, data, enc.metadata.Bytes())
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

	bufpool.PutUnsized(enc.metadata)
	enc.pageData = nil
	enc.dataLen = 0
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

	dataOffset int  // Byte offset in the data region where column starts.
	closed     bool // True if ColumnEncoder has been closed.

	memPages      []*dataset.MemPage    // Pages to write.
	pageDescs     []*datasetmd.PageDesc // Page descriptions.
	totalPageSize int                   // Total size of all pages.
}

func newColumnEncoder(parent *Encoder, dataOffset int) *ColumnEncoder {
	return &ColumnEncoder{
		parent: parent,

		dataOffset: dataOffset,
	}
}

// AppendPage appends a new page to enc. The page must not be modified after
// passing to AppendPage.
//
// AppendPage fails if enc is closed.
func (enc *ColumnEncoder) AppendPage(page *dataset.MemPage) error {
	if enc.closed {
		return errClosed
	}

	// NOTE(rfratto): The caller can pass invalid values for the page info, but
	// these don't impact encoding so we don't provide any validation.
	enc.pageDescs = append(enc.pageDescs, &datasetmd.PageDesc{
		UncompressedSize: uint64(page.Desc.UncompressedSize),
		CompressedSize:   uint64(page.Desc.CompressedSize),
		Crc32:            page.Desc.CRC32,
		RowsCount:        uint64(page.Desc.RowCount),
		ValuesCount:      uint64(page.Desc.ValuesCount),
		Encoding:         page.Desc.Encoding,

		DataOffset: uint64(enc.dataOffset + enc.totalPageSize),
		DataSize:   uint64(len(page.Data)),

		Statistics: page.Desc.Stats,
	})

	enc.memPages = append(enc.memPages, page)
	enc.totalPageSize += len(page.Data)
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

	metadata := enc.buildMetadata()

	columnMetadata := bufpool.Get(protocodec.Size(metadata))
	defer bufpool.Put(columnMetadata)

	if err := protocodec.Encode(columnMetadata, metadata); err != nil {
		return err
	}

	return enc.parent.appendColumn(enc.memPages, enc.totalPageSize, columnMetadata.Bytes())
}

// Discard discards the column, discarding any data written to it. After Discard
// is called, enc can no longer be used.
func (enc *ColumnEncoder) Discard() error {
	if enc.closed {
		return errClosed
	}
	enc.closed = true

	return enc.parent.appendColumn(nil, 0, nil) // Notify parent of discard.
}

// buildMetadata builds the metadata message for the column.
func (enc *ColumnEncoder) buildMetadata() proto.Message {
	return &datasetmd.ColumnMetadata{Pages: enc.pageDescs}
}
