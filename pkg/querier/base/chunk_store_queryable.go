package base

import (
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/storage"
	"github.com/prometheus/prometheus/tsdb/chunkenc"

	"github.com/grafana/loki/pkg/ingester/client"
	seriesset "github.com/grafana/loki/pkg/querier/series"
	"github.com/grafana/loki/pkg/storage/chunk"
)

const seriesFunc = "series"

type chunkIteratorFunc func(chunks []chunk.Chunk, from, through model.Time) chunkenc.Iterator

// Series in the returned set are sorted alphabetically by labels.
func partitionChunks(chunks []chunk.Chunk, mint, maxt int64, iteratorFunc chunkIteratorFunc) storage.SeriesSet {
	chunksBySeries := map[string][]chunk.Chunk{}
	for _, c := range chunks {
		key := client.LabelsToKeyString(c.Metric)
		chunksBySeries[key] = append(chunksBySeries[key], c)
	}

	series := make([]storage.Series, 0, len(chunksBySeries))
	for i := range chunksBySeries {
		series = append(series, &chunkSeries{
			labels:            chunksBySeries[i][0].Metric,
			chunks:            chunksBySeries[i],
			chunkIteratorFunc: iteratorFunc,
			mint:              mint,
			maxt:              maxt,
		})
	}

	return seriesset.NewConcreteSeriesSet(series)
}

// Implements SeriesWithChunks
type chunkSeries struct {
	labels            labels.Labels
	chunks            []chunk.Chunk
	chunkIteratorFunc chunkIteratorFunc
	mint, maxt        int64
}

func (s *chunkSeries) Labels() labels.Labels {
	return s.labels
}

// Iterator returns a new iterator of the data of the series.
func (s *chunkSeries) Iterator() chunkenc.Iterator {
	return s.chunkIteratorFunc(s.chunks, model.Time(s.mint), model.Time(s.maxt))
}

// Chunks implements SeriesWithChunks interface.
func (s *chunkSeries) Chunks() []chunk.Chunk {
	return s.chunks
}
