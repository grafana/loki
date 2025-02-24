package bench

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/user"
	"github.com/prometheus/prometheus/promql"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql"
)

const testTenant = "test-tenant"

//go:generate go run ./cmd/generate/main.go -size 2147483648 -dir ./data -tenant test-tenant

// setupBenchmark sets up the benchmark environment and returns the necessary components
func setupBenchmark(tb testing.TB) (*logql.Engine, *GeneratorConfig) {
	tb.Helper()
	entries, err := os.ReadDir(DefaultDataDir)
	if err != nil || len(entries) == 0 {
		tb.Fatal("Data directory is empty or does not exist. Please run 'go generate ./...' first to generate test data")
	}

	store, err := NewDataObjStore(DefaultDataDir, testTenant)
	if err != nil {
		tb.Fatal(err)
	}

	// Load and validate the generator config
	config, err := LoadConfig(DefaultDataDir)
	if err != nil {
		tb.Fatal(err)
	}

	querier, err := store.Querier()
	if err != nil {
		tb.Fatal(err)
	}

	engine := logql.NewEngine(logql.EngineOpts{}, querier, logql.NoLimits,
		level.NewFilter(log.NewLogfmtLogger(os.Stdout), level.AllowWarn()))

	return engine, config
}

func TestLogQLQueries(t *testing.T) {
	// We keep this test for debugging even though it's too slow for now.
	t.Skip("Too slow for now.")
	engine, config := setupBenchmark(t)
	ctx := user.InjectOrgID(context.Background(), testTenant)

	// Generate test cases
	cases := config.GenerateTestCases()

	// Log all unique queries
	uniqueQueries := make(map[string]struct{})
	for _, c := range cases {
		if _, exists := uniqueQueries[c.Query]; exists {
			continue
		}
		uniqueQueries[c.Query] = struct{}{}

		t.Log(c.Description())
		params, err := logql.NewLiteralParams(
			c.Query,
			c.Start,
			c.Start.Add(5*time.Minute),
			1*time.Minute,
			0,
			c.Direction,
			1000,
			nil,
			nil,
		)
		require.NoError(t, err)

		q := engine.Query(params)
		res, err := q.Exec(ctx)
		require.NoError(t, err)
		if testing.Verbose() {
			// Log the result type and some basic stats
			t.Logf("Result Type: %s", res.Data.Type())
			switch v := res.Data.(type) {
			case promql.Vector:
				t.Logf("Number of Samples: %d", len(v))
				if len(v) > 0 {
					t.Logf("First Sample: %+v", v[0])
				}
			case promql.Matrix:
				t.Logf("Number of Series: %d", len(v))
				if len(v) > 0 {
					t.Logf("First Series: %+v", v[0])
				}
			}
			t.Log("----------------------------------------")
		}
	}
}

func BenchmarkLogQL(b *testing.B) {
	engine, config := setupBenchmark(b)
	ctx := user.InjectOrgID(context.Background(), testTenant)

	// Generate test cases using the loaded config
	cases := config.GenerateTestCases()

	for _, c := range cases {
		b.Run(c.Name(), func(b *testing.B) {
			params, err := logql.NewLiteralParams(
				c.Query,
				c.Start,
				c.End,
				c.Step,
				0,
				c.Direction,
				1000,
				nil,
				nil,
			)
			require.NoError(b, err)

			q := engine.Query(params)

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_, err := q.Exec(ctx)
				require.NoError(b, err)
			}
		})
	}
}

func TestPrintBenchmarkQueries(t *testing.T) {
	_, config := setupBenchmark(t)
	cases := config.GenerateTestCases()

	t.Log("Benchmark Queries:")
	t.Log("================")

	var logQueries, metricQueries int
	for _, c := range cases {
		// Count query types
		if strings.Contains(c.Query, "rate(") || strings.Contains(c.Query, "sum(") {
			metricQueries++
		} else {
			if c.Direction == logproto.FORWARD {
				logQueries++
			}
		}

		t.Log(c.Description())
		t.Log("----------------------------------------")
	}

	t.Logf("\nSummary:")
	t.Logf("- Log queries: %d (will run in both directions)", logQueries)
	t.Logf("- Metric queries: %d (forward only)", metricQueries)
	t.Logf("- Total benchmark cases: %d", len(cases))
}

func TestChunkStore(t *testing.T) {
	chunkStore, err := NewChunkStore(DefaultDataDir, testTenant)
	require.NoError(t, err)

	// Create builder with default options and the store
	builder := NewBuilder(DefaultDataDir, DefaultOpt(), chunkStore)

	// Generate the data
	ctx := context.Background()
	if err := builder.Generate(ctx, 2147483648); err != nil {
		t.Fatalf("Failed to generate dataset: %v\n", err)
	}
}
