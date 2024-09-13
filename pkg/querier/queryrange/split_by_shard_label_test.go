package queryrange

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/grafana/dskit/user"
	"github.com/grafana/loki/v3/pkg/logproto"

	// "github.com/grafana/loki/v3/pkg/querier/queryrange"
	"github.com/grafana/loki/v3/pkg/querier/queryrange/queryrangebase"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"
)

func Test_SplitByShardLabel(t *testing.T) {
	fooBarMatcher := `{foo="bar"}`
	now := time.Now().Round(time.Hour)
	modelNow := model.TimeFromUnixNano(now.UnixNano())

	iqo := ingesterQueryOpts{
		queryStoreOnly:       false,
		queryIngestersWithin: 0,
	}
	ctx := user.InjectOrgID(context.TODO(), "tenant")

	labelsHandler := func(shardValues []string) queryrangebase.HandlerFunc {
		return queryrangebase.HandlerFunc(
			func(ctx context.Context, req queryrangebase.Request) (queryrangebase.Response, error) {
				resp := LokiLabelNamesResponse{
					Status: "success",
					Data:   shardValues,
				}

				return &resp, nil
			},
		)
	}

	for _, tc := range []struct {
		name             string
		targetBuckets    int
		shardValues      []string
		expectedMatchers []string
		splitDuration    time.Duration
		now              model.Time
		then             model.Time
	}{
		{
			name:          "shard groupings not evenly divisible by target buckets, two time splits",
			targetBuckets: 4,
			shardValues:   []string{"0", "1", "2", "3", "4", "5", "6", "7"},
			splitDuration: 30 * time.Minute,
			now:           modelNow,
			then:          modelNow.Add(-1 * time.Hour),
			// Two time splits for each shard split
			expectedMatchers: []string{
				`{foo="bar", __stream_shard__=""}`,
				`{foo="bar", __stream_shard__=""}`,
				`{foo="bar", __stream_shard__=~"(0|1|2)"}`,
				`{foo="bar", __stream_shard__=~"(0|1|2)"}`,
				`{foo="bar", __stream_shard__=~"(3|4|5)"}`,
				`{foo="bar", __stream_shard__=~"(3|4|5)"}`,
				`{foo="bar", __stream_shard__=~"(6|7)"}`,
				`{foo="bar", __stream_shard__=~"(6|7)"}`,
			},
		},
		{
			name:          "shard groupings not evenly divisible by target buckets, one time split",
			targetBuckets: 4,
			shardValues:   []string{"0", "1", "2", "3", "4", "5", "6", "7"},
			splitDuration: 1 * time.Hour,
			now:           modelNow,
			then:          modelNow.Add(-1 * time.Hour),
			// Two time splits for each shard split
			expectedMatchers: []string{
				`{foo="bar", __stream_shard__=""}`,
				`{foo="bar", __stream_shard__=~"(0|1|2)"}`,
				`{foo="bar", __stream_shard__=~"(3|4|5)"}`,
				`{foo="bar", __stream_shard__=~"(6|7)"}`,
			},
		},
		{
			name:          "shard groupings evenly divisible by target buckets, two time splits",
			targetBuckets: 3,                                      // 1 extra, last bucket reserved for no shard
			shardValues:   []string{"0", "1", "2", "3", "4", "5"}, // 6 shards, evenly divisible by 2
			splitDuration: 30 * time.Minute,
			now:           modelNow,
			then:          modelNow.Add(-1 * time.Hour),
			// Two time splits for each shard split
			expectedMatchers: []string{
				`{foo="bar", __stream_shard__=""}`,
				`{foo="bar", __stream_shard__=""}`,
				`{foo="bar", __stream_shard__=~"(0|1|2)"}`,
				`{foo="bar", __stream_shard__=~"(0|1|2)"}`,
				`{foo="bar", __stream_shard__=~"(3|4|5)"}`,
				`{foo="bar", __stream_shard__=~"(3|4|5)"}`,
			},
		},
		{
			name:          "shard groupings evenly divisible by target buckets, one time splits",
			targetBuckets: 3,                                      // 1 extra, last bucket reserved for no shard
			shardValues:   []string{"0", "1", "2", "3", "4", "5"}, // 6 shards, evenly divisible by 2
			splitDuration: 1 * time.Hour,
			now:           modelNow,
			then:          modelNow.Add(-1 * time.Hour),
			// Two time splits for each shard split
			expectedMatchers: []string{
				`{foo="bar", __stream_shard__=""}`,
				`{foo="bar", __stream_shard__=~"(0|1|2)"}`,
				`{foo="bar", __stream_shard__=~"(3|4|5)"}`,
			},
		},
		{
			name:          "fewer shards than target buckets, two time splits",
			targetBuckets: 4,                  // 1 extra, last bucket reserved for no shard
			shardValues:   []string{"0", "1"}, // 1 shards, not divisible by 3
			splitDuration: 30 * time.Minute,
			now:           modelNow,
			then:          modelNow.Add(-1 * time.Hour),
			// Two time splits for each shard split
			expectedMatchers: []string{
				`{foo="bar", __stream_shard__=""}`,
				`{foo="bar", __stream_shard__=""}`,
				`{foo="bar", __stream_shard__=~"(0)"}`,
				`{foo="bar", __stream_shard__=~"(0)"}`,
				`{foo="bar", __stream_shard__=~"(1)"}`,
				`{foo="bar", __stream_shard__=~"(1)"}`,
			},
		},
		{
			name:          "no shards, two time splits",
			targetBuckets: 4,
			shardValues:   []string{},
			splitDuration: 30 * time.Minute,
			now:           modelNow,
			then:          modelNow.Add(-1 * time.Hour),
			// Two time splits for each shard split
			expectedMatchers: []string{
				`{foo="bar"}`,
				`{foo="bar"}`,
			},
		},
		{
			name:          "no shards, one time splits",
			targetBuckets: 4,
			shardValues:   []string{},
			splitDuration: 1 * time.Hour,
			now:           modelNow,
			then:          modelNow.Add(-1 * time.Hour),
			// Two time splits for each shard split
			expectedMatchers: []string{
				`{foo="bar"}`,
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			lims := limits{
				Limits:        nil,
				splitDuration: &tc.splitDuration,
			}

			splitByShardLbl := newShardLabelSplitter(
				lims,
				iqo,
				tc.targetBuckets,
				labelsHandler(tc.shardValues),
			)

			req := logproto.VolumeRequest{
				From:     tc.then,
				Through:  tc.now,
				Matchers: fooBarMatcher,
			}

			reqs, err := splitByShardLbl.split(
				ctx,
				tc.now.Time(),
				[]string{"tenant"},
				&req,
				tc.splitDuration,
			)
			require.NoError(t, err)

			matchers := make([]string, 0, len(reqs))
			for _, r := range reqs {
				volReq, ok := r.(*logproto.VolumeRequest)
				require.True(t, ok)
				matchers = append(matchers, volReq.Matchers)
			}

			slices.Sort(matchers)
			require.Equal(t, tc.expectedMatchers, matchers)

			idxStatsReq := logproto.IndexStatsRequest{
				From:     tc.then,
				Through:  tc.now,
				Matchers: fooBarMatcher,
			}

			idxStatsReqs, err := splitByShardLbl.split(
				ctx,
				tc.now.Time(),
				[]string{"tenant"},
				&idxStatsReq,
				tc.splitDuration,
			)
			require.NoError(t, err)

			matchers = make([]string, 0, len(idxStatsReqs))
			for _, r := range idxStatsReqs {
				req, ok := r.(*logproto.IndexStatsRequest)
				require.True(t, ok)
				matchers = append(matchers, req.Matchers)
			}

			slices.Sort(matchers)
			require.Equal(t, tc.expectedMatchers, matchers)
		})
	}

	t.Run(
		"does not further split queries that already have __stream_shard__ label",
		func(t *testing.T) {
			splitDuration := 30 * time.Minute
			lims := limits{
				Limits:        nil,
				splitDuration: &splitDuration,
			}

			splitByShardLbl := newShardLabelSplitter(
				lims,
				iqo,
				4,
				labelsHandler([]string{"0", "1", "2", "3", "4", "5"}),
			)

			matcher := `{foo="bar", __stream_shard__="0"}`
			now := time.Now().Round(time.Hour)
			modelNow := model.TimeFromUnixNano(now.UnixNano())
			req := logproto.VolumeRequest{
				From:     modelNow.Add(-1 * time.Hour),
				Through:  modelNow,
				Matchers: matcher,
			}

			reqs, err := splitByShardLbl.split(
				ctx,
				now,
				[]string{"tenant"},
				&req,
				splitDuration,
			)
			require.NoError(t, err)

			matchers := make([]string, 0, len(reqs))
			for _, r := range reqs {
				volReq, ok := r.(*logproto.VolumeRequest)
				require.True(t, ok)
				matchers = append(matchers, volReq.Matchers)
			}

			slices.Sort(matchers)
			require.Equal(t, []string{
				matcher,
				matcher,
			}, matchers)

			idxStatsReq := logproto.IndexStatsRequest{
				From:     modelNow.Add(-1 * time.Hour),
				Through:  modelNow,
				Matchers: matcher,
			}

			idxStatsReqs, err := splitByShardLbl.split(
				ctx,
				now,
				[]string{"tenant"},
				&idxStatsReq,
				splitDuration,
			)
			require.NoError(t, err)

			matchers = make([]string, 0, len(idxStatsReqs))
			for _, r := range idxStatsReqs {
				req, ok := r.(*logproto.IndexStatsRequest)
				require.True(t, ok)
				matchers = append(matchers, req.Matchers)
			}

			slices.Sort(matchers)
			require.Equal(t, []string{
				matcher,
				matcher,
			}, matchers)
		},
	)
}
