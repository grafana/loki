package index

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

	chunk_util "github.com/grafana/loki/pkg/storage/chunk/util"
)

// Identifier has all the information needed to resolve a TSDB index
// Notably this abstracts away OS path separators, etc.
type Identifier struct {
	Tenant        string
	From, Through model.Time
	Checksum      uint32
}

func (i Identifier) String() string {
	return filepath.Join(
		i.Tenant,
		fmt.Sprintf(
			"%s-%d-%d-%x.tsdb",
			IndexFilename,
			i.From,
			i.Through,
			i.Checksum,
		),
	)
}

func (i Identifier) Combine(parentDir string) string {
	return filepath.Join(parentDir, i.String())
}

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
	chunks ChunkMetas
}

func NewBuilder() *Builder {
	return &Builder{streams: make(map[string]*stream)}
}

func (b *Builder) AddSeries(ls labels.Labels, chks []ChunkMeta) {
	id := ls.String()
	s, ok := b.streams[id]
	if !ok {
		s = &stream{
			labels: ls,
		}
		b.streams[id] = s
	}

	s.chunks = append(s.chunks, chks...)
}

func (b *Builder) Build(ctx context.Context, dir, tenant string) (id Identifier, err error) {
	// Ensure the parent dir exists (i.e. index/<bucket>/<tenant>/)
	parent := filepath.Join(dir, tenant)
	if parent != "" {
		if err := chunk_util.EnsureDirectory(parent); err != nil {
			return id, err
		}
	}

	// First write tenant/index-bounds-random.staging
	rng := rand.Int63()
	name := fmt.Sprintf("%s-%x.staging", IndexFilename, rng)
	tmpPath := filepath.Join(parent, name)

	writer, err := NewWriter(ctx, tmpPath)
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
		if err := writer.AddSeries(storage.SeriesRef(i), s.labels, s.chunks.finalize()...); err != nil {
			return id, err
		}
	}

	if err := writer.Close(); err != nil {
		return id, err
	}

	reader, err := NewFileReader(tmpPath)
	if err != nil {
		return id, err
	}

	from, through := reader.Bounds()

	// load the newly compacted index to grab checksum, promptly close
	id = Identifier{
		Tenant:   tenant,
		From:     model.Time(from),
		Through:  model.Time(through),
		Checksum: reader.Checksum(),
	}

	reader.Close()
	defer func() {
		if err != nil {
			os.RemoveAll(tmpPath)
		}
	}()

	dst := id.Combine(dir)

	if err := os.Rename(tmpPath, dst); err != nil {
		return id, err
	}

	return id, nil

}
