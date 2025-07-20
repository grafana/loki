package logproto

import (
	"fmt"
	"slices"
	"sort"
	"strings"
	"sync/atomic" //lint:ignore faillint we can't use go.uber.org/atomic with a protobuf struct without wrapping it.
	"time"

	"github.com/cespare/xxhash/v2"
	"github.com/dustin/go-humanize"
	jsoniter "github.com/json-iterator/go"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/index"
	"github.com/grafana/loki/v3/pkg/util/constants"
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
		Labels: make([]SeriesIdentifier_LabelsEntry, 0, in.Len()),
	}
	in.Range(func(l labels.Label) {
		id.Labels = append(id.Labels, SeriesIdentifier_LabelsEntry{Key: l.Name, Value: l.Value})
	})
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
			Level   string    `json:"level"`
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
		r.Series = append(r.Series, &PatternSeries{Pattern: d.Pattern, Level: d.Level, Samples: samples})
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

func (m *QueryPatternsRequest) GetSampleQuery() (string, error) {
	expr, err := syntax.ParseExpr(m.Query)
	if err != nil {
		return "", err
	}

	// Extract matchers from the expression
	var matchers []*labels.Matcher
	switch e := expr.(type) {
	case *syntax.MatchersExpr:
		matchers = e.Mts
	case syntax.LogSelectorExpr:
		matchers = e.Matchers()
	default:
		// Cannot extract matchers
		return "", nil
	}

	// Find service_name from matchers
	var serviceName string
	var serviceMatcher labels.MatchType
	for i, m := range matchers {
		if m.Name == "service_name" {
			matchers = slices.Delete(matchers, i, i+1)
			serviceName = m.Value
			serviceMatcher = m.Type
			break
		}
	}

	if serviceName == "" {
		serviceName = ".+"
		serviceMatcher = labels.MatchRegexp
	}

	// Build LogQL query for persisted patterns
	logqlQuery := buildPatternLogQLQuery(serviceName, serviceMatcher, matchers, m.Step)

	return logqlQuery, nil
}

func buildPatternLogQLQuery(serviceName string, serviceMatcher labels.MatchType, matchers []*labels.Matcher, step int64) string {
	// Use step duration for sum_over_time window
	stepDuration := max(time.Duration(step)*time.Millisecond, 10*time.Second)

	if len(matchers) == 0 {
		return buildPatterLogQLQueryString(serviceName, serviceMatcher.String(), "", stepDuration.String())
	}

	stringBuilder := strings.Builder{}
	for i, matcher := range matchers {
		stringBuilder.WriteString(matcher.String())
		if i < len(matchers)-1 {
			stringBuilder.WriteString(" | ")
		}
	}

	return buildPatterLogQLQueryString(serviceName, serviceMatcher.String(), stringBuilder.String(), stepDuration.String())
}

func buildPatterLogQLQueryString(serviceName, serviceMatcher, matchers, step string) string {
	decodePatternTransform := `label_format decoded_pattern=` + "`{{urldecode .detected_pattern}}`"

	matchAndTransform := ""
	if matchers == "" {
		matchAndTransform = decodePatternTransform
	} else {
		matchAndTransform = fmt.Sprintf(`%s | %s`, matchers, decodePatternTransform)

	}
	return fmt.Sprintf(
		`sum by (decoded_pattern, %s) (sum_over_time({__pattern__%s"%s"} | logfmt | %s | unwrap count [%s]))`,
		constants.LevelLabel,
		serviceMatcher,
		serviceName,
		matchAndTransform,
		step,
	)
}
