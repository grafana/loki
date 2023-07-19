package logproto

import (
	"sort"
	"strings"
	"sync/atomic" //lint:ignore faillint we can't use go.uber.org/atomic with a protobuf struct without wrapping it.

	"github.com/cespare/xxhash/v2"
	"github.com/dustin/go-humanize"
	"github.com/prometheus/common/model"

	//"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/pkg/storage/stores/tsdb/index"
)

var seps = []byte{'\xff'}

func (id SeriesIdentifier) Hash(b []byte, keysForLabels []string) (uint64, []string) {
	keysForLabels = keysForLabels[:0]
	for k := range id.Labels {
		keysForLabels = append(keysForLabels, k)
	}
	sort.Strings(keysForLabels)

	// Use xxhash.Sum64(b) for fast path as it's faster.
	b = b[:0]
	for i, name := range keysForLabels {
		value := id.Labels[name]
		if len(b)+len(name)+len(value)+2 >= cap(b) {
			// If labels entry is 1KB+ do not allocate whole entry.
			h := xxhash.New()
			_, _ = h.Write(b)
			for _, name := range keysForLabels[i:] {
				value := id.Labels[name]
				_, _ = h.WriteString(name)
				_, _ = h.Write(seps)
				_, _ = h.WriteString(value)
				_, _ = h.Write(seps)
			}
			return h.Sum64(), keysForLabels
		}

		b = append(b, name...)
		b = append(b, seps[0])
		b = append(b, value...)
		b = append(b, seps[0])
	}
	return xxhash.Sum64(b), keysForLabels
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
