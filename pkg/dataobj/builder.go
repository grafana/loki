package dataobj

import (
	"bytes"
	"fmt"
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
// After succesfully calling Append, sec is reset and can be reused.
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

// Flush flushes all buffered data to the buffer provided. Calling Flush can result
// in a no-op if there is no buffered data to flush.
//
// [Builder.Reset] is called after a successful Flush to discard any pending data and allow new data to be appended.
func (b *Builder) Flush(output *bytes.Buffer) error {
	err := b.buildObject(output)
	if err != nil {
		return fmt.Errorf("building object: %w", err)
	}

	b.Reset()
	return nil
}

func (b *Builder) buildObject(output *bytes.Buffer) error {
	return b.encoder.Flush(output)
}

// Reset discards pending data and resets the builder to an empty state.
func (b *Builder) Reset() {
	b.encoder.Reset()
}
