package bench

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/dustin/go-humanize"
	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/user"
	"github.com/prometheus/prometheus/promql"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql"
	"github.com/grafana/loki/v3/pkg/logqlmodel"
)

var (
	slowTests     = flag.Bool("slow-tests", false, "run slow tests")
	rangeType     = flag.String("range-type", "range", "range type to use for test cases")
	rangeInterval = flag.String("range-interval", "1h", "interval to use for range aggregations")
)

const testTenant = "test-tenant"

const (
	StoreDataObj         = "dataobj"
	StoreDataObjV2Engine = "dataobj-engine"
	StoreChunk           = "chunk"
)

var allStores = []string{StoreDataObj, StoreDataObjV2Engine, StoreChunk}

//go:generate go run ./cmd/generate/main.go -size 2147483648 -dir ./data -tenant test-tenant

// setupBenchmarkWithStore sets up the benchmark environment with the specified store type
// and returns the necessary components
func setupBenchmarkWithStore(tb testing.TB, storeType string) logql.Engine {
	tb.Helper()
	entries, err := os.ReadDir(DefaultDataDir)
	if err != nil || len(entries) == 0 {
		tb.Fatal("Data directory is empty or does not exist. Please run 'go generate ./...' first to generate test data")
	}

	var querier logql.Querier
	switch storeType {
	case StoreDataObjV2Engine:
		store, err := NewDataObjV2EngineStore(DefaultDataDir, testTenant)
		if err != nil {
			tb.Fatal(err)
		}

		return store.engine
	case StoreDataObj:
		store, err := NewDataObjStore(DefaultDataDir, testTenant)
		if err != nil {
			tb.Fatal(err)
		}
		querier, err = store.Querier()
		if err != nil {
			tb.Fatal(err)
		}
	case StoreChunk:
		store, err := NewChunkStore(DefaultDataDir, testTenant)
		if err != nil {
			tb.Fatal(err)
		}
		querier, err = store.Querier()
		if err != nil {
			tb.Fatal(err)
		}
	default:
		tb.Fatalf("Unknown store type: %s", storeType)
	}

	engine := logql.NewEngine(logql.EngineOpts{}, querier, logql.NoLimits,
		level.NewFilter(log.NewLogfmtLogger(os.Stdout), level.AllowWarn()))

	return engine
}

// TestStorageEquality ensures that for each test case, all known storages
// return the same query result.
func TestStorageEquality(t *testing.T) {
	ctx := user.InjectOrgID(t.Context(), testTenant)

	if !*slowTests {
		t.Skip("test skipped because -slow-tests flag is not set")
	}

	type store struct {
		Name   string
		Cases  []TestCase
		Engine logql.Engine
	}

	generateStore := func(name string) *store {
		engine := setupBenchmarkWithStore(t, name)
		// Load and validate the generator config
		config, err := LoadConfig(DefaultDataDir)
		if err != nil {
			t.Fatal(err)
		}

		cases := NewTestCaseGenerator(TestCaseGeneratorConfig{RangeType: *rangeType}, config).Generate()

		return &store{
			Name:   name,
			Cases:  cases,
			Engine: engine,
		}
	}

	// Generate a list of stores. The chunks store (as the most stable) acts as
	// the baseline.
	var (
		stores    []*store
		baseStore *store
	)
	for _, name := range allStores {
		store := generateStore(name)
		stores = append(stores, store)

		if name == StoreChunk {
			baseStore = store
		}
	}
	if len(stores) < 2 {
		t.Skipf("not enough stores to compare; need at least 2, got %d", len(stores))
	}

	for _, baseCase := range baseStore.Cases {
		for _, store := range stores {
			if store == baseStore {
				continue
			}

			t.Run(fmt.Sprintf("query=%s/kind=%s/store=%s", baseCase.Name(), baseCase.Kind(), store.Name), func(t *testing.T) {
				defer func() {
					if t.Failed() {
						t.Logf("Re-run just this test with -test.run='%s'", testNameRegex(t.Name()))
					}
				}()

				t.Logf("Query information:\n%s", baseCase.Description())

				params, err := logql.NewLiteralParams(
					baseCase.Query,
					baseCase.Start,
					baseCase.End,
					baseCase.Step,
					0,
					baseCase.Direction,
					1000,
					nil,
					nil,
				)
				require.NoError(t, err)

				expected, err := baseStore.Engine.Query(params).Exec(ctx)
				require.NoError(t, err)

				t.Logf(`Summary stats: store=%s lines_processed=%d, entries_returned=%d, bytes_processed=%s, execution_time_in_secs=%d, bytes_processed_per_sec=%s`,
					baseStore.Name,
					expected.Statistics.Summary.TotalLinesProcessed,
					expected.Statistics.Summary.TotalEntriesReturned,
					humanize.Bytes(uint64(expected.Statistics.Summary.TotalBytesProcessed)),
					uint64(expected.Statistics.Summary.ExecTime),
					humanize.Bytes(uint64(expected.Statistics.Summary.BytesProcessedPerSecond)),
				)

				// Find matching test case in other stores and then compare results.
				idx := slices.IndexFunc(store.Cases, func(tc TestCase) bool {
					return tc == baseCase
				})
				if idx == -1 {
					t.Logf("Store %s missing test case %s", store.Name, baseCase.Name())
					return
				}

				actual, err := store.Engine.Query(params).Exec(ctx)
				if err != nil && errors.Is(err, errStoreUnimplemented) {
					t.Logf("Store %s does not implement test case %s", store.Name, baseCase.Name())
					return
				} else if assert.NoError(t, err) {
					t.Logf(`Summary stats: store=%s lines_processed=%d, entries_returned=%d, bytes_processed=%s, execution_time_in_secs=%d, bytes_processed_per_sec=%s`,
						store.Name,
						actual.Statistics.Summary.TotalLinesProcessed,
						actual.Statistics.Summary.TotalEntriesReturned,
						humanize.Bytes(uint64(actual.Statistics.Summary.TotalBytesProcessed)),
						uint64(actual.Statistics.Summary.ExecTime),
						humanize.Bytes(uint64(actual.Statistics.Summary.BytesProcessedPerSecond)),
					)

					assert.Equal(t, expected.Data, actual.Data, "store %q results do not match base store %q", store.Name, baseStore.Name)
				}
			})
		}
	}
}

// testNameRegex converts the test name into an argument that can be passed to
// -test.run.
func testNameRegex(name string) string {
	// -test.run accepts a sequence of regexes separated by '/'. To pass a
	// literal test name, we need to escape the regex characters in the name.
	var newParts []string
	for part := range strings.SplitSeq(name, "/") {
		newParts = append(newParts, regexp.QuoteMeta(part))
	}
	return strings.Join(newParts, "/")
}

func TestLogQLQueries(t *testing.T) {
	// We keep this test for debugging even though it's too slow for now.
	t.Skip("Too slow for now.")
	store := StoreDataObjV2Engine
	engine := setupBenchmarkWithStore(t, store)
	// Load and validate the generator config
	config, err := LoadConfig(DefaultDataDir)
	if err != nil {
		t.Fatal(err)
	}

	ctx := user.InjectOrgID(context.Background(), testTenant)

	// Generate test cases
	cases := NewTestCaseGenerator(
		TestCaseGeneratorConfig{RangeType: *rangeType, RangeInterval: *rangeInterval},
		config,
	).Generate()

	// Log all unique queries
	uniqueQueries := make(map[string]struct{})

	// NB: for testing purposes, we can filter out the metric queries
	// var logOnlyCases []TestCase
	// for _, c := range cases {
	// 	if c.Kind() == "log" {
	// 		logOnlyCases = append(logOnlyCases, c)
	// 	}
	// }

	for _, c := range cases {
		t.Run(fmt.Sprintf("query=%s/kind=%s/store=%s", c.Name(), c.Kind(), store), func(t *testing.T) {

			// Uncomment this to run only log queries
			// if c.Kind() != "log" {
			// 	continue
			// }
			if _, exists := uniqueQueries[c.Query]; exists {
				t.Skip("skipping duplicate query: " + c.Query)
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
			require.Equal(t, logqlmodel.ValueTypeStreams, string(res.Data.Type()))
			xs := res.Data.(logqlmodel.Streams)
			require.Greater(t, len(xs), 0, "no streams returned")

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
		})
	}
}

func BenchmarkLogQL(b *testing.B) {
	// Run benchmarks for both storage types
	for _, storeType := range allStores {
		engine := setupBenchmarkWithStore(b, storeType)
		// Load and validate the generator config
		config, err := LoadConfig(DefaultDataDir)
		if err != nil {
			b.Fatal(err)
		}

		// Generate test cases using the loaded config
		cases := NewTestCaseGenerator(TestCaseGeneratorConfig{RangeType: *rangeType}, config).Generate()

		for _, c := range cases {
			b.Run(fmt.Sprintf("query=%s/kind=%s/store=%s", c.Name(), c.Kind(), storeType), func(b *testing.B) {
				ctx := user.InjectOrgID(context.Background(), testTenant)
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

				b.ReportAllocs()
				b.ResetTimer()

				for i := 0; i < b.N; i++ {
					ctx, cancel := context.WithTimeout(ctx, time.Minute)
					defer cancel()
					r, err := q.Exec(ctx)
					require.NoError(b, err)
					b.ReportMetric(float64(r.Statistics.Summary.TotalLinesProcessed), "linesProcessed")
					b.ReportMetric(float64(r.Statistics.Summary.TotalPostFilterLines), "postFilterLines")
					b.ReportMetric(float64(r.Statistics.Summary.TotalBytesProcessed)/1024, "kilobytesProcessed")
				}
			})
		}
	}
}

func TestPrintBenchmarkQueries(t *testing.T) {
	cases := NewTestCaseGenerator(TestCaseGeneratorConfig{RangeType: *rangeType}, &defaultGeneratorConfig).Generate()

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
