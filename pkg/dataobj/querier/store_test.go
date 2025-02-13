package querier

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/user"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"
	"github.com/thanos-io/objstore"
	"github.com/thanos-io/objstore/providers/filesystem"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
	"github.com/grafana/loki/v3/pkg/dataobj/uploader"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/querier/plan"
)

func TestStore_SelectSeries(t *testing.T) {
	const testTenant = "test-tenant"
	builder := newTestDataBuilder(t, testTenant)
	defer builder.close()

	// Setup test data
	now := setupTestData(t, builder)

	store := NewStore(builder.bucket)
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

	store := NewStore(builder.bucket)
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

	store := NewStore(builder.bucket)
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

func setupTestData(t *testing.T, builder *testDataBuilder) time.Time {
	t.Helper()
	now := time.Now()

	// First object with app=foo series
	builder.addStream(
		`{app="foo", env="prod"}`,
		logproto.Entry{Timestamp: now, Line: "foo1"},
		logproto.Entry{Timestamp: now.Add(time.Second), Line: "foo2"},
	)
	builder.addStream(
		`{app="foo", env="dev"}`,
		logproto.Entry{Timestamp: now, Line: "foo3"},
		logproto.Entry{Timestamp: now.Add(time.Second), Line: "foo4"},
	)
	builder.flush()

	// Second object with app=bar series
	builder.addStream(
		`{app="bar", env="prod"}`,
		logproto.Entry{Timestamp: now, Line: "bar1"},
		logproto.Entry{Timestamp: now.Add(time.Second), Line: "bar2"},
	)
	builder.addStream(
		`{app="bar", env="dev"}`,
		logproto.Entry{Timestamp: now, Line: "bar3"},
		logproto.Entry{Timestamp: now.Add(time.Second), Line: "bar4"},
	)
	builder.flush()

	// Third object with app=baz series
	builder.addStream(
		`{app="baz", env="prod", team="a"}`,
		logproto.Entry{Timestamp: now, Line: "baz1"},
		logproto.Entry{Timestamp: now.Add(time.Second), Line: "baz2"},
	)
	builder.flush()

	return now
}

func labelsFromSeriesID(id logproto.SeriesIdentifier) string {
	ls := make(labels.Labels, 0, len(id.Labels))
	for _, l := range id.Labels {
		ls = append(ls, labels.Label{Name: l.Key, Value: l.Value})
	}
	sort.Sort(ls)
	return ls.String()
}

func mustParseSeriesID(s string) logproto.SeriesIdentifier {
	ls, err := syntax.ParseLabels(s)
	if err != nil {
		panic(err)
	}
	return logproto.SeriesIdentifier{
		Labels: labelsToSeriesLabels(ls),
	}
}

func labelsToSeriesLabels(ls labels.Labels) []logproto.SeriesIdentifier_LabelsEntry {
	entries := make([]logproto.SeriesIdentifier_LabelsEntry, 0, len(ls))
	for _, l := range ls {
		entries = append(entries, logproto.SeriesIdentifier_LabelsEntry{
			Key:   l.Name,
			Value: l.Value,
		})
	}
	return entries
}

func planFromString(s string) *plan.QueryPlan {
	if s == "" {
		return nil
	}
	expr, err := syntax.ParseExpr(s)
	if err != nil {
		panic(err)
	}
	return &plan.QueryPlan{
		AST: expr,
	}
}

// testDataBuilder helps build test data for querier tests.
type testDataBuilder struct {
	t      *testing.T
	bucket objstore.Bucket
	dir    string

	tenantID string
	builder  *dataobj.Builder
	meta     *metastore.Manager
	uploader *uploader.Uploader
}

func newTestDataBuilder(t *testing.T, tenantID string) *testDataBuilder {
	dir := t.TempDir()
	bucket, err := filesystem.NewBucket(dir)
	require.NoError(t, err)

	// Create required directories for metastore
	metastoreDir := filepath.Join(dir, "tenant-"+tenantID, "metastore")
	require.NoError(t, os.MkdirAll(metastoreDir, 0o755))

	builder, err := dataobj.NewBuilder(dataobj.BuilderConfig{
		TargetPageSize:    1024 * 1024,      // 1MB
		TargetObjectSize:  10 * 1024 * 1024, // 10MB
		TargetSectionSize: 1024 * 1024,      // 1MB
		BufferSize:        1024 * 1024,      // 1MB
	})
	require.NoError(t, err)

	meta := metastore.NewManager(bucket, tenantID, log.NewLogfmtLogger(os.Stdout))
	require.NoError(t, meta.RegisterMetrics(prometheus.NewRegistry()))

	uploader := uploader.New(uploader.Config{SHAPrefixSize: 2}, bucket, tenantID)
	require.NoError(t, uploader.RegisterMetrics(prometheus.NewRegistry()))

	return &testDataBuilder{
		t:        t,
		bucket:   bucket,
		dir:      dir,
		tenantID: tenantID,
		builder:  builder,
		meta:     meta,
		uploader: uploader,
	}
}

func (b *testDataBuilder) addStream(labels string, entries ...logproto.Entry) {
	err := b.builder.Append(logproto.Stream{
		Labels:  labels,
		Entries: entries,
	})
	require.NoError(b.t, err)
}

func (b *testDataBuilder) flush() {
	buf := bytes.NewBuffer(make([]byte, 0, 1024*1024))
	stats, err := b.builder.Flush(buf)
	require.NoError(b.t, err)

	// Upload the data object using the uploader
	path, err := b.uploader.Upload(context.Background(), buf)
	require.NoError(b.t, err)

	// Update metastore with the new data object
	err = b.meta.UpdateMetastore(context.Background(), path, stats)
	require.NoError(b.t, err)

	b.builder.Reset()
}

func (b *testDataBuilder) close() {
	require.NoError(b.t, b.bucket.Close())
	os.RemoveAll(b.dir)
}
