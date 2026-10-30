package tsdb

import (
	"context"
	"fmt"
	"io"
	"iter"
	"math/rand"
	"os"
	"path/filepath"
	"slices"
	"sort"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/storage"

	chunk_util "github.com/grafana/loki/v4/pkg/storage/chunk/client/util"
	"github.com/grafana/loki/v4/pkg/storage/stores/shipper/indexshipper/tsdb/index"
)

// Builder is a helper used to create tsdb indices.
// It can accept streams in any order and will create the tsdb
// index appropriately via `Build()`
// It can even receive multiple writes for the same stream with the caveat
// that chunks must be added in order and not duplicated
type Builder struct {
	// streams is keyed by labels hash rather than labels.String() so the
	// builder doesn't hold a second copy of every series' labels. Streams whose
	// hash collides with an existing entry are kept in collisions.
	streams         map[uint64]*stream
	collisions      map[uint64][]*stream
	hash            func(labels.Labels) uint64 // overridable in tests to force collisions
	chunksFinalized bool
	version         int
}

type stream struct {
	labels labels.Labels
	fp     model.Fingerprint
	chunks index.ChunkMetas
}

// NewBuilder returns a Builder that writes indexes in the given format version.
func NewBuilder(version int) *Builder {
	return &Builder{
		streams: make(map[uint64]*stream),
		hash:    labels.Labels.Hash,
		version: version,
	}
}

// AddSeries adds chunks to the stream with the given labels, creating the stream if it doesn't exist.
func (b *Builder) AddSeries(ls labels.Labels, fp model.Fingerprint, chks []index.ChunkMeta) {
	h := b.hash(ls)
	s := b.getStreamWithHash(ls, h)
	if s == nil {
		s = &stream{
			labels: ls,
			fp:     fp,
		}
		b.putStream(h, s)
	}

	s.chunks = append(s.chunks, chks...)
}

// getStream returns the stream with the given labels or nil if it doesn't exist.
func (b *Builder) getStream(ls labels.Labels) *stream {
	return b.getStreamWithHash(ls, b.hash(ls))
}

func (b *Builder) getStreamWithHash(ls labels.Labels, h uint64) *stream {
	if s, ok := b.streams[h]; ok && labels.Equal(s.labels, ls) {
		return s
	}
	for _, s := range b.collisions[h] {
		if labels.Equal(s.labels, ls) {
			return s
		}
	}
	return nil
}

func (b *Builder) putStream(h uint64, s *stream) {
	if _, ok := b.streams[h]; !ok {
		b.streams[h] = s
		return
	}
	if b.collisions == nil {
		b.collisions = make(map[uint64][]*stream)
	}
	b.collisions[h] = append(b.collisions[h], s)
}

// deleteStream removes the stream with the given labels and reports whether it existed.
func (b *Builder) deleteStream(ls labels.Labels) bool {
	h := b.hash(ls)
	if s, ok := b.streams[h]; ok && labels.Equal(s.labels, ls) {
		delete(b.streams, h)
		return true
	}

	cs := b.collisions[h]
	for i, s := range cs {
		if !labels.Equal(s.labels, ls) {
			continue
		}
		if len(cs) == 1 {
			delete(b.collisions, h)
		} else {
			b.collisions[h] = slices.Delete(cs, i, i+1)
		}
		return true
	}
	return false
}

// allStreams iterates every stream in the builder. Streams must not be added or removed while iterating.
func (b *Builder) allStreams() iter.Seq[*stream] {
	return func(yield func(*stream) bool) {
		for _, s := range b.streams {
			if !yield(s) {
				return
			}
		}
		for _, cs := range b.collisions {
			for _, s := range cs {
				if !yield(s) {
					return
				}
			}
		}
	}
}

func (b *Builder) numStreams() int {
	n := len(b.streams)
	for _, cs := range b.collisions {
		n += len(cs)
	}
	return n
}

// dropEmptyStreams removes all the streams without any chunks.
func (b *Builder) dropEmptyStreams() {
	var empty []labels.Labels
	for s := range b.allStreams() {
		if len(s.chunks) == 0 {
			empty = append(empty, s.labels)
		}
	}
	for _, ls := range empty {
		b.deleteStream(ls)
	}
}

// FinalizeChunks sorts and dedupes the chunks of every stream. It must be called before the chunks are modified.
func (b *Builder) FinalizeChunks() {
	for s := range b.allStreams() {
		s.chunks = s.chunks.Finalize()
	}
	b.chunksFinalized = true
}

// InsertChunk adds a chunk to the stream with the given labels. The stream must exist.
func (b *Builder) InsertChunk(ls labels.Labels, chk index.ChunkMeta) error {
	if !b.chunksFinalized {
		return fmt.Errorf("chunk insertion is only allowed on finalized chunks")
	}

	s := b.getStream(ls)
	if s == nil {
		return fmt.Errorf("chunk insertion is only allowed on existing streams")
	}

	s.chunks = s.chunks.Add(chk)
	return nil
}

// DropChunk removes a chunk from the stream with the given labels and reports whether it was found. The stream must exist.
func (b *Builder) DropChunk(ls labels.Labels, chk index.ChunkMeta) (bool, error) {
	if !b.chunksFinalized {
		return false, fmt.Errorf("dropping of chunk is only allowed on finalized chunks")
	}

	s := b.getStream(ls)
	if s == nil {
		return false, fmt.Errorf("dropping of chunk is only allowed on existing streams")
	}

	var chunkFound bool
	s.chunks, chunkFound = s.chunks.Drop(chk)
	return chunkFound, nil
}

// HasChunk reports whether the stream with the given labels has the chunk.
func (b *Builder) HasChunk(ls labels.Labels, chk index.ChunkMeta) (bool, error) {
	if !b.chunksFinalized {
		return false, fmt.Errorf("checking chunk existence is only allowed on finalized chunks")
	}

	s := b.getStream(ls)
	if s == nil {
		return false, nil
	}

	return s.chunks.HasChunk(chk), nil
}

// Build writes the index to scratchDir and moves it to the path of the Identifier returned by createFn.
func (b *Builder) Build(
	ctx context.Context,
	scratchDir string,
	// Determines how to create the resulting Identifier and file name.
	// This is variable as we use Builder for multiple reasons,
	// such as building multi-tenant tsdbs on the ingester
	// and per tenant ones during compaction
	createFn func(from, through model.Time, checksum uint32) Identifier,
) (id Identifier, err error) {
	// Ensure the parent dir exists (i.e. index/<bucket>/<tenant>/)
	if scratchDir != "" {
		if err := chunk_util.EnsureDirectory(scratchDir); err != nil {
			return id, err
		}
	}

	// First write tenant/index-bounds-random.staging
	rng := rand.Int63() //#nosec G404 -- just generating a random filename in a slightly unidiomatic way. Collision resistance is not a concern. -- nosemgrep: math-random-used
	name := fmt.Sprintf("%s-%x.staging", index.IndexFilename, rng)
	tmpPath := filepath.Join(scratchDir, name)

	writer, err := index.NewFileWriterWithVersion(ctx, b.version, tmpPath)
	if err != nil {
		return id, err
	}

	if _, err := b.build(writer, false); err != nil {
		return id, err
	}

	reader, err := index.NewMmapFileReader(tmpPath)
	if err != nil {
		return id, err
	}

	from, through := reader.Bounds()

	// load the newly compacted index to grab checksum, promptly close
	dst := createFn(model.Time(from), model.Time(through), reader.Checksum())

	reader.Close()
	defer func() {
		if err != nil {
			os.RemoveAll(tmpPath)
		}
	}()

	if err := chunk_util.EnsureDirectory(filepath.Dir(dst.Path())); err != nil {
		return id, err
	}
	dstPath := dst.Path()
	if err := os.Rename(tmpPath, dstPath); err != nil {
		return id, err
	}

	return dst, nil
}

func (b *Builder) build(
	writer *index.Creator,
	reader bool, // whether to return the ReadCloser of the underlying DB
) (io.ReadCloser, error) {
	// TODO(owen-d): multithread

	// Sort series
	streams := make([]*stream, 0, b.numStreams())
	streams = slices.AppendSeq(streams, b.allStreams())

	// Use the supplied fingerprints instead of hashing labels for two reasons:
	// 1) Correctness: fingerprints differ from label hashes because
	// we add a synthesized __loki_tennat__ label, which is eventually compacted away.
	// 2) Speed: No hashing required
	sort.Slice(streams, func(i, j int) bool {
		if streams[i].fp != streams[j].fp {
			return streams[i].fp < streams[j].fp
		}
		return labels.Compare(streams[i].labels, streams[j].labels) < 0
	})

	// Build symbols
	symbolsMap := make(map[string]struct{})
	for _, s := range streams {
		s.labels.Range(func(l labels.Label) {
			symbolsMap[l.Name] = struct{}{}
			symbolsMap[l.Value] = struct{}{}
		})
	}

	// Sort symbols
	symbols := make([]string, 0, len(symbolsMap))
	for s := range symbolsMap {
		symbols = append(symbols, s)
	}
	sort.Strings(symbols)

	// Add symbols
	for _, symbol := range symbols {
		if err := writer.AddSymbol(symbol); err != nil {
			return nil, err
		}
	}

	// Add series
	for i, s := range streams {
		if !b.chunksFinalized {
			s.chunks = s.chunks.Finalize()
		}
		if err := writer.AddSeries(storage.SeriesRef(i), s.labels, s.fp, s.chunks...); err != nil {
			return nil, err
		}
	}

	return writer.Close(reader)
}

// BuildInMemory builds the index in memory and returns its bytes.
func (b *Builder) BuildInMemory(
	ctx context.Context,
	// Determines how to create the resulting Identifier and file name.
	// This is variable as we use Builder for multiple reasons,
	// such as building multi-tenant tsdbs on the ingester
	// and per tenant ones during compaction
	createFn func(from, through model.Time, checksum uint32) Identifier,
) (id Identifier, data []byte, err error) {
	writer, err := index.NewMemWriterWithVersion(ctx, b.version)
	if err != nil {
		return id, nil, err
	}

	readCloser, err := b.build(writer, true)
	if err != nil {
		return id, nil, err
	}
	defer readCloser.Close()

	data, err = io.ReadAll(readCloser)
	if err != nil {
		return nil, nil, err
	}

	reader, err := index.NewByteSliceReader(index.RealByteSlice(data))
	if err != nil {
		return id, nil, err
	}
	defer reader.Close()

	from, through := reader.Bounds()
	id = createFn(model.Time(from), model.Time(through), reader.Checksum())

	return id, data, nil
}
