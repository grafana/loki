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
//
// It also queries two log streams whose labels collide on StableHash. Those two are kept
// ingester-only on purpose: once flushed, the fp-mapper remaps one of them, so its store chunk
// carries an index fingerprint that the stream-first store reader cannot align with the ingester's
// StableHash — meaning it could not be deduplicated across sources under stream-first (a documented
// limitation). Keeping them in memory still exercises the stream-first path for co-resident
// hash-colliding streams without tripping that limitation.
func TestSampleQueryStreamOrderingEquivalence(t *testing.T) {
	// Two "pod" values whose full pushed label set {cluster="prod", job="varlog", pod=...} collides on
	// labels.StableHash (found offline with Pollard's rho over StableHash).
	const collidePodA, collidePodB = "058c983fb1464690", "0c38052add338aba"
	require.Equal(t,
		labels.StableHash(labels.FromStrings("cluster", "prod", "job", "varlog", "pod", collidePodA)),
		labels.StableHash(labels.FromStrings("cluster", "prod", "job", "varlog", "pod", collidePodB)),
		"collision fixture no longer collides on StableHash — regenerate a colliding pod pair")

	now := time.Now()
	at := func(minutesAgo int) time.Time { return now.Add(-time.Duration(minutesAgo) * time.Minute) }

	type fixture struct {
		lbls  map[string]string
		lines []string

		// crossSource holds whether the lgo lines should be flushed to store and retained in the ingester too
		// (used to test deduplication).
		crossSource bool
	}
	fixtures := []fixture{
		{map[string]string{"job": "varlog", "cluster": "prod", "app": "a"}, []string{"a1", "a2", "a3"}, true},
		{map[string]string{"job": "varlog", "cluster": "prod", "app": "b"}, []string{"b1", "b2"}, true},
		{map[string]string{"job": "varlog", "cluster": "prod", "app": "c"}, []string{"c1"}, true},
		// Hash-colliding pair, ingester-only (see doc comment).
		{map[string]string{"job": "varlog", "cluster": "prod", "pod": collidePodA}, []string{"pa1", "pa2"}, false},
		{map[string]string{"job": "varlog", "cluster": "prod", "pod": collidePodB}, []string{"pb1", "pb2"}, false},
	}

	// Expected per-series count (one per line).
	expected := map[string]float64{}
	for _, f := range fixtures {
		expected[labels.FromMap(f.lbls).String()] = float64(len(f.lines))
	}

	// runOrdered spins up a fresh single-binary cluster (with stream-first execution optionally
	// enabled), ingests the fixtures so the cross-source streams exist in both the store and the
	// ingester, and returns the per-series counts from `count_over_time({cluster="prod"}[1h])`.
	runOrdered := func(streamOrdered bool) map[string]float64 {
		clu := cluster.New(nil, cluster.SchemaWithTSDB, func(c *cluster.Cluster) { c.SetSchemaVer("v13") })
		defer func() { assert.NoError(t, clu.Cleanup()) }()

		// chunks-retain-period keeps flushed chunks in memory, so a flush leaves a copy in both the
		// store and the ingester for the querier to merge and deduplicate. wal-disk-full-threshold=0
		// disables write throttling so the test doesn't depend on the host's free disk.
		flags := []string{"-target=all", "-ingester.chunks-retain-period=1h", "-ingester.wal-disk-full-threshold=0"}
		if streamOrdered {
			flags = append(flags, "-querier.engine.stream-ordered-execution-enabled=true")
		}
		tAll := clu.AddComponent("all", flags...)
		require.NoError(t, clu.Run())

		cli := client.New(randStringRunes(), "", tAll.HTTPURL())
		cli.Now = now

		// Cross-source streams: push, then flush -> present in both the store and (retained) memory.
		for _, f := range fixtures {
			if !f.crossSource {
				continue
			}
			for i, line := range f.lines {
				require.NoError(t, cli.PushLogLine(line, at(30-i), nil, f.lbls))
			}
		}
		require.NoError(t, cli.Flush())

		// Ingester-only streams: pushed after the flush so they never reach the store.
		for _, f := range fixtures {
			if f.crossSource {
				continue
			}
			for i, line := range f.lines {
				require.NoError(t, cli.PushLogLine(line, at(30-i), nil, f.lbls))
			}
		}

		resp, err := cli.RunQuery(context.Background(), `count_over_time({cluster="prod"}[1h])`)
		require.NoError(t, err)
		require.Equal(t, "vector", resp.Data.ResultType)

		// The query-stats summary must record the ordering the engine actually used: stream-first
		// sub-evaluations when the flag is on (count_over_time is decomposable), timestamp-first
		// otherwise. This exercises the stats plumbing end to end (engine decision -> response stats).
		sum := resp.Data.Statistics.Summary
		if streamOrdered {
			require.Positive(t, sum.StreamFirstSubqueries, "stream-first run must record stream-first sub-evaluations")
			require.Zero(t, sum.TimestampFirstSubqueries, "stream-first run must record no timestamp-first sub-evaluations")
		} else {
			require.Positive(t, sum.TimestampFirstSubqueries, "timestamp-first run must record timestamp-first sub-evaluations")
			require.Zero(t, sum.StreamFirstSubqueries, "timestamp-first run must record no stream-first sub-evaluations")
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

	// Each ordering matches the expected deduplicated counts.
	require.Equal(t, expected, byTimestamp, "timestamp-first counts should match the expected deduplicated result")
	require.Equal(t, expected, byStream, "stream-first counts should match the expected deduplicated result")

	// Both orderings return the same result.
	require.Equal(t, byTimestamp, byStream, "timestamp-first and stream-first should return identical results")
}
