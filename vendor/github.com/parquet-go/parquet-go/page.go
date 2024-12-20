package parquet

import (
	"bytes"
	"fmt"
	"io"

	"github.com/parquet-go/parquet-go/deprecated"
	"github.com/parquet-go/parquet-go/encoding"
	"github.com/parquet-go/parquet-go/internal/bitpack"
	"github.com/parquet-go/parquet-go/internal/debug"
)

// Page values represent sequences of parquet values. From the Parquet
// documentation: "Column chunks are a chunk of the data for a particular
// column. They live in a particular row group and are guaranteed to be
// contiguous in the file. Column chunks are divided up into pages. A page is
// conceptually an indivisible unit (in terms of compression and encoding).
// There can be multiple page types which are interleaved in a column chunk."
//
// https://github.com/apache/parquet-format#glossary
type Page interface {
	// Returns the type of values read from this page.
	//
	// The returned type can be used to encode the page data, in the case of
	// an indexed page (which has a dictionary), the type is configured to
	// encode the indexes stored in the page rather than the plain values.
	Type() Type

	// Returns the column index that this page belongs to.
	Column() int

	// If the page contains indexed values, calling this method returns the
	// dictionary in which the values are looked up. Otherwise, the method
	// returns nil.
	Dictionary() Dictionary

	// Returns the number of rows, values, and nulls in the page. The number of
	// rows may be less than the number of values in the page if the page is
	// part of a repeated column.
	NumRows() int64
	NumValues() int64
	NumNulls() int64

	// Returns the page's min and max values.
	//
	// The third value is a boolean indicating whether the page bounds were
	// available. Page bounds may not be known if the page contained no values
	// or only nulls, or if they were read from a parquet file which had neither
	// page statistics nor a page index.
	Bounds() (min, max Value, ok bool)

	// Returns the size of the page in bytes (uncompressed).
	Size() int64

	// Returns a reader exposing the values contained in the page.
	//
	// Depending on the underlying implementation, the returned reader may
	// support reading an array of typed Go values by implementing interfaces
	// like parquet.Int32Reader. Applications should use type assertions on
	// the returned reader to determine whether those optimizations are
	// available.
	Values() ValueReader

	// Returns a new page which is as slice of the receiver between row indexes
	// i and j.
	Slice(i, j int64) Page

	// Expose the lists of repetition and definition levels of the page.
	//
	// The returned slices may be empty when the page has no repetition or
	// definition levels.
	RepetitionLevels() []byte
	DefinitionLevels() []byte

	// Returns the in-memory buffer holding the page values.
	//
	// The intent is for the returned value to be used as input parameter when
	// calling the Encode method of the associated Type.
	//
	// The slices referenced by the encoding.Values may be the same across
	// multiple calls to this method, applications must treat the content as
	// immutable.
	Data() encoding.Values
}

// PageReader is an interface implemented by types that support producing a
// sequence of pages.
type PageReader interface {
	// Reads and returns the next page from the sequence. When all pages have
	// been read, or if the sequence was closed, the method returns io.EOF.
	ReadPage() (Page, error)
}

// PageWriter is an interface implemented by types that support writing pages
// to an underlying storage medium.
type PageWriter interface {
	WritePage(Page) (int64, error)
}

// Pages is an interface implemented by page readers returned by calling the
// Pages method of ColumnChunk instances.
type Pages interface {
	PageReader
	RowSeeker
	io.Closer
}

// AsyncPages wraps the given Pages instance to perform page reads
// asynchronously in a separate goroutine.
//
// Performing page reads asynchronously is important when the application may
// be reading pages from a high latency backend, and the last
// page read may be processed while initiating reading of the next page.
func AsyncPages(pages Pages) Pages {
	p := new(asyncPages)
	p.init(pages, nil)
	// If the pages object gets garbage collected without Close being called,
	// this finalizer would ensure that the goroutine is stopped and doesn't
	// leak.
	debug.SetFinalizer(p, func(p *asyncPages) { p.Close() })
	return p
}

type asyncPages struct {
	read    <-chan asyncPage
	seek    chan<- int64
	done    chan<- struct{}
	version int64
}

type asyncPage struct {
	page    Page
	err     error
	version int64
}

func (pages *asyncPages) init(base Pages, done chan struct{}) {
	read := make(chan asyncPage)
	seek := make(chan int64, 1)

	pages.read = read
	pages.seek = seek

	if done == nil {
		done = make(chan struct{})
		pages.done = done
	}

	go readPages(base, read, seek, done)
}

func (pages *asyncPages) Close() (err error) {
	if pages.done != nil {
		close(pages.done)
		pages.done = nil
	}
	for p := range pages.read {
		// Capture the last error, which is the value returned from closing the
		// underlying Pages instance.
		err = p.err
	}
	pages.seek = nil
	return err
}

func (pages *asyncPages) ReadPage() (Page, error) {
	for {
		p, ok := <-pages.read
		if !ok {
			return nil, io.EOF
		}
		// Because calls to SeekToRow might be made concurrently to reading
		// pages, it is possible for ReadPage to see pages that were read before
		// the last SeekToRow call.
		//
		// A version number is attached to each page read asynchronously to
		// discard outdated pages and ensure that we maintain a consistent view
		// of the sequence of pages read.
		if p.version == pages.version {
			return p.page, p.err
		}
	}
}

func (pages *asyncPages) SeekToRow(rowIndex int64) error {
	if pages.seek == nil {
		return io.ErrClosedPipe
	}
	// The seek channel has a capacity of 1 to allow the first SeekToRow call to
	// be non-blocking.
	//
	// If SeekToRow calls are performed faster than they can be handled by the
	// goroutine reading pages, this path might become a contention point.
	pages.seek <- rowIndex
	pages.version++
	return nil
}

func readPages(pages Pages, read chan<- asyncPage, seek <-chan int64, done <-chan struct{}) {
	defer func() {
		read <- asyncPage{err: pages.Close(), version: -1}
		close(read)
	}()

	version := int64(0)
	for {
		page, err := pages.ReadPage()

		for {
			select {
			case <-done:
				return
			case read <- asyncPage{
				page:    page,
				err:     err,
				version: version,
			}:
			case rowIndex := <-seek:
				version++
				err = pages.SeekToRow(rowIndex)
			}
			if err == nil {
				break
			}
		}
	}
}

type singlePage struct {
	page    Page
	seek    int64
	numRows int64
}

func (r *singlePage) ReadPage() (Page, error) {
	if r.page != nil {
		if r.seek < r.numRows {
			seek := r.seek
			r.seek = r.numRows
			if seek > 0 {
				return r.page.Slice(seek, r.numRows), nil
			}
			return r.page, nil
		}
	}
	return nil, io.EOF
}

func (r *singlePage) SeekToRow(rowIndex int64) error {
	r.seek = rowIndex
	return nil
}

func (r *singlePage) Close() error {
	r.page = nil
	r.seek = 0
	return nil
}

func onePage(page Page) Pages {
	return &singlePage{page: page, numRows: page.NumRows()}
}

// CopyPages copies pages from src to dst, returning the number of values that
// were copied.
//
// The function returns any error it encounters reading or writing pages, except
// for io.EOF from the reader which indicates that there were no more pages to
// read.
func CopyPages(dst PageWriter, src PageReader) (numValues int64, err error) {
	for {
		p, err := src.ReadPage()
		if err != nil {
			if err == io.EOF {
				err = nil
			}
			return numValues, err
		}
		n, err := dst.WritePage(p)
		numValues += n
		if err != nil {
			return numValues, err
		}
	}
}

// errorPage is an implementation of the Page interface which always errors when
// attempting to read its values.
//
// The error page declares that it contains one value (even if it does not)
// as a way to ensure that it is not ignored due to being empty when written
// to a file.
type errorPage struct {
	typ         Type
	err         error
	columnIndex int
}

func newErrorPage(typ Type, columnIndex int, msg string, args ...interface{}) *errorPage {
	return &errorPage{
		typ:         typ,
		err:         fmt.Errorf(msg, args...),
		columnIndex: columnIndex,
	}
}

func (page *errorPage) Type() Type                        { return page.typ }
func (page *errorPage) Column() int                       { return page.columnIndex }
func (page *errorPage) Dictionary() Dictionary            { return nil }
func (page *errorPage) NumRows() int64                    { return 1 }
func (page *errorPage) NumValues() int64                  { return 1 }
func (page *errorPage) NumNulls() int64                   { return 0 }
func (page *errorPage) Bounds() (min, max Value, ok bool) { return }
func (page *errorPage) Slice(i, j int64) Page             { return page }
func (page *errorPage) Size() int64                       { return 1 }
func (page *errorPage) RepetitionLevels() []byte          { return nil }
func (page *errorPage) DefinitionLevels() []byte          { return nil }
func (page *errorPage) Data() encoding.Values             { return encoding.Values{} }
func (page *errorPage) Values() ValueReader               { return errorPageValues{page: page} }

type errorPageValues struct{ page *errorPage }

func (r errorPageValues) ReadValues([]Value) (int, error) { return 0, r.page.err }
func (r errorPageValues) Close() error                    { return nil }

func errPageBoundsOutOfRange(i, j, n int64) error {
	return fmt.Errorf("page bounds out of range [%d:%d]: with length %d", i, j, n)
}

type optionalPage struct {
	base               Page
	maxDefinitionLevel byte
	definitionLevels   []byte
}

func newOptionalPage(base Page, maxDefinitionLevel byte, definitionLevels []byte) *optionalPage {
	return &optionalPage{
		base:               base,
		maxDefinitionLevel: maxDefinitionLevel,
		definitionLevels:   definitionLevels,
	}
}

func (page *optionalPage) Type() Type { return page.base.Type() }

func (page *optionalPage) Column() int { return page.base.Column() }

func (page *optionalPage) Dictionary() Dictionary { return page.base.Dictionary() }

func (page *optionalPage) NumRows() int64 { return int64(len(page.definitionLevels)) }

func (page *optionalPage) NumValues() int64 { return int64(len(page.definitionLevels)) }

func (page *optionalPage) NumNulls() int64 {
	return int64(countLevelsNotEqual(page.definitionLevels, page.maxDefinitionLevel))
}

func (page *optionalPage) Bounds() (min, max Value, ok bool) { return page.base.Bounds() }

func (page *optionalPage) Size() int64 { return int64(len(page.definitionLevels)) + page.base.Size() }

func (page *optionalPage) RepetitionLevels() []byte { return nil }

func (page *optionalPage) DefinitionLevels() []byte { return page.definitionLevels }

func (page *optionalPage) Data() encoding.Values { return page.base.Data() }

func (page *optionalPage) Values() ValueReader {
	return &optionalPageValues{
		page:   page,
		values: page.base.Values(),
	}
}

func (page *optionalPage) Slice(i, j int64) Page {
	maxDefinitionLevel := page.maxDefinitionLevel
	definitionLevels := page.definitionLevels
	numNulls1 := int64(countLevelsNotEqual(definitionLevels[:i], maxDefinitionLevel))
	numNulls2 := int64(countLevelsNotEqual(definitionLevels[i:j], maxDefinitionLevel))
	return newOptionalPage(
		page.base.Slice(i-numNulls1, j-(numNulls1+numNulls2)),
		maxDefinitionLevel,
		definitionLevels[i:j:j],
	)
}

type repeatedPage struct {
	base               Page
	maxRepetitionLevel byte
	maxDefinitionLevel byte
	definitionLevels   []byte
	repetitionLevels   []byte
}

func newRepeatedPage(base Page, maxRepetitionLevel, maxDefinitionLevel byte, repetitionLevels, definitionLevels []byte) *repeatedPage {
	return &repeatedPage{
		base:               base,
		maxRepetitionLevel: maxRepetitionLevel,
		maxDefinitionLevel: maxDefinitionLevel,
		definitionLevels:   definitionLevels,
		repetitionLevels:   repetitionLevels,
	}
}

func (page *repeatedPage) Type() Type { return page.base.Type() }

func (page *repeatedPage) Column() int { return page.base.Column() }

func (page *repeatedPage) Dictionary() Dictionary { return page.base.Dictionary() }

func (page *repeatedPage) NumRows() int64 { return int64(countLevelsEqual(page.repetitionLevels, 0)) }

func (page *repeatedPage) NumValues() int64 { return int64(len(page.definitionLevels)) }

func (page *repeatedPage) NumNulls() int64 {
	return int64(countLevelsNotEqual(page.definitionLevels, page.maxDefinitionLevel))
}

func (page *repeatedPage) Bounds() (min, max Value, ok bool) { return page.base.Bounds() }

func (page *repeatedPage) Size() int64 {
	return int64(len(page.repetitionLevels)) + int64(len(page.definitionLevels)) + page.base.Size()
}

func (page *repeatedPage) RepetitionLevels() []byte { return page.repetitionLevels }

func (page *repeatedPage) DefinitionLevels() []byte { return page.definitionLevels }

func (page *repeatedPage) Data() encoding.Values { return page.base.Data() }

func (page *repeatedPage) Values() ValueReader {
	return &repeatedPageValues{
		page:   page,
		values: page.base.Values(),
	}
}

func (page *repeatedPage) Slice(i, j int64) Page {
	numRows := page.NumRows()
	if i < 0 || i > numRows {
		panic(errPageBoundsOutOfRange(i, j, numRows))
	}
	if j < 0 || j > numRows {
		panic(errPageBoundsOutOfRange(i, j, numRows))
	}
	if i > j {
		panic(errPageBoundsOutOfRange(i, j, numRows))
	}

	maxRepetitionLevel := page.maxRepetitionLevel
	maxDefinitionLevel := page.maxDefinitionLevel
	repetitionLevels := page.repetitionLevels
	definitionLevels := page.definitionLevels

	rowIndex0 := 0
	rowIndex1 := len(repetitionLevels)
	rowIndex2 := len(repetitionLevels)

	for k, def := range repetitionLevels {
		if def == 0 {
			if rowIndex0 == int(i) {
				rowIndex1 = k
				break
			}
			rowIndex0++
		}
	}

	for k, def := range repetitionLevels[rowIndex1:] {
		if def == 0 {
			if rowIndex0 == int(j) {
				rowIndex2 = rowIndex1 + k
				break
			}
			rowIndex0++
		}
	}

	numNulls1 := countLevelsNotEqual(definitionLevels[:rowIndex1], maxDefinitionLevel)
	numNulls2 := countLevelsNotEqual(definitionLevels[rowIndex1:rowIndex2], maxDefinitionLevel)

	i = int64(rowIndex1 - numNulls1)
	j = int64(rowIndex2 - (numNulls1 + numNulls2))

	return newRepeatedPage(
		page.base.Slice(i, j),
		maxRepetitionLevel,
		maxDefinitionLevel,
		repetitionLevels[rowIndex1:rowIndex2:rowIndex2],
		definitionLevels[rowIndex1:rowIndex2:rowIndex2],
	)
}

type booleanPage struct {
	typ         Type
	bits        []byte
	offset      int32
	numValues   int32
	columnIndex int16
}

func newBooleanPage(typ Type, columnIndex int16, numValues int32, values encoding.Values) *booleanPage {
	return &booleanPage{
		typ:         typ,
		bits:        values.Boolean()[:bitpack.ByteCount(uint(numValues))],
		numValues:   numValues,
		columnIndex: ^columnIndex,
	}
}

func (page *booleanPage) Type() Type { return page.typ }

func (page *booleanPage) Column() int { return int(^page.columnIndex) }

func (page *booleanPage) Dictionary() Dictionary { return nil }

func (page *booleanPage) NumRows() int64 { return int64(page.numValues) }

func (page *booleanPage) NumValues() int64 { return int64(page.numValues) }

func (page *booleanPage) NumNulls() int64 { return 0 }

func (page *booleanPage) Size() int64 { return int64(len(page.bits)) }

func (page *booleanPage) RepetitionLevels() []byte { return nil }

func (page *booleanPage) DefinitionLevels() []byte { return nil }

func (page *booleanPage) Data() encoding.Values { return encoding.BooleanValues(page.bits) }

func (page *booleanPage) Values() ValueReader { return &booleanPageValues{page: page} }

func (page *booleanPage) valueAt(i int) bool {
	j := uint32(int(page.offset)+i) / 8
	k := uint32(int(page.offset)+i) % 8
	return ((page.bits[j] >> k) & 1) != 0
}

func (page *booleanPage) min() bool {
	for i := 0; i < int(page.numValues); i++ {
		if !page.valueAt(i) {
			return false
		}
	}
	return page.numValues > 0
}

func (page *booleanPage) max() bool {
	for i := 0; i < int(page.numValues); i++ {
		if page.valueAt(i) {
			return true
		}
	}
	return false
}

func (page *booleanPage) bounds() (min, max bool) {
	hasFalse, hasTrue := false, false

	for i := 0; i < int(page.numValues); i++ {
		v := page.valueAt(i)
		if v {
			hasTrue = true
		} else {
			hasFalse = true
		}
		if hasTrue && hasFalse {
			break
		}
	}

	min = !hasFalse
	max = hasTrue
	return min, max
}

func (page *booleanPage) Bounds() (min, max Value, ok bool) {
	if ok = page.numValues > 0; ok {
		minBool, maxBool := page.bounds()
		min = page.makeValue(minBool)
		max = page.makeValue(maxBool)
	}
	return min, max, ok
}

func (page *booleanPage) Slice(i, j int64) Page {
	lowWithOffset := i + int64(page.offset)
	highWithOffset := j + int64(page.offset)

	off := lowWithOffset / 8
	end := highWithOffset / 8

	if (highWithOffset % 8) != 0 {
		end++
	}

	return &booleanPage{
		typ:         page.typ,
		bits:        page.bits[off:end],
		offset:      int32(lowWithOffset % 8),
		numValues:   int32(j - i),
		columnIndex: page.columnIndex,
	}
}

func (page *booleanPage) makeValue(v bool) Value {
	value := makeValueBoolean(v)
	value.columnIndex = page.columnIndex
	return value
}

type int32Page struct {
	typ         Type
	values      []int32
	columnIndex int16
}

func newInt32Page(typ Type, columnIndex int16, numValues int32, values encoding.Values) *int32Page {
	return &int32Page{
		typ:         typ,
		values:      values.Int32()[:numValues],
		columnIndex: ^columnIndex,
	}
}

func (page *int32Page) Type() Type { return page.typ }

func (page *int32Page) Column() int { return int(^page.columnIndex) }

func (page *int32Page) Dictionary() Dictionary { return nil }

func (page *int32Page) NumRows() int64 { return int64(len(page.values)) }

func (page *int32Page) NumValues() int64 { return int64(len(page.values)) }

func (page *int32Page) NumNulls() int64 { return 0 }

func (page *int32Page) Size() int64 { return 4 * int64(len(page.values)) }

func (page *int32Page) RepetitionLevels() []byte { return nil }

func (page *int32Page) DefinitionLevels() []byte { return nil }

func (page *int32Page) Data() encoding.Values { return encoding.Int32Values(page.values) }

func (page *int32Page) Values() ValueReader { return &int32PageValues{page: page} }

func (page *int32Page) min() int32 { return minInt32(page.values) }

func (page *int32Page) max() int32 { return maxInt32(page.values) }

func (page *int32Page) bounds() (min, max int32) { return boundsInt32(page.values) }

func (page *int32Page) Bounds() (min, max Value, ok bool) {
	if ok = len(page.values) > 0; ok {
		minInt32, maxInt32 := page.bounds()
		min = page.makeValue(minInt32)
		max = page.makeValue(maxInt32)
	}
	return min, max, ok
}

func (page *int32Page) Slice(i, j int64) Page {
	return &int32Page{
		typ:         page.typ,
		values:      page.values[i:j],
		columnIndex: page.columnIndex,
	}
}

func (page *int32Page) makeValue(v int32) Value {
	value := makeValueInt32(v)
	value.columnIndex = page.columnIndex
	return value
}

type int64Page struct {
	typ         Type
	values      []int64
	columnIndex int16
}

func newInt64Page(typ Type, columnIndex int16, numValues int32, values encoding.Values) *int64Page {
	return &int64Page{
		typ:         typ,
		values:      values.Int64()[:numValues],
		columnIndex: ^columnIndex,
	}
}

func (page *int64Page) Type() Type { return page.typ }

func (page *int64Page) Column() int { return int(^page.columnIndex) }

func (page *int64Page) Dictionary() Dictionary { return nil }

func (page *int64Page) NumRows() int64 { return int64(len(page.values)) }

func (page *int64Page) NumValues() int64 { return int64(len(page.values)) }

func (page *int64Page) NumNulls() int64 { return 0 }

func (page *int64Page) Size() int64 { return 8 * int64(len(page.values)) }

func (page *int64Page) RepetitionLevels() []byte { return nil }

func (page *int64Page) DefinitionLevels() []byte { return nil }

func (page *int64Page) Data() encoding.Values { return encoding.Int64Values(page.values) }

func (page *int64Page) Values() ValueReader { return &int64PageValues{page: page} }

func (page *int64Page) min() int64 { return minInt64(page.values) }

func (page *int64Page) max() int64 { return maxInt64(page.values) }

func (page *int64Page) bounds() (min, max int64) { return boundsInt64(page.values) }

func (page *int64Page) Bounds() (min, max Value, ok bool) {
	if ok = len(page.values) > 0; ok {
		minInt64, maxInt64 := page.bounds()
		min = page.makeValue(minInt64)
		max = page.makeValue(maxInt64)
	}
	return min, max, ok
}

func (page *int64Page) Slice(i, j int64) Page {
	return &int64Page{
		typ:         page.typ,
		values:      page.values[i:j],
		columnIndex: page.columnIndex,
	}
}

func (page *int64Page) makeValue(v int64) Value {
	value := makeValueInt64(v)
	value.columnIndex = page.columnIndex
	return value
}

type int96Page struct {
	typ         Type
	values      []deprecated.Int96
	columnIndex int16
}

func newInt96Page(typ Type, columnIndex int16, numValues int32, values encoding.Values) *int96Page {
	return &int96Page{
		typ:         typ,
		values:      values.Int96()[:numValues],
		columnIndex: ^columnIndex,
	}
}

func (page *int96Page) Type() Type { return page.typ }

func (page *int96Page) Column() int { return int(^page.columnIndex) }

func (page *int96Page) Dictionary() Dictionary { return nil }

func (page *int96Page) NumRows() int64 { return int64(len(page.values)) }

func (page *int96Page) NumValues() int64 { return int64(len(page.values)) }

func (page *int96Page) NumNulls() int64 { return 0 }

func (page *int96Page) Size() int64 { return 12 * int64(len(page.values)) }

func (page *int96Page) RepetitionLevels() []byte { return nil }

func (page *int96Page) DefinitionLevels() []byte { return nil }

func (page *int96Page) Data() encoding.Values { return encoding.Int96Values(page.values) }

func (page *int96Page) Values() ValueReader { return &int96PageValues{page: page} }

func (page *int96Page) min() deprecated.Int96 { return deprecated.MinInt96(page.values) }

func (page *int96Page) max() deprecated.Int96 { return deprecated.MaxInt96(page.values) }

func (page *int96Page) bounds() (min, max deprecated.Int96) {
	return deprecated.MinMaxInt96(page.values)
}

func (page *int96Page) Bounds() (min, max Value, ok bool) {
	if ok = len(page.values) > 0; ok {
		minInt96, maxInt96 := page.bounds()
		min = page.makeValue(minInt96)
		max = page.makeValue(maxInt96)
	}
	return min, max, ok
}

func (page *int96Page) Slice(i, j int64) Page {
	return &int96Page{
		typ:         page.typ,
		values:      page.values[i:j],
		columnIndex: page.columnIndex,
	}
}

func (page *int96Page) makeValue(v deprecated.Int96) Value {
	value := makeValueInt96(v)
	value.columnIndex = page.columnIndex
	return value
}

type floatPage struct {
	typ         Type
	values      []float32
	columnIndex int16
}

func newFloatPage(typ Type, columnIndex int16, numValues int32, values encoding.Values) *floatPage {
	return &floatPage{
		typ:         typ,
		values:      values.Float()[:numValues],
		columnIndex: ^columnIndex,
	}
}

func (page *floatPage) Type() Type { return page.typ }

func (page *floatPage) Column() int { return int(^page.columnIndex) }

func (page *floatPage) Dictionary() Dictionary { return nil }

func (page *floatPage) NumRows() int64 { return int64(len(page.values)) }

func (page *floatPage) NumValues() int64 { return int64(len(page.values)) }

func (page *floatPage) NumNulls() int64 { return 0 }

func (page *floatPage) Size() int64 { return 4 * int64(len(page.values)) }

func (page *floatPage) RepetitionLevels() []byte { return nil }

func (page *floatPage) DefinitionLevels() []byte { return nil }

func (page *floatPage) Data() encoding.Values { return encoding.FloatValues(page.values) }

func (page *floatPage) Values() ValueReader { return &floatPageValues{page: page} }

func (page *floatPage) min() float32 { return minFloat32(page.values) }

func (page *floatPage) max() float32 { return maxFloat32(page.values) }

func (page *floatPage) bounds() (min, max float32) { return boundsFloat32(page.values) }

func (page *floatPage) Bounds() (min, max Value, ok bool) {
	if ok = len(page.values) > 0; ok {
		minFloat32, maxFloat32 := page.bounds()
		min = page.makeValue(minFloat32)
		max = page.makeValue(maxFloat32)
	}
	return min, max, ok
}

func (page *floatPage) Slice(i, j int64) Page {
	return &floatPage{
		typ:         page.typ,
		values:      page.values[i:j],
		columnIndex: page.columnIndex,
	}
}

func (page *floatPage) makeValue(v float32) Value {
	value := makeValueFloat(v)
	value.columnIndex = page.columnIndex
	return value
}

type doublePage struct {
	typ         Type
	values      []float64
	columnIndex int16
}

func newDoublePage(typ Type, columnIndex int16, numValues int32, values encoding.Values) *doublePage {
	return &doublePage{
		typ:         typ,
		values:      values.Double()[:numValues],
		columnIndex: ^columnIndex,
	}
}

func (page *doublePage) Type() Type { return page.typ }

func (page *doublePage) Column() int { return int(^page.columnIndex) }

func (page *doublePage) Dictionary() Dictionary { return nil }

func (page *doublePage) NumRows() int64 { return int64(len(page.values)) }

func (page *doublePage) NumValues() int64 { return int64(len(page.values)) }

func (page *doublePage) NumNulls() int64 { return 0 }

func (page *doublePage) Size() int64 { return 8 * int64(len(page.values)) }

func (page *doublePage) RepetitionLevels() []byte { return nil }

func (page *doublePage) DefinitionLevels() []byte { return nil }

func (page *doublePage) Data() encoding.Values { return encoding.DoubleValues(page.values) }

func (page *doublePage) Values() ValueReader { return &doublePageValues{page: page} }

func (page *doublePage) min() float64 { return minFloat64(page.values) }

func (page *doublePage) max() float64 { return maxFloat64(page.values) }

func (page *doublePage) bounds() (min, max float64) { return boundsFloat64(page.values) }

func (page *doublePage) Bounds() (min, max Value, ok bool) {
	if ok = len(page.values) > 0; ok {
		minFloat64, maxFloat64 := page.bounds()
		min = page.makeValue(minFloat64)
		max = page.makeValue(maxFloat64)
	}
	return min, max, ok
}

func (page *doublePage) Slice(i, j int64) Page {
	return &doublePage{
		typ:         page.typ,
		values:      page.values[i:j],
		columnIndex: page.columnIndex,
	}
}

func (page *doublePage) makeValue(v float64) Value {
	value := makeValueDouble(v)
	value.columnIndex = page.columnIndex
	return value
}

type byteArrayPage struct {
	typ         Type
	values      []byte
	offsets     []uint32
	columnIndex int16
}

func newByteArrayPage(typ Type, columnIndex int16, numValues int32, values encoding.Values) *byteArrayPage {
	data, offsets := values.ByteArray()
	return &byteArrayPage{
		typ:         typ,
		values:      data,
		offsets:     offsets[:numValues+1],
		columnIndex: ^columnIndex,
	}
}

func (page *byteArrayPage) Type() Type { return page.typ }

func (page *byteArrayPage) Column() int { return int(^page.columnIndex) }

func (page *byteArrayPage) Dictionary() Dictionary { return nil }

func (page *byteArrayPage) NumRows() int64 { return int64(page.len()) }

func (page *byteArrayPage) NumValues() int64 { return int64(page.len()) }

func (page *byteArrayPage) NumNulls() int64 { return 0 }

func (page *byteArrayPage) Size() int64 { return int64(len(page.values)) + 4*int64(len(page.offsets)) }

func (page *byteArrayPage) RepetitionLevels() []byte { return nil }

func (page *byteArrayPage) DefinitionLevels() []byte { return nil }

func (page *byteArrayPage) Data() encoding.Values {
	return encoding.ByteArrayValues(page.values, page.offsets)
}

func (page *byteArrayPage) Values() ValueReader { return &byteArrayPageValues{page: page} }

func (page *byteArrayPage) len() int { return len(page.offsets) - 1 }

func (page *byteArrayPage) index(i int) []byte {
	j := page.offsets[i+0]
	k := page.offsets[i+1]
	return page.values[j:k:k]
}

func (page *byteArrayPage) min() (min []byte) {
	if n := page.len(); n > 0 {
		min = page.index(0)

		for i := 1; i < n; i++ {
			v := page.index(i)

			if bytes.Compare(v, min) < 0 {
				min = v
			}
		}
	}
	return min
}

func (page *byteArrayPage) max() (max []byte) {
	if n := page.len(); n > 0 {
		max = page.index(0)

		for i := 1; i < n; i++ {
			v := page.index(i)

			if bytes.Compare(v, max) > 0 {
				max = v
			}
		}
	}
	return max
}

func (page *byteArrayPage) bounds() (min, max []byte) {
	if n := page.len(); n > 0 {
		min = page.index(0)
		max = min

		for i := 1; i < n; i++ {
			v := page.index(i)

			switch {
			case bytes.Compare(v, min) < 0:
				min = v
			case bytes.Compare(v, max) > 0:
				max = v
			}
		}
	}
	return min, max
}

func (page *byteArrayPage) Bounds() (min, max Value, ok bool) {
	if ok = len(page.offsets) > 1; ok {
		minBytes, maxBytes := page.bounds()
		min = page.makeValueBytes(minBytes)
		max = page.makeValueBytes(maxBytes)
	}
	return min, max, ok
}

func (page *byteArrayPage) cloneValues() []byte {
	values := make([]byte, len(page.values))
	copy(values, page.values)
	return values
}

func (page *byteArrayPage) cloneOffsets() []uint32 {
	offsets := make([]uint32, len(page.offsets))
	copy(offsets, page.offsets)
	return offsets
}

func (page *byteArrayPage) Slice(i, j int64) Page {
	return &byteArrayPage{
		typ:         page.typ,
		values:      page.values,
		offsets:     page.offsets[i : j+1],
		columnIndex: page.columnIndex,
	}
}

func (page *byteArrayPage) makeValueBytes(v []byte) Value {
	value := makeValueBytes(ByteArray, v)
	value.columnIndex = page.columnIndex
	return value
}

func (page *byteArrayPage) makeValueString(v string) Value {
	value := makeValueString(ByteArray, v)
	value.columnIndex = page.columnIndex
	return value
}

type fixedLenByteArrayPage struct {
	typ         Type
	data        []byte
	size        int
	columnIndex int16
}

func newFixedLenByteArrayPage(typ Type, columnIndex int16, numValues int32, values encoding.Values) *fixedLenByteArrayPage {
	data, size := values.FixedLenByteArray()
	return &fixedLenByteArrayPage{
		typ:         typ,
		data:        data[:int(numValues)*size],
		size:        size,
		columnIndex: ^columnIndex,
	}
}

func (page *fixedLenByteArrayPage) Type() Type { return page.typ }

func (page *fixedLenByteArrayPage) Column() int { return int(^page.columnIndex) }

func (page *fixedLenByteArrayPage) Dictionary() Dictionary { return nil }

func (page *fixedLenByteArrayPage) NumRows() int64 { return int64(len(page.data) / page.size) }

func (page *fixedLenByteArrayPage) NumValues() int64 { return int64(len(page.data) / page.size) }

func (page *fixedLenByteArrayPage) NumNulls() int64 { return 0 }

func (page *fixedLenByteArrayPage) Size() int64 { return int64(len(page.data)) }

func (page *fixedLenByteArrayPage) RepetitionLevels() []byte { return nil }

func (page *fixedLenByteArrayPage) DefinitionLevels() []byte { return nil }

func (page *fixedLenByteArrayPage) Data() encoding.Values {
	return encoding.FixedLenByteArrayValues(page.data, page.size)
}

func (page *fixedLenByteArrayPage) Values() ValueReader {
	return &fixedLenByteArrayPageValues{page: page}
}

func (page *fixedLenByteArrayPage) min() []byte { return minFixedLenByteArray(page.data, page.size) }

func (page *fixedLenByteArrayPage) max() []byte { return maxFixedLenByteArray(page.data, page.size) }

func (page *fixedLenByteArrayPage) bounds() (min, max []byte) {
	return boundsFixedLenByteArray(page.data, page.size)
}

func (page *fixedLenByteArrayPage) Bounds() (min, max Value, ok bool) {
	if ok = len(page.data) > 0; ok {
		minBytes, maxBytes := page.bounds()
		min = page.makeValueBytes(minBytes)
		max = page.makeValueBytes(maxBytes)
	}
	return min, max, ok
}

func (page *fixedLenByteArrayPage) Slice(i, j int64) Page {
	return &fixedLenByteArrayPage{
		typ:         page.typ,
		data:        page.data[i*int64(page.size) : j*int64(page.size)],
		size:        page.size,
		columnIndex: page.columnIndex,
	}
}

func (page *fixedLenByteArrayPage) makeValueBytes(v []byte) Value {
	value := makeValueBytes(FixedLenByteArray, v)
	value.columnIndex = page.columnIndex
	return value
}

func (page *fixedLenByteArrayPage) makeValueString(v string) Value {
	value := makeValueString(FixedLenByteArray, v)
	value.columnIndex = page.columnIndex
	return value
}

type uint32Page struct {
	typ         Type
	values      []uint32
	columnIndex int16
}

func newUint32Page(typ Type, columnIndex int16, numValues int32, values encoding.Values) *uint32Page {
	return &uint32Page{
		typ:         typ,
		values:      values.Uint32()[:numValues],
		columnIndex: ^columnIndex,
	}
}

func (page *uint32Page) Type() Type { return page.typ }

func (page *uint32Page) Column() int { return int(^page.columnIndex) }

func (page *uint32Page) Dictionary() Dictionary { return nil }

func (page *uint32Page) NumRows() int64 { return int64(len(page.values)) }

func (page *uint32Page) NumValues() int64 { return int64(len(page.values)) }

func (page *uint32Page) NumNulls() int64 { return 0 }

func (page *uint32Page) Size() int64 { return 4 * int64(len(page.values)) }

func (page *uint32Page) RepetitionLevels() []byte { return nil }

func (page *uint32Page) DefinitionLevels() []byte { return nil }

func (page *uint32Page) Data() encoding.Values { return encoding.Uint32Values(page.values) }

func (page *uint32Page) Values() ValueReader { return &uint32PageValues{page: page} }

func (page *uint32Page) min() uint32 { return minUint32(page.values) }

func (page *uint32Page) max() uint32 { return maxUint32(page.values) }

func (page *uint32Page) bounds() (min, max uint32) { return boundsUint32(page.values) }

func (page *uint32Page) Bounds() (min, max Value, ok bool) {
	if ok = len(page.values) > 0; ok {
		minUint32, maxUint32 := page.bounds()
		min = page.makeValue(minUint32)
		max = page.makeValue(maxUint32)
	}
	return min, max, ok
}

func (page *uint32Page) Slice(i, j int64) Page {
	return &uint32Page{
		typ:         page.typ,
		values:      page.values[i:j],
		columnIndex: page.columnIndex,
	}
}

func (page *uint32Page) makeValue(v uint32) Value {
	value := makeValueUint32(v)
	value.columnIndex = page.columnIndex
	return value
}

type uint64Page struct {
	typ         Type
	values      []uint64
	columnIndex int16
}

func newUint64Page(typ Type, columnIndex int16, numValues int32, values encoding.Values) *uint64Page {
	return &uint64Page{
		typ:         typ,
		values:      values.Uint64()[:numValues],
		columnIndex: ^columnIndex,
	}
}

func (page *uint64Page) Type() Type { return page.typ }

func (page *uint64Page) Column() int { return int(^page.columnIndex) }

func (page *uint64Page) Dictionary() Dictionary { return nil }

func (page *uint64Page) NumRows() int64 { return int64(len(page.values)) }

func (page *uint64Page) NumValues() int64 { return int64(len(page.values)) }

func (page *uint64Page) NumNulls() int64 { return 0 }

func (page *uint64Page) Size() int64 { return 8 * int64(len(page.values)) }

func (page *uint64Page) RepetitionLevels() []byte { return nil }

func (page *uint64Page) DefinitionLevels() []byte { return nil }

func (page *uint64Page) Data() encoding.Values { return encoding.Uint64Values(page.values) }

func (page *uint64Page) Values() ValueReader { return &uint64PageValues{page: page} }

func (page *uint64Page) min() uint64 { return minUint64(page.values) }

func (page *uint64Page) max() uint64 { return maxUint64(page.values) }

func (page *uint64Page) bounds() (min, max uint64) { return boundsUint64(page.values) }

func (page *uint64Page) Bounds() (min, max Value, ok bool) {
	if ok = len(page.values) > 0; ok {
		minUint64, maxUint64 := page.bounds()
		min = page.makeValue(minUint64)
		max = page.makeValue(maxUint64)
	}
	return min, max, ok
}

func (page *uint64Page) Slice(i, j int64) Page {
	return &uint64Page{
		typ:         page.typ,
		values:      page.values[i:j],
		columnIndex: page.columnIndex,
	}
}

func (page *uint64Page) makeValue(v uint64) Value {
	value := makeValueUint64(v)
	value.columnIndex = page.columnIndex
	return value
}

type be128Page struct {
	typ         Type
	values      [][16]byte
	columnIndex int16
}

func newBE128Page(typ Type, columnIndex int16, numValues int32, values encoding.Values) *be128Page {
	return &be128Page{
		typ:         typ,
		values:      values.Uint128()[:numValues],
		columnIndex: ^columnIndex,
	}
}

func (page *be128Page) Type() Type { return page.typ }

func (page *be128Page) Column() int { return int(^page.columnIndex) }

func (page *be128Page) Dictionary() Dictionary { return nil }

func (page *be128Page) NumRows() int64 { return int64(len(page.values)) }

func (page *be128Page) NumValues() int64 { return int64(len(page.values)) }

func (page *be128Page) NumNulls() int64 { return 0 }

func (page *be128Page) Size() int64 { return 16 * int64(len(page.values)) }

func (page *be128Page) RepetitionLevels() []byte { return nil }

func (page *be128Page) DefinitionLevels() []byte { return nil }

func (page *be128Page) Data() encoding.Values { return encoding.Uint128Values(page.values) }

func (page *be128Page) Values() ValueReader { return &be128PageValues{page: page} }

func (page *be128Page) min() []byte { return minBE128(page.values) }

func (page *be128Page) max() []byte { return maxBE128(page.values) }

func (page *be128Page) bounds() (min, max []byte) { return boundsBE128(page.values) }

func (page *be128Page) Bounds() (min, max Value, ok bool) {
	if ok = len(page.values) > 0; ok {
		minBytes, maxBytes := page.bounds()
		min = page.makeValueBytes(minBytes)
		max = page.makeValueBytes(maxBytes)
	}
	return min, max, ok
}

func (page *be128Page) Slice(i, j int64) Page {
	return &be128Page{
		typ:         page.typ,
		values:      page.values[i:j],
		columnIndex: page.columnIndex,
	}
}

func (page *be128Page) makeValue(v *[16]byte) Value {
	return page.makeValueBytes(v[:])
}

func (page *be128Page) makeValueBytes(v []byte) Value {
	value := makeValueBytes(FixedLenByteArray, v)
	value.columnIndex = page.columnIndex
	return value
}

func (page *be128Page) makeValueString(v string) Value {
	value := makeValueString(FixedLenByteArray, v)
	value.columnIndex = page.columnIndex
	return value
}

type nullPage struct {
	typ    Type
	column int
	count  int
}

func newNullPage(typ Type, columnIndex int16, numValues int32) *nullPage {
	return &nullPage{
		typ:    typ,
		column: int(columnIndex),
		count:  int(numValues),
	}
}

func (page *nullPage) Type() Type                        { return page.typ }
func (page *nullPage) Column() int                       { return page.column }
func (page *nullPage) Dictionary() Dictionary            { return nil }
func (page *nullPage) NumRows() int64                    { return int64(page.count) }
func (page *nullPage) NumValues() int64                  { return int64(page.count) }
func (page *nullPage) NumNulls() int64                   { return int64(page.count) }
func (page *nullPage) Bounds() (min, max Value, ok bool) { return }
func (page *nullPage) Size() int64                       { return 1 }
func (page *nullPage) Values() ValueReader {
	return &nullPageValues{column: page.column, remain: page.count}
}
func (page *nullPage) Slice(i, j int64) Page {
	return &nullPage{column: page.column, count: page.count - int(j-i)}
}
func (page *nullPage) RepetitionLevels() []byte { return nil }
func (page *nullPage) DefinitionLevels() []byte { return nil }
func (page *nullPage) Data() encoding.Values    { return encoding.Values{} }
