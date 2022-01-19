package chunk

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"
)

func TestSeriesStore_LabelValuesForMetricName(t *testing.T) {
	ctx := context.Background()
	now := model.Now()

	fooMetric1 := labels.Labels{
		{Name: labels.MetricName, Value: "foo"},
		{Name: "bar", Value: "baz"},
		{Name: "flip", Value: "flop"},
		{Name: "toms", Value: "code"},
		{Name: "env", Value: "dev"},
		{Name: "class", Value: "not-secret"},
	}
	fooMetric2 := labels.Labels{
		{Name: labels.MetricName, Value: "foo"},
		{Name: "bar", Value: "beep"},
		{Name: "toms", Value: "code"},
		{Name: "env", Value: "prod"},
		{Name: "class", Value: "secret"},
	}

	fooChunk1 := dummyChunkFor(now, fooMetric1)
	fooChunk2 := dummyChunkFor(now, fooMetric2)

	for _, tc := range []struct {
		metricName, labelName string
		expect                []string
		matchers              []*labels.Matcher
	}{
		{
			`foo`, `class`,
			[]string{"not-secret"},
			[]*labels.Matcher{labels.MustNewMatcher(labels.MatchEqual, "env", "dev")},
		},
		{
			`foo`, `bar`,
			[]string{"baz"},
			[]*labels.Matcher{
				labels.MustNewMatcher(labels.MatchNotEqual, "env", "prod"),
				labels.MustNewMatcher(labels.MatchEqual, "toms", "code"),
			},
		},
	} {
		for _, schema := range seriesStoreSchemas {
			for _, storeCase := range stores {
				t.Run(fmt.Sprintf("%s / %s / %s / %s", tc.metricName, tc.labelName, schema, storeCase.name), func(t *testing.T) {
					t.Log("========= Running labelValues with metricName", tc.metricName, "with labelName", tc.labelName, "with schema", schema)
					storeCfg := storeCase.configFn()
					store, _ := newTestChunkStoreConfig(t, schema, storeCfg)
					defer store.Stop()

					if err := store.Put(ctx, []Chunk{
						fooChunk1,
						fooChunk2,
					}); err != nil {
						t.Fatal(err)
					}

					// Query with ordinary time-range
					labelValues1, err := store.LabelValuesForMetricName(ctx, userID, now.Add(-time.Hour), now, tc.metricName, tc.labelName, tc.matchers...)
					require.NoError(t, err)
					require.ElementsMatch(t, tc.expect, labelValues1)

					// Pushing end of time-range into future should yield exact same resultset
					labelValues2, err := store.LabelValuesForMetricName(ctx, userID, now.Add(-time.Hour), now.Add(time.Hour*24*10), tc.metricName, tc.labelName, tc.matchers...)
					require.NoError(t, err)
					require.ElementsMatch(t, tc.expect, labelValues2)
				})
			}
		}
	}
}
