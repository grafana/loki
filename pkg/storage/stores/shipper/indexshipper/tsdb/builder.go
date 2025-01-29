package tsdb

import (
	"context"
	"fmt"
	"io"
	"math/rand"
	"os"
	"path/filepath"
	"sort"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/storage"

	chunk_util "github.com/grafana/loki/v3/pkg/storage/chunk/client/util"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/index"
)

// Builder is a helper used to create tsdb indices.
// It can accept streams in any order and will create the tsdb
// index appropriately via `Build()`
// It can even receive multiple writes for the same stream with the caveat
// that chunks must be added in order and not duplicated
type Builder struct {
	streams         map[string]*stream
	chunksFinalized bool
	version         int
}

type stream struct {
	labels labels.Labels
	fp     model.Fingerprint
	chunks index.ChunkMetas
	stats  *index.StreamStats
}

func NewBuilder(version int) *Builder {
	return &Builder{
		streams: make(map[string]*stream),
		version: version,
	}
}

func (b *Builder) AddSeries(ls labels.Labels, fp model.Fingerprint, chks []index.ChunkMeta, stats *index.StreamStats) {
	id := ls.String()
	s, ok := b.streams[id]
	if !ok {
		s = &stream{
			labels: ls,
			fp:     fp,
			stats:  index.NewStreamStats(),
		}
		b.streams[id] = s
	}

	// h11: Seeing how we append insteasd of setting the chunks points that AddSeries can be called multiple times.
	// Doesn't look like we check for duplicated chunks so I think we keep adding chunks to the series until we rotate the index
	// when we reach a new period (new day). If we can verify this, then, for stats we could either merge as we do here of just set.
	// But I'm not sure if then we should only reset the stream stats when we rotate the index.
	s.chunks = append(s.chunks, chks...)

	if stats != nil {
		s.stats.Merge(stats)
	}
}

func (b *Builder) FinalizeChunks() {
	for id := range b.streams {
		b.streams[id].chunks = b.streams[id].chunks.Finalize()
	}
	b.chunksFinalized = true
}

func (b *Builder) InsertChunk(streamID string, chk index.ChunkMeta) error {
	if !b.chunksFinalized {
		return fmt.Errorf("chunk insertion is only allowed on finalized chunks")
	}

	s, ok := b.streams[streamID]
	if !ok {
		return fmt.Errorf("chunk insertion is only allowed on existing streams")
	}

	s.chunks = s.chunks.Add(chk)
	return nil
}

func (b *Builder) DropChunk(streamID string, chk index.ChunkMeta) (bool, error) {
	if !b.chunksFinalized {
		return false, fmt.Errorf("dropping of chunk is only allowed on finalized chunks")
	}

	s, ok := b.streams[streamID]
	if !ok {
		return false, fmt.Errorf("dropping of chunk is only allowed on existing streams")
	}

	var chunkFound bool
	s.chunks, chunkFound = s.chunks.Drop(chk)
	return chunkFound, nil
}

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
	rng := rand.Int63() //#nosec G404 -- just generating a random filename in a slightly unidiomatic way. Collision resistance is not a concern.
	name := fmt.Sprintf("%s-%x.staging", index.IndexFilename, rng)
	tmpPath := filepath.Join(scratchDir, name)

	writer, err := index.NewFileWriterWithVersion(ctx, b.version, tmpPath)
	if err != nil {
		return id, err
	}

	if _, err := b.build(writer, false); err != nil {
		return id, err
	}

	reader, err := index.NewFileReader(tmpPath)
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
	streams := make([]*stream, 0, len(b.streams))
	for _, s := range b.streams {
		streams = append(streams, s)
	}

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
		// Add labels/names to symbols
		for _, l := range s.labels {
			symbolsMap[l.Name] = struct{}{}
			symbolsMap[l.Value] = struct{}{}
		}

		// Add SM stats to symbols
		if s.stats != nil {
			for smName := range s.stats.StructuredMetadataFieldNames {
				symbolsMap[smName] = struct{}{}
			}
		}
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
		if err := writer.AddSeries(storage.SeriesRef(i), s.labels, s.fp, s.chunks, s.stats); err != nil {
			return nil, err
		}
	}

	return writer.Close(reader)
}

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

	reader, err := index.NewReader(index.RealByteSlice(data))
	if err != nil {
		return id, nil, err
	}
	defer reader.Close()

	from, through := reader.Bounds()
	id = createFn(model.Time(from), model.Time(through), reader.Checksum())

	return id, data, nil
}
