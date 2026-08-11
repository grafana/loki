package pattern

import (
	"bufio"
	"context"
	"os"
	"runtime"
	"strconv"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/pattern/aggregation"
	"github.com/grafana/loki/v3/pkg/pattern/drain"
	"github.com/grafana/loki/v3/pkg/util/constants"
)

// These benchmarks measure the memory behaviour of the pattern ingester above
// the Drain level: the per-entry ingest path (stream.Push, instance.Observe),
// the periodic sweep (stream.prune) and the steady-state heap held per stream.
//
// Allocation-rate benchmarks report per-entry numbers, so allocs/op reads as
// "allocations per ingested log line". Retained-heap benchmarks report only
// custom metrics; their ns/op includes forced GCs and is not meaningful.
//
// Compare before/after with benchstat, e.g.
//
//	go test ./pkg/pattern/ -run XXX -bench 'Memory' -count 6 > /tmp/before.txt
//	# apply change
//	go test ./pkg/pattern/ -run XXX -bench 'Memory' -count 6 > /tmp/after.txt
//	benchstat /tmp/before.txt /tmp/after.txt

const benchCorpus = "drain/testdata/kubernetes.txt"

var benchLabels = labels.FromStrings(
	"cluster", "prod-us-central-0",
	"namespace", "loki-ops",
	"pod", "querier-5f7b9c8d4-abcde",
	"service_name", "querier",
)

// retainSink keeps the last batch of benchmark objects reachable so that heap
// profiles taken at the end of a run attribute the retained memory.
var retainSink any

type nopEntryWriter struct{ entries int }

func (w *nopEntryWriter) WriteEntry(_ time.Time, _ string, _ labels.Labels, _ []logproto.LabelAdapter) {
	w.entries++
}

func (w *nopEntryWriter) Stop() {}

func benchLines(tb testing.TB) []string {
	tb.Helper()

	file, err := os.Open(benchCorpus)
	require.NoError(tb, err)
	defer file.Close()

	var lines []string
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	require.NoError(tb, scanner.Err())
	require.NotEmpty(tb, lines)
	return lines
}

func newBenchStream(tb testing.TB, writer aggregation.EntryWriter, format string) *stream {
	tb.Helper()

	s, err := newStream(
		model.Fingerprint(labels.StableHash(benchLabels)),
		benchLabels,
		newIngesterMetrics(nil, "bench"),
		log.NewNopLogger(),
		format,
		"tenant",
		drain.DefaultConfig(),
		&fakeLimits{patternPersistenceEnabled: true, persistenceGranularity: time.Hour},
		writer,
		aggregation.NewMetrics(nil),
		0.99,
	)
	require.NoError(tb, err)
	return s
}

// liveHeap returns the bytes reachable from the program after a full GC cycle.
func liveHeap() uint64 {
	var ms runtime.MemStats
	runtime.GC()
	runtime.GC()
	runtime.ReadMemStats(&ms)
	return ms.HeapAlloc
}

// BenchmarkStreamMemory_Push measures the per-entry cost of stream.Push. The
// with_level variant carries a detected_level structured metadata label, which
// is the shape the distributor tees; the no_level variant shows the cost of the
// structured metadata handling alone.
func BenchmarkStreamMemory_Push(b *testing.B) {
	lines := benchLines(b)
	base := time.Now()

	for _, tc := range []struct {
		name     string
		metadata []logproto.LabelAdapter
	}{
		{name: "no_metadata"},
		{name: "with_level", metadata: []logproto.LabelAdapter{
			{Name: constants.LevelLabel, Value: constants.LogLevelInfo},
		}},
		{name: "level_plus_metadata", metadata: []logproto.LabelAdapter{
			{Name: constants.LevelLabel, Value: constants.LogLevelInfo},
			{Name: "trace_id", Value: "3a2b1c0d4e5f6789"},
			{Name: "span_id", Value: "0f1e2d3c"},
		}},
	} {
		b.Run(tc.name, func(b *testing.B) {
			s := newBenchStream(b, nil, drain.DetectLogFormat(lines[0]))
			ctx := context.Background()

			// Reused single-entry batch so the harness itself allocates nothing.
			batch := make([]logproto.Entry, 1)
			batch[0].StructuredMetadata = tc.metadata

			push := func(i int) {
				batch[0].Timestamp = base.Add(time.Duration(i) * time.Millisecond)
				batch[0].Line = lines[i%len(lines)]
				_ = s.Push(ctx, batch)
			}

			for i := 0; i < len(lines); i++ { // warm up: create the clusters
				push(i)
			}

			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				push(len(lines) + i)
			}
		})
	}
}

// BenchmarkInstanceMemory_Observe measures the metric aggregation path, which
// every teed entry goes through regardless of stream ownership. B/op and
// allocs/op are per Observe call (one per stream per push request); the
// B/entry and allocs/entry metrics normalize by entry so the fixed per-call
// cost can be told apart from the per-entry cost.
func BenchmarkInstanceMemory_Observe(b *testing.B) {
	lines := benchLines(b)

	for _, entriesPerCall := range []int{1, 32} {
		b.Run("entries="+strconv.Itoa(entriesPerCall), func(b *testing.B) {
			inst, err := newInstance(
				"tenant",
				log.NewNopLogger(),
				newIngesterMetrics(nil, "bench"),
				drain.DefaultConfig(),
				&fakeLimits{metricAggregationEnabled: true},
				nil, // ring client is not used by Observe
				"ingester-0",
				&nopEntryWriter{},
				&nopEntryWriter{},
				aggregation.NewMetrics(nil),
				0.99,
			)
			require.NoError(b, err)

			ctx := context.Background()
			lbls := benchLabels.String()
			now := time.Now()
			batch := make([]logproto.Entry, entriesPerCall)
			for i := range batch {
				batch[i] = logproto.Entry{
					Timestamp: now.Add(time.Duration(i) * time.Millisecond),
					Line:      lines[i%len(lines)],
					StructuredMetadata: []logproto.LabelAdapter{
						{Name: constants.LevelLabel, Value: constants.LogLevelInfo},
					},
				}
			}

			b.ReportAllocs()
			b.ResetTimer()

			var before, after runtime.MemStats
			runtime.ReadMemStats(&before)
			for i := 0; i < b.N; i++ {
				inst.Observe(ctx, lbls, batch)
			}
			runtime.ReadMemStats(&after)
			b.StopTimer()

			entries := float64(b.N * entriesPerCall)
			b.ReportMetric(float64(after.Mallocs-before.Mallocs)/entries, "allocs/entry")
			b.ReportMetric(float64(after.TotalAlloc-before.TotalAlloc)/entries, "B/entry")
		})
	}
}

// BenchmarkStreamMemory_PruneNoop measures one sweep over a stream whose data is
// all still within the retention window: nothing is pruned and nothing is
// written, so this is the fixed cost the flush loop pays per stream per
// FlushCheckPeriod. It walks every Drain's prefix tree and materializes its
// cluster list, so it is sensitive to how often Clusters() is called.
func BenchmarkStreamMemory_PruneNoop(b *testing.B) {
	lines := benchLines(b)
	writer := &nopEntryWriter{}
	s := newBenchStream(b, writer, drain.DetectLogFormat(lines[0]))

	ctx := context.Background()
	batch := make([]logproto.Entry, 1)
	batch[0].StructuredMetadata = []logproto.LabelAdapter{
		{Name: constants.LevelLabel, Value: constants.LogLevelInfo},
	}
	now := time.Now()
	for i, line := range lines {
		batch[0].Timestamp = now.Add(time.Duration(i) * time.Millisecond)
		batch[0].Line = line
		require.NoError(b, s.Push(ctx, batch))
	}

	clusters := 0
	for _, p := range s.patterns {
		clusters += len(p.Clusters())
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s.prune(24 * time.Hour)
	}
	b.StopTimer()

	b.ReportMetric(float64(clusters), "clusters")
	require.Zero(b, writer.entries, "sanity: nothing should have been persisted")
}

// BenchmarkStreamMemory_WritePatternsBucketed measures the persistence path for
// a single cluster's pruned samples: bucketing by persistence granularity and
// building the pattern entry that gets pushed back to Loki.
func BenchmarkStreamMemory_WritePatternsBucketed(b *testing.B) {
	for _, sampleCount := range []int{6, 60, 360} {
		b.Run("samples="+strconv.Itoa(sampleCount), func(b *testing.B) {
			writer := &nopEntryWriter{}
			s := newBenchStream(b, writer, drain.FormatLogfmt)

			start := model.TimeFromUnixNano(time.Now().Add(-2 * time.Hour).UnixNano())
			samples := make([]*logproto.PatternSample, 0, sampleCount)
			for i := 0; i < sampleCount; i++ {
				samples = append(samples, &logproto.PatternSample{
					Timestamp: start.Add(time.Duration(i) * 10 * time.Second),
					Value:     int64(1 + i%7),
				})
			}
			pattern := `ts=<_> level=info caller=<_> msg="<_>" duration=<_>`

			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				s.writePatternsBucketed(samples, benchLabels, pattern, constants.LogLevelInfo)
			}
		})
	}
}

// BenchmarkStreamMemory_PatternEntry isolates the log line that gets pushed back
// to Loki for one persisted pattern. It is the bulk of the per-entry cost inside
// writePatternsBucketed, and it grows with the number of stream labels because
// aggregation.internalEntry appends one fmt.Sprintf per label.
func BenchmarkStreamMemory_PatternEntry(b *testing.B) {
	pattern := `ts=<_> level=info caller=<_> msg="<_>" duration=<_>`
	now := time.Now()

	for _, tc := range []struct {
		name string
		lbls labels.Labels
	}{
		{name: "labels=4", lbls: benchLabels},
		{name: "labels=12", lbls: labels.FromStrings(
			"cluster", "prod-us-central-0", "namespace", "loki-ops",
			"pod", "querier-5f7b9c8d4-abcde", "service_name", "querier",
			"container", "querier", "job", "loki-ops/querier",
			"instance", "10.128.4.17:3100", "zone", "us-central1-b",
			"region", "us-central1", "env", "prod",
			"team", "logs", "component", "querier",
		)},
	} {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				_ = aggregation.PatternEntry(now, 42, pattern, tc.lbls)
			}
		})
	}
}

// BenchmarkStreamMemory_Retained reports the steady-state heap held per stream.
// A stream holds one Drain per log level in constants.LogLevels, so the
// single_level case shows the floor paid even when only one level is observed
// and the all_levels case shows the ceiling.
func BenchmarkStreamMemory_Retained(b *testing.B) {
	const streamsPerOp = 8

	for _, tc := range []struct {
		name   string
		levels []string
	}{
		{name: "single_level", levels: []string{constants.LogLevelInfo}},
		{name: "all_levels", levels: constants.LogLevels},
	} {
		b.Run(tc.name, func(b *testing.B) {
			lines := benchLines(b)
			format := drain.DetectLogFormat(lines[0])
			ctx := context.Background()

			var totalRetained, totalClusters uint64
			for i := 0; i < b.N; i++ {
				base := liveHeap()

				streams := make([]*stream, 0, streamsPerOp)
				for j := 0; j < streamsPerOp; j++ {
					s := newBenchStream(b, nil, format)
					batch := make([]logproto.Entry, 1)
					now := time.Now()
					for k, line := range lines {
						batch[0].Timestamp = now.Add(time.Duration(k) * time.Millisecond)
						batch[0].Line = line
						batch[0].StructuredMetadata = []logproto.LabelAdapter{
							{Name: constants.LevelLabel, Value: tc.levels[k%len(tc.levels)]},
						}
						require.NoError(b, s.Push(ctx, batch))
					}
					streams = append(streams, s)
				}

				totalRetained += liveHeap() - base
				for _, s := range streams {
					for _, p := range s.patterns {
						totalClusters += uint64(len(p.Clusters()))
					}
				}
				runtime.KeepAlive(streams)
				// Keep the last batch reachable so that -memprofile attributes
				// the retained heap by allocation site:
				//   go test ./pkg/pattern/ -run XXX -bench Retained/single_level \
				//     -benchtime 3x -memprofile /tmp/inuse.pb.gz
				//   go tool pprof -inuse_space -top /tmp/inuse.pb.gz
				retainSink = streams
			}

			perOp := float64(b.N * streamsPerOp)
			b.ReportMetric(float64(totalRetained)/perOp, "retained_B/stream")
			b.ReportMetric(float64(totalClusters)/perOp, "clusters/stream")
		})
	}
}
