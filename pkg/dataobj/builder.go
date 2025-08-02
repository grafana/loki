package dataobj

import (
	"bytes"
	"fmt"
	"io"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/util/bufpool"
)

// A Builder builds data objects from a set of incoming log data. Log data is
// appended to a builder by calling [Builder.Append]. Buffered log data is
// flushed manually by calling [Builder.Flush].
//
// Methods on Builder are not goroutine-safe; callers are responsible for
// synchronizing calls.
type Builder struct {
	encoder *encoder
}

// A Builder accumulates data from a set of in-progress sections. A Builder can
// be flushed into a data object by calling [Builder.Flush].
func NewBuilder() *Builder {
	return &Builder{encoder: newEncoder()}
}

// Append flushes a [SectionBuilder], buffering its data and metadata into b.
// Append does not enforce ordering; sections may be flushed to the dataobj in
// any order.
//
// Append returns an error if the section failed to flush.
//
// After successfully calling Append, sec is reset and can be reused.
func (b *Builder) Append(sec SectionBuilder) error {
	w := builderSectionWriter{typ: sec.Type(), enc: b.encoder}
	if _, err := sec.Flush(w); err != nil {
		return err
	}

	// SectionBuilder implementations are expected to automatically Reset after a
	// Flush, but we'll call it again to be safe.
	sec.Reset()
	return nil
}

type builderSectionWriter struct {
	typ SectionType
	enc *encoder
}

func (w builderSectionWriter) WriteSection(data, metadata []byte) (n int64, err error) {
	w.enc.AppendSection(w.typ, data, metadata)
	return int64(len(data) + len(metadata)), nil
}

// Bytes returns the current number of bytes buffered in b for all appended
// sections.
func (b *Builder) Bytes() int {
	return b.encoder.Bytes()
}

// Flush constructs a new Object from the accumulated sections. Allocated
// resources for the Object must be released by calling Close on the returned
// io.Closer. After closing, the returned Object must no longer be read.
//
// Flush returns an error if the object could not be constructed.
// [Builder.Reset] is called after a successful flush to discard any pending
// data, allowing new data to be appended.
func (b *Builder) Flush() (*Object, io.Closer, error) {
	flushSize, err := b.encoder.FlushSize()
	if err != nil {
		return nil, nil, fmt.Errorf("determining object size: %w", err)
	}

	buf := bufpool.Get(int(flushSize))

	closer := func() error {
		bufpool.Put(buf)
		return nil
	}

	sz, err := b.encoder.Flush(buf)
	if err != nil {
		return nil, nil, fmt.Errorf("flushing object: %w", err)
	}

	obj, err := FromReaderAt(bytes.NewReader(buf.Bytes()), sz)
	if err != nil {
		bufpool.Put(buf)
		return nil, nil, fmt.Errorf("error building object: %w", err)
	}

	b.Reset()
	return obj, funcIOCloser(closer), nil
}

// Reset discards pending data and resets the builder to an empty state.
func (b *Builder) Reset() {
	b.encoder.Reset()
}

type funcIOCloser func() error

func (fc funcIOCloser) Close() error {
	return fc()
}
