package parquet

import (
	"errors"
	"fmt"
	"io"

	"github.com/parquet-go/parquet-go/encoding"
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
	//
	// In the data page format version 1, it wasn't specified whether pages
	// must start with a new row. Legacy writers have produced parquet files
	// where row values were overlapping between two consecutive pages.
	// As a result, the values read must not be assumed to start at the
	// beginning of a row, unless the program knows that it is only working
	// with parquet files that used the data page format version 2 (which is
	// the default behavior for parquet-go).
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
	read := make(chan asyncPage)
	seek := make(chan asyncSeek, 1)
	init := make(chan struct{})
	done := make(chan struct{})

	go readPages(pages, read, seek, init, done)

	p := &asyncPages{
		read: read,
		seek: seek,
		init: init,
		done: done,
	}

	// If the pages object gets garbage collected without Close being called,
	// this finalizer would ensure that the goroutine is stopped and doesn't
	// leak.
	debug.SetFinalizer(p, func(p *asyncPages) { p.Close() })
	return p
}

type asyncPages struct {
	read    chan asyncPage
	seek    chan asyncSeek
	init    chan struct{}
	done    chan struct{}
	version int64
}

type asyncPage struct {
	page    Page
	err     error
	version int64
}

type asyncSeek struct {
	rowIndex int64
	version  int64
}

func (pages *asyncPages) Close() (err error) {
	if pages.init != nil {
		close(pages.init)
		pages.init = nil
	}
	if pages.done != nil {
		close(pages.done)
		pages.done = nil
	}
	for p := range pages.read {
		Release(p.page)

		// Capture the last error, which is the value returned from closing the
		// underlying Pages instance.
		err = p.err
	}
	pages.seek = nil
	return err
}

func (pages *asyncPages) ReadPage() (Page, error) {
	pages.start()
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

		// the page is being dropped here b/c it was the wrong version
		Release(p.page)
	}
}

func (pages *asyncPages) SeekToRow(rowIndex int64) error {
	if pages.seek == nil {
		return io.ErrClosedPipe
	}
	// First flush the channel in case SeekToRow is called twice or more in a
	// row, otherwise we would block if readPages had already exited.
	select {
	case <-pages.seek:
	default:
		pages.version++
	}
	// The seek channel has a capacity of 1 to allow the first SeekToRow call to
	// be non-blocking.
	//
	// If SeekToRow calls are performed faster than they can be handled by the
	// goroutine reading pages, this path might become a contention point.
	pages.seek <- asyncSeek{rowIndex: rowIndex, version: pages.version}
	pages.start()
	return nil
}

func (pages *asyncPages) start() {
	if pages.init != nil {
		close(pages.init)
		pages.init = nil
	}
}

func readPages(pages Pages, read chan<- asyncPage, seek <-chan asyncSeek, init, done <-chan struct{}) {
	defer func() {
		read <- asyncPage{err: pages.Close(), version: -1}
		close(read)
	}()

	// To avoid reading pages before the first SeekToRow call, we wait for the
	// reader to be initialized, which means it either received a call to
	// ReadPage, SeekToRow, or Close.
	select {
	case <-init:
	case <-done:
		return
	}

	// If SeekToRow was invoked before ReadPage, the seek channel contains the
	// new position of the reader.
	//
	// Note that we have a default case in this select because we don't want to
	// block if the first call was ReadPage and no values were ever produced to
	// the seek channel.
	var seekTo asyncSeek
	select {
	case seekTo = <-seek:
	default:
		seekTo.rowIndex = -1
	}

	var err error

	for {
		var page Page

		// if err is not fatal we consider the underlying pages object to be in an unknown state
		// and we only repeatedly return that error
		if !isFatalError(err) {
			if seekTo.rowIndex >= 0 {
				err = pages.SeekToRow(seekTo.rowIndex)
				if err == nil {
					seekTo.rowIndex = -1
					continue
				}
			} else {
				page, err = pages.ReadPage()
			}
		}

		select {
		case read <- asyncPage{
			page:    page,
			err:     err,
			version: seekTo.version,
		}:
		case seekTo = <-seek:
			Release(page)
		case <-done:
			Release(page)
			return
		}
	}
}

func isFatalError(err error) bool {
	return err != nil && err != io.EOF && !errors.Is(err, ErrSeekOutOfRange) // ErrSeekOutOfRange can be returned from FilePages but is recoverable
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
		Release(p)
		if err != nil {
			return numValues, err
		}
	}
}

// errorPage is an implementation of the Page interface which always errors when
// values are read from it.
type errorPage struct {
	typ         Type
	err         error
	columnIndex int
}

func newErrorPage(typ Type, columnIndex int, msg string, args ...any) *errorPage {
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

var (
	_ Page       = (*optionalPage)(nil)
	_ Page       = (*repeatedPage)(nil)
	_ Page       = (*booleanPage)(nil)
	_ Page       = (*int32Page)(nil)
	_ Page       = (*int64Page)(nil)
	_ Page       = (*int96Page)(nil)
	_ Page       = (*floatPage)(nil)
	_ Page       = (*doublePage)(nil)
	_ Page       = (*byteArrayPage)(nil)
	_ Page       = (*fixedLenByteArrayPage)(nil)
	_ Page       = (*uint32Page)(nil)
	_ Page       = (*uint64Page)(nil)
	_ Page       = (*be128Page)(nil)
	_ Page       = (*nullPage)(nil)
	_ Pages      = (*singlePage)(nil)
	_ PageReader = (*singlePage)(nil)
)
