package dataobj

import (
	"errors"
	"fmt"
	"io"

	"github.com/grafana/loki/v3/pkg/scratch"
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

// NewBuilder returns a Builder that buffers completed sections in
// scratchStore. If scratchStore is nil, NewBuilder uses an in-memory store.
//
// A disk-backed scratchStore limits the memory of a builder to the memory of
// its in-progress sections.
func NewBuilder(scratchStore scratch.Store, opts ...BuilderOption) *Builder {
	if scratchStore == nil {
		scratchStore = scratch.NewMemory()
	}

	var cfg builderConfig
	for _, opt := range opts {
		opt(&cfg)
	}

	return &Builder{
		encoder: newEncoder(scratchStore, cfg.prioritySections, cfg.sizeLeveled),
	}
}

// BuilderOption configures a [Builder] in [NewBuilder].
type BuilderOption func(*builderConfig)

type builderConfig struct {
	prioritySections []SectionType
	sizeLeveled      *SizeLeveledCompaction
}

// WithPrioritySections places sections with the given types before all other
// sections. Types match by [SectionType.Equals], and sections are grouped in
// the order of types. Sections of the same group keep the order of
// [Builder.Append].
//
// The order applies to [Object.Sections], to the metadata regions, and to the
// data regions. Every metadata region comes before every data region.
//
// The index of a section among sections of its own type does not change. Its
// index in [Object.Sections] can change.
func WithPrioritySections(types ...SectionType) BuilderOption {
	return func(cfg *builderConfig) {
		cfg.prioritySections = append(cfg.prioritySections, types...)
	}
}

// SizeLeveledCompaction records where the size-leveled compaction strategy
// placed an object.
type SizeLeveledCompaction struct {
	// The planner computes Level from the base and ratio of the strategy at
	// plan time. A later planner can compute a different level for the same run.
	Level uint32
}

// WithSizeLeveledCompaction writes c into the file metadata of every object
// that the builder flushes, also after [Builder.Reset].
func WithSizeLeveledCompaction(c SizeLeveledCompaction) BuilderOption {
	return func(cfg *builderConfig) { cfg.sizeLeveled = &c }
}

// Append flushes a [SectionBuilder], buffering its data and metadata into b.
// The object lists sections in the order of Append calls, except that
// [WithPrioritySections] moves sections of the given types first.
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

func (w builderSectionWriter) WriteSection(opts *WriteSectionOptions, data, metadata []byte) (n int64, err error) {
	w.enc.AppendSection(w.typ, opts, data, metadata)
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
// Flush returns an error if the object could not be constructed. No object is
// handed to the caller in that case, so the sections buffered so far are
// released from the scratch store instead.
//
// [Builder.Reset] is called by Flush, whether it succeeds or fails, to discard
// any pending data and allow new data to be appended.
func (b *Builder) Flush() (*Object, io.Closer, error) {
	defer b.Reset()

	snapshot, err := b.encoder.Flush()
	if err != nil {
		return nil, nil, fmt.Errorf("flushing object: %w", err)
	}

	obj, err := FromReaderAt(snapshot, snapshot.Size())
	if err != nil {
		// The snapshot took over the buffered sections and is never handed to
		// the caller, so closing it here is the only way to release them.
		return nil, nil, errors.Join(fmt.Errorf("error building object: %w", err), snapshot.Close())
	}

	return obj, snapshot, nil
}

// Reset discards pending data and resets the builder to an empty state,
// releasing any sections it still holds from the scratch store.
func (b *Builder) Reset() {
	b.encoder.Reset()
}
