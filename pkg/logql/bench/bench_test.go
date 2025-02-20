package bench

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/user"
	"github.com/prometheus/prometheus/promql"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql"
	"github.com/stretchr/testify/require"
)

//go:generate go run ./cmd/generate/main.go -size 1073741824 -dir ./data -tenant test-tenant

type labelMatcher struct {
	name, value string
}

func buildLabelSelector(matchers []labelMatcher) string {
	var parts []string
	for _, m := range matchers {
		parts = append(parts, fmt.Sprintf(`%s="%s"`, m.name, m.value))
	}
	return "{" + strings.Join(parts, ", ") + "}"
}

// TestCase represents a LogQL test case for benchmarking and testing
type TestCase struct {
	Query     string
	Start     time.Time
	End       time.Time
	Direction logproto.Direction
}

// Name returns a descriptive name for the test case.
// For log queries, it includes the direction.
// For metric queries (rate, sum), it returns just the query as they are always forward.
func (c TestCase) Name() string {
	if strings.Contains(c.Query, "rate(") || strings.Contains(c.Query, "sum(") {
		return c.Query
	}
	return fmt.Sprintf("%s [%v]", c.Query, c.Direction)
}

// Description returns a detailed description of the test case including time range
func (c TestCase) Description() string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("Query: %s\n", c.Query))
	b.WriteString(fmt.Sprintf("Time Range: %v to %v\n", c.Start.Format(time.RFC3339), c.End.Format(time.RFC3339)))
	b.WriteString(fmt.Sprintf("Direction: %v", c.Direction))
	return b.String()
}

func generateLabelCombinations(config *GeneratorConfig) [][]labelMatcher {
	rnd := config.NewRand()

	// Helper to create a matcher with random value
	randomMatcher := func(name string, values []string) labelMatcher {
		return labelMatcher{name: name, value: values[rnd.Intn(len(values))]}
	}

	combinations := [][]labelMatcher{
		// Single label matchers
		{randomMatcher("component", config.LabelConfig.Components)},
		{randomMatcher("env", config.LabelConfig.EnvTypes)},
		{randomMatcher("region", config.LabelConfig.Regions)},
		{randomMatcher("datacenter", config.LabelConfig.Datacenters)},

		// Two label combinations
		{
			randomMatcher("component", config.LabelConfig.Components),
			randomMatcher("env", config.LabelConfig.EnvTypes),
		},
		{
			randomMatcher("region", config.LabelConfig.Regions),
			randomMatcher("env", config.LabelConfig.EnvTypes),
		},

		// Three label combinations
		{
			randomMatcher("component", config.LabelConfig.Components),
			randomMatcher("env", config.LabelConfig.EnvTypes),
			randomMatcher("region", config.LabelConfig.Regions),
		},
	}

	return combinations
}

// GenerateTestCases creates a sorted list of test cases using the provided configuration
func GenerateTestCases(config *GeneratorConfig) []TestCase {
	var cases []TestCase
	labelCombos := generateLabelCombinations(config)
	end := config.StartTime.Add(config.TimeSpread)

	// Use a map to track unique queries
	uniqueQueries := make(map[string]struct{})

	// Helper to add both forward and backward variants if query is unique
	addBidirectional := func(query string, start, end time.Time) {
		if _, exists := uniqueQueries[query]; exists {
			return // Skip duplicate queries
		}
		uniqueQueries[query] = struct{}{}
		cases = append(cases,
			TestCase{
				Query:     query,
				Start:     start,
				End:       end,
				Direction: logproto.FORWARD,
			},
			TestCase{
				Query:     query,
				Start:     start,
				End:       end,
				Direction: logproto.BACKWARD,
			},
		)
	}

	// Helper to add metric query if unique
	addMetricQuery := func(query string, start, end time.Time) {
		if _, exists := uniqueQueries[query]; exists {
			return // Skip duplicate queries
		}
		uniqueQueries[query] = struct{}{}
		cases = append(cases,
			TestCase{
				Query:     query,
				Start:     start,
				End:       end,
				Direction: logproto.FORWARD,
			},
		)
	}

	// Basic label selector queries with line filters
	for _, combo := range labelCombos {
		selector := buildLabelSelector(combo)

		// Basic selector
		addBidirectional(selector, config.StartTime, end)

		// With line filter
		addBidirectional(selector+` |= "error"`, config.StartTime, end)

		// With regex filter
		addBidirectional(selector+` |~ "error|warn"`, config.StartTime, end)

		// With JSON parsing and field filter
		addBidirectional(selector+` | json | duration > 100`, config.StartTime, end)

		// With logfmt parsing
		addBidirectional(selector+` | logfmt | level="error"`, config.StartTime, end)

		// Metric queries
		rateQuery := fmt.Sprintf(`rate(%s[5m])`, selector)
		addMetricQuery(rateQuery, config.StartTime.Add(5*time.Minute), end)

		// Aggregations
		if len(combo) > 1 {
			sumQuery := fmt.Sprintf(`sum by (%s) (rate(%s[5m]))`, combo[0].name, selector)
			addMetricQuery(sumQuery, config.StartTime.Add(5*time.Minute), end)
		}
	}

	// Dense period queries
	for _, interval := range config.DenseIntervals {
		combo := labelCombos[config.NewRand().Intn(len(labelCombos))]
		selector := buildLabelSelector(combo)
		addBidirectional(
			selector,
			interval.Start,
			interval.Start.Add(interval.Duration),
		)
	}

	// Sort test cases by name for consistent ordering
	sort.Slice(cases, func(i, j int) bool {
		return cases[i].Name() < cases[j].Name()
	})

	return cases
}

// setupBenchmark sets up the benchmark environment and returns the necessary components
func setupBenchmark(tb testing.TB) (*logql.Engine, *GeneratorConfig) {
	tb.Helper()
	dataDir := "./data"
	entries, err := os.ReadDir(dataDir)
	if err != nil || len(entries) == 0 {
		tb.Fatal("Data directory is empty or does not exist. Please run 'go generate ./...' first to generate test data")
	}

	store, err := NewDataObjStore(dataDir, "test-tenant")
	if err != nil {
		tb.Fatal(err)
	}

	// Load and validate the generator config
	config, err := LoadConfig(dataDir)
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
	engine, config := setupBenchmark(t)
	ctx := user.InjectOrgID(context.Background(), "test-tenant")

	// Generate test cases
	cases := GenerateTestCases(config)

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
			c.End,
			0,
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
		t.Logf("Stats: %+v\n", res.Statistics)
		t.Log("----------------------------------------")
	}
}

func BenchmarkLogQL(b *testing.B) {
	engine, config := setupBenchmark(b)
	ctx := user.InjectOrgID(context.Background(), "test-tenant")

	// Generate test cases using the loaded config
	cases := GenerateTestCases(config)

	for _, c := range cases {
		b.Run(c.Name(), func(b *testing.B) {
			params, err := logql.NewLiteralParams(
				c.Query,
				c.Start,
				c.End,
				0,
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
				res, err := q.Exec(ctx)
				require.NoError(b, err)
				if testing.Verbose() {
					b.Log(res.Data.String())
				}
			}
		})
	}
}

func TestPrintBenchmarkQueries(t *testing.T) {
	_, config := setupBenchmark(t)
	cases := GenerateTestCases(config)

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
