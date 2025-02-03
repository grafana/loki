package logproto

import (
	"sort"
	"strings"
	"sync/atomic" //lint:ignore faillint we can't use go.uber.org/atomic with a protobuf struct without wrapping it.

	"github.com/cespare/xxhash/v2"
	"github.com/dustin/go-humanize"
	jsoniter "github.com/json-iterator/go"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/index"
)

// This is the separator define in the Prometheus Labels.Hash function.
var seps = []byte{'\xff'}

// Hash returns hash of the labels according to Prometheus' Labels.Hash function.
// `b` is a buffer that should be reused to avoid allocations.
func (id SeriesIdentifier) Hash(b []byte) uint64 {
	sort.Sort(id)

	// Use xxhash.Sum64(b) for fast path as it's faster.
	b = b[:0]
	for i, pair := range id.Labels {
		name := pair.Key
		value := pair.Value
		if len(b)+len(name)+len(value)+2 >= cap(b) {
			// If labels entry is 1KB+ do not allocate whole entry.
			h := xxhash.New()
			_, _ = h.Write(b)
			for _, pair := range id.Labels[i:] {
				name := pair.Key
				value := pair.Value
				_, _ = h.WriteString(name)
				_, _ = h.Write(seps)
				_, _ = h.WriteString(value)
				_, _ = h.Write(seps)
			}
			return h.Sum64()
		}

		b = append(b, name...)
		b = append(b, seps[0])
		b = append(b, value...)
		b = append(b, seps[0])
	}
	return xxhash.Sum64(b)
}

func (id SeriesIdentifier) Get(key string) string {
	for _, entry := range id.Labels {
		if entry.Key == key {
			return entry.Value
		}
	}

	return ""
}

func SeriesIdentifierFromMap(in map[string]string) SeriesIdentifier {
	id := SeriesIdentifier{
		Labels: make([]SeriesIdentifier_LabelsEntry, 0, len(in)),
	}
	for k, v := range in {
		id.Labels = append(id.Labels, SeriesIdentifier_LabelsEntry{Key: k, Value: v})
	}
	return id
}

func SeriesIdentifierFromLabels(in labels.Labels) SeriesIdentifier {
	id := SeriesIdentifier{
		Labels: make([]SeriesIdentifier_LabelsEntry, len(in)),
	}
	for i, l := range in {
		id.Labels[i] = SeriesIdentifier_LabelsEntry{Key: l.Name, Value: l.Value}
	}
	return id
}

func MustNewSeriesEntries(labels ...string) []SeriesIdentifier_LabelsEntry {
	if len(labels)%2 != 0 {
		panic("invalid number of labels")
	}
	r := make([]SeriesIdentifier_LabelsEntry, 0, len(labels)/2)
	for i := 0; i < len(labels); i += 2 {
		r = append(r, SeriesIdentifier_LabelsEntry{Key: labels[i], Value: labels[i+1]})
	}
	return r
}

func (id SeriesIdentifier) Len() int           { return len(id.Labels) }
func (id SeriesIdentifier) Swap(i, j int)      { id.Labels[i], id.Labels[j] = id.Labels[j], id.Labels[i] }
func (id SeriesIdentifier) Less(i, j int) bool { return id.Labels[i].Key < id.Labels[j].Key }

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

func (m *Shard) SpaceFor(stats *IndexStatsResponse, targetShardBytes uint64) bool {
	curDelta := max(m.Stats.Bytes, targetShardBytes) - min(m.Stats.Bytes, targetShardBytes)
	updated := m.Stats.Bytes + stats.Bytes
	newDelta := max(updated, targetShardBytes) - min(updated, targetShardBytes)
	return newDelta <= curDelta
}

type DetectedFieldType string

const (
	DetectedFieldString   DetectedFieldType = "string"
	DetectedFieldInt      DetectedFieldType = "int"
	DetectedFieldFloat    DetectedFieldType = "float"
	DetectedFieldBoolean  DetectedFieldType = "boolean"
	DetectedFieldDuration DetectedFieldType = "duration"
	DetectedFieldBytes    DetectedFieldType = "bytes"
)

// UnmarshalJSON implements the json.Unmarshaler interface.
// QueryPatternsResponse json representation is different from the proto
//
//	`{"status":"success","data":[{"pattern":"foo <*> bar","samples":[[1,1],[2,2]]},{"pattern":"foo <*> buzz","samples":[[3,1],[3,2]]}]}`
func (r *QueryPatternsResponse) UnmarshalJSON(data []byte) error {
	var v struct {
		Status string `json:"status"`
		Data   []struct {
			Pattern string    `json:"pattern"`
			Samples [][]int64 `json:"samples"`
		} `json:"data"`
	}
	if err := jsoniter.ConfigFastest.Unmarshal(data, &v); err != nil {
		return err
	}
	r.Series = make([]*PatternSeries, 0, len(v.Data))
	for _, d := range v.Data {
		samples := make([]*PatternSample, 0, len(d.Samples))
		for _, s := range d.Samples {
			samples = append(samples, &PatternSample{Timestamp: model.TimeFromUnix(s[0]), Value: s[1]})
		}
		r.Series = append(r.Series, &PatternSeries{Pattern: d.Pattern, Samples: samples})
	}
	return nil
}

func (d DetectedFieldType) String() string {
	return string(d)
}

func (m *ShardsResponse) Merge(other *ShardsResponse) {
	m.Shards = append(m.Shards, other.Shards...)
	m.ChunkGroups = append(m.ChunkGroups, other.ChunkGroups...)
	m.Statistics.Merge(other.Statistics)
}
