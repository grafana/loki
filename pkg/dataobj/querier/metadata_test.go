package querier

import (
	"context"
	"sort"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/user"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql"
)

func TestStore_SelectSeries(t *testing.T) {
	const testTenant = "test-tenant"
	builder := newTestDataBuilder(t, testTenant)
	defer builder.close()

	// Setup test data
	now := setupTestData(t, builder)
	meta := metastore.NewObjectMetastore(builder.bucket)
	store := NewStore(builder.bucket, log.NewNopLogger(), meta)
	ctx := user.InjectOrgID(context.Background(), testTenant)

	tests := []struct {
		name     string
		selector string
		want     []string
	}{
		{
			name:     "select all series",
			selector: ``,
			want: []string{
				`{app="foo", env="prod"}`,
				`{app="foo", env="dev"}`,
				`{app="bar", env="prod"}`,
				`{app="bar", env="dev"}`,
				`{app="baz", env="prod", team="a"}`,
			},
		},
		{
			name:     "select with equality matcher",
			selector: `{app="foo"}`,
			want: []string{
				`{app="foo", env="prod"}`,
				`{app="foo", env="dev"}`,
			},
		},
		{
			name:     "select with regex matcher",
			selector: `{app=~"foo|bar"}`,
			want: []string{
				`{app="foo", env="prod"}`,
				`{app="foo", env="dev"}`,
				`{app="bar", env="prod"}`,
				`{app="bar", env="dev"}`,
			},
		},
		{
			name:     "select with negative equality matcher",
			selector: `{app=~".+", app!="foo"}`,
			want: []string{
				`{app="bar", env="prod"}`,
				`{app="bar", env="dev"}`,
				`{app="baz", env="prod", team="a"}`,
			},
		},
		{
			name:     "select with negative regex matcher",
			selector: `{app=~".+", app!~"foo|bar"}`,
			want: []string{
				`{app="baz", env="prod", team="a"}`,
			},
		},
		{
			name:     "select with multiple matchers",
			selector: `{app="foo", env="prod"}`,
			want: []string{
				`{app="foo", env="prod"}`,
			},
		},
		{
			name:     "select with regex and equality matchers",
			selector: `{app=~"foo|bar", env="prod"}`,
			want: []string{
				`{app="foo", env="prod"}`,
				`{app="bar", env="prod"}`,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			series, err := store.SelectSeries(ctx, logql.SelectLogParams{
				QueryRequest: &logproto.QueryRequest{
					Start:    now.Add(-time.Hour),
					End:      now.Add(time.Hour),
					Plan:     planFromString(tt.selector),
					Selector: tt.selector,
				},
			})
			require.NoError(t, err)

			var got []string
			for _, s := range series {
				got = append(got, labelsFromSeriesID(s))
			}
			require.ElementsMatch(t, tt.want, got)
		})
	}

	t.Run("sharding", func(t *testing.T) {
		// Query first shard
		series1, err := store.SelectSeries(ctx, logql.SelectLogParams{
			QueryRequest: &logproto.QueryRequest{
				Start:    now.Add(-time.Hour),
				End:      now.Add(time.Hour),
				Plan:     planFromString(`{app=~"foo|bar|baz"}`),
				Selector: `{app=~"foo|bar|baz"}`,
				Shards:   []string{"0_of_2"},
			},
		})
		require.NoError(t, err)
		require.NotEmpty(t, series1)
		require.Less(t, len(series1), 5) // Should get less than all series

		// Query second shard
		series2, err := store.SelectSeries(ctx, logql.SelectLogParams{
			QueryRequest: &logproto.QueryRequest{
				Start:    now.Add(-time.Hour),
				End:      now.Add(time.Hour),
				Plan:     planFromString(`{app=~"foo|bar|baz"}`),
				Selector: `{app=~"foo|bar|baz"}`,
				Shards:   []string{"1_of_2"},
			},
		})
		require.NoError(t, err)
		require.NotEmpty(t, series2)

		// Combined shards should equal all series
		var allSeries []string
		for _, s := range append(series1, series2...) {
			allSeries = append(allSeries, labelsFromSeriesID(s))
		}

		want := []string{
			`{app="foo", env="prod"}`,
			`{app="foo", env="dev"}`,
			`{app="bar", env="prod"}`,
			`{app="bar", env="dev"}`,
			`{app="baz", env="prod", team="a"}`,
		}
		require.ElementsMatch(t, want, allSeries)
	})
}

func TestStore_LabelNamesForMetricName(t *testing.T) {
	const testTenant = "test-tenant"
	builder := newTestDataBuilder(t, testTenant)
	defer builder.close()

	// Setup test data
	now := setupTestData(t, builder)
	meta := metastore.NewObjectMetastore(builder.bucket)
	store := NewStore(builder.bucket, log.NewNopLogger(), meta)
	ctx := user.InjectOrgID(context.Background(), testTenant)

	tests := []struct {
		name     string
		matchers []*labels.Matcher
		want     []string
	}{
		{
			name:     "no matchers",
			matchers: nil,
			want:     []string{"app", "env", "team"},
		},
		{
			name: "with equality matcher",
			matchers: []*labels.Matcher{
				labels.MustNewMatcher(labels.MatchEqual, "app", "foo"),
			},
			want: []string{"app", "env"},
		},
		{
			name: "with regex matcher",
			matchers: []*labels.Matcher{
				labels.MustNewMatcher(labels.MatchRegexp, "app", "foo|bar"),
			},
			want: []string{"app", "env"},
		},
		{
			name: "with negative matcher",
			matchers: []*labels.Matcher{
				labels.MustNewMatcher(labels.MatchNotEqual, "app", "foo"),
			},
			want: []string{"app", "env", "team"},
		},
		{
			name: "with negative regex matcher",
			matchers: []*labels.Matcher{
				labels.MustNewMatcher(labels.MatchNotRegexp, "app", "foo|bar"),
			},
			want: []string{"app", "env", "team"},
		},
		{
			name: "with multiple matchers",
			matchers: []*labels.Matcher{
				labels.MustNewMatcher(labels.MatchEqual, "app", "foo"),
				labels.MustNewMatcher(labels.MatchEqual, "env", "prod"),
			},
			want: []string{"app", "env"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			names, err := store.LabelNamesForMetricName(ctx, "", model.TimeFromUnixNano(now.Add(-time.Hour).UnixNano()), model.TimeFromUnixNano(now.Add(time.Hour).UnixNano()), "", tt.matchers...)
			require.NoError(t, err)
			require.ElementsMatch(t, tt.want, names)
		})
	}
}

func TestStore_LabelValuesForMetricName(t *testing.T) {
	const testTenant = "test-tenant"
	builder := newTestDataBuilder(t, testTenant)
	defer builder.close()

	// Setup test data
	now := setupTestData(t, builder)
	meta := metastore.NewObjectMetastore(builder.bucket)
	store := NewStore(builder.bucket, log.NewNopLogger(), meta)
	ctx := user.InjectOrgID(context.Background(), testTenant)

	tests := []struct {
		name      string
		labelName string
		matchers  []*labels.Matcher
		want      []string
	}{
		{
			name:      "app label without matchers",
			labelName: "app",
			matchers:  nil,
			want:      []string{"bar", "baz", "foo"},
		},
		{
			name:      "env label without matchers",
			labelName: "env",
			matchers:  nil,
			want:      []string{"dev", "prod"},
		},
		{
			name:      "team label without matchers",
			labelName: "team",
			matchers:  nil,
			want:      []string{"a"},
		},
		{
			name:      "env label with app equality matcher",
			labelName: "env",
			matchers: []*labels.Matcher{
				labels.MustNewMatcher(labels.MatchEqual, "app", "foo"),
			},
			want: []string{"dev", "prod"},
		},
		{
			name:      "env label with app regex matcher",
			labelName: "env",
			matchers: []*labels.Matcher{
				labels.MustNewMatcher(labels.MatchRegexp, "app", "foo|bar"),
			},
			want: []string{"dev", "prod"},
		},
		{
			name:      "env label with app negative matcher",
			labelName: "env",
			matchers: []*labels.Matcher{
				labels.MustNewMatcher(labels.MatchNotEqual, "app", "foo"),
			},
			want: []string{"dev", "prod"},
		},
		{
			name:      "env label with app negative regex matcher",
			labelName: "env",
			matchers: []*labels.Matcher{
				labels.MustNewMatcher(labels.MatchNotRegexp, "app", "foo|bar"),
			},
			want: []string{"prod"},
		},
		{
			name:      "env label with multiple matchers",
			labelName: "env",
			matchers: []*labels.Matcher{
				labels.MustNewMatcher(labels.MatchEqual, "app", "foo"),
				labels.MustNewMatcher(labels.MatchEqual, "env", "prod"),
			},
			want: []string{"prod"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			values, err := store.LabelValuesForMetricName(ctx, "", model.TimeFromUnixNano(now.Add(-time.Hour).UnixNano()), model.TimeFromUnixNano(now.Add(time.Hour).UnixNano()), "", tt.labelName, tt.matchers...)
			require.NoError(t, err)
			require.Equal(t, tt.want, values)
		})
	}
}

func labelsFromSeriesID(id logproto.SeriesIdentifier) string {
	ls := make(labels.Labels, 0, len(id.Labels))
	for _, l := range id.Labels {
		ls = append(ls, labels.Label{Name: l.Key, Value: l.Value})
	}
	sort.Sort(ls)
	return ls.String()
}
