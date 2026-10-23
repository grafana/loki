//go:build integration

package integration

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/integration/client"
	"github.com/grafana/loki/v3/integration/cluster"
)

// TestSampleQueryStreamOrderingEquivalence checks, end to end, that a LogQL metric (sample) query
// returns the same result under timestamp-first and stream-first execution when the queried samples
// live in both the ingester (in memory) and the chunk store (flushed) and must be deduplicated
// across those two sources.
func TestSampleQueryStreamOrderingEquivalence(t *testing.T) {
	now := time.Now()
	at := func(minutesAgo int) time.Time { return now.Add(-time.Duration(minutesAgo) * time.Minute) }

	type fixture struct {
		lbls  map[string]string
		lines []string
	}
	fixtures := []fixture{
		{map[string]string{"job": "varlog", "cluster": "prod", "app": "a"}, []string{"a1", "a2", "a3"}},
		{map[string]string{"job": "varlog", "cluster": "prod", "app": "b"}, []string{"b1", "b2"}},
		{map[string]string{"job": "varlog", "cluster": "prod", "app": "c"}, []string{"c1"}},
	}

	// Expected per-series count (one per line).
	expected := map[string]float64{}
	for _, f := range fixtures {
		expected[labels.FromMap(f.lbls).String()] = float64(len(f.lines))
	}

	// Grouping by every label the fixtures use keeps one output series per input stream, so
	// expected above carries over unchanged.
	const query = `sum by (cluster, job, app) (count_over_time({cluster="prod"}[1h]))`

	// runOrdered spins up a fresh single-binary cluster (with stream-first execution optionally
	// enabled), ingests the fixtures so they exist in both the store and the ingester, and returns
	// the per-series counts from query.
	runOrdered := func(streamOrdered bool) map[string]float64 {
		clu := cluster.New(nil, cluster.SchemaWithTSDB, func(c *cluster.Cluster) { c.SetSchemaVer("v13") })
		defer func() { assert.NoError(t, clu.Cleanup()) }()

		// chunks-retain-period keeps flushed chunks in memory, so a flush leaves a copy in both the
		// store and the ingester for the querier to merge and deduplicate. wal-disk-full-threshold=0
		// disables write throttling so the test doesn't depend on the host's free disk.
		flags := []string{"-target=all", "-ingester.chunks-retain-period=1h", "-ingester.wal-disk-full-threshold=0"}
		if streamOrdered {
			flags = append(flags, "-querier.stream-first-execution-enabled=true")
		}
		tAll := clu.AddComponent("all", flags...)
		require.NoError(t, clu.Run())

		cli := client.New(randStringRunes(), "", tAll.HTTPURL())
		cli.Now = now

		// Push, then flush -> each stream is present in both the store and the (retained) ingester,
		// so the querier must merge and deduplicate across the two sources.
		for _, f := range fixtures {
			for i, line := range f.lines {
				require.NoError(t, cli.PushLogLine(line, at(30-i), nil, f.lbls))
			}
		}
		require.NoError(t, cli.Flush())

		resp, err := cli.RunQuery(context.Background(), query)
		require.NoError(t, err)
		require.Equal(t, "vector", resp.Data.ResultType)

		// The query-stats summary must record the ordering the engine actually used: stream-first
		// when the flag is on, timestamp-first otherwise.
		sum := resp.Data.Statistics.Summary
		if streamOrdered {
			require.Positive(t, sum.StreamFirstQueries, "stream-first run must record a stream-first query")
			require.Zero(t, sum.TimestampFirstQueries, "stream-first run must record no timestamp-first query")
		} else {
			require.Positive(t, sum.TimestampFirstQueries, "timestamp-first run must record a timestamp-first query")
			require.Zero(t, sum.StreamFirstQueries, "timestamp-first run must record no stream-first query")
		}

		got := map[string]float64{}
		for _, s := range resp.Data.Vector {
			v, err := strconv.ParseFloat(s.Value, 64)
			require.NoError(t, err)
			got[labels.FromMap(s.Metric).String()] = v
		}
		return got
	}

	byTimestamp := runOrdered(false)
	byStream := runOrdered(true)

	// Each ordering matches the expected deduplicated counts, and therefore each other.
	require.Equal(t, expected, byTimestamp, "timestamp-first counts should match the expected deduplicated result")
	require.Equal(t, expected, byStream, "stream-first counts should match the expected deduplicated result")
}
