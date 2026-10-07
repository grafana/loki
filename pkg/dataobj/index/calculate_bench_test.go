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
	"github.com/grafana/loki/v3/pkg/dataobj/index/indexobj"
	"github.com/grafana/loki/v3/pkg/dataobj/logsobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/logs"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/scratch"

	"github.com/grafana/loki/pkg/push"
)

// BenchmarkCalculator_Calculate exercises Calculator end-to-end on a synthetic
// object with enough logs sections to stress the builderMtx contention inside
// Calculate's errgroup.
//
//	go test -bench=. -benchtime=10x -run=^$ ./pkg/dataobj/index/...
func BenchmarkCalculator_Calculate(b *testing.B) {
	if runtime.GOMAXPROCS(0) < 2 {
		b.Skip("benchmark requires GOMAXPROCS >= 2 to exercise errgroup contention")
	}

	const (
		streamCount      = 800
		entriesPerStream = 300
	)

	obj, cleanup := buildBenchDataobj(b, streamCount, entriesPerStream)
	b.Cleanup(cleanup)

	b.ReportMetric(float64(obj.Sections().Count(logs.CheckSection)), "logs_sections")
	b.ReportMetric(float64(obj.Sections().Count(streams.CheckSection)), "streams_sections")

	logger := log.NewNopLogger()
	ctx := context.Background()

	b.ResetTimer()
	b.ReportAllocs()

	for i := range b.N {
		indexBuilder, err := indexobj.NewBuilder(benchCalculatorConfig, scratch.NewMemory(), indexobj.NewBuilderMetrics(nil))
		require.NoError(b, err)

		calc := NewCalculator(indexBuilder, NewCalculatorMetrics(nil))
		require.NoError(b, calc.Calculate(ctx, logger, obj, fmt.Sprintf("bench/path-%d", i)))

		_, closer, _, err := calc.Flush()
		require.NoError(b, err)
		_ = closer.Close()
	}
}

const benchTenant = "bench-tenant"

var benchCalculatorConfig = logsobj.BuilderBaseConfig{
	TargetPageSize:   128 * 1024,
	TargetObjectSize: 1 << 28, // 256 MiB, large enough for the whole object
	BufferSize:       2 << 20,
	// TargetSectionSize is set to 1 byte so the index builder rolls a new
	// section as soon as anything is written. This forces many small index
	// sections, which exercises the parallel flush path in Calculate (each
	// section flush contends on builderMtx) and is what makes this benchmark
	// sensitive to lock-contention regressions. Matches calculate_test.go.
	SectionStripeMergeLimit: 2,
	TargetSectionSize:       1,
}

// buildBenchDataobj builds a synthetic object for [benchTenant] shaped to
// produce multiple logs sections (via a small TargetSectionSize) so
// Calculate's errgroup runs enough parallel workers to contend on builderMtx.
func buildBenchDataobj(tb testing.TB, streamCount, entriesPerStream int) (*dataobj.Object, func()) {
	tb.Helper()

	builder, err := logsobj.NewBuilder(logsobj.BuilderBaseConfig{
		TargetPageSize:          128 * 1024,
		TargetObjectSize:        1 << 30, // 1 GiB ceiling
		TargetSectionSize:       flagext.Bytes(2 << 20),
		BufferSize:              4 << 20,
		SectionStripeMergeLimit: 2,
	}, scratch.NewMemory(), logsobj.NewBuilderMetrics(), log.NewNopLogger(), nil)
	require.NoError(tb, err)

	// Deterministic so iteration-to-iteration variance is only from scheduling.
	rng := rand.New(rand.NewSource(0xC0FFEE))
	clusters := []string{"prod1", "prod2", "prod3", "prod4"}
	namespaces := []string{"prod-ns", "ops-ns", "ingress", "observability", "test-billing", "platform"}
	apps := []string{"distributor", "ingester", "querier", "compactor", "gateway", "frontend", "indexer", "ruler"}
	envs := []string{"prod", "staging"}
	const hex = "0123456789abcdef"

	lineFiller := make([]byte, 96)
	for i := range lineFiller {
		lineFiller[i] = byte('a' + rng.Intn(26))
	}
	baseTime := time.Unix(1_700_000_000, 0).UTC()

	randStr := func(n int) string {
		out := make([]byte, n)
		for i := range out {
			out[i] = hex[rng.Intn(len(hex))]
		}
		return string(out)
	}

	for streamIdx := range streamCount {
		lbls := fmt.Sprintf(
			`{cluster=%q,namespace=%q,app=%q,env=%q,pod="pod-%d",stream_id="s-%d"}`,
			clusters[streamIdx%len(clusters)],
			namespaces[streamIdx%len(namespaces)],
			apps[streamIdx%len(apps)],
			envs[streamIdx%len(envs)],
			streamIdx%32,
			streamIdx,
		)

		entries := make([]push.Entry, entriesPerStream)
		for entryIdx := range entries {
			entries[entryIdx] = push.Entry{
				Timestamp: baseTime.Add(time.Duration(streamIdx*entriesPerStream+entryIdx) * time.Millisecond),
				Line:      string(lineFiller) + fmt.Sprintf(" req=%d stream=%d", entryIdx, streamIdx),
				StructuredMetadata: push.LabelsAdapter{
					{Name: "trace_id", Value: randStr(16)},
					{Name: "span_id", Value: randStr(8)},
					{Name: "user_id", Value: fmt.Sprintf("u-%d", rng.Intn(10000))},
				},
			}
		}

		require.NoError(tb, builder.Append(benchTenant, logproto.Stream{Labels: lbls, Entries: entries}, entries[0].Timestamp))
	}

	obj, closer, err := builder.Flush()
	require.NoError(tb, err)

	// Without multiple logs sections we aren't exercising the errgroup contention.
	require.GreaterOrEqual(tb, obj.Sections().Count(logs.CheckSection), 2, "need multiple logs sections")
	require.Equal(tb, 1, obj.Sections().Count(streams.CheckSection))

	return obj, func() { _ = closer.Close() }
}
