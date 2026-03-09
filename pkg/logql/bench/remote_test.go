//go:build remote_correctness

package bench

import (
	"flag"
	"fmt"
	"slices"
	"sort"
	"testing"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/promql"
	"github.com/prometheus/prometheus/promql/parser"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"

	"github.com/grafana/loki/v3/pkg/logcli/client"
	"github.com/grafana/loki/v3/pkg/logqlmodel"
)

var (
	remoteAddr1       = flag.String("addr-1", "", "Address of baseline Loki instance")
	remoteAddr2       = flag.String("addr-2", "", "Address of test Loki instance")
	remoteOrgID       = flag.String("org-id", "", "Tenant org ID")
	remoteUsername    = flag.String("username", "", "HTTP basic auth username")
	remotePassword    = flag.String("password", "", "HTTP basic auth password")
	remoteBearerToken = flag.String("bearer-token", "", "Bearer token for auth")
	remoteMetadataDir = flag.String("metadata-dir", "", "Directory containing dataset_metadata.json")
	remoteTolerance   = flag.Float64("tolerance", 1e-5, "Float comparison tolerance")
	remoteSeed        = flag.Int64("seed", 42, "Random seed for query template resolution")
	remoteRangeType   = flag.String("remote-range-type", "range", "Query range type: instant or range")
	remoteIncludeSkip = flag.Bool("remote-include-skipped", false, "Include skipped queries")
)

// loadRemoteTestCases loads test cases for remote correctness testing
func loadRemoteTestCases(tb testing.TB) []TestCase {
	tb.Helper()

	metadata, err := LoadMetadata(*remoteMetadataDir)
	if err != nil {
		tb.Fatalf("failed to load metadata from %s: %v", *remoteMetadataDir, err)
	}

	registry := NewQueryRegistry("./queries")
	suites := []Suite{SuiteFast, SuiteRegression, SuiteExhaustive}
	if err := registry.Load(suites...); err != nil {
		tb.Fatalf("failed to load query registry: %v", err)
	}

	resolver := NewMetadataVariableResolver(metadata, *remoteSeed)
	isInstant := *remoteRangeType == "instant"
	queryDefs := registry.GetQueries(*remoteIncludeSkip, suites...)

	var cases []TestCase
	for _, def := range queryDefs {
		expanded, err := registry.ExpandQuery(def, resolver, isInstant)
		if err != nil {
			tb.Fatalf("failed to expand query %q: %v", def.Description, err)
		}
		cases = append(cases, expanded...)
	}

	if isInstant {
		filtered := cases[:0]
		for _, tc := range cases {
			if tc.Kind() == "metric" {
				tc.Start = tc.End
				tc.Step = 0
				filtered = append(filtered, tc)
			}
		}
		cases = filtered
	}

	tb.Logf("Loaded %d remote test cases (range-type=%s)", len(cases), *remoteRangeType)
	return cases
}

// queryRemote executes a test case against a remote Loki instance
func queryRemote(t *testing.T, c *client.DefaultClient, tc TestCase) (parser.Value, error) {
	t.Helper()
	const limit = 1000 // matches TestStorageEquality

	if tc.Kind() == "metric" && tc.Start.Equal(tc.End) {
		// Instant query
		resp, err := c.Query(tc.Query, limit, tc.End, tc.Direction, true)
		if err != nil {
			return nil, fmt.Errorf("query failed: %w", err)
		}
		return ConvertResult(resp.Data.Result)
	}

	// Range query (both log and metric)
	resp, err := c.QueryRange(tc.Query, limit, tc.Start, tc.End, tc.Direction, tc.Step, 0, true)
	if err != nil {
		return nil, fmt.Errorf("query failed: %w", err)
	}
	return ConvertResult(resp.Data.Result)
}

// sortVector sorts a promql.Vector by metric labels (primary) then timestamp (secondary)
func sortVector(v promql.Vector) {
	slices.SortFunc(v, func(a, b promql.Sample) int {
		if c := labels.Compare(a.Metric, b.Metric); c != 0 {
			return c
		}
		// tie-break by timestamp
		if a.T < b.T {
			return -1
		}
		if a.T > b.T {
			return 1
		}
		return 0
	})
}

// sortMatrix sorts a promql.Matrix by metric labels
func sortMatrix(m promql.Matrix) {
	slices.SortFunc(m, func(a, b promql.Series) int {
		return labels.Compare(a.Metric, b.Metric)
	})
}

// sortStreams sorts logqlmodel.Streams by label string
func sortStreams(s logqlmodel.Streams) {
	sort.Sort(s)
}

// normalizeResult normalizes ordering of a query result for stable comparison
func normalizeResult(v parser.Value) parser.Value {
	switch val := v.(type) {
	case promql.Vector:
		sortVector(val)
		return val
	case promql.Matrix:
		sortMatrix(val)
		return val
	case logqlmodel.Streams:
		sortStreams(val)
		return val
	default:
		return v // scalars need no sorting
	}
}

// TestRemoteStorageEquality compares query results between two live Loki endpoints.
// It requires --addr-1, --addr-2, and --metadata-dir flags to be set.
func TestRemoteStorageEquality(t *testing.T) {
	if *remoteAddr1 == "" || *remoteAddr2 == "" {
		t.Skip("skipping: --addr-1 and --addr-2 required")
	}
	if *remoteMetadataDir == "" {
		t.Skip("skipping: --metadata-dir required")
	}

	cases := loadRemoteTestCases(t)

	baseline := &client.DefaultClient{
		Address:     *remoteAddr1,
		OrgID:       *remoteOrgID,
		Username:    *remoteUsername,
		Password:    *remotePassword,
		BearerToken: *remoteBearerToken,
	}
	test := &client.DefaultClient{
		Address:     *remoteAddr2,
		OrgID:       *remoteOrgID,
		Username:    *remoteUsername,
		Password:    *remotePassword,
		BearerToken: *remoteBearerToken,
	}

	for _, tc := range cases {
		t.Run(fmt.Sprintf("%s/kind=%s", tc.Source, tc.Kind()), func(t *testing.T) {
			t.Logf("Query: %s", tc.Description())

			var expected, actual parser.Value
			g, _ := errgroup.WithContext(t.Context())

			g.Go(func() error {
				v, err := queryRemote(t, baseline, tc)
				expected = v
				return err
			})
			g.Go(func() error {
				v, err := queryRemote(t, test, tc)
				actual = v
				return err
			})
			require.NoError(t, g.Wait())

			// Normalize ordering before comparison
			expected = normalizeResult(expected)
			actual = normalizeResult(actual)

			assertResultNotEmpty(t, expected, "baseline returned empty")
			assertResultNotEmpty(t, actual, "test endpoint returned empty")
			assertDataEqualWithTolerance(t, expected, actual, *remoteTolerance)
		})
	}
}
