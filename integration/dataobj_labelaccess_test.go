//go:build integration

package integration

import (
	"strconv"
	"testing"
	"time"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/integration/client"
	"github.com/grafana/loki/v3/pkg/labelaccess"
	"github.com/grafana/loki/v3/pkg/labelaccess/types"
)

const dataObjLabelAccessQuery = `sum by (classification, env, job) (count_over_time({job="varlog"}[2h]))`

// TestDataObjLabelAccess checks that label-based access control returns the same streams from data
// objects as from chunks. It ingests four streams through Kafka, then runs one table of policies
// twice: first while the querier reads chunks, then after a restart that makes it read data objects.
func TestDataObjLabelAccess(t *testing.T) {
	clu := startDataObjCluster(t, withChunksOnlyQuerier(), withExtraFlags("-lbac.enabled=true"))

	var (
		ctx    = t.Context()
		tenant = randStringRunes()
		now    = time.Now()

		confidentialDev = map[string]string{"job": "varlog", "env": "dev", "classification": "confidential"}
		noLabels        = map[string]string{"job": "varlog"}
		secretDev       = map[string]string{"job": "varlog", "env": "dev", "classification": "secret"}
		secretProd      = map[string]string{"job": "varlog", "env": "prod", "classification": "secret"}
	)

	testCases := []struct {
		name     string
		policies []types.LabelPolicy
		streams  []map[string]string
	}{
		{
			name:    "returns every stream when the request carries no policy",
			streams: []map[string]string{confidentialDev, noLabels, secretDev, secretProd},
		},
		{
			name:     "returns no stream when no stream matches the policy",
			policies: []types.LabelPolicy{newPolicy(`{not="existing"}`)},
		},
		{
			name:     "returns only the streams that match an equality policy",
			policies: []types.LabelPolicy{newPolicy(`{env="dev"}`)},
			streams:  []map[string]string{confidentialDev, secretDev},
		},
		{
			name:     "keeps the streams that lack the label of a negative policy",
			policies: []types.LabelPolicy{newPolicy(`{env!="dev"}`)},
			streams:  []map[string]string{noLabels, secretProd},
		},
		{
			name: "returns the streams that match any of several policies",
			policies: []types.LabelPolicy{
				newPolicy(`{env="dev", classification!="secret"}`),
				newPolicy(`{classification=~"secre.*"}`),
			},
			streams: []map[string]string{confidentialDev, secretDev, secretProd},
		},
	}

	cliDistributor := client.New(tenant, "", clu.distributor.HTTPURL())
	cliIngester := client.New(tenant, "", clu.ingester.HTTPURL())
	cliIndexGateway := client.New("", "", clu.indexGateway.HTTPURL())

	queryClient := func(policies []types.LabelPolicy) *client.Client {
		var opts []client.Option
		if len(policies) > 0 {
			opts = append(opts, client.InjectHeadersOption{
				labelaccess.HTTPHeaderKey: buildLBACPolicyHeaders(tenant, policies),
			})
		}
		cli := client.New(tenant, "", clu.queryFrontend.HTTPURL(), opts...)
		cli.Now = now
		return cli
	}

	require.Eventually(t, func() bool {
		return cliDistributor.PushLogLine("__warmup__", now, nil, map[string]string{"job": "warmup"}) == nil
	}, 60*time.Second, 250*time.Millisecond, "distributor should be able to produce to an active partition")

	require.NoError(t, cliDistributor.PushLogLine("line1", now.Add(-6*time.Minute), nil, confidentialDev))
	require.NoError(t, cliDistributor.PushLogLine("line2", now.Add(-5*time.Minute), nil, noLabels))
	require.NoError(t, cliDistributor.PushLogLine("line3", now.Add(-4*time.Minute), nil, secretDev))
	require.NoError(t, cliDistributor.PushLogLine("line4", now.Add(-3*time.Minute), nil, secretProd))

	allStreams := []map[string]string{confidentialDev, noLabels, secretDev, secretProd}
	unrestricted := queryClient(nil)

	require.Eventually(t, func() bool {
		if err := cliIngester.FlushTenant(""); err != nil {
			t.Logf("flush: %v", err)
			return false
		}
		if _, err := cliIndexGateway.TriggerSyncIndexes(); err != nil {
			t.Logf("sync trigger: %v", err)
			return false
		}
		resp, err := unrestricted.RunQuery(ctx, dataObjLabelAccessQuery)
		if err != nil {
			t.Logf("chunk query: %v", err)
			return false
		}
		return equalCounts(resp, countsOf(allStreams))
	}, 60*time.Second, 500*time.Millisecond, "the chunk store should eventually serve the complete expected result")

	runPolicies := func(t *testing.T, readsDataObjects bool) {
		for _, testCase := range testCases {
			t.Run(testCase.name, func(t *testing.T) {
				cli := queryClient(testCase.policies)
				expected := countsOf(testCase.streams)

				instant, err := cli.RunQuery(ctx, dataObjLabelAccessQuery)
				require.NoError(t, err)
				require.Equal(t, "vector", instant.Data.ResultType)
				require.Equal(t, expected, vectorCounts(t, instant))

				if len(testCase.streams) > 0 {
					store := instant.Data.Statistics.Querier.Store
					if readsDataObjects {
						require.Positive(t, store.Dataobj.PrePredicateDecompressedRows)
						require.Zero(t, store.Chunk.DecompressedLines)
					} else {
						require.Positive(t, store.Chunk.DecompressedLines)
						require.Zero(t, store.Dataobj.PrePredicateDecompressedRows)
					}
				}

				ranged, err := cli.RunRangeQueryWithStartEnd(ctx, dataObjLabelAccessQuery, now.Add(-time.Hour), now.Add(time.Second))
				require.NoError(t, err)
				require.Equal(t, "matrix", ranged.Data.ResultType)
				require.Equal(t, len(testCase.streams), len(ranged.Data.Matrix))
				require.EqualValues(t, len(testCase.streams), maxStepValue(t, ranged.Data.Matrix))
			})
		}
	}

	t.Run("chunk store", func(t *testing.T) {
		runPolicies(t, false)
	})

	clu.restartQuerierOnDataObjects(t)

	require.Eventually(t, func() bool {
		resp, err := unrestricted.RunQuery(ctx, dataObjLabelAccessQuery)
		if err != nil {
			t.Logf("data-object query: %v", err)
			return false
		}
		return equalCounts(resp, countsOf(allStreams)) && resp.Data.Statistics.Querier.Store.Dataobj.PrePredicateDecompressedRows > 0
	}, 60*time.Second, 500*time.Millisecond, "the data-object store should eventually serve the complete expected result")

	t.Run("data objects", func(t *testing.T) {
		runPolicies(t, true)
	})
}

// countsOf returns the expected result of dataObjLabelAccessQuery for the streams: one line each.
func countsOf(streams []map[string]string) map[string]float64 {
	counts := make(map[string]float64, len(streams))
	for _, stream := range streams {
		counts[labels.FromMap(stream).String()] = 1
	}
	return counts
}

// vectorCounts returns the samples of an instant query result by their label set.
func vectorCounts(t *testing.T, resp *client.Response) map[string]float64 {
	t.Helper()

	counts := make(map[string]float64, len(resp.Data.Vector))
	for _, sample := range resp.Data.Vector {
		value, err := strconv.ParseFloat(sample.Value, 64)
		require.NoError(t, err)
		counts[labels.FromMap(sample.Metric).String()] = value
	}
	return counts
}

// equalCounts reports whether the instant query result holds exactly the expected samples. It
// returns false instead of failing, so a retry loop can call it.
func equalCounts(resp *client.Response, expected map[string]float64) bool {
	if resp.Data.ResultType != "vector" || len(resp.Data.Vector) != len(expected) {
		return false
	}
	for _, sample := range resp.Data.Vector {
		value, err := strconv.ParseFloat(sample.Value, 64)
		if err != nil || expected[labels.FromMap(sample.Metric).String()] != value {
			return false
		}
	}
	return true
}
