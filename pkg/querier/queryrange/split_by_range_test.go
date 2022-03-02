package queryrange

import (
	"context"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/querier/queryrange/queryrangebase"
	"github.com/stretchr/testify/require"
	"github.com/weaveworks/common/user"
)

func Test_RangeVectorSplit(t *testing.T) {
	srm, err := SplitByRangeMiddleware(log.NewNopLogger(), fakeLimits{
		maxSeries: 10000,
		splits: map[string]time.Duration{
			"tenant1": time.Minute,
		},
	}, nilShardingMetrics)
	require.NoError(t, err)

	ctx := user.InjectOrgID(context.TODO(), "tenant1")

	resp, err := srm.Wrap(queryrangebase.HandlerFunc(func(ctx context.Context, r queryrangebase.Request) (queryrangebase.Response, error) {
		return &LokiPromResponse{
			Response: &queryrangebase.PrometheusResponse{
				Status: "success",
				Data: queryrangebase.PrometheusData{
					ResultType: "vector",
					Result: []queryrangebase.SampleStream{
						{
							Labels: []logproto.LabelAdapter{
								{Name: "foo", Value: "bar"},
							},
							Samples: []logproto.LegacySample{
								{TimestampMs: 1000, Value: 1},
							},
						},
					},
				},
			},
		}, nil
	})).Do(ctx, &LokiInstantRequest{
		Query:  `sum(rate({foo="bar"}[10m]))`,
		TimeTs: time.Unix(1, 0),
		Path:   "/loki/api/v1/query",
	})
	require.NoError(t, err)
	require.Equal(t, &queryrangebase.PrometheusResponse{
		Status: "success",
		Data: queryrangebase.PrometheusData{
			ResultType: "vector",
			Result: []queryrangebase.SampleStream{
				{
					Labels: []logproto.LabelAdapter{},
					Samples: []logproto.LegacySample{
						{TimestampMs: 1000, Value: 10},
					},
				},
			},
		},
	}, resp.(*LokiPromResponse).Response)
}
