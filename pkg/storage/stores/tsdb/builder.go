package tsdb

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"sort"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/storage"

	chunk_util "github.com/grafana/loki/pkg/storage/chunk/client/util"
	"github.com/grafana/loki/pkg/storage/stores/tsdb/index"
)

// Builder is a helper used to create tsdb indices.
// It can accept streams in any order and will create the tsdb
// index appropriately via `Build()`
// It can even receive multiple writes for the same stream with the caveat
// that chunks must be added in order and not duplicated
type Builder struct {
	streams map[string]*stream
}

type stream struct {
	labels labels.Labels
	fp     model.Fingerprint
	chunks index.ChunkMetas
}

func NewBuilder() *Builder {
	return &Builder{streams: make(map[string]*stream)}
}

func (b *Builder) AddSeries(ls labels.Labels, fp model.Fingerprint, chks []index.ChunkMeta) {
	id := ls.String()
	s, ok := b.streams[id]
	if !ok {
		s = &stream{
			labels: ls,
			fp:     fp,
		}
		b.streams[id] = s
	}

	s.chunks = append(s.chunks, chks...)
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
	rng := rand.Int63()
	name := fmt.Sprintf("%s-%x.staging", index.IndexFilename, rng)
	tmpPath := filepath.Join(scratchDir, name)

	writer, err := index.NewWriter(ctx, tmpPath)
	if err != nil {
		return id, err
	}
	// TODO(owen-d): multithread

	// Sort series
	streams := make([]*stream, 0, len(b.streams))
	for _, s := range b.streams {
		streams = append(streams, s)
	}
	sort.Slice(streams, func(i, j int) bool {
		if a, b := streams[i].labels.Hash(), streams[j].labels.Hash(); a != b {
			return a < b
		}
		return labels.Compare(streams[i].labels, streams[j].labels) < 0
	})

	// Build symbols
	symbolsMap := make(map[string]struct{})
	for _, s := range streams {
		for _, l := range s.labels {
			symbolsMap[l.Name] = struct{}{}
			symbolsMap[l.Value] = struct{}{}
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
			return id, err
		}
	}

	// Add series
	for i, s := range streams {
		if err := writer.AddSeries(storage.SeriesRef(i), s.labels, s.fp, s.chunks.Finalize()...); err != nil {
			return id, err
		}
	}

	if err := writer.Close(); err != nil {
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
