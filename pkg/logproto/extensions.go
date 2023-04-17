package logproto

import (
	"strings"
	"sync/atomic" //lint:ignore faillint we can't use go.uber.org/atomic with a protobuf struct without wrapping it.

	"github.com/dustin/go-humanize"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/pkg/storage/stores/tsdb/index"
)

// Note, this is not very efficient and use should be minimized as it requires label construction on each comparison
type SeriesIdentifiers []SeriesIdentifier

func (ids SeriesIdentifiers) Len() int      { return len(ids) }
func (ids SeriesIdentifiers) Swap(i, j int) { ids[i], ids[j] = ids[j], ids[i] }
func (ids SeriesIdentifiers) Less(i, j int) bool {
	a, b := labels.FromMap(ids[i].Labels), labels.FromMap(ids[j].Labels)
	return labels.Compare(a, b) <= 0
}

type Streams []Stream

func (xs Streams) Len() int           { return len(xs) }
func (xs Streams) Swap(i, j int)      { xs[i], xs[j] = xs[j], xs[i] }
func (xs Streams) Less(i, j int) bool { return xs[i].Labels <= xs[j].Labels }

func (s Series) Len() int           { return len(s.Samples) }
func (s Series) Swap(i, j int)      { s.Samples[i], s.Samples[j] = s.Samples[j], s.Samples[i] }
func (s Series) Less(i, j int) bool { return s.Samples[i].Timestamp < s.Samples[j].Timestamp }

// Safe for concurrent use
func (m *IndexStatsResponse) AddStream(_ model.Fingerprint) {
	atomic.AddUint64(&m.Streams, 1)
}

// Safe for concurrent use
func (m *IndexStatsResponse) AddChunkStats(s index.ChunkStats) {
	atomic.AddUint64(&m.Chunks, s.Chunks)
	atomic.AddUint64(&m.Bytes, s.KB<<10)
	atomic.AddUint64(&m.Entries, s.Entries)
}

func (m *IndexStatsResponse) Stats() IndexStatsResponse {
	return *m
}

// Helper function for returning the key value pairs
// to be passed to a logger
func (m *IndexStatsResponse) LoggingKeyValues() []interface{} {
	if m == nil {
		return nil
	}
	return []interface{}{
		"bytes", strings.Replace(humanize.Bytes(m.Bytes), " ", "", 1),
		"chunks", m.Chunks,
		"streams", m.Streams,
		"entries", m.Entries,
	}
}
