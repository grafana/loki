package index

import (
	"context"
	"sort"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/storage"
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

func (b *Builder) Build(ctx context.Context, dir string) error {
	writer, err := NewWriter(ctx, dir)
	if err != nil {
		return err
	}
	// TODO(owen-d): multithread

	// Sort series
	streams := make([]*stream, 0, len(b.streams))
	for _, s := range b.streams {
		streams = append(streams, s)
	}
	sort.Slice(streams, func(i, j int) bool {
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
			return err
		}
	}

	// Add series
	for i, s := range streams {
		if err := writer.AddSeries(storage.SeriesRef(i), s.labels, s.chunks.finalize()...); err != nil {
			return err
		}
	}

	return writer.Close()
}
