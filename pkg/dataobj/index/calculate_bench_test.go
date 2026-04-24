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

// BenchmarkCalculator_Calculate exercises Calculator end-to-end on a synthetic
// object with enough tenants and logs sections per tenant to stress the
// builderMtx contention inside Calculate's errgroup.
//
//	go test -bench=. -benchtime=10x -run=^$ ./pkg/dataobj/index/...
func BenchmarkCalculator_Calculate(b *testing.B) {
	if runtime.GOMAXPROCS(0) < 2 {
		b.Skip("benchmark requires GOMAXPROCS >= 2 to exercise errgroup contention")
	}

	const (
		tenants          = 4
		streamsPerTenant = 200
		entriesPerStream = 300
	)

	obj, cleanup := buildBenchDataobj(b, tenants, streamsPerTenant, entriesPerStream)
	b.Cleanup(cleanup)

	b.ReportMetric(float64(tenants), "tenants")
	b.ReportMetric(float64(obj.Sections().Count(logs.CheckSection)), "logs_sections")
	b.ReportMetric(float64(obj.Sections().Count(streams.CheckSection)), "streams_sections")

	logger := log.NewNopLogger()
	ctx := context.Background()

	b.ResetTimer()
	b.ReportAllocs()

	for i := range b.N {
		indexBuilder, err := indexobj.NewBuilder(benchCalculatorConfig, scratch.NewMemory())
		require.NoError(b, err)

		calc := NewCalculator(indexBuilder)
		require.NoError(b, calc.Calculate(ctx, logger, obj, fmt.Sprintf("bench/path-%d", i)))

		_, closer, err := calc.Flush()
		require.NoError(b, err)
		_ = closer.Close()
	}
}

var benchCalculatorConfig = logsobj.BuilderBaseConfig{
	TargetPageSize:          128 * 1024,
	TargetObjectSize:        1 << 28, // 256 MiB, large enough for all tenants
	BufferSize:              2 << 20,
	SectionStripeMergeLimit: 2,
	TargetSectionSize:       1, // matches calculate_test.go
}

// buildBenchDataobj builds a synthetic object shaped to produce multiple logs
// sections per tenant (via a small TargetSectionSize) so Calculate's errgroup
// runs enough parallel workers to contend on builderMtx.
func buildBenchDataobj(tb testing.TB, tenants, streamsPerTenant, entriesPerStream int) (*dataobj.Object, func()) {
	tb.Helper()

	builder, err := logsobj.NewBuilder(logsobj.BuilderConfig{
		BuilderBaseConfig: logsobj.BuilderBaseConfig{
			TargetPageSize:          128 * 1024,
			TargetObjectSize:        1 << 30, // 1 GiB ceiling
			TargetSectionSize:       flagext.Bytes(2 << 20),
			BufferSize:              4 << 20,
			SectionStripeMergeLimit: 2,
		},
	}, scratch.NewMemory())
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

	for tenantIdx := range tenants {
		tenantID := fmt.Sprintf("tenant-%d", tenantIdx)
		for streamIdx := range streamsPerTenant {
			lbls := fmt.Sprintf(
				`{cluster=%q,namespace=%q,app=%q,env=%q,pod="pod-%d",stream_id="s-%d-%d"}`,
				clusters[streamIdx%len(clusters)],
				namespaces[streamIdx%len(namespaces)],
				apps[streamIdx%len(apps)],
				envs[streamIdx%len(envs)],
				streamIdx%32,
				tenantIdx, streamIdx,
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

			require.NoError(tb, builder.Append(tenantID, logproto.Stream{Labels: lbls, Entries: entries}))
		}
	}

	obj, closer, err := builder.Flush()
	require.NoError(tb, err)

	// Without multiple logs sections we aren't exercising the errgroup contention.
	require.GreaterOrEqual(tb, obj.Sections().Count(logs.CheckSection), 2, "need multiple logs sections")
	require.Equal(tb, tenants, obj.Sections().Count(streams.CheckSection))

	return obj, func() { _ = closer.Close() }
}
