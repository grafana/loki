package parquet

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"io"
	"sort"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/parquet-go/parquet-go/encoding/thrift"
	"github.com/parquet-go/parquet-go/format"
)

const (
	defaultDictBufferSize = 8192
	defaultReadBufferSize = 4096
)

// File represents a parquet file. The layout of a Parquet file can be found
// here: https://github.com/apache/parquet-format#file-format
type File struct {
	metadata      format.FileMetaData
	protocol      thrift.CompactProtocol
	reader        io.ReaderAt
	size          int64
	schema        *Schema
	root          *Column
	columnIndexes []format.ColumnIndex
	offsetIndexes []format.OffsetIndex
	rowGroups     []RowGroup
	config        *FileConfig
}

// OpenFile opens a parquet file and reads the content between offset 0 and the given
// size in r.
//
// Only the parquet magic bytes and footer are read, column chunks and other
// parts of the file are left untouched; this means that successfully opening
// a file does not validate that the pages have valid checksums.
func OpenFile(r io.ReaderAt, size int64, options ...FileOption) (*File, error) {
	b := make([]byte, 8)
	c, err := NewFileConfig(options...)
	if err != nil {
		return nil, err
	}
	f := &File{reader: r, size: size, config: c}

	if _, err := readAt(r, b[:4], 0); err != nil {
		return nil, fmt.Errorf("reading magic header of parquet file: %w", err)
	}
	if string(b[:4]) != "PAR1" {
		return nil, fmt.Errorf("invalid magic header of parquet file: %q", b[:4])
	}

	if cast, ok := f.reader.(interface{ SetMagicFooterSection(offset, length int64) }); ok {
		cast.SetMagicFooterSection(size-8, 8)
	}
	if n, err := r.ReadAt(b[:8], size-8); n != 8 {
		return nil, fmt.Errorf("reading magic footer of parquet file: %w", err)
	}
	if string(b[4:8]) != "PAR1" {
		return nil, fmt.Errorf("invalid magic footer of parquet file: %q", b[4:8])
	}

	footerSize := int64(binary.LittleEndian.Uint32(b[:4]))
	footerData := make([]byte, footerSize)

	if cast, ok := f.reader.(interface{ SetFooterSection(offset, length int64) }); ok {
		cast.SetFooterSection(size-(footerSize+8), footerSize)
	}
	if _, err := f.readAt(footerData, size-(footerSize+8)); err != nil {
		return nil, fmt.Errorf("reading footer of parquet file: %w", err)
	}
	if err := thrift.Unmarshal(&f.protocol, footerData, &f.metadata); err != nil {
		return nil, fmt.Errorf("reading parquet file metadata: %w", err)
	}
	if len(f.metadata.Schema) == 0 {
		return nil, ErrMissingRootColumn
	}

	if !c.SkipPageIndex {
		if f.columnIndexes, f.offsetIndexes, err = f.ReadPageIndex(); err != nil {
			return nil, fmt.Errorf("reading page index of parquet file: %w", err)
		}
	}

	if f.root, err = openColumns(f); err != nil {
		return nil, fmt.Errorf("opening columns of parquet file: %w", err)
	}

	var schema *Schema
	if c.Schema != nil {
		schema = c.Schema
	} else {
		schema = NewSchema(f.root.Name(), f.root)
	}
	columns := make([]*Column, 0, numLeafColumnsOf(f.root))
	f.schema = schema
	f.root.forEachLeaf(func(c *Column) { columns = append(columns, c) })

	rowGroups := make([]fileRowGroup, len(f.metadata.RowGroups))
	for i := range rowGroups {
		rowGroups[i].init(f, schema, columns, &f.metadata.RowGroups[i])
	}
	f.rowGroups = make([]RowGroup, len(rowGroups))
	for i := range rowGroups {
		f.rowGroups[i] = &rowGroups[i]
	}

	if !c.SkipBloomFilters {
		section := io.NewSectionReader(r, 0, size)
		rbuf, rbufpool := getBufioReader(section, c.ReadBufferSize)
		defer putBufioReader(rbuf, rbufpool)

		header := format.BloomFilterHeader{}
		compact := thrift.CompactProtocol{}
		decoder := thrift.NewDecoder(compact.NewReader(rbuf))

		for i := range rowGroups {
			g := &rowGroups[i]

			for j := range g.columns {
				c := g.columns[j].(*fileColumnChunk)

				if offset := c.chunk.MetaData.BloomFilterOffset; offset > 0 {
					section.Seek(offset, io.SeekStart)
					rbuf.Reset(section)

					header = format.BloomFilterHeader{}
					if err := decoder.Decode(&header); err != nil {
						return nil, fmt.Errorf("decoding bloom filter header: %w", err)
					}

					offset, _ = section.Seek(0, io.SeekCurrent)
					offset -= int64(rbuf.Buffered())

					if cast, ok := r.(interface{ SetBloomFilterSection(offset, length int64) }); ok {
						bloomFilterOffset := c.chunk.MetaData.BloomFilterOffset
						bloomFilterLength := (offset - bloomFilterOffset) + int64(header.NumBytes)
						cast.SetBloomFilterSection(bloomFilterOffset, bloomFilterLength)
					}

					c.bloomFilter = newBloomFilter(r, offset, &header)
				}
			}
		}
	}

	sortKeyValueMetadata(f.metadata.KeyValueMetadata)
	return f, nil
}

// ReadPageIndex reads the page index section of the parquet file f.
//
// If the file did not contain a page index, the method returns two empty slices
// and a nil error.
//
// Only leaf columns have indexes, the returned indexes are arranged using the
// following layout:
//
//	------------------
//	| col 0: chunk 0 |
//	------------------
//	| col 1: chunk 0 |
//	------------------
//	| ...            |
//	------------------
//	| col 0: chunk 1 |
//	------------------
//	| col 1: chunk 1 |
//	------------------
//	| ...            |
//	------------------
//
// This method is useful in combination with the SkipPageIndex option to delay
// reading the page index section until after the file was opened. Note that in
// this case the page index is not cached within the file, programs are expected
// to make use of independently from the parquet package.
func (f *File) ReadPageIndex() ([]format.ColumnIndex, []format.OffsetIndex, error) {
	if len(f.metadata.RowGroups) == 0 {
		return nil, nil, nil
	}

	columnIndexOffset := f.metadata.RowGroups[0].Columns[0].ColumnIndexOffset
	offsetIndexOffset := f.metadata.RowGroups[0].Columns[0].OffsetIndexOffset
	columnIndexLength := int64(0)
	offsetIndexLength := int64(0)

	forEachColumnChunk := func(do func(int, int, *format.ColumnChunk) error) error {
		for i := range f.metadata.RowGroups {
			for j := range f.metadata.RowGroups[i].Columns {
				c := &f.metadata.RowGroups[i].Columns[j]
				if err := do(i, j, c); err != nil {
					return err
				}
			}
		}
		return nil
	}

	forEachColumnChunk(func(_, _ int, c *format.ColumnChunk) error {
		columnIndexLength += int64(c.ColumnIndexLength)
		offsetIndexLength += int64(c.OffsetIndexLength)
		return nil
	})

	if columnIndexLength == 0 && offsetIndexLength == 0 {
		return nil, nil, nil
	}

	numRowGroups := len(f.metadata.RowGroups)
	numColumns := len(f.metadata.RowGroups[0].Columns)
	numColumnChunks := numRowGroups * numColumns

	columnIndexes := make([]format.ColumnIndex, numColumnChunks)
	offsetIndexes := make([]format.OffsetIndex, numColumnChunks)
	indexBuffer := make([]byte, max(int(columnIndexLength), int(offsetIndexLength)))

	if columnIndexOffset > 0 {
		columnIndexData := indexBuffer[:columnIndexLength]

		if cast, ok := f.reader.(interface{ SetColumnIndexSection(offset, length int64) }); ok {
			cast.SetColumnIndexSection(columnIndexOffset, columnIndexLength)
		}
		if _, err := f.readAt(columnIndexData, columnIndexOffset); err != nil {
			return nil, nil, fmt.Errorf("reading %d bytes column index at offset %d: %w", columnIndexLength, columnIndexOffset, err)
		}

		err := forEachColumnChunk(func(i, j int, c *format.ColumnChunk) error {
			// Some parquet files are missing the column index on some columns.
			//
			// An example of this file is testdata/alltypes_tiny_pages_plain.parquet
			// which was added in https://github.com/apache/parquet-testing/pull/24.
			if c.ColumnIndexOffset > 0 {
				offset := c.ColumnIndexOffset - columnIndexOffset
				length := int64(c.ColumnIndexLength)
				buffer := columnIndexData[offset : offset+length]
				if err := thrift.Unmarshal(&f.protocol, buffer, &columnIndexes[(i*numColumns)+j]); err != nil {
					return fmt.Errorf("decoding column index: rowGroup=%d columnChunk=%d/%d: %w", i, j, numColumns, err)
				}
			}
			return nil
		})
		if err != nil {
			return nil, nil, err
		}
	}

	if offsetIndexOffset > 0 {
		offsetIndexData := indexBuffer[:offsetIndexLength]

		if cast, ok := f.reader.(interface{ SetOffsetIndexSection(offset, length int64) }); ok {
			cast.SetOffsetIndexSection(offsetIndexOffset, offsetIndexLength)
		}
		if _, err := f.readAt(offsetIndexData, offsetIndexOffset); err != nil {
			return nil, nil, fmt.Errorf("reading %d bytes offset index at offset %d: %w", offsetIndexLength, offsetIndexOffset, err)
		}

		err := forEachColumnChunk(func(i, j int, c *format.ColumnChunk) error {
			if c.OffsetIndexOffset > 0 {
				offset := c.OffsetIndexOffset - offsetIndexOffset
				length := int64(c.OffsetIndexLength)
				buffer := offsetIndexData[offset : offset+length]
				if err := thrift.Unmarshal(&f.protocol, buffer, &offsetIndexes[(i*numColumns)+j]); err != nil {
					return fmt.Errorf("decoding column index: rowGroup=%d columnChunk=%d/%d: %w", i, j, numColumns, err)
				}
			}
			return nil
		})
		if err != nil {
			return nil, nil, err
		}
	}

	return columnIndexes, offsetIndexes, nil
}

// NumRows returns the number of rows in the file.
func (f *File) NumRows() int64 { return f.metadata.NumRows }

// RowGroups returns the list of row groups in the file.
func (f *File) RowGroups() []RowGroup { return f.rowGroups }

// Root returns the root column of f.
func (f *File) Root() *Column { return f.root }

// Schema returns the schema of f.
func (f *File) Schema() *Schema { return f.schema }

// Metadata returns the metadata of f.
func (f *File) Metadata() *format.FileMetaData { return &f.metadata }

// Size returns the size of f (in bytes).
func (f *File) Size() int64 { return f.size }

// ReadAt reads bytes into b from f at the given offset.
//
// The method satisfies the io.ReaderAt interface.
func (f *File) ReadAt(b []byte, off int64) (int, error) {
	if off < 0 || off >= f.size {
		return 0, io.EOF
	}

	if limit := f.size - off; limit < int64(len(b)) {
		n, err := f.readAt(b[:limit], off)
		if err == nil {
			err = io.EOF
		}
		return n, err
	}

	return f.readAt(b, off)
}

// ColumnIndexes returns the page index of the parquet file f.
//
// If the file did not contain a column index, the method returns an empty slice
// and nil error.
func (f *File) ColumnIndexes() []format.ColumnIndex { return f.columnIndexes }

// OffsetIndexes returns the page index of the parquet file f.
//
// If the file did not contain an offset index, the method returns an empty
// slice and nil error.
func (f *File) OffsetIndexes() []format.OffsetIndex { return f.offsetIndexes }

// Lookup returns the value associated with the given key in the file key/value
// metadata.
//
// The ok boolean will be true if the key was found, false otherwise.
func (f *File) Lookup(key string) (value string, ok bool) {
	return lookupKeyValueMetadata(f.metadata.KeyValueMetadata, key)
}

func (f *File) hasIndexes() bool {
	return f.columnIndexes != nil && f.offsetIndexes != nil
}

var _ io.ReaderAt = (*File)(nil)

func sortKeyValueMetadata(keyValueMetadata []format.KeyValue) {
	sort.Slice(keyValueMetadata, func(i, j int) bool {
		switch {
		case keyValueMetadata[i].Key < keyValueMetadata[j].Key:
			return true
		case keyValueMetadata[i].Key > keyValueMetadata[j].Key:
			return false
		default:
			return keyValueMetadata[i].Value < keyValueMetadata[j].Value
		}
	})
}

func lookupKeyValueMetadata(keyValueMetadata []format.KeyValue, key string) (value string, ok bool) {
	i := sort.Search(len(keyValueMetadata), func(i int) bool {
		return keyValueMetadata[i].Key >= key
	})
	if i == len(keyValueMetadata) || keyValueMetadata[i].Key != key {
		return "", false
	}
	return keyValueMetadata[i].Value, true
}

type fileRowGroup struct {
	schema   *Schema
	rowGroup *format.RowGroup
	columns  []ColumnChunk
	sorting  []SortingColumn
	config   *FileConfig
}

func (g *fileRowGroup) init(file *File, schema *Schema, columns []*Column, rowGroup *format.RowGroup) {
	g.schema = schema
	g.rowGroup = rowGroup
	g.config = file.config
	g.columns = make([]ColumnChunk, len(rowGroup.Columns))
	g.sorting = make([]SortingColumn, len(rowGroup.SortingColumns))
	fileColumnChunks := make([]fileColumnChunk, len(rowGroup.Columns))

	for i := range g.columns {
		fileColumnChunks[i] = fileColumnChunk{
			file:     file,
			column:   columns[i],
			rowGroup: rowGroup,
			chunk:    &rowGroup.Columns[i],
		}

		if file.hasIndexes() {
			j := (int(rowGroup.Ordinal) * len(columns)) + i
			fileColumnChunks[i].columnIndex.Store(&file.columnIndexes[j])
			fileColumnChunks[i].offsetIndex.Store(&file.offsetIndexes[j])
		}

		g.columns[i] = &fileColumnChunks[i]
	}

	for i := range g.sorting {
		g.sorting[i] = &fileSortingColumn{
			column:     columns[rowGroup.SortingColumns[i].ColumnIdx],
			descending: rowGroup.SortingColumns[i].Descending,
			nullsFirst: rowGroup.SortingColumns[i].NullsFirst,
		}
	}
}

func (g *fileRowGroup) Schema() *Schema                 { return g.schema }
func (g *fileRowGroup) NumRows() int64                  { return g.rowGroup.NumRows }
func (g *fileRowGroup) ColumnChunks() []ColumnChunk     { return g.columns }
func (g *fileRowGroup) SortingColumns() []SortingColumn { return g.sorting }
func (g *fileRowGroup) Rows() Rows                      { return newRowGroupRows(g, g.config.ReadMode) }

type fileSortingColumn struct {
	column     *Column
	descending bool
	nullsFirst bool
}

func (s *fileSortingColumn) Path() []string   { return s.column.Path() }
func (s *fileSortingColumn) Descending() bool { return s.descending }
func (s *fileSortingColumn) NullsFirst() bool { return s.nullsFirst }
func (s *fileSortingColumn) String() string {
	b := new(strings.Builder)
	if s.nullsFirst {
		b.WriteString("nulls_first+")
	}
	if s.descending {
		b.WriteString("descending(")
	} else {
		b.WriteString("ascending(")
	}
	b.WriteString(columnPath(s.Path()).String())
	b.WriteString(")")
	return b.String()
}

type fileColumnChunk struct {
	file        *File
	column      *Column
	bloomFilter *bloomFilter
	rowGroup    *format.RowGroup
	columnIndex atomic.Pointer[format.ColumnIndex]
	offsetIndex atomic.Pointer[format.OffsetIndex]
	chunk       *format.ColumnChunk
}

func (c *fileColumnChunk) Type() Type {
	return c.column.Type()
}

func (c *fileColumnChunk) Column() int {
	return int(c.column.Index())
}

func (c *fileColumnChunk) Pages() Pages {
	r := new(filePages)
	r.init(c)
	return r
}

func (c *fileColumnChunk) ColumnIndex() (ColumnIndex, error) {
	index, err := c.readColumnIndex()
	if err != nil {
		return nil, err
	}
	if index == nil || c.chunk.ColumnIndexOffset == 0 {
		return nil, ErrMissingColumnIndex
	}
	return fileColumnIndex{c}, nil
}

func (c *fileColumnChunk) OffsetIndex() (OffsetIndex, error) {
	index, err := c.readOffsetIndex()
	if err != nil {
		return nil, err
	}
	if index == nil || c.chunk.OffsetIndexOffset == 0 {
		return nil, ErrMissingOffsetIndex
	}
	return (*fileOffsetIndex)(index), nil
}

func (c *fileColumnChunk) BloomFilter() BloomFilter {
	if c.bloomFilter == nil {
		return nil
	}
	return c.bloomFilter
}

func (c *fileColumnChunk) NumValues() int64 {
	return c.chunk.MetaData.NumValues
}

func (c *fileColumnChunk) readColumnIndex() (*format.ColumnIndex, error) {
	if index := c.columnIndex.Load(); index != nil {
		return index, nil
	}
	chunkMeta := c.file.metadata.RowGroups[c.rowGroup.Ordinal].Columns[c.Column()]
	offset, length := chunkMeta.ColumnIndexOffset, chunkMeta.ColumnIndexLength
	if offset == 0 {
		return nil, nil
	}

	indexData := make([]byte, int(length))
	var columnIndex format.ColumnIndex
	if _, err := readAt(c.file.reader, indexData, offset); err != nil {
		return nil, fmt.Errorf("read %d bytes column index at offset %d: %w", length, offset, err)
	}
	if err := thrift.Unmarshal(&c.file.protocol, indexData, &columnIndex); err != nil {
		return nil, fmt.Errorf("decode column index: rowGroup=%d columnChunk=%d/%d: %w", c.rowGroup.Ordinal, c.Column(), len(c.rowGroup.Columns), err)
	}
	index := &columnIndex
	// We do a CAS (and Load on CAS failure) instead of a simple Store for
	// the nice property that concurrent calling goroutines will only ever
	// observe a single pointer value for the result.
	if !c.columnIndex.CompareAndSwap(nil, index) {
		// another goroutine populated it since we last read the pointer
		return c.columnIndex.Load(), nil
	}
	return index, nil
}

func (c *fileColumnChunk) readOffsetIndex() (*format.OffsetIndex, error) {
	if index := c.offsetIndex.Load(); index != nil {
		return index, nil
	}
	chunkMeta := c.file.metadata.RowGroups[c.rowGroup.Ordinal].Columns[c.Column()]
	offset, length := chunkMeta.OffsetIndexOffset, chunkMeta.OffsetIndexLength
	if offset == 0 {
		return nil, nil
	}

	indexData := make([]byte, int(length))
	var offsetIndex format.OffsetIndex
	if _, err := readAt(c.file.reader, indexData, offset); err != nil {
		return nil, fmt.Errorf("read %d bytes offset index at offset %d: %w", length, offset, err)
	}
	if err := thrift.Unmarshal(&c.file.protocol, indexData, &offsetIndex); err != nil {
		return nil, fmt.Errorf("decode offset index: rowGroup=%d columnChunk=%d/%d: %w", c.rowGroup.Ordinal, c.Column(), len(c.rowGroup.Columns), err)
	}
	index := &offsetIndex
	if !c.offsetIndex.CompareAndSwap(nil, index) {
		// another goroutine populated it since we last read the pointer
		return c.offsetIndex.Load(), nil
	}
	return index, nil
}

type filePages struct {
	chunk    *fileColumnChunk
	rbuf     *bufio.Reader
	rbufpool *sync.Pool
	section  io.SectionReader

	protocol thrift.CompactProtocol
	decoder  thrift.Decoder

	baseOffset int64
	dataOffset int64
	dictOffset int64
	index      int
	skip       int64
	dictionary Dictionary

	bufferSize int
}

func (f *filePages) init(c *fileColumnChunk) {
	f.chunk = c
	f.baseOffset = c.chunk.MetaData.DataPageOffset
	f.dataOffset = f.baseOffset
	f.bufferSize = c.file.config.ReadBufferSize

	if c.chunk.MetaData.DictionaryPageOffset != 0 {
		f.baseOffset = c.chunk.MetaData.DictionaryPageOffset
		f.dictOffset = f.baseOffset
	}

	f.section = *io.NewSectionReader(c.file, f.baseOffset, c.chunk.MetaData.TotalCompressedSize)
	f.rbuf, f.rbufpool = getBufioReader(&f.section, f.bufferSize)
	f.decoder.Reset(f.protocol.NewReader(f.rbuf))
}

func (f *filePages) ReadPage() (Page, error) {
	if f.chunk == nil {
		return nil, io.EOF
	}

	for {
		// Instantiate a new format.PageHeader for each page.
		//
		// A previous implementation reused page headers to save allocations.
		// https://github.com/segmentio/parquet-go/pull/484
		// The optimization turned out to be less effective than expected,
		// because all the values referenced by pointers in the page header
		// are lost when the header is reset and put back in the pool.
		// https://github.com/parquet-go/parquet-go/pull/11
		//
		// Even after being reset, reusing page headers still produced instability
		// issues.
		// https://github.com/parquet-go/parquet-go/issues/70
		header := new(format.PageHeader)
		if err := f.decoder.Decode(header); err != nil {
			return nil, err
		}
		data, err := f.readPage(header, f.rbuf)
		if err != nil {
			return nil, err
		}

		var page Page
		switch header.Type {
		case format.DataPageV2:
			page, err = f.readDataPageV2(header, data)
		case format.DataPage:
			page, err = f.readDataPageV1(header, data)
		case format.DictionaryPage:
			// Sometimes parquet files do not have the dictionary page offset
			// recorded in the column metadata. We account for this by lazily
			// reading dictionary pages when we encounter them.
			err = f.readDictionaryPage(header, data)
		default:
			err = fmt.Errorf("cannot read values of type %s from page", header.Type)
		}

		data.unref()

		if err != nil {
			return nil, fmt.Errorf("decoding page %d of column %q: %w", f.index, f.columnPath(), err)
		}

		if page == nil {
			continue
		}

		f.index++
		if f.skip == 0 {
			return page, nil
		}

		// TODO: what about pages that don't embed the number of rows?
		// (data page v1 with no offset index in the column chunk).
		numRows := page.NumRows()

		if numRows <= f.skip {
			Release(page)
		} else {
			tail := page.Slice(f.skip, numRows)
			Release(page)
			f.skip = 0
			return tail, nil
		}

		f.skip -= numRows
	}
}

func (f *filePages) readDictionary() error {
	chunk := io.NewSectionReader(f.chunk.file, f.baseOffset, f.chunk.chunk.MetaData.TotalCompressedSize)
	rbuf, pool := getBufioReader(chunk, f.bufferSize)
	defer putBufioReader(rbuf, pool)

	decoder := thrift.NewDecoder(f.protocol.NewReader(rbuf))

	header := new(format.PageHeader)

	if err := decoder.Decode(header); err != nil {
		return err
	}

	page := buffers.get(int(header.CompressedPageSize))
	defer page.unref()

	if _, err := io.ReadFull(rbuf, page.data); err != nil {
		return err
	}

	return f.readDictionaryPage(header, page)
}

func (f *filePages) readDictionaryPage(header *format.PageHeader, page *buffer) error {
	if header.DictionaryPageHeader == nil {
		return ErrMissingPageHeader
	}
	d, err := f.chunk.column.decodeDictionary(DictionaryPageHeader{header.DictionaryPageHeader}, page, header.UncompressedPageSize)
	if err != nil {
		return err
	}
	f.dictionary = d
	return nil
}

func (f *filePages) readDataPageV1(header *format.PageHeader, page *buffer) (Page, error) {
	if header.DataPageHeader == nil {
		return nil, ErrMissingPageHeader
	}
	if isDictionaryFormat(header.DataPageHeader.Encoding) && f.dictionary == nil {
		if err := f.readDictionary(); err != nil {
			return nil, err
		}
	}
	return f.chunk.column.decodeDataPageV1(DataPageHeaderV1{header.DataPageHeader}, page, f.dictionary, header.UncompressedPageSize)
}

func (f *filePages) readDataPageV2(header *format.PageHeader, page *buffer) (Page, error) {
	if header.DataPageHeaderV2 == nil {
		return nil, ErrMissingPageHeader
	}
	if isDictionaryFormat(header.DataPageHeaderV2.Encoding) && f.dictionary == nil {
		// If the program seeked to a row passed the first page, the dictionary
		// page may not have been seen, in which case we have to lazily load it
		// from the beginning of column chunk.
		if err := f.readDictionary(); err != nil {
			return nil, err
		}
	}
	return f.chunk.column.decodeDataPageV2(DataPageHeaderV2{header.DataPageHeaderV2}, page, f.dictionary, header.UncompressedPageSize)
}

func (f *filePages) readPage(header *format.PageHeader, reader *bufio.Reader) (*buffer, error) {
	page := buffers.get(int(header.CompressedPageSize))
	defer page.unref()

	if _, err := io.ReadFull(reader, page.data); err != nil {
		return nil, err
	}

	if header.CRC != 0 {
		headerChecksum := uint32(header.CRC)
		bufferChecksum := crc32.ChecksumIEEE(page.data)

		if headerChecksum != bufferChecksum {
			// The parquet specs indicate that corruption errors could be
			// handled gracefully by skipping pages, tho this may not always
			// be practical. Depending on how the pages are consumed,
			// missing rows may cause unpredictable behaviors in algorithms.
			//
			// For now, we assume these errors to be fatal, but we may
			// revisit later and improve error handling to be more resilient
			// to data corruption.
			return nil, fmt.Errorf("crc32 checksum mismatch in page of column %q: want=0x%08X got=0x%08X: %w",
				f.columnPath(),
				headerChecksum,
				bufferChecksum,
				ErrCorrupted,
			)
		}
	}

	page.ref()
	return page, nil
}

func (f *filePages) SeekToRow(rowIndex int64) (err error) {
	if f.chunk == nil {
		return io.ErrClosedPipe
	}
	if index := f.chunk.offsetIndex.Load(); index == nil {
		_, err = f.section.Seek(f.dataOffset-f.baseOffset, io.SeekStart)
		f.skip = rowIndex
		f.index = 0
		if f.dictOffset > 0 {
			f.index = 1
		}
	} else {
		pages := index.PageLocations
		index := sort.Search(len(pages), func(i int) bool {
			return pages[i].FirstRowIndex > rowIndex
		}) - 1
		if index < 0 {
			return ErrSeekOutOfRange
		}
		_, err = f.section.Seek(pages[index].Offset-f.baseOffset, io.SeekStart)
		f.skip = rowIndex - pages[index].FirstRowIndex
		f.index = index
	}
	f.rbuf.Reset(&f.section)
	return err
}

func (f *filePages) Close() error {
	putBufioReader(f.rbuf, f.rbufpool)
	f.chunk = nil
	f.section = io.SectionReader{}
	f.rbuf = nil
	f.rbufpool = nil
	f.baseOffset = 0
	f.dataOffset = 0
	f.dictOffset = 0
	f.index = 0
	f.skip = 0
	f.dictionary = nil
	return nil
}

func (f *filePages) columnPath() columnPath {
	return columnPath(f.chunk.column.Path())
}

type putBufioReaderFunc func()

var (
	bufioReaderPoolLock sync.Mutex
	bufioReaderPool     = map[int]*sync.Pool{}
)

func getBufioReader(r io.Reader, bufferSize int) (*bufio.Reader, *sync.Pool) {
	pool := getBufioReaderPool(bufferSize)
	rbuf, _ := pool.Get().(*bufio.Reader)
	if rbuf == nil {
		rbuf = bufio.NewReaderSize(r, bufferSize)
	} else {
		rbuf.Reset(r)
	}
	return rbuf, pool
}

func putBufioReader(rbuf *bufio.Reader, pool *sync.Pool) {
	if rbuf != nil && pool != nil {
		rbuf.Reset(nil)
		pool.Put(rbuf)
	}
}

func getBufioReaderPool(size int) *sync.Pool {
	bufioReaderPoolLock.Lock()
	defer bufioReaderPoolLock.Unlock()

	if pool := bufioReaderPool[size]; pool != nil {
		return pool
	}

	pool := &sync.Pool{}
	bufioReaderPool[size] = pool
	return pool
}

func (f *File) readAt(p []byte, off int64) (int, error) {
	return readAt(f.reader, p, off)
}

func readAt(r io.ReaderAt, p []byte, off int64) (n int, err error) {
	n, err = r.ReadAt(p, off)
	if n == len(p) {
		err = nil
		// p was fully read.There is no further need to check for errors. This
		// operation is a success in principle.
		return
	}
	return
}
