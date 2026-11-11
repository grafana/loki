package querier

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/push"
	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/postings"
	dataobjstreams "github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
	"github.com/grafana/loki/v3/pkg/logproto"
)

// TestDataObjStore_SelectSamples_IndexRuns checks that splitting the indexes into runs, as the
// compactor will, does not change the samples of a query. Each index object of a run holds a
// range of posting keys, so the labels of one stream sit in different index objects.
func TestDataObjStore_SelectSamples_IndexRuns(t *testing.T) {
	var corpus []logproto.Stream
	for i, app := range []string{"api", "db", "web", "worker"} {
		for j, env := range []string{"dev", "prod", "staging"} {
			lbls := fmt.Sprintf(`{app=%q, env=%q, region=%q}`, app, env, []string{"eu", "us"}[(i+j)%2])
			if (i+j)%3 == 0 {
				lbls = fmt.Sprintf(`{app=%q, env=%q, region=%q, tier="gold"}`, app, env, []string{"eu", "us"}[(i+j)%2])
			}
			corpus = append(corpus, logproto.Stream{
				Labels: lbls,
				Entries: []push.Entry{
					entry(t, 1, "started", "trace_id", fmt.Sprintf("t-%d-%d", i, j), "level", "info"),
					entry(t, 2, "failed", "trace_id", fmt.Sprintf("t-%d-%d", i, j), "level", "error"),
					entry(t, 3, "done", "level", "info"),
				},
			})
		}
	}

	// Every query but the last two matches samples, so an empty result can't pass by accident.
	queries := []string{
		`sum by (app, env) (count_over_time({app="api", env="prod"}[1m]))`,
		`sum by (app, env) (count_over_time({app=~"api|web", region="eu"}[1m]))`,
		`sum by (app, env) (count_over_time({env!="dev", app=~".+"}[1m]))`,
		`sum by (app, env) (count_over_time({app=~".+", tier=""}[1m]))`,
		`sum by (app, env) (count_over_time({app=~".+", tier!="gold"}[1m]))`,
		`sum by (app, env) (count_over_time({app!~"db|worker", region=~"e.*"}[1m]))`,
		`sum by (app, env) (count_over_time({region="us"} | level="error" [1m]))`,
		`sum by (app, env) (count_over_time({app=~".+"} | trace_id="t-1-2" [1m]))`,
		`sum by (app, env) (count_over_time({app="api"} | trace_id="missing" [1m]))`,
		`sum by (app, env) (count_over_time({namespace="missing"}[1m]))`,
	}

	layout := dataobjstreams.MustShardLayoutFromBuckets(8)
	overrides := &shardLayoutOverrides{layout: layout}

	single := newTestDataObjStore(t, corpus, withTenantOverrides(overrides))
	for _, query := range queries[:len(queries)-2] {
		require.NotEmpty(t, single.selectSamples(testCtx(t), query, at(0), at(10)), query)
	}
	for _, parts := range []int{2, 3, 8} {
		runs := newTestDataObjStore(t, corpus, withTenantOverrides(overrides), withIndexRuns(parts))
		require.Greater(t, len(runs.runFiles), 1, "the index must split into a run of more than one index object")

		for _, query := range queries {
			t.Run(fmt.Sprintf("returns the same samples from a run of %d index objects for %s", parts, query), func(t *testing.T) {
				want := single.selectSamples(testCtx(t), query, at(0), at(10))
				got := runs.selectSamples(testCtx(t), query, at(0), at(10))
				require.Equal(t, want, got)
			})

			t.Run(fmt.Sprintf("returns the same samples from a run of %d index objects for each shard of %s", parts, query), func(t *testing.T) {
				for shard := range uint32(4) {
					shardOf := func(req *logproto.SampleQueryRequest) {
						req.Shards = []string{powerOfTwoShard(shard, 4).String()}
					}
					want := single.selectSamples(testCtx(t), query, at(0), at(10), shardOf)
					got := runs.selectSamples(testCtx(t), query, at(0), at(10), shardOf)
					require.Equal(t, want, got, "shard %d", shard)
				}
			})
		}
	}

	t.Run("returns the samples of a query when the index objects that hold none of its labels are gone", func(t *testing.T) {
		const query = `sum by (app, env) (count_over_time({app="api", env="prod"}[1m]))`
		runs := newTestDataObjStore(t, corpus, withTenantOverrides(overrides), withIndexRuns(8))
		want := single.selectSamples(testCtx(t), query, at(0), at(10))
		require.NotEmpty(t, want)

		var deleted int
		for _, file := range runs.runFiles {
			if file.PostingKeys.OverlapsColumn(postings.KindLabel, "app") || file.PostingKeys.OverlapsColumn(postings.KindLabel, "env") {
				continue
			}
			require.NoError(t, runs.bucket.Delete(t.Context(), metastore.IndexStoragePrefix+"/"+file.Path))
			deleted++
		}
		require.Positive(t, deleted, "the run must hold an index object without app or env postings")

		require.Equal(t, want, runs.selectSamples(testCtx(t), query, at(0), at(10)))
	})
}
