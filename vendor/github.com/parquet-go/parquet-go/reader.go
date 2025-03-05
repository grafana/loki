package parquet

import (
	"errors"
	"fmt"
	"io"
	"reflect"

	"github.com/parquet-go/parquet-go/format"
)

// GenericReader is similar to a Reader but uses a type parameter to define the
// Go type representing the schema of rows being read.
//
// See GenericWriter for details about the benefits over the classic Reader API.
type GenericReader[T any] struct {
	base Reader
	read readFunc[T]
}

// NewGenericReader is like NewReader but returns GenericReader[T] suited to write
// rows of Go type T.
//
// The type parameter T should be a map, struct, or any. Any other types will
// cause a panic at runtime. Type checking is a lot more effective when the
// generic parameter is a struct type, using map and interface types is somewhat
// similar to using a Writer.
//
// If the option list may explicitly declare a schema, it must be compatible
// with the schema generated from T.
func NewGenericReader[T any](input io.ReaderAt, options ...ReaderOption) *GenericReader[T] {
	c, err := NewReaderConfig(options...)
	if err != nil {
		panic(err)
	}

	f, err := openFile(input)
	if err != nil {
		panic(err)
	}

	rowGroup := fileRowGroupOf(f)

	t := typeOf[T]()
	if c.Schema == nil {
		if t == nil {
			c.Schema = rowGroup.Schema()
		} else {
			c.Schema = schemaOf(dereference(t))
		}
	}

	r := &GenericReader[T]{
		base: Reader{
			file: reader{
				file:     f,
				schema:   c.Schema,
				rowGroup: rowGroup,
			},
		},
	}

	if !EqualNodes(c.Schema, f.schema) {
		r.base.file.rowGroup = convertRowGroupTo(r.base.file.rowGroup, c.Schema)
	}

	r.base.read.init(r.base.file.schema, r.base.file.rowGroup)
	r.read = readFuncOf[T](t, r.base.file.schema)
	return r
}

func NewGenericRowGroupReader[T any](rowGroup RowGroup, options ...ReaderOption) *GenericReader[T] {
	c, err := NewReaderConfig(options...)
	if err != nil {
		panic(err)
	}

	t := typeOf[T]()
	if c.Schema == nil {
		if t == nil {
			c.Schema = rowGroup.Schema()
		} else {
			c.Schema = schemaOf(dereference(t))
		}
	}

	r := &GenericReader[T]{
		base: Reader{
			file: reader{
				schema:   c.Schema,
				rowGroup: rowGroup,
			},
		},
	}

	if !EqualNodes(c.Schema, rowGroup.Schema()) {
		r.base.file.rowGroup = convertRowGroupTo(r.base.file.rowGroup, c.Schema)
	}

	r.base.read.init(r.base.file.schema, r.base.file.rowGroup)
	r.read = readFuncOf[T](t, r.base.file.schema)
	return r
}

func (r *GenericReader[T]) Reset() {
	r.base.Reset()
}

// Read reads the next rows from the reader into the given rows slice up to len(rows).
//
// The returned values are safe to reuse across Read calls and do not share
// memory with the reader's underlying page buffers.
//
// The method returns the number of rows read and io.EOF when no more rows
// can be read from the reader.
func (r *GenericReader[T]) Read(rows []T) (int, error) {
	return r.read(r, rows)
}

func (r *GenericReader[T]) ReadRows(rows []Row) (int, error) {
	return r.base.ReadRows(rows)
}

func (r *GenericReader[T]) Schema() *Schema {
	return r.base.Schema()
}

func (r *GenericReader[T]) NumRows() int64 {
	return r.base.NumRows()
}

func (r *GenericReader[T]) SeekToRow(rowIndex int64) error {
	return r.base.SeekToRow(rowIndex)
}

func (r *GenericReader[T]) Close() error {
	return r.base.Close()
}

// File returns a FileView of the underlying parquet file.
func (r *GenericReader[T]) File() FileView {
	return r.base.File()
}

// readRows reads the next rows from the reader into the given rows slice up to len(rows).
//
// The returned values are safe to reuse across readRows calls and do not share
// memory with the reader's underlying page buffers.
//
// The method returns the number of rows read and io.EOF when no more rows
// can be read from the reader.
func (r *GenericReader[T]) readRows(rows []T) (int, error) {
	nRequest := len(rows)
	if cap(r.base.rowbuf) < nRequest {
		r.base.rowbuf = make([]Row, nRequest)
	} else {
		r.base.rowbuf = r.base.rowbuf[:nRequest]
	}

	var n, nTotal int
	var err error
	for {
		// ReadRows reads the minimum remaining rows in a column page across all columns
		// of the underlying reader, unless the length of the slice passed to it is smaller.
		// In that case, ReadRows will read the number of rows equal to the length of the
		// given slice argument. We limit that length to never be more than requested
		// because sequential reads can cross page boundaries.
		n, err = r.base.ReadRows(r.base.rowbuf[:nRequest-nTotal])
		if n > 0 {
			schema := r.base.Schema()

			for i, row := range r.base.rowbuf[:n] {
				if err2 := schema.Reconstruct(&rows[nTotal+i], row); err2 != nil {
					return nTotal + i, err2
				}
			}
		}
		nTotal += n
		if n == 0 || nTotal == nRequest || err != nil {
			break
		}
	}

	return nTotal, err
}

var (
	_ Rows                = (*GenericReader[any])(nil)
	_ RowReaderWithSchema = (*Reader)(nil)

	_ Rows                = (*GenericReader[struct{}])(nil)
	_ RowReaderWithSchema = (*GenericReader[struct{}])(nil)

	_ Rows                = (*GenericReader[map[struct{}]struct{}])(nil)
	_ RowReaderWithSchema = (*GenericReader[map[struct{}]struct{}])(nil)
)

type readFunc[T any] func(*GenericReader[T], []T) (int, error)

func readFuncOf[T any](t reflect.Type, schema *Schema) readFunc[T] {
	if t == nil {
		return (*GenericReader[T]).readRows
	}
	switch t.Kind() {
	case reflect.Interface, reflect.Map:
		return (*GenericReader[T]).readRows

	case reflect.Struct:
		return (*GenericReader[T]).readRows

	case reflect.Pointer:
		if e := t.Elem(); e.Kind() == reflect.Struct {
			return (*GenericReader[T]).readRows
		}
	}
	panic("cannot create reader for values of type " + t.String())
}

// Deprecated: A Reader reads Go values from parquet files.
//
// This example showcases a typical use of parquet readers:
//
//	reader := parquet.NewReader(file)
//	rows := []RowType{}
//	for {
//		row := RowType{}
//		err := reader.Read(&row)
//		if err != nil {
//			if err == io.EOF {
//				break
//			}
//			...
//		}
//		rows = append(rows, row)
//	}
//	if err := reader.Close(); err != nil {
//		...
//	}
//
// For programs building with Go 1.18 or later, the GenericReader[T] type
// supersedes this one.
type Reader struct {
	seen     reflect.Type
	file     reader
	read     reader
	rowIndex int64
	rowbuf   []Row
}

// NewReader constructs a parquet reader reading rows from the given
// io.ReaderAt.
//
// In order to read parquet rows, the io.ReaderAt must be converted to a
// parquet.File. If r is already a parquet.File it is used directly; otherwise,
// the io.ReaderAt value is expected to either have a `Size() int64` method or
// implement io.Seeker in order to determine its size.
//
// The function panics if the reader configuration is invalid. Programs that
// cannot guarantee the validity of the options passed to NewReader should
// construct the reader configuration independently prior to calling this
// function:
//
//	config, err := parquet.NewReaderConfig(options...)
//	if err != nil {
//		// handle the configuration error
//		...
//	} else {
//		// this call to create a reader is guaranteed not to panic
//		reader := parquet.NewReader(input, config)
//		...
//	}
func NewReader(input io.ReaderAt, options ...ReaderOption) *Reader {
	c, err := NewReaderConfig(options...)
	if err != nil {
		panic(err)
	}

	f, err := openFile(input)
	if err != nil {
		panic(err)
	}

	r := &Reader{
		file: reader{
			file:     f,
			schema:   f.schema,
			rowGroup: fileRowGroupOf(f),
		},
	}

	if c.Schema != nil {
		r.file.schema = c.Schema
		r.file.rowGroup = convertRowGroupTo(r.file.rowGroup, c.Schema)
	}

	r.read.init(r.file.schema, r.file.rowGroup)
	return r
}

func openFile(input io.ReaderAt) (*File, error) {
	f, _ := input.(*File)
	if f != nil {
		return f, nil
	}
	n, err := sizeOf(input)
	if err != nil {
		return nil, err
	}
	return OpenFile(input, n)
}

func fileRowGroupOf(f *File) RowGroup {
	switch rowGroups := f.RowGroups(); len(rowGroups) {
	case 0:
		return newEmptyRowGroup(f.Schema())
	case 1:
		return rowGroups[0]
	default:
		// TODO: should we attempt to merge the row groups via MergeRowGroups
		// to preserve the global order of sorting columns within the file?
		return MultiRowGroup(rowGroups...)
	}
}

// NewRowGroupReader constructs a new Reader which reads rows from the RowGroup
// passed as argument.
func NewRowGroupReader(rowGroup RowGroup, options ...ReaderOption) *Reader {
	c, err := NewReaderConfig(options...)
	if err != nil {
		panic(err)
	}

	if c.Schema != nil {
		rowGroup = convertRowGroupTo(rowGroup, c.Schema)
	}

	r := &Reader{
		file: reader{
			schema:   rowGroup.Schema(),
			rowGroup: rowGroup,
		},
	}

	r.read.init(r.file.schema, r.file.rowGroup)
	return r
}

func convertRowGroupTo(rowGroup RowGroup, schema *Schema) RowGroup {
	if rowGroupSchema := rowGroup.Schema(); !EqualNodes(schema, rowGroupSchema) {
		conv, err := Convert(schema, rowGroupSchema)
		if err != nil {
			// TODO: this looks like something we should not be panicking on,
			// but the current NewReader API does not offer a mechanism to
			// report errors.
			panic(err)
		}
		rowGroup = ConvertRowGroup(rowGroup, conv)
	}
	return rowGroup
}

func sizeOf(r io.ReaderAt) (int64, error) {
	switch f := r.(type) {
	case interface{ Size() int64 }:
		return f.Size(), nil
	case io.Seeker:
		off, err := f.Seek(0, io.SeekCurrent)
		if err != nil {
			return 0, err
		}
		end, err := f.Seek(0, io.SeekEnd)
		if err != nil {
			return 0, err
		}
		_, err = f.Seek(off, io.SeekStart)
		return end, err
	default:
		return 0, fmt.Errorf("cannot determine length of %T", r)
	}
}

// Reset repositions the reader at the beginning of the underlying parquet file.
func (r *Reader) Reset() {
	r.file.Reset()
	r.read.Reset()
	r.rowIndex = 0
	clearRows(r.rowbuf)
}

// Read reads the next row from r. The type of the row must match the schema
// of the underlying parquet file or an error will be returned.
//
// The method returns io.EOF when no more rows can be read from r.
func (r *Reader) Read(row interface{}) error {
	if rowType := dereference(reflect.TypeOf(row)); rowType.Kind() == reflect.Struct {
		if r.seen != rowType {
			if err := r.updateReadSchema(rowType); err != nil {
				return fmt.Errorf("cannot read parquet row into go value of type %T: %w", row, err)
			}
		}
	}

	if err := r.read.SeekToRow(r.rowIndex); err != nil {
		if errors.Is(err, io.ErrClosedPipe) {
			return io.EOF
		}
		return fmt.Errorf("seeking reader to row %d: %w", r.rowIndex, err)
	}

	if cap(r.rowbuf) == 0 {
		r.rowbuf = make([]Row, 1)
	} else {
		r.rowbuf = r.rowbuf[:1]
	}

	n, err := r.read.ReadRows(r.rowbuf[:])
	if n == 0 {
		return err
	}

	r.rowIndex++
	return r.read.schema.Reconstruct(row, r.rowbuf[0])
}

func (r *Reader) updateReadSchema(rowType reflect.Type) error {
	schema := schemaOf(rowType)

	if EqualNodes(schema, r.file.schema) {
		r.read.init(schema, r.file.rowGroup)
	} else {
		conv, err := Convert(schema, r.file.schema)
		if err != nil {
			return err
		}
		r.read.init(schema, ConvertRowGroup(r.file.rowGroup, conv))
	}

	r.seen = rowType
	return nil
}

// ReadRows reads the next rows from r into the given Row buffer.
//
// The returned values are laid out in the order expected by the
// parquet.(*Schema).Reconstruct method.
//
// The method returns io.EOF when no more rows can be read from r.
func (r *Reader) ReadRows(rows []Row) (int, error) {
	if err := r.file.SeekToRow(r.rowIndex); err != nil {
		return 0, err
	}
	n, err := r.file.ReadRows(rows)
	r.rowIndex += int64(n)
	return n, err
}

// Schema returns the schema of rows read by r.
func (r *Reader) Schema() *Schema { return r.file.schema }

// NumRows returns the number of rows that can be read from r.
func (r *Reader) NumRows() int64 { return r.file.rowGroup.NumRows() }

// SeekToRow positions r at the given row index.
func (r *Reader) SeekToRow(rowIndex int64) error {
	if err := r.file.SeekToRow(rowIndex); err != nil {
		return err
	}
	r.rowIndex = rowIndex
	return nil
}

// Close closes the reader, preventing more rows from being read.
func (r *Reader) Close() error {
	if err := r.read.Close(); err != nil {
		return err
	}
	if err := r.file.Close(); err != nil {
		return err
	}
	return nil
}

// reader is a subtype used in the implementation of Reader to support the two
// use cases of either reading rows calling the ReadRow method (where full rows
// are read from the underlying parquet file), or calling the Read method to
// read rows into Go values, potentially doing partial reads on a subset of the
// columns due to using a converted row group view.
type reader struct {
	file     *File
	schema   *Schema
	rowGroup RowGroup
	rows     Rows
	rowIndex int64
}

func (r *reader) init(schema *Schema, rowGroup RowGroup) {
	r.schema = schema
	r.rowGroup = rowGroup
	r.Reset()
}

func (r *reader) Reset() {
	r.rowIndex = 0

	if rows, ok := r.rows.(interface{ Reset() }); ok {
		// This optimization works for the common case where the underlying type
		// of the Rows instance is rowGroupRows, which should be true in most
		// cases since even external implementations of the RowGroup interface
		// can construct values of this type via the NewRowGroupRowReader
		// function.
		//
		// Foreign implementations of the Rows interface may also define a Reset
		// method in order to participate in this optimization.
		rows.Reset()
		return
	}

	if r.rows != nil {
		r.rows.Close()
		r.rows = nil
	}
}

func (r *reader) ReadRows(rows []Row) (int, error) {
	if r.rowGroup == nil {
		return 0, io.EOF
	}
	if r.rows == nil {
		r.rows = r.rowGroup.Rows()
		if r.rowIndex > 0 {
			if err := r.rows.SeekToRow(r.rowIndex); err != nil {
				return 0, err
			}
		}
	}
	n, err := r.rows.ReadRows(rows)
	r.rowIndex += int64(n)
	return n, err
}

func (r *reader) SeekToRow(rowIndex int64) error {
	if r.rowGroup == nil {
		return io.ErrClosedPipe
	}
	if rowIndex != r.rowIndex {
		if r.rows != nil {
			if err := r.rows.SeekToRow(rowIndex); err != nil {
				return err
			}
		}
		r.rowIndex = rowIndex
	}
	return nil
}

func (r *reader) Close() (err error) {
	r.rowGroup = nil
	if r.rows != nil {
		err = r.rows.Close()
	}
	return err
}

var (
	_ Rows                = (*Reader)(nil)
	_ RowReaderWithSchema = (*Reader)(nil)

	_ RowReader = (*reader)(nil)
	_ RowSeeker = (*reader)(nil)
)

type readerFileView struct {
	reader *reader
	schema *Schema
}

// File returns a FileView of the parquet file being read.
// Only available if Reader was created with a File.
func (r *Reader) File() FileView {
	if r.file.schema == nil || r.file.file == nil {
		return nil
	}
	return &readerFileView{
		&r.file,
		r.file.schema,
	}
}

func (r *readerFileView) Metadata() *format.FileMetaData {
	if r.reader.file != nil {
		return r.reader.file.Metadata()
	}
	return nil
}

func (r *readerFileView) Schema() *Schema {
	return r.schema
}

func (r *readerFileView) NumRows() int64 {
	return r.reader.rowGroup.NumRows()
}

func (r *readerFileView) Lookup(key string) (string, bool) {
	if meta := r.Metadata(); meta != nil {
		return lookupKeyValueMetadata(meta.KeyValueMetadata, key)
	}
	return "", false
}

func (r *readerFileView) Size() int64 {
	if r.reader.file != nil {
		return r.reader.file.Size()
	}
	return 0
}

func (r *readerFileView) ColumnIndexes() []format.ColumnIndex {
	if r.reader.file != nil {
		return r.reader.file.ColumnIndexes()
	}
	return nil
}

func (r *readerFileView) OffsetIndexes() []format.OffsetIndex {
	if r.reader.file != nil {
		return r.reader.file.OffsetIndexes()
	}
	return nil
}

func (r *readerFileView) Root() *Column {
	if meta := r.Metadata(); meta != nil {
		root, _ := openColumns(nil, meta, r.ColumnIndexes(), r.OffsetIndexes())
		return root
	}
	return nil
}

func (r *readerFileView) RowGroups() []RowGroup {
	file := r.reader.file
	if file == nil {
		return nil
	}
	columns := makeLeafColumns(r.Root())
	fileRowGroups := makeFileRowGroups(file, columns)
	return makeRowGroups(fileRowGroups)
}

func makeLeafColumns(root *Column) []*Column {
	columns := make([]*Column, 0, numLeafColumnsOf(root))
	root.forEachLeaf(func(c *Column) { columns = append(columns, c) })
	return columns
}

func makeFileRowGroups(file *File, columns []*Column) []FileRowGroup {
	rowGroups := file.metadata.RowGroups
	fileRowGroups := make([]FileRowGroup, len(rowGroups))
	for i := range fileRowGroups {
		fileRowGroups[i].init(file, columns, &rowGroups[i])
	}
	return fileRowGroups
}

func makeRowGroups(fileRowGroups []FileRowGroup) []RowGroup {
	rowGroups := make([]RowGroup, len(fileRowGroups))
	for i := range fileRowGroups {
		rowGroups[i] = &fileRowGroups[i]
	}
	return rowGroups
}
