//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/integration/client"
	"github.com/grafana/loki/v3/integration/cluster"
	"github.com/grafana/loki/v3/pkg/storage/config"
)

// TestLogsVolumeOffsetThroughQueryFrontend sends the sub-queries the Grafana
// Loki datasource builds for the logs volume histogram (count_over_time with
// `offset 1ns`, see grafana-loki-datasource#209) over HTTP to a real
// query-frontend. Unlike the in-process engine tests in pkg/logql, the request
// goes through interval splitting, sharding and, in the cached variant, the
// results cache.
func TestLogsVolumeOffsetThroughQueryFrontend(t *testing.T) {
	for name, extraArgs := range map[string][]string{
		"no_cache": nil,
		// Isolates the sharding middleware from interval splitting.
		"no_sharding": {"-frontend.min-sharding-lookback=87600h"},
		"results_cache": {
			"-querier.cache-results=true",
			"-frontend.embedded-cache.enabled=true",
			"-frontend.embedded-cache.max-size-mb=64",
		},
	} {
		t.Run(name, func(t *testing.T) {
			runLogsVolumeOffsetScenarios(t, extraArgs)
		})
	}
}

func runLogsVolumeOffsetScenarios(t *testing.T, extraArgs []string) {
	clu := cluster.New(nil, cluster.SchemaWithTSDB, func(c *cluster.Cluster) {
		c.SetSchemaVer(fmt.Sprintf("v%d", config.LatestSchemaVersion))
	})
	defer func() {
		assert.NoError(t, clu.Cleanup())
	}()

	args := append([]string{
		"-target=all",
		"-querier.split-queries-by-interval=1h",
		"-frontend.max-cache-freshness=1m",
		// Dev machines with a nearly full disk would otherwise reject pushes.
		"-ingester.wal-disk-full-threshold=0",
	}, extraArgs...)
	tAll := clu.AddComponent("all", args...)
	require.NoError(t, clu.Run())

	tenant := randStringRunes()
	cli := client.New(tenant, "", tAll.HTTPURL())

	// Two hours of data ending at the start of the current hour, so the range
	// crosses one split boundary and stays within the ingesters' window.
	base := time.Now().Truncate(time.Hour).Add(-2 * time.Hour)
	const minutes = 120
	for i := 0; i < minutes; i++ {
		// One line in the middle of each minute, and one exactly on each
		// minute boundary, so a (start, end] window and a [start, end)
		// window attribute the boundary lines to different buckets.
		require.NoError(t, cli.PushLogLine("mid", base.Add(time.Duration(i)*time.Minute+30*time.Second), nil, map[string]string{"job": "volume"}))
		require.NoError(t, cli.PushLogLine("edge", base.Add(time.Duration(i)*time.Minute), nil, map[string]string{"job": "volume"}))
	}

	start, end := base, base.Add(minutes*time.Minute)
	const totalLines = 2 * minutes

	tests := map[string]struct {
		query string
		start time.Time
		end   time.Time
		want  float64
	}{
		// The interior part of a split logs volume query: aligned to the
		// step, spanning the 1h split boundary. The window shifted by 1ns
		// covers [t-1m, t), so every pushed line, including the one on each
		// bucket's lower boundary, is counted exactly once.
		"offset_1ns_aligned_split": {
			query: `sum by (job) (count_over_time({job="volume"}[1m] offset 1ns))`,
			start: start.Add(time.Minute), end: end, want: totalLines,
		},
		// Without the offset the window is (t-1m, t]: the line on the last
		// boundary was never pushed, so the final bucket only sees "mid".
		"no_offset_aligned_split": {
			query: `sum by (job) (count_over_time({job="volume"}[1m]))`,
			start: start.Add(time.Minute), end: end, want: totalLines - 1,
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			// Run twice: the second request is served from the results cache
			// when it is enabled.
			for _, run := range []string{"first", "repeat"} {
				got, series, raw := queryRangeTotal(t, tAll.HTTPURL(), tenant, tc.query, tc.start, tc.end, time.Minute)
				require.NotZero(t, series, "%s: query returned no series: %s", run, raw)
				require.Equal(t, tc.want, got, "%s: unexpected total: %s", run, raw)
			}
		})
	}
}

// queryRangeTotal runs a range query with an explicit step and returns the sum
// of all sample values, the number of series, and the raw response body.
func queryRangeTotal(t *testing.T, baseURL, tenant, query string, start, end time.Time, step time.Duration) (float64, int, string) {
	t.Helper()
	v := url.Values{}
	v.Set("query", query)
	v.Set("start", client.FormatTS(start))
	v.Set("end", client.FormatTS(end))
	v.Set("step", strconv.FormatInt(step.Milliseconds(), 10)+"ms")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/loki/api/v1/query_range?"+v.Encode(), nil)
	require.NoError(t, err)
	req.Header.Set("X-Scope-OrgID", tenant)
	res, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, res.StatusCode, string(body))

	var parsed struct {
		Data struct {
			Result []struct {
				Values [][]interface{} `json:"values"`
			} `json:"result"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(body, &parsed))
	var total float64
	for _, series := range parsed.Data.Result {
		for _, sample := range series.Values {
			value, err := strconv.ParseFloat(sample[1].(string), 64)
			require.NoError(t, err)
			total += value
		}
	}
	return total, len(parsed.Data.Result), string(body)
}
