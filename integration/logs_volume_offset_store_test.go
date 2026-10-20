//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/integration/client"
	"github.com/grafana/loki/v3/integration/cluster"
	"github.com/grafana/loki/v3/pkg/storage/config"
)

// TestLogsVolumeOffsetMicroServices reproduces the logs volume histogram
// queries (count_over_time with `offset 1ns`) against a multi-component
// cluster, with the data first held only in the ingester, then only in the
// store (flushed chunks + shipped TSDB index), and finally in both. The
// single-binary test in logs_volume_offset_test.go only ever queried
// in-ingester data; the production reports associate the empty result with
// the chunk/store path, so this test forces the data through it.
//
// Two independent symptoms are asserted separately for every query:
//   - row presence: the set of series and per-bucket values must match the
//     ground truth computed from the pushed timestamps, and
//   - bucket timestamps: every returned sample must sit exactly on a step.
//
// The sharded variant forces the frontend to split the query into many TSDB
// shards. The frontend merges shard results by looking samples up at the
// exact step timestamps, so a sample mislabeled by the offset handling turns
// into an empty result there rather than a wrong label.
//
// This is the regression test for the range vector sample timestamp rounding
// fix. Without it (at the fix's parent commit) every sub-millisecond offset
// case fails in every phase: the querier returns all rows with timestamps 1ms
// before the step, and the query-frontend, sharded or not, returns no series
// at all because it discards samples that are not on the step grid.
func TestLogsVolumeOffsetMicroServices(t *testing.T) {
	for name, variant := range map[string]volumeVariant{
		"unsharded": {},
		"sharded": {
			frontendFlags: []string{"-querier.tsdb-max-bytes-per-shard=1KB"},
			wantSharded:   true,
		},
	} {
		t.Run(name, func(t *testing.T) {
			runLogsVolumeOffsetMicroServices(t, variant)
		})
	}
}

type volumeVariant struct {
	frontendFlags []string
	wantSharded   bool
}

func runLogsVolumeOffsetMicroServices(t *testing.T, variant volumeVariant) {
	clu := cluster.New(nil, cluster.SchemaWithTSDB, func(c *cluster.Cluster) {
		c.SetSchemaVer(fmt.Sprintf("v%d", config.LatestSchemaVersion))
	})
	defer func() {
		assert.NoError(t, clu.Cleanup())
	}()

	var (
		// A long compaction interval keeps the compactor from rewriting the
		// freshly shipped TSDB index while the test queries it.
		tCompactor = clu.AddComponent(
			"compactor",
			"-target=compactor",
			"-compactor.compaction-interval=1h",
		)
		tIndexGateway = clu.AddComponent(
			"index-gateway",
			"-target=index-gateway",
			"-tsdb.shipper.resync-interval=250ms",
		)
		tDistributor = clu.AddComponent(
			"distributor",
			"-target=distributor",
		)
	)
	require.NoError(t, clu.Run())

	var (
		tIngester = clu.AddComponent(
			"ingester",
			"-target=ingester",
			"-tsdb.shipper.index-gateway-client.server-address="+tIndexGateway.GRPCURL(),
			// Keep flushed chunks in memory so the merged phase really has
			// the data on both paths.
			"-ingester.chunks-retain-period=1h",
			// Dev machines with a nearly full disk would otherwise reject pushes.
			"-ingester.wal-disk-full-threshold=0",
		)
		tQueryScheduler = clu.AddComponent(
			"query-scheduler",
			"-target=query-scheduler",
			"-query-scheduler.use-scheduler-ring=false",
			"-tsdb.shipper.index-gateway-client.server-address="+tIndexGateway.GRPCURL(),
		)
	)
	require.NoError(t, clu.Run())

	tQuerier := clu.AddComponent(
		"querier",
		"-target=querier",
		"-querier.scheduler-address="+tQueryScheduler.GRPCURL(),
		"-tsdb.shipper.resync-interval=250ms",
		"-tsdb.shipper.index-gateway-client.server-address="+tIndexGateway.GRPCURL(),
		"-common.compactor-address="+tCompactor.HTTPURL(),
	)
	require.NoError(t, clu.Run())

	tQueryFrontend := clu.AddComponent(
		"query-frontend",
		append([]string{
			"-target=query-frontend",
			"-frontend.scheduler-address=" + tQueryScheduler.GRPCURL(),
			"-tsdb.shipper.index-gateway-client.server-address=" + tIndexGateway.GRPCURL(),
			"-common.compactor-address=" + tCompactor.HTTPURL(),
			"-frontend.encoding=protobuf",
		}, variant.frontendFlags...)...,
	)
	require.NoError(t, clu.Run())

	tenant := randStringRunes()
	cliDistributor := client.New(tenant, "", tDistributor.HTTPURL())

	// Ten one-minute buckets ending two minutes ago, kept inside a single hour
	// so the frontend does not split the query (the production report had
	// splits=0, shards=1).
	const buckets = 10
	base := time.Now().Truncate(time.Minute).Add(-(buckets + 2) * time.Minute)
	if base.Truncate(time.Hour) != base.Add(buckets*time.Minute).Truncate(time.Hour) {
		base = base.Add(-15 * time.Minute)
	}
	start, end := base.Add(time.Minute), base.Add(buckets*time.Minute)
	const step = time.Minute

	// Per bucket, lines sit exactly on the minute boundary, at boundary±1ns,
	// and at other nanosecond-precision positions inside the minute.
	positions := []time.Duration{
		0,
		time.Nanosecond,
		time.Millisecond,
		7*time.Second + 123456789*time.Nanosecond,
		30 * time.Second,
		30*time.Second + time.Nanosecond,
		time.Minute - time.Millisecond,
		time.Minute - time.Microsecond,
		time.Minute - time.Nanosecond,
	}
	levels := []string{"info", "warn", "error"}

	var entries []volumeEntry
	for i := 0; i < buckets; i++ {
		for _, level := range levels {
			for j, pos := range positions {
				e := volumeEntry{
					ts:    base.Add(time.Duration(i)*time.Minute + pos),
					lbls:  map[string]string{"job": "volume", "level": level},
					line:  fmt.Sprintf("level=%s bucket=%02d pos=%d ts=%d", level, i, j, base.Add(time.Duration(i)*time.Minute+pos).UnixNano()),
					level: level,
				}
				// Exercise structured metadata on two of the three streams.
				if level != "info" {
					e.sm = map[string]string{"traceID": fmt.Sprintf("%x", e.ts.UnixNano())}
				}
				require.NoError(t, cliDistributor.PushLogLine(e.line, e.ts, e.sm, e.lbls))
				entries = append(entries, e)
			}
		}
	}
	t.Logf("pushed %d lines across %d buckets, query range [%s, %s] step %s", len(entries), buckets, start.Format(time.RFC3339Nano), end.Format(time.RFC3339Nano), step)

	selector := `{job="volume"}`
	queries := []volumeQuery{
		{name: "count_by_level", agg: aggCount, byLevel: true},
		{name: "count_by_level_offset_1ns", agg: aggCount, byLevel: true, offset: time.Nanosecond},
		{name: "count_by_level_offset_1us", agg: aggCount, byLevel: true, offset: time.Microsecond},
		{name: "count_by_level_offset_1ms", agg: aggCount, byLevel: true, offset: time.Millisecond},
		{name: "count_by_level_offset_1s", agg: aggCount, byLevel: true, offset: time.Second},
		{name: "count_by_level_offset_60s", agg: aggCount, byLevel: true, offset: time.Minute},
		{name: "count_no_sum_offset_1ns", agg: aggCount, offset: time.Nanosecond},
		{name: "rate_by_level_offset_1ns", agg: aggRate, byLevel: true, offset: time.Nanosecond},
		{name: "bytes_by_level_offset_1ns", agg: aggBytes, byLevel: true, offset: time.Nanosecond},
	}
	for i := range queries {
		queries[i].logql = queries[i].build(selector)
	}

	endpoints := func() map[string]string {
		return map[string]string{
			"frontend": tQueryFrontend.HTTPURL(),
			"querier":  tQuerier.HTTPURL(),
		}
	}

	runMatrix := func(t *testing.T, phase string, checkStats func(*testing.T, *client.Response)) {
		for via, baseURL := range endpoints() {
			for _, q := range queries {
				t.Run(via+"/"+q.name, func(t *testing.T) {
					resp := queryRangeNoCache(t, baseURL, tenant, q.logql, start, end, step)
					logVolumeStats(t, phase, via, q.name, resp)
					checkStats(t, resp)
					if variant.wantSharded && via == "frontend" {
						require.Greater(t, resp.Data.Statistics.Summary.Shards, int64(1), "frontend should have sharded the query")
					}
					assertVolumeResult(t, entries, q, start, end, step, resp)
				})
			}
		}
	}

	// Phase 1: everything is still in the ingester; nothing has been flushed.
	t.Run("ingester_only", func(t *testing.T) {
		runMatrix(t, "ingester_only", func(t *testing.T, resp *client.Response) {
			st := resp.Data.Statistics
			require.Positive(t, st.Ingester.TotalReached, "query must have reached the ingester")
			require.Zero(t, st.Querier.Store.TotalChunksRef, "no chunks should be in the store yet")
		})
	})

	// Phase 2: flush the tenant (chunks + index shipped), then read only from
	// the store. The ingester still holds the chunks, so the querier must be
	// told to ignore it.
	t.Run("store_only", func(t *testing.T) {
		cliIngester := client.New(tenant, "", tIngester.HTTPURL())
		require.NoError(t, cliIngester.FlushTenant(""))

		tQuerier.AddFlags("-querier.query-store-only=true")
		require.NoError(t, tQuerier.Restart())

		baseline := queries[0]
		wantTotal := totalOf(expectedVolume(entries, baseline, start, end, step))
		require.Eventually(t, func() bool {
			resp, err := tryQueryRangeNoCache(tQueryFrontend.HTTPURL(), tenant, baseline.logql, start, end, step)
			if err != nil {
				return false
			}
			return resp.Data.Statistics.Querier.Store.TotalChunksRef > 0 && totalOf(matrixValues(resp)) == wantTotal
		}, 60*time.Second, 500*time.Millisecond, "flushed chunks should become queryable from the store")

		runMatrix(t, "store_only", func(t *testing.T, resp *client.Response) {
			st := resp.Data.Statistics
			require.Zero(t, st.Ingester.TotalReached, "store-only querier must not reach the ingester")
			require.Positive(t, st.Querier.Store.TotalChunksRef, "query must have referenced store chunks")
		})
	})

	// Phase 3: both paths hold the data; the querier merges and deduplicates.
	t.Run("ingester_and_store", func(t *testing.T) {
		tQuerier.AddFlags("-querier.query-store-only=false")
		require.NoError(t, tQuerier.Restart())

		runMatrix(t, "ingester_and_store", func(t *testing.T, resp *client.Response) {
			st := resp.Data.Statistics
			require.Positive(t, st.Ingester.TotalReached, "query must have reached the ingester")
			require.Positive(t, st.Querier.Store.TotalChunksRef, "query must have referenced store chunks")
		})
	})
}

type volumeEntry struct {
	ts    time.Time
	lbls  map[string]string
	sm    map[string]string
	line  string
	level string
}

type volumeAgg int

const (
	aggCount volumeAgg = iota
	aggRate
	aggBytes
)

type volumeQuery struct {
	name    string
	agg     volumeAgg
	byLevel bool
	offset  time.Duration
	logql   string
}

func (q volumeQuery) build(selector string) string {
	fn := map[volumeAgg]string{aggCount: "count_over_time", aggRate: "rate", aggBytes: "bytes_over_time"}[q.agg]
	rng := selector + "[1m]"
	if q.offset != 0 {
		rng += " offset " + q.offset.String()
	}
	inner := fmt.Sprintf("%s(%s)", fn, rng)
	if q.byLevel {
		return fmt.Sprintf("sum by (level) (%s)", inner)
	}
	return inner
}

// expectedVolume evaluates the query by hand over the pushed entries: at each
// step t the range window is (t-1m-offset, t-offset]. Buckets with no matching
// line produce no sample, mirroring the engine.
func expectedVolume(entries []volumeEntry, q volumeQuery, start, end time.Time, step time.Duration) map[string]map[int64]float64 {
	out := map[string]map[int64]float64{}
	for t := start; !t.After(end); t = t.Add(step) {
		hi := t.Add(-q.offset)
		lo := hi.Add(-time.Minute)
		for _, e := range entries {
			if !e.ts.After(lo) || e.ts.After(hi) {
				continue
			}
			var key string
			if q.byLevel {
				key = labels.FromStrings("level", e.level).String()
			} else {
				// Without an aggregation the engine promotes structured
				// metadata to output labels, so each traceID is its own series.
				all := make(map[string]string, len(e.lbls)+len(e.sm))
				for k, v := range e.lbls {
					all[k] = v
				}
				for k, v := range e.sm {
					all[k] = v
				}
				key = labels.FromMap(all).String()
			}
			if out[key] == nil {
				out[key] = map[int64]float64{}
			}
			switch q.agg {
			case aggCount:
				out[key][t.UnixMilli()]++
			case aggRate:
				out[key][t.UnixMilli()] += 1.0 / 60
			case aggBytes:
				out[key][t.UnixMilli()] += float64(len(e.line))
			}
		}
	}
	return out
}

// matrixValues flattens a matrix response into series -> step(ms) -> value.
// Step timestamps are rounded to the millisecond so a sample mislabeled by
// 1ms shows up as a missing/extra bucket rather than being silently absorbed.
func matrixValues(resp *client.Response) map[string]map[int64]float64 {
	out := map[string]map[int64]float64{}
	for _, s := range resp.Data.Matrix {
		key := labels.FromMap(s.Metric).String()
		if out[key] == nil {
			out[key] = map[int64]float64{}
		}
		for _, sample := range s.Values {
			tsSec := sample[0].(float64)
			v, err := strconv.ParseFloat(sample[1].(string), 64)
			if err != nil {
				panic(err)
			}
			out[key][int64(math.Round(tsSec*1000))] += v
		}
	}
	return out
}

func totalOf(m map[string]map[int64]float64) float64 {
	var total float64
	for _, series := range m {
		for _, v := range series {
			total += v
		}
	}
	return total
}

// assertVolumeResult checks row presence (series set, bucket set, values) and
// bucket timestamps (every returned sample sits exactly on a step) separately
// so the failure message says which symptom was hit.
func assertVolumeResult(t *testing.T, entries []volumeEntry, q volumeQuery, start, end time.Time, step time.Duration, resp *client.Response) {
	t.Helper()
	require.Equal(t, "matrix", resp.Data.ResultType)
	want := expectedVolume(entries, q, start, end, step)
	got := matrixValues(resp)

	// Symptom 1: zero rows / missing series.
	require.NotEmpty(t, resp.Data.Matrix, "%s returned no series (expected %d series, %.0f total)", q.logql, len(want), totalOf(want))
	require.ElementsMatch(t, keysOf(want), keysOf(got), "%s: series mismatch", q.logql)

	// Symptom 2: bucket timestamps off the step grid.
	for _, s := range resp.Data.Matrix {
		for _, sample := range s.Values {
			tsMs := int64(math.Round(sample[0].(float64) * 1000))
			require.Zero(t, (tsMs-start.UnixMilli())%step.Milliseconds(),
				"%s: sample timestamp %d is not on the step grid starting at %d", q.logql, tsMs, start.UnixMilli())
			require.False(t, tsMs < start.UnixMilli() || tsMs > end.UnixMilli(),
				"%s: sample timestamp %d outside [%d, %d]", q.logql, tsMs, start.UnixMilli(), end.UnixMilli())
		}
	}

	// Per-bucket values against ground truth.
	for key, wantSeries := range want {
		gotSeries := got[key]
		require.ElementsMatch(t, int64KeysOf(wantSeries), int64KeysOf(gotSeries), "%s: bucket set mismatch for %s", q.logql, key)
		for ts, wantV := range wantSeries {
			require.InDelta(t, wantV, gotSeries[ts], 1e-9, "%s: value mismatch for %s at %d", q.logql, key, ts)
		}
	}
}

func keysOf(m map[string]map[int64]float64) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func int64KeysOf(m map[int64]float64) []int64 {
	keys := make([]int64, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	return keys
}

// logVolumeStats prints the per-path statistics the production investigation
// relied on, so a failure can be attributed to the ingester, store or merge.
func logVolumeStats(t *testing.T, phase, via, name string, resp *client.Response) {
	t.Helper()
	st := resp.Data.Statistics
	t.Logf("[%s/%s/%s] series=%d ingester{reached=%d linesSent=%d headChunkLines=%d postFilterLines=%d} store{chunksRef=%d chunksDownloaded=%d decompressedLines=%d postFilterLines=%d} summary{entriesReturned=%d postFilterLines=%d splits=%d shards=%d}",
		phase, via, name, len(resp.Data.Matrix),
		st.Ingester.TotalReached, st.Ingester.TotalLinesSent, st.Ingester.Store.Chunk.HeadChunkLines, st.Ingester.Store.Chunk.PostFilterLines,
		st.Querier.Store.TotalChunksRef, st.Querier.Store.TotalChunksDownloaded, st.Querier.Store.Chunk.DecompressedLines, st.Querier.Store.Chunk.PostFilterLines,
		st.Summary.TotalEntriesReturned, st.Summary.TotalPostFilterLines, st.Summary.Splits, st.Summary.Shards)
}

// queryRangeNoCache runs a range query with an explicit step and
// Cache-Control: no-cache, like the production reproduction.
func queryRangeNoCache(t *testing.T, baseURL, tenant, query string, start, end time.Time, step time.Duration) *client.Response {
	t.Helper()
	resp, err := tryQueryRangeNoCache(baseURL, tenant, query, start, end, step)
	require.NoError(t, err)
	return resp
}

func tryQueryRangeNoCache(baseURL, tenant, query string, start, end time.Time, step time.Duration) (*client.Response, error) {
	v := url.Values{}
	v.Set("query", query)
	v.Set("start", client.FormatTS(start))
	v.Set("end", client.FormatTS(end))
	v.Set("step", strconv.FormatInt(step.Milliseconds(), 10)+"ms")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/loki/api/v1/query_range?"+v.Encode(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Scope-OrgID", tenant)
	req.Header.Set("Cache-Control", "no-cache")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d: %s", res.StatusCode, strings.TrimSpace(string(body)))
	}
	var resp client.Response
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("decoding response: %w: %s", err, body)
	}
	return &resp, nil
}
