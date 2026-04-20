package index

import (
	"context"
	"fmt"
	"math/rand"
	"runtime"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/flagext"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/consumer/logsobj"
	"github.com/grafana/loki/v3/pkg/dataobj/index/indexobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/logs"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/scratch"

	"github.com/grafana/loki/pkg/push"
)

// benchmarkShape captures the synthetic workload knobs for the Calculator
// benchmark. The defaults are tuned to surface mutex contention on
// indexobjBuilder during Calculate: multiple tenants (so per-tenant locks
// could parallelize) and multiple logs sections per tenant (so the errgroup
// in Calculate actually runs workers in parallel).
type benchmarkShape struct {
	tenants                int
	streamsPerTenant       int
	entriesPerStream       int
	logLineBytes           int
	structuredMetadataKeys int
	// Controls how many logs sections we expect per tenant. Smaller
	// TargetSectionSize => more sections. See
	// pkg/dataobj/consumer/logsobj/builder.go:324 where the inner builder
	// flushes a new logs section once UncompressedSize exceeds this.
	targetSectionSize int
}

// defaultBenchmarkShape produces ~240K rows across 4 tenants (200 streams x
// 300 entries), with TargetSectionSize tuned so each tenant emits multiple
// logs sections. Probed output: 4 streams sections + ~8 logs sections = 12
// parallel units for the errgroup in Calculate. That comfortably exceeds
// typical GOMAXPROCS and exercises the builderMtx contention we want to
// measure, while keeping per-op wall time in the ~200-800ms range.
func defaultBenchmarkShape() benchmarkShape {
	return benchmarkShape{
		tenants:                4,
		streamsPerTenant:       200,
		entriesPerStream:       300,
		logLineBytes:           96,
		structuredMetadataKeys: 3,
		targetSectionSize:      2 << 20, // 2 MiB per logs section
	}
}

// BenchmarkCalculator_Calculate benchmarks the index Calculator end-to-end on
// a synthetic dataobj.Object with enough tenants, sections, and rows to
// exercise the contention on Calculator.builderMtx.
//
// Run with real parallelism, e.g.:
//
//	go test -bench=. -benchtime=10x -run=^$ ./pkg/dataobj/index/...
//
// The benchmark itself does NOT call b.RunParallel; the parallelism under
// test lives inside Calculate via errgroup + GOMAXPROCS.
func BenchmarkCalculator_Calculate(b *testing.B) {
	if runtime.GOMAXPROCS(0) < 2 {
		b.Skip("benchmark requires GOMAXPROCS >= 2 to exercise errgroup contention")
	}

	shape := defaultBenchmarkShape()

	// Build the source dataobj.Object once and reuse its bytes across iterations.
	// dataobj.Object is read-only for the Calculator, so reuse is safe and
	// avoids dominating the benchmark with setup time.
	obj, cleanup := buildBenchDataobj(b, shape)
	b.Cleanup(cleanup)

	b.ReportMetric(float64(shape.tenants), "tenants")
	b.ReportMetric(float64(obj.Sections().Count(logs.CheckSection)), "logs_sections")
	b.ReportMetric(float64(obj.Sections().Count(streams.CheckSection)), "streams_sections")

	logger := log.NewNopLogger()
	ctx := context.Background()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		// Each iteration uses a fresh Calculator so the benchmark measures a
		// full Calculate run on an empty builder, matching the production
		// shape where one Calculator handles one source dataobj before flush.
		indexBuilder, err := indexobj.NewBuilder(benchCalculatorConfig, scratch.NewMemory())
		if err != nil {
			b.Fatal(err)
		}
		calc := NewCalculator(indexBuilder)

		if err := calc.Calculate(ctx, logger, obj, fmt.Sprintf("bench/path-%d", i)); err != nil {
			b.Fatalf("Calculate failed: %v", err)
		}

		// Flush and discard; we want to include the full end-to-end work the
		// real caller does, but keep the measurement focused on Calculate.
		_, closer, err := calc.Flush()
		if err != nil {
			b.Fatalf("Flush failed: %v", err)
		}
		_ = closer.Close()
	}
}

// benchCalculatorConfig mirrors testCalculatorConfig in calculate_test.go but
// with a slightly larger target object size so the indexobj builder doesn't
// return ErrBuilderFull during a single benchmark iteration.
var benchCalculatorConfig = logsobj.BuilderBaseConfig{
	TargetPageSize:          128 * 1024,
	TargetObjectSize:        1 << 28, // 256 MiB, large enough for all tenants
	BufferSize:              2 << 20, // 2 MiB
	SectionStripeMergeLimit: 2,
	TargetSectionSize:       1, // force one section per append, matching calculate_test.go
}

// buildBenchDataobj constructs a synthetic *dataobj.Object shaped to exercise
// the Calculator's parallel section processing + single builder mutex.
//
// The object has `shape.tenants` streams sections (one per tenant, produced
// by logsobj.Builder for each tenant's data) and N logs sections per tenant
// where N depends on total uncompressed log bytes vs TargetSectionSize. The
// Calculator's errgroup runs one goroutine per section, so with N > GOMAXPROCS
// every goroutine will contend on builderMtx for every batch write.
func buildBenchDataobj(tb testing.TB, shape benchmarkShape) (*dataobj.Object, func()) {
	tb.Helper()

	cfg := logsobj.BuilderConfig{
		BuilderBaseConfig: logsobj.BuilderBaseConfig{
			TargetPageSize: 128 * 1024,
			// Keep TargetObjectSize large so the builder doesn't return
			// ErrBuilderFull mid-construction. We are deliberately producing
			// a big-ish object to mimic the ~480 MiB production shape
			// (scaled down for runtime).
			TargetObjectSize:        1 << 30, // 1 GiB ceiling
			TargetSectionSize:       flagext.Bytes(shape.targetSectionSize),
			BufferSize:              4 << 20, // 4 MiB
			SectionStripeMergeLimit: 2,
		},
	}

	builder, err := logsobj.NewBuilder(cfg, scratch.NewMemory())
	require.NoError(tb, err)

	// Deterministic generator so iteration-to-iteration variance is only
	// from scheduling, not label/line content.
	rng := rand.New(rand.NewSource(0xC0FFEE))

	// A handful of realistic label names with varied values — bloom filter /
	// postings calculation should do meaningful work.
	clusters := []string{"prod-us-east-1", "prod-us-west-2", "prod-eu-west-1", "prod-ap-south-1"}
	namespaces := []string{"loki-prod", "loki-ops", "ingress", "observability", "billing", "platform"}
	apps := []string{"distributor", "ingester", "querier", "compactor", "gateway", "frontend", "indexer", "ruler"}
	envs := []string{"prod", "staging"}
	pods := make([]string, 32)
	for i := range pods {
		pods[i] = fmt.Sprintf("pod-%d", i)
	}

	traceIDChars := "0123456789abcdef"
	lineFiller := make([]byte, shape.logLineBytes)
	for i := range lineFiller {
		lineFiller[i] = byte('a' + rng.Intn(26))
	}

	baseTime := time.Unix(1_700_000_000, 0).UTC()

	for tenantIdx := 0; tenantIdx < shape.tenants; tenantIdx++ {
		tenantID := fmt.Sprintf("tenant-%d", tenantIdx)

		for streamIdx := 0; streamIdx < shape.streamsPerTenant; streamIdx++ {
			lbls := fmt.Sprintf(
				`{cluster=%q,namespace=%q,app=%q,env=%q,pod=%q,stream_id=%q}`,
				clusters[streamIdx%len(clusters)],
				namespaces[streamIdx%len(namespaces)],
				apps[streamIdx%len(apps)],
				envs[streamIdx%len(envs)],
				pods[streamIdx%len(pods)],
				fmt.Sprintf("s-%d-%d", tenantIdx, streamIdx),
			)

			entries := make([]push.Entry, shape.entriesPerStream)
			for entryIdx := 0; entryIdx < shape.entriesPerStream; entryIdx++ {
				ts := baseTime.Add(time.Duration(streamIdx*shape.entriesPerStream+entryIdx) * time.Millisecond)

				// Rotate a small pool of line bodies so compression isn't
				// trivial but setup stays deterministic.
				line := string(lineFiller) + fmt.Sprintf(" req=%d stream=%d", entryIdx, streamIdx)

				md := make(push.LabelsAdapter, 0, shape.structuredMetadataKeys)
				if shape.structuredMetadataKeys > 0 {
					md = append(md, push.LabelAdapter{Name: "trace_id", Value: randomHex(rng, traceIDChars, 16)})
				}
				if shape.structuredMetadataKeys > 1 {
					md = append(md, push.LabelAdapter{Name: "span_id", Value: randomHex(rng, traceIDChars, 8)})
				}
				if shape.structuredMetadataKeys > 2 {
					md = append(md, push.LabelAdapter{Name: "user_id", Value: fmt.Sprintf("u-%d", rng.Intn(10000))})
				}

				entries[entryIdx] = push.Entry{
					Timestamp:          ts,
					Line:               line,
					StructuredMetadata: md,
				}
			}

			stream := logproto.Stream{Labels: lbls, Entries: entries}
			if err := builder.Append(tenantID, stream); err != nil {
				tb.Fatalf("builder.Append tenant=%s stream=%d: %v", tenantID, streamIdx, err)
			}
		}
	}

	obj, closer, err := builder.Flush()
	require.NoError(tb, err)

	// Sanity: we must actually have multiple sections or the benchmark is
	// not exercising the contention we care about.
	logsSections := obj.Sections().Count(logs.CheckSection)
	streamsSections := obj.Sections().Count(streams.CheckSection)
	if logsSections < 2 {
		tb.Fatalf("expected >= 2 logs sections to exercise errgroup parallelism, got %d", logsSections)
	}
	if streamsSections != shape.tenants {
		tb.Fatalf("expected %d streams sections (one per tenant), got %d", shape.tenants, streamsSections)
	}

	return obj, func() { _ = closer.Close() }
}

// randomHex returns a fixed-length string built from the provided alphabet.
func randomHex(rng *rand.Rand, alphabet string, n int) string {
	out := make([]byte, n)
	for i := range out {
		out[i] = alphabet[rng.Intn(len(alphabet))]
	}
	return string(out)
}
