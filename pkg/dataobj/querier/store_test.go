package querier

import (
	"bytes"
	"cmp"
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/go-kit/log"
	gocmp "github.com/google/go-cmp/cmp"
	"github.com/grafana/dskit/user"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	"github.com/thanos-io/objstore"
	"github.com/thanos-io/objstore/providers/filesystem"

	"github.com/grafana/loki/v3/pkg/dataobj/consumer/logsobj"
	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/logs"
	"github.com/grafana/loki/v3/pkg/dataobj/uploader"
	"github.com/grafana/loki/v3/pkg/iter"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/querier/plan"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/index"
)

type sampleWithLabels struct {
	Labels  string
	Samples logproto.Sample
}

func TestStore_SelectSamples(t *testing.T) {
	const testTenant = "test-tenant"
	builder := newTestDataBuilder(t, testTenant)
	defer builder.close()

	// Setup test data
	now := setupTestData(t, builder)
	meta := metastore.NewObjectMetastore(builder.bucket, log.NewNopLogger())
	store := NewStore(builder.bucket, log.NewNopLogger(), meta)
	ctx := user.InjectOrgID(context.Background(), testTenant)

	tests := []struct {
		name     string
		selector string
		start    time.Time
		end      time.Time
		shards   []string
		want     []sampleWithLabels
	}{
		{
			name:     "select all samples in range",
			selector: `rate({app=~".+"}[1h])`,
			start:    now,
			end:      now.Add(time.Hour),
			want: []sampleWithLabels{
				{Labels: `{app="bar", env="dev"}`, Samples: logproto.Sample{Timestamp: now.Add(8 * time.Second).UnixNano(), Value: 1}},
				{Labels: `{app="bar", env="dev"}`, Samples: logproto.Sample{Timestamp: now.Add(18 * time.Second).UnixNano(), Value: 1}},
				{Labels: `{app="bar", env="dev"}`, Samples: logproto.Sample{Timestamp: now.Add(38 * time.Second).UnixNano(), Value: 1}},

				{Labels: `{app="bar", env="prod"}`, Samples: logproto.Sample{Timestamp: now.Add(5 * time.Second).UnixNano(), Value: 1}},
				{Labels: `{app="bar", env="prod"}`, Samples: logproto.Sample{Timestamp: now.Add(15 * time.Second).UnixNano(), Value: 1}},
				{Labels: `{app="bar", env="prod"}`, Samples: logproto.Sample{Timestamp: now.Add(25 * time.Second).UnixNano(), Value: 1}},
				{Labels: `{app="bar", env="prod"}`, Samples: logproto.Sample{Timestamp: now.Add(40 * time.Second).UnixNano(), Value: 1}},

				{Labels: `{app="baz", env="prod", team="a"}`, Samples: logproto.Sample{Timestamp: now.Add(12 * time.Second).UnixNano(), Value: 1}},
				{Labels: `{app="baz", env="prod", team="a"}`, Samples: logproto.Sample{Timestamp: now.Add(22 * time.Second).UnixNano(), Value: 1}},
				{Labels: `{app="baz", env="prod", team="a"}`, Samples: logproto.Sample{Timestamp: now.Add(32 * time.Second).UnixNano(), Value: 1}},
				{Labels: `{app="baz", env="prod", team="a"}`, Samples: logproto.Sample{Timestamp: now.Add(42 * time.Second).UnixNano(), Value: 1}},

				{Labels: `{app="foo", env="dev"}`, Samples: logproto.Sample{Timestamp: now.Add(10 * time.Second).UnixNano(), Value: 1}},
				{Labels: `{app="foo", env="dev"}`, Samples: logproto.Sample{Timestamp: now.Add(20 * time.Second).UnixNano(), Value: 1}},
				{Labels: `{app="foo", env="dev"}`, Samples: logproto.Sample{Timestamp: now.Add(35 * time.Second).UnixNano(), Value: 1}},

				{Labels: `{app="foo", env="prod"}`, Samples: logproto.Sample{Timestamp: now.UnixNano(), Value: 1}},
				{Labels: `{app="foo", env="prod"}`, Samples: logproto.Sample{Timestamp: now.Add(30 * time.Second).UnixNano(), Value: 1}},
				{Labels: `{app="foo", env="prod"}`, Samples: logproto.Sample{Timestamp: now.Add(45 * time.Second).UnixNano(), Value: 1}},
				{Labels: `{app="foo", env="prod"}`, Samples: logproto.Sample{Timestamp: now.Add(50 * time.Second).UnixNano(), Value: 1}},
			},
		},
		{
			name:     "select with time range filter",
			selector: `rate({app="baz", env="prod", team="a"}[1h])`,
			start:    now.Add(20 * time.Second),
			end:      now.Add(40 * time.Second),
			want: []sampleWithLabels{
				{Labels: `{app="baz", env="prod", team="a"}`, Samples: logproto.Sample{Timestamp: now.Add(22 * time.Second).UnixNano(), Value: 1}},
				{Labels: `{app="baz", env="prod", team="a"}`, Samples: logproto.Sample{Timestamp: now.Add(32 * time.Second).UnixNano(), Value: 1}},
			},
		},
		{
			name:     "select with label matcher",
			selector: `rate({app="foo"}[1h])`,
			start:    now,
			end:      now.Add(time.Hour),
			want: []sampleWithLabels{
				{Labels: `{app="foo", env="dev"}`, Samples: logproto.Sample{Timestamp: now.Add(10 * time.Second).UnixNano(), Value: 1}},
				{Labels: `{app="foo", env="dev"}`, Samples: logproto.Sample{Timestamp: now.Add(20 * time.Second).UnixNano(), Value: 1}},
				{Labels: `{app="foo", env="dev"}`, Samples: logproto.Sample{Timestamp: now.Add(35 * time.Second).UnixNano(), Value: 1}},

				{Labels: `{app="foo", env="prod"}`, Samples: logproto.Sample{Timestamp: now.UnixNano(), Value: 1}},
				{Labels: `{app="foo", env="prod"}`, Samples: logproto.Sample{Timestamp: now.Add(30 * time.Second).UnixNano(), Value: 1}},
				{Labels: `{app="foo", env="prod"}`, Samples: logproto.Sample{Timestamp: now.Add(45 * time.Second).UnixNano(), Value: 1}},
				{Labels: `{app="foo", env="prod"}`, Samples: logproto.Sample{Timestamp: now.Add(50 * time.Second).UnixNano(), Value: 1}},
			},
		},
		{
			name:     "select with regex matcher",
			selector: `rate({app=~"foo|bar", env="prod"}[1h])`,
			start:    now,
			end:      now.Add(time.Hour),
			want: []sampleWithLabels{
				{Labels: `{app="bar", env="prod"}`, Samples: logproto.Sample{Timestamp: 3605000000000, Value: 1}},
				{Labels: `{app="bar", env="prod"}`, Samples: logproto.Sample{Timestamp: 3615000000000, Value: 1}},
				{Labels: `{app="bar", env="prod"}`, Samples: logproto.Sample{Timestamp: 3625000000000, Value: 1}},
				{Labels: `{app="bar", env="prod"}`, Samples: logproto.Sample{Timestamp: 3640000000000, Value: 1}},

				{Labels: `{app="foo", env="prod"}`, Samples: logproto.Sample{Timestamp: 3600000000000, Value: 1}},
				{Labels: `{app="foo", env="prod"}`, Samples: logproto.Sample{Timestamp: 3630000000000, Value: 1}},
				{Labels: `{app="foo", env="prod"}`, Samples: logproto.Sample{Timestamp: 3645000000000, Value: 1}},
				{Labels: `{app="foo", env="prod"}`, Samples: logproto.Sample{Timestamp: 3650000000000, Value: 1}},
			},
		},
		{
			name:     "select first shard",
			selector: `rate({app=~".+"}[1h])`,
			start:    now,
			end:      now.Add(time.Hour),
			shards:   []string{"0_of_2"},
			want: []sampleWithLabels{
				{Labels: `{app="bar", env="dev"}`, Samples: logproto.Sample{Timestamp: now.Add(8 * time.Second).UnixNano(), Value: 1}},
				{Labels: `{app="bar", env="dev"}`, Samples: logproto.Sample{Timestamp: now.Add(18 * time.Second).UnixNano(), Value: 1}},
				{Labels: `{app="bar", env="dev"}`, Samples: logproto.Sample{Timestamp: now.Add(38 * time.Second).UnixNano(), Value: 1}},

				{Labels: `{app="bar", env="prod"}`, Samples: logproto.Sample{Timestamp: now.Add(5 * time.Second).UnixNano(), Value: 1}},
				{Labels: `{app="bar", env="prod"}`, Samples: logproto.Sample{Timestamp: now.Add(15 * time.Second).UnixNano(), Value: 1}},
				{Labels: `{app="bar", env="prod"}`, Samples: logproto.Sample{Timestamp: now.Add(25 * time.Second).UnixNano(), Value: 1}},
				{Labels: `{app="bar", env="prod"}`, Samples: logproto.Sample{Timestamp: now.Add(40 * time.Second).UnixNano(), Value: 1}},

				{Labels: `{app="foo", env="dev"}`, Samples: logproto.Sample{Timestamp: now.Add(10 * time.Second).UnixNano(), Value: 1}},
				{Labels: `{app="foo", env="dev"}`, Samples: logproto.Sample{Timestamp: now.Add(20 * time.Second).UnixNano(), Value: 1}},
				{Labels: `{app="foo", env="dev"}`, Samples: logproto.Sample{Timestamp: now.Add(35 * time.Second).UnixNano(), Value: 1}},

				{Labels: `{app="foo", env="prod"}`, Samples: logproto.Sample{Timestamp: now.UnixNano(), Value: 1}},
				{Labels: `{app="foo", env="prod"}`, Samples: logproto.Sample{Timestamp: now.Add(30 * time.Second).UnixNano(), Value: 1}},
				{Labels: `{app="foo", env="prod"}`, Samples: logproto.Sample{Timestamp: now.Add(45 * time.Second).UnixNano(), Value: 1}},
				{Labels: `{app="foo", env="prod"}`, Samples: logproto.Sample{Timestamp: now.Add(50 * time.Second).UnixNano(), Value: 1}},
			},
		},
		{
			name:     "select second shard",
			selector: `rate({app=~".+"}[1h])`,
			start:    now,
			end:      now.Add(time.Hour),
			shards:   []string{"1_of_2"},
			want: []sampleWithLabels{
				{Labels: `{app="baz", env="prod", team="a"}`, Samples: logproto.Sample{Timestamp: now.Add(12 * time.Second).UnixNano(), Value: 1}},
				{Labels: `{app="baz", env="prod", team="a"}`, Samples: logproto.Sample{Timestamp: now.Add(22 * time.Second).UnixNano(), Value: 1}},
				{Labels: `{app="baz", env="prod", team="a"}`, Samples: logproto.Sample{Timestamp: now.Add(32 * time.Second).UnixNano(), Value: 1}},
				{Labels: `{app="baz", env="prod", team="a"}`, Samples: logproto.Sample{Timestamp: now.Add(42 * time.Second).UnixNano(), Value: 1}},
			},
		},
		{
			name:     "select all samples in range with a filter",
			selector: `count_over_time({app=~".+"} |= "bar2"[1h])`,
			start:    now,
			end:      now.Add(time.Hour),
			want: []sampleWithLabels{
				{Labels: `{app="bar", env="prod"}`, Samples: logproto.Sample{Timestamp: now.Add(15 * time.Second).UnixNano(), Value: 1}},
			},
		},
		{
			name:     "select all samples in range with multiple line filters",
			selector: `rate({app=~".+"} != "bar" |~ "foo[3-6]"[1h])`,
			start:    now,
			end:      now.Add(time.Hour),
			want: []sampleWithLabels{
				{Labels: `{app="foo", env="dev"}`, Samples: logproto.Sample{Timestamp: now.Add(10 * time.Second).UnixNano(), Value: 1}},
				{Labels: `{app="foo", env="dev"}`, Samples: logproto.Sample{Timestamp: now.Add(20 * time.Second).UnixNano(), Value: 1}},

				{Labels: `{app="foo", env="prod"}`, Samples: logproto.Sample{Timestamp: now.Add(45 * time.Second).UnixNano(), Value: 1}},
				{Labels: `{app="foo", env="prod"}`, Samples: logproto.Sample{Timestamp: now.Add(50 * time.Second).UnixNano(), Value: 1}},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			it, err := store.SelectSamples(ctx, logql.SelectSampleParams{
				SampleQueryRequest: &logproto.SampleQueryRequest{
					Start:    tt.start,
					End:      tt.end,
					Plan:     planFromString(tt.selector),
					Selector: tt.selector,
					Shards:   tt.shards,
				},
			})
			require.NoError(t, err)
			samples, err := readAllSamples(it)
			require.NoError(t, err)

			// Sort the output by labels and timestamp; changes to how data objects
			// are written can change the order of what we see, which is not what we
			// care to test here.
			slices.SortFunc(samples, func(a, b sampleWithLabels) int {
				res := cmp.Compare(a.Labels, b.Labels)
				if res == 0 {
					return cmp.Compare(a.Samples.Timestamp, b.Samples.Timestamp)
				}
				return res
			})

			if diff := gocmp.Diff(tt.want, samples); diff != "" {
				t.Errorf("samples mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestStore_SelectLogs(t *testing.T) {
	const testTenant = "test-tenant"
	builder := newTestDataBuilder(t, testTenant)
	defer builder.close()

	// Setup test data
	now := setupTestData(t, builder)
	meta := metastore.NewObjectMetastore(builder.bucket, log.NewNopLogger())
	store := NewStore(builder.bucket, log.NewLogfmtLogger(os.Stdout), meta)
	ctx := user.InjectOrgID(context.Background(), testTenant)

	tests := []struct {
		name      string
		selector  string
		start     time.Time
		end       time.Time
		shards    []string
		limit     uint32
		direction logproto.Direction
		want      []entryWithLabels
	}{
		{
			name:      "select all logs in range",
			selector:  `{app=~".+"}`,
			start:     now,
			end:       now.Add(time.Hour),
			limit:     100,
			direction: logproto.FORWARD,
			want: []entryWithLabels{
				{Labels: `{app="bar", env="dev"}`, Entry: logproto.Entry{Timestamp: now.Add(8 * time.Second), Line: "bar5"}},
				{Labels: `{app="bar", env="dev"}`, Entry: logproto.Entry{Timestamp: now.Add(18 * time.Second), Line: "bar6"}},
				{Labels: `{app="bar", env="dev"}`, Entry: logproto.Entry{Timestamp: now.Add(38 * time.Second), Line: "bar7"}},

				{Labels: `{app="bar", env="prod"}`, Entry: logproto.Entry{Timestamp: now.Add(5 * time.Second), Line: "bar1"}},
				{Labels: `{app="bar", env="prod"}`, Entry: logproto.Entry{Timestamp: now.Add(15 * time.Second), Line: "bar2"}},
				{Labels: `{app="bar", env="prod"}`, Entry: logproto.Entry{Timestamp: now.Add(25 * time.Second), Line: "bar3"}},
				{Labels: `{app="bar", env="prod"}`, Entry: logproto.Entry{Timestamp: now.Add(40 * time.Second), Line: "bar4"}},

				{Labels: `{app="baz", env="prod", team="a"}`, Entry: logproto.Entry{Timestamp: now.Add(12 * time.Second), Line: "baz1"}},
				{Labels: `{app="baz", env="prod", team="a"}`, Entry: logproto.Entry{Timestamp: now.Add(22 * time.Second), Line: "baz2"}},
				{Labels: `{app="baz", env="prod", team="a"}`, Entry: logproto.Entry{Timestamp: now.Add(32 * time.Second), Line: "baz3"}},
				{Labels: `{app="baz", env="prod", team="a"}`, Entry: logproto.Entry{Timestamp: now.Add(42 * time.Second), Line: "baz4"}},

				{Labels: `{app="foo", env="dev"}`, Entry: logproto.Entry{Timestamp: now.Add(10 * time.Second), Line: "foo5"}},
				{Labels: `{app="foo", env="dev"}`, Entry: logproto.Entry{Timestamp: now.Add(20 * time.Second), Line: "foo6"}},
				{Labels: `{app="foo", env="dev"}`, Entry: logproto.Entry{Timestamp: now.Add(35 * time.Second), Line: "foo7"}},

				{Labels: `{app="foo", env="prod"}`, Entry: logproto.Entry{Timestamp: now, Line: "foo1"}},
				{Labels: `{app="foo", env="prod"}`, Entry: logproto.Entry{Timestamp: now.Add(30 * time.Second), Line: "foo2"}},
				{Labels: `{app="foo", env="prod"}`, Entry: logproto.Entry{Timestamp: now.Add(45 * time.Second), Line: "foo3"}},
				{Labels: `{app="foo", env="prod"}`, Entry: logproto.Entry{Timestamp: now.Add(50 * time.Second), Line: "foo4"}},
			},
		},
		{
			name:      "select with time range filter",
			selector:  `{app="baz", env="prod", team="a"}`,
			start:     now.Add(20 * time.Second),
			end:       now.Add(40 * time.Second),
			limit:     100,
			direction: logproto.FORWARD,
			want: []entryWithLabels{
				{Labels: `{app="baz", env="prod", team="a"}`, Entry: logproto.Entry{Timestamp: now.Add(22 * time.Second), Line: "baz2"}},
				{Labels: `{app="baz", env="prod", team="a"}`, Entry: logproto.Entry{Timestamp: now.Add(32 * time.Second), Line: "baz3"}},
			},
		},
		{
			name:      "select with label matcher",
			selector:  `{app="foo"}`,
			start:     now,
			end:       now.Add(time.Hour),
			limit:     100,
			direction: logproto.FORWARD,
			want: []entryWithLabels{
				{Labels: `{app="foo", env="dev"}`, Entry: logproto.Entry{Timestamp: now.Add(10 * time.Second), Line: "foo5"}},
				{Labels: `{app="foo", env="dev"}`, Entry: logproto.Entry{Timestamp: now.Add(20 * time.Second), Line: "foo6"}},
				{Labels: `{app="foo", env="dev"}`, Entry: logproto.Entry{Timestamp: now.Add(35 * time.Second), Line: "foo7"}},

				{Labels: `{app="foo", env="prod"}`, Entry: logproto.Entry{Timestamp: now, Line: "foo1"}},
				{Labels: `{app="foo", env="prod"}`, Entry: logproto.Entry{Timestamp: now.Add(30 * time.Second), Line: "foo2"}},
				{Labels: `{app="foo", env="prod"}`, Entry: logproto.Entry{Timestamp: now.Add(45 * time.Second), Line: "foo3"}},
				{Labels: `{app="foo", env="prod"}`, Entry: logproto.Entry{Timestamp: now.Add(50 * time.Second), Line: "foo4"}},
			},
		},
		{
			name:      "select first shard",
			selector:  `{app=~".+"}`,
			start:     now,
			end:       now.Add(time.Hour),
			shards:    []string{"0_of_2"},
			limit:     100,
			direction: logproto.FORWARD,
			want: []entryWithLabels{
				{Labels: `{app="bar", env="dev"}`, Entry: logproto.Entry{Timestamp: now.Add(8 * time.Second), Line: "bar5"}},
				{Labels: `{app="bar", env="dev"}`, Entry: logproto.Entry{Timestamp: now.Add(18 * time.Second), Line: "bar6"}},
				{Labels: `{app="bar", env="dev"}`, Entry: logproto.Entry{Timestamp: now.Add(38 * time.Second), Line: "bar7"}},

				{Labels: `{app="bar", env="prod"}`, Entry: logproto.Entry{Timestamp: now.Add(5 * time.Second), Line: "bar1"}},
				{Labels: `{app="bar", env="prod"}`, Entry: logproto.Entry{Timestamp: now.Add(15 * time.Second), Line: "bar2"}},
				{Labels: `{app="bar", env="prod"}`, Entry: logproto.Entry{Timestamp: now.Add(25 * time.Second), Line: "bar3"}},
				{Labels: `{app="bar", env="prod"}`, Entry: logproto.Entry{Timestamp: now.Add(40 * time.Second), Line: "bar4"}},

				{Labels: `{app="foo", env="dev"}`, Entry: logproto.Entry{Timestamp: now.Add(10 * time.Second), Line: "foo5"}},
				{Labels: `{app="foo", env="dev"}`, Entry: logproto.Entry{Timestamp: now.Add(20 * time.Second), Line: "foo6"}},
				{Labels: `{app="foo", env="dev"}`, Entry: logproto.Entry{Timestamp: now.Add(35 * time.Second), Line: "foo7"}},

				{Labels: `{app="foo", env="prod"}`, Entry: logproto.Entry{Timestamp: now, Line: "foo1"}},
				{Labels: `{app="foo", env="prod"}`, Entry: logproto.Entry{Timestamp: now.Add(30 * time.Second), Line: "foo2"}},
				{Labels: `{app="foo", env="prod"}`, Entry: logproto.Entry{Timestamp: now.Add(45 * time.Second), Line: "foo3"}},
				{Labels: `{app="foo", env="prod"}`, Entry: logproto.Entry{Timestamp: now.Add(50 * time.Second), Line: "foo4"}},
			},
		},
		{
			name:      "select second shard",
			selector:  `{app=~".+"}`,
			start:     now,
			end:       now.Add(time.Hour),
			shards:    []string{"1_of_2"},
			limit:     100,
			direction: logproto.FORWARD,
			want: []entryWithLabels{
				{Labels: `{app="baz", env="prod", team="a"}`, Entry: logproto.Entry{Timestamp: now.Add(12 * time.Second), Line: "baz1"}},
				{Labels: `{app="baz", env="prod", team="a"}`, Entry: logproto.Entry{Timestamp: now.Add(22 * time.Second), Line: "baz2"}},
				{Labels: `{app="baz", env="prod", team="a"}`, Entry: logproto.Entry{Timestamp: now.Add(32 * time.Second), Line: "baz3"}},
				{Labels: `{app="baz", env="prod", team="a"}`, Entry: logproto.Entry{Timestamp: now.Add(42 * time.Second), Line: "baz4"}},
			},
		},
		{
			name:      "select with line filter",
			selector:  `{app=~".+"} |= "bar2"`,
			start:     now,
			end:       now.Add(time.Hour),
			limit:     100,
			direction: logproto.FORWARD,
			want: []entryWithLabels{
				{Labels: `{app="bar", env="prod"}`, Entry: logproto.Entry{Timestamp: now.Add(15 * time.Second), Line: "bar2"}},
			},
		},
		{
			name:      "select with multiple line filters",
			selector:  `{app=~".+"} != "bar" |~ "foo[3-6]"`,
			start:     now,
			end:       now.Add(time.Hour),
			limit:     100,
			direction: logproto.FORWARD,
			want: []entryWithLabels{
				{Labels: `{app="foo", env="dev"}`, Entry: logproto.Entry{Timestamp: now.Add(10 * time.Second), Line: "foo5"}},
				{Labels: `{app="foo", env="dev"}`, Entry: logproto.Entry{Timestamp: now.Add(20 * time.Second), Line: "foo6"}},

				{Labels: `{app="foo", env="prod"}`, Entry: logproto.Entry{Timestamp: now.Add(45 * time.Second), Line: "foo3"}},
				{Labels: `{app="foo", env="prod"}`, Entry: logproto.Entry{Timestamp: now.Add(50 * time.Second), Line: "foo4"}},
			},
		},
	}

	emptyLabelAdapters := logproto.EmptyLabelAdapters()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Make sure all empty label sets use an empty slice, rather than nil, to make assertions below easier.
			for i := range tt.want {
				if len(tt.want[i].Entry.Parsed) == 0 {
					tt.want[i].Entry.Parsed = emptyLabelAdapters
				}

				if len(tt.want[i].Entry.StructuredMetadata) == 0 {
					tt.want[i].Entry.StructuredMetadata = emptyLabelAdapters
				}
			}

			it, err := store.SelectLogs(ctx, logql.SelectLogParams{
				QueryRequest: &logproto.QueryRequest{
					Start:     tt.start,
					End:       tt.end,
					Plan:      planFromString(tt.selector),
					Selector:  tt.selector,
					Shards:    tt.shards,
					Limit:     tt.limit,
					Direction: tt.direction,
				},
			})
			require.NoError(t, err)
			entries, err := readAllEntries(it)
			require.NoError(t, err)

			// Sort the output by labels; changes to how data objects are written can
			// change the order of what we see, which is not what we care to test
			// here.
			slices.SortFunc(entries, func(a, b entryWithLabels) int {
				res := cmp.Compare(a.Labels, b.Labels)
				if res == 0 {
					return a.Entry.Timestamp.Compare(b.Entry.Timestamp)
				}
				return res
			})

			if diff := gocmp.Diff(tt.want, entries); diff != "" {
				t.Errorf("entries mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func setupTestData(t *testing.T, builder *testDataBuilder) time.Time {
	t.Helper()
	now := time.Unix(0, int64(time.Hour)).UTC()

	// Data before the query range (should not be included in results)
	builder.addStream(
		`{app="foo", env="prod"}`,
		logproto.Entry{Timestamp: now.Add(-2 * time.Hour), Line: "foo_before1"},
		logproto.Entry{Timestamp: now.Add(-2 * time.Hour).Add(30 * time.Second), Line: "foo_before2"},
		logproto.Entry{Timestamp: now.Add(-2 * time.Hour).Add(45 * time.Second), Line: "foo_before3"},
	)
	builder.flush()

	// Data within query range
	builder.addStream(
		`{app="foo", env="prod"}`,
		logproto.Entry{Timestamp: now, Line: "foo1"},
		logproto.Entry{Timestamp: now.Add(30 * time.Second), Line: "foo2"},
		logproto.Entry{Timestamp: now.Add(45 * time.Second), Line: "foo3"},
		logproto.Entry{Timestamp: now.Add(50 * time.Second), Line: "foo4"},
	)
	builder.addStream(
		`{app="foo", env="dev"}`,
		logproto.Entry{Timestamp: now.Add(10 * time.Second), Line: "foo5"},
		logproto.Entry{Timestamp: now.Add(20 * time.Second), Line: "foo6"},
		logproto.Entry{Timestamp: now.Add(35 * time.Second), Line: "foo7"},
	)
	builder.flush()

	builder.addStream(
		`{app="bar", env="prod"}`,
		logproto.Entry{Timestamp: now.Add(5 * time.Second), Line: "bar1"},
		logproto.Entry{Timestamp: now.Add(15 * time.Second), Line: "bar2"},
		logproto.Entry{Timestamp: now.Add(25 * time.Second), Line: "bar3"},
		logproto.Entry{Timestamp: now.Add(40 * time.Second), Line: "bar4"},
	)
	builder.addStream(
		`{app="bar", env="dev"}`,
		logproto.Entry{Timestamp: now.Add(8 * time.Second), Line: "bar5"},
		logproto.Entry{Timestamp: now.Add(18 * time.Second), Line: "bar6"},
		logproto.Entry{Timestamp: now.Add(38 * time.Second), Line: "bar7"},
	)
	builder.flush()

	builder.addStream(
		`{app="baz", env="prod", team="a"}`,
		logproto.Entry{Timestamp: now.Add(12 * time.Second), Line: "baz1"},
		logproto.Entry{Timestamp: now.Add(22 * time.Second), Line: "baz2"},
		logproto.Entry{Timestamp: now.Add(32 * time.Second), Line: "baz3"},
		logproto.Entry{Timestamp: now.Add(42 * time.Second), Line: "baz4"},
	)
	builder.flush()

	// Data after the query range (should not be included in results)
	builder.addStream(
		`{app="foo", env="prod"}`,
		logproto.Entry{Timestamp: now.Add(2 * time.Hour), Line: "foo_after1"},
		logproto.Entry{Timestamp: now.Add(2 * time.Hour).Add(30 * time.Second), Line: "foo_after2"},
		logproto.Entry{Timestamp: now.Add(2 * time.Hour).Add(45 * time.Second), Line: "foo_after3"},
	)
	builder.flush()

	return now
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
	builder  *logsobj.Builder
	meta     *metastore.Updater
	uploader *uploader.Uploader
}

func newTestDataBuilder(t *testing.T, tenantID string) *testDataBuilder {
	dir := t.TempDir()
	bucket, err := filesystem.NewBucket(dir)
	require.NoError(t, err)

	// Create required directories for metastore
	metastoreDir := filepath.Join(dir, "tenant-"+tenantID, "metastore")
	require.NoError(t, os.MkdirAll(metastoreDir, 0o755))

	builder, err := logsobj.NewBuilder(logsobj.BuilderConfig{
		TargetPageSize:    1024 * 1024,      // 1MB
		TargetObjectSize:  10 * 1024 * 1024, // 10MB
		TargetSectionSize: 1024 * 1024,      // 1MB
		BufferSize:        1024 * 1024,      // 1MB

		SectionStripeMergeLimit: 2,
	})
	require.NoError(t, err)

	meta := metastore.NewUpdater(bucket, tenantID, log.NewNopLogger())
	require.NoError(t, meta.RegisterMetrics(prometheus.NewRegistry()))

	uploader := uploader.New(uploader.Config{SHAPrefixSize: 2}, bucket, tenantID, log.NewNopLogger())
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
	err = b.meta.Update(context.Background(), path, stats.MinTimestamp, stats.MaxTimestamp)
	require.NoError(b.t, err)

	b.builder.Reset()
}

func (b *testDataBuilder) close() {
	require.NoError(b.t, b.bucket.Close())
	os.RemoveAll(b.dir)
}

// Helper function to read all samples from an iterator
func readAllSamples(it iter.SampleIterator) ([]sampleWithLabels, error) {
	var result []sampleWithLabels
	defer it.Close()
	for it.Next() {
		sample := it.At()
		result = append(result, sampleWithLabels{
			Labels:  it.Labels(),
			Samples: sample,
		})
	}
	return result, it.Err()
}

// Helper function to read all entries from an iterator
func readAllEntries(it iter.EntryIterator) ([]entryWithLabels, error) {
	var result []entryWithLabels
	defer it.Close()
	for it.Next() {
		result = append(result, entryWithLabels{
			Labels: it.Labels(),
			Entry:  it.At(),
		})
	}
	return result, it.Err()
}

func TestShardSections(t *testing.T) {
	tests := []struct {
		name      string
		metadatas []sectionsStats
		shard     logql.Shard
		want      [][]int
	}{
		{
			name: "single section, no sharding",
			metadatas: []sectionsStats{
				{LogsSections: 1},
			},
			shard: logql.Shard{
				PowerOfTwo: &index.ShardAnnotation{
					Shard: 0,
					Of:    1,
				},
			},
			want: [][]int{
				{0},
			},
		},
		{
			name: "multiple sections, no sharding",
			metadatas: []sectionsStats{
				{LogsSections: 3},
				{LogsSections: 2},
			},
			shard: logql.Shard{
				PowerOfTwo: &index.ShardAnnotation{
					Shard: 0,
					Of:    1,
				},
			},
			want: [][]int{
				{0, 1, 2},
				{0, 1},
			},
		},
		{
			name: "multiple sections, shard 0 of 2",
			metadatas: []sectionsStats{
				{LogsSections: 3},
				{LogsSections: 2},
			},
			shard: logql.Shard{
				PowerOfTwo: &index.ShardAnnotation{
					Shard: 0,
					Of:    2,
				},
			},
			want: [][]int{
				{0, 2},
				{1},
			},
		},
		{
			name: "multiple sections, shard 1 of 2",
			metadatas: []sectionsStats{
				{LogsSections: 3},
				{LogsSections: 2},
			},
			shard: logql.Shard{
				PowerOfTwo: &index.ShardAnnotation{
					Shard: 1,
					Of:    2,
				},
			},
			want: [][]int{
				{1},
				{0},
			},
		},
		{
			name: "more sections than shards, shard 0 of 2",
			metadatas: []sectionsStats{
				{LogsSections: 5},
			},
			shard: logql.Shard{
				PowerOfTwo: &index.ShardAnnotation{
					Shard: 0,
					Of:    2,
				},
			},
			want: [][]int{
				{0, 2, 4},
			},
		},
		{
			name: "more sections than shards, shard 1 of 2",
			metadatas: []sectionsStats{
				{LogsSections: 5},
			},
			shard: logql.Shard{
				PowerOfTwo: &index.ShardAnnotation{
					Shard: 1,
					Of:    2,
				},
			},
			want: [][]int{
				{1, 3},
			},
		},
		{
			name: "complex case, shard 0 of 4",
			metadatas: []sectionsStats{
				{LogsSections: 7}, // sections 0,4
				{LogsSections: 5}, // sections 0,4
				{LogsSections: 3}, // sections 0
			},
			shard: logql.Shard{
				PowerOfTwo: &index.ShardAnnotation{
					Shard: 0,
					Of:    4,
				},
			},
			want: [][]int{
				{0, 4},
				{1},
				{0},
			},
		},
		{
			name: "complex case, shard 1 of 4",
			metadatas: []sectionsStats{
				{LogsSections: 7}, // sections 1,5
				{LogsSections: 5}, // sections 1
				{LogsSections: 3}, // sections 1
			},
			shard: logql.Shard{
				PowerOfTwo: &index.ShardAnnotation{
					Shard: 1,
					Of:    4,
				},
			},
			want: [][]int{
				{1, 5},
				{2},
				{1},
			},
		},
		{
			name: "complex case, shard 2 of 4",
			metadatas: []sectionsStats{
				{LogsSections: 7}, // sections 2,6
				{LogsSections: 5}, // sections 2
				{LogsSections: 3}, // sections 2
			},
			shard: logql.Shard{
				PowerOfTwo: &index.ShardAnnotation{
					Shard: 2,
					Of:    4,
				},
			},
			want: [][]int{
				{2, 6},
				{3},
				{2},
			},
		},
		{
			name: "complex case, shard 3 of 4",
			metadatas: []sectionsStats{
				{LogsSections: 7}, // sections 3
				{LogsSections: 5}, // sections 3
				{LogsSections: 3}, // no sections
			},
			shard: logql.Shard{
				PowerOfTwo: &index.ShardAnnotation{
					Shard: 3,
					Of:    4,
				},
			},
			want: [][]int{
				{3},
				{0, 4},
				{},
			},
		},
		{
			name: "less sections than shards, shard 1 of 4",
			metadatas: []sectionsStats{
				{LogsSections: 1},
			},
			shard: logql.Shard{
				PowerOfTwo: &index.ShardAnnotation{
					Shard: 1,
					Of:    4,
				},
			},
			want: [][]int{{}},
		},
		{
			name: "less sections than shards, shard 0 of 4",
			metadatas: []sectionsStats{
				{LogsSections: 1},
			},
			shard: logql.Shard{
				PowerOfTwo: &index.ShardAnnotation{
					Shard: 0,
					Of:    4,
				},
			},
			want: [][]int{{0}},
		},
		{
			name: "multiple streams sections not supported",
			metadatas: []sectionsStats{
				{LogsSections: 1, StreamsSections: 2},
			},
			shard: logql.Shard{
				PowerOfTwo: &index.ShardAnnotation{
					Shard: 0,
					Of:    1,
				},
			},
			want: [][]int{nil},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shardSections(tt.metadatas, tt.shard)
			require.Equal(t, tt.want, got)

			// For test cases with multiple shards, verify that all sections are covered exactly once
			if tt.shard.PowerOfTwo != nil && tt.shard.PowerOfTwo.Of > 1 {
				// Skip verification for invalid metadata
				for _, meta := range tt.metadatas {
					if meta.StreamsSections > 1 {
						return
					}
				}

				// Track which sections are assigned
				type sectionKey struct {
					metaIdx int
					section int
				}
				sectionCounts := make(map[sectionKey]int)

				// Count sections from all shards
				for shardIdx := uint32(0); shardIdx < tt.shard.PowerOfTwo.Of; shardIdx++ {
					shard := logql.Shard{
						PowerOfTwo: &index.ShardAnnotation{
							Shard: shardIdx,
							Of:    tt.shard.PowerOfTwo.Of,
						},
					}
					sections := shardSections(tt.metadatas, shard)
					for metaIdx, metaSections := range sections {
						for _, section := range metaSections {
							key := sectionKey{metaIdx: metaIdx, section: section}
							sectionCounts[key]++
						}
					}
				}

				// Verify each section is assigned exactly once
				for metaIdx, meta := range tt.metadatas {
					for section := range meta.LogsSections {
						key := sectionKey{metaIdx: metaIdx, section: section}
						count := sectionCounts[key]
						require.Equal(t, 1, count, "section %d in metadata %d was assigned %d times", section, metaIdx, count)
					}
				}
			}
		})
	}
}

func TestBuildLogsPredicateFromPipeline(t *testing.T) {
	var evalPredicate func(p logs.RowPredicate, item []byte) bool
	evalPredicate = func(p logs.RowPredicate, line []byte) bool {
		switch p := p.(type) {
		case logs.LogMessageFilterRowPredicate:
			return p.Keep(line)
		case logs.AndRowPredicate:
			return evalPredicate(p.Left, line) && evalPredicate(p.Right, line)
		default:
			t.Fatalf("unsupported predicate type: %T", p)
			return false
		}
	}

	// helper function to test predicates against sample data
	testPredicate := func(t *testing.T, pred logs.RowPredicate, testData [][]byte, expected []bool) {
		t.Helper()
		require.Equal(t, len(testData), len(expected), "test data and expected results must have the same length")

		for i, line := range testData {
			result := evalPredicate(pred, line)
			require.Equal(t, expected[i], result, "predicate mismatch for line: %s", line)
		}
	}

	// Create test data sets
	testData := [][]byte{
		[]byte("this is an error message"),
		[]byte("this is a critical error"),
		[]byte("this is a success message"),
		[]byte("this is a warning message"),
	}

	for _, tt := range []struct {
		name         string
		query        syntax.LogSelectorExpr
		expectedExpr syntax.LogSelectorExpr
		testFunc     func(t *testing.T, pred logs.RowPredicate)
	}{
		{
			name:         "single line match equal filter",
			query:        mustParseLogSelector(t, `{app="foo"} |= "error"`),
			expectedExpr: mustParseLogSelector(t, `{app="foo"}`), // Line filter should be removed
			testFunc: func(t *testing.T, pred logs.RowPredicate) {
				require.NotNil(t, pred)
				require.IsType(t, logs.LogMessageFilterRowPredicate{}, pred)

				// Verify the predicate works correctly
				expected := []bool{true, true, false, false}
				testPredicate(t, pred, testData, expected)
			},
		},
		{
			name:         "single line match not equal filter",
			query:        mustParseLogSelector(t, `{app="foo"} != "error"`),
			expectedExpr: mustParseLogSelector(t, `{app="foo"}`), // Line filter should be removed
			testFunc: func(t *testing.T, pred logs.RowPredicate) {
				require.NotNil(t, pred)
				require.IsType(t, logs.LogMessageFilterRowPredicate{}, pred)

				// Verify the predicate works correctly
				expected := []bool{false, false, true, true}
				testPredicate(t, pred, testData, expected)
			},
		},
		{
			name:         "multiple line filters",
			query:        mustParseLogSelector(t, `{app="foo"} |= "error" |= "critical"`),
			expectedExpr: mustParseLogSelector(t, `{app="foo"}`), // Both line filters should be removed
			testFunc: func(t *testing.T, pred logs.RowPredicate) {
				require.NotNil(t, pred)
				// we might expect AND predicate here, but the original expression
				// only contains a single Filterer stage with chained OR filters
				require.IsType(t, logs.LogMessageFilterRowPredicate{}, pred)

				// The result should match logs containing both "error" and "critical"
				expected := []bool{false, true, false, false}
				testPredicate(t, pred, testData, expected)
			},
		},
		{
			name:         "line filter stage after line_format",
			query:        mustParseLogSelector(t, `{app="foo"} | line_format "{{.message}}" |= "error"`),
			expectedExpr: mustParseLogSelector(t, `{app="foo"} | line_format "{{.message}}" |= "error"`), // No filters should be removed
			testFunc: func(t *testing.T, pred logs.RowPredicate) {
				// Line filters after stages that mutate the line should be ignored
				require.Nil(t, pred, "expected nil predicate for line filter after parser")
			},
		},
		{
			name:         "no line filter",
			query:        mustParseLogSelector(t, `{app="foo"} | json | bar>100`),
			expectedExpr: mustParseLogSelector(t, `{app="foo"} | json | bar>100`), // No filters to remove
			testFunc: func(t *testing.T, pred logs.RowPredicate) {
				require.Nil(t, pred, "expected nil predicate for query without line filter")
			},
		},
		{
			name:         "line filter stage after label_fmt",
			query:        mustParseLogSelector(t, `{app="foo"} | label_format foo=bar |= "error"`),
			expectedExpr: mustParseLogSelector(t, `{app="foo"} | label_format foo=bar`), // Line filter should be removed
			testFunc: func(t *testing.T, pred logs.RowPredicate) {
				require.NotNil(t, pred)
				require.IsType(t, logs.LogMessageFilterRowPredicate{}, pred)

				// Verify the predicate works correctly
				expected := []bool{true, true, false, false}
				testPredicate(t, pred, testData, expected)
			},
		},
		{
			name:         "mixed filters with some removable",
			query:        mustParseLogSelector(t, `{app="foo"} |= "error" | json | status="critical" |= "critical"`),
			expectedExpr: mustParseLogSelector(t, `{app="foo"} | json | status="critical"`),
			testFunc: func(t *testing.T, pred logs.RowPredicate) {
				require.NotNil(t, pred)
				require.IsType(t, logs.AndRowPredicate{}, pred)
				expected := []bool{false, true, false, false}
				testPredicate(t, pred, testData, expected)
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			p, actualExpr := buildLogsPredicateFromPipeline(tt.query)

			tt.testFunc(t, p)

			// Verify the updated expression
			syntax.AssertExpressions(t, tt.expectedExpr, actualExpr)
		})
	}
}

func TestBuildLogsPredicateFromSampleExpr(t *testing.T) {
	for _, tt := range []struct {
		name         string
		query        syntax.SampleExpr
		expectedExpr syntax.SampleExpr
		testFunc     func(t *testing.T, pred logs.RowPredicate)
	}{
		{
			name:         "range aggregation with line filter",
			query:        mustParseSampleExpr(t, `count_over_time({app="foo"} |= "error" [5m])`),
			expectedExpr: mustParseSampleExpr(t, `count_over_time({app="foo"}[5m])`),
			testFunc: func(t *testing.T, p logs.RowPredicate) {
				require.NotNil(t, p)
				require.IsType(t, logs.LogMessageFilterRowPredicate{}, p)
			},
		},
		{
			name:         "vector aggregation with line filter",
			query:        mustParseSampleExpr(t, `sum by (app)(count_over_time({app="foo"} |= "error"[5m]))`),
			expectedExpr: mustParseSampleExpr(t, `sum by (app)(count_over_time({app="foo"}[5m]))`),
			testFunc: func(t *testing.T, p logs.RowPredicate) {
				require.NotNil(t, p)
				require.IsType(t, logs.LogMessageFilterRowPredicate{}, p)
			},
		},
		{
			name:         "binary expressions are not modified",
			query:        mustParseSampleExpr(t, `(count_over_time({app="foo"} |= "error"[5m]) + count_over_time({app="bar"} |= "info"[5m]))`),
			expectedExpr: mustParseSampleExpr(t, `(count_over_time({app="foo"} |= "error"[5m]) + count_over_time({app="bar"} |= "info"[5m]))`),
			testFunc: func(t *testing.T, p logs.RowPredicate) {
				require.Nil(t, p)
			},
		},
		{
			name:         "label replace with line filter",
			query:        mustParseSampleExpr(t, `label_replace(rate({app="foo"} |= "error"[5m]), "new_label", "new_value", "old_label", "old_value")`),
			expectedExpr: mustParseSampleExpr(t, `label_replace(rate({app="foo"}[5m]),"new_label","new_value","old_label","old_value")`),
			testFunc: func(t *testing.T, p logs.RowPredicate) {
				require.NotNil(t, p)
				require.IsType(t, logs.LogMessageFilterRowPredicate{}, p)
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			p, actualExpr := buildLogsPredicateFromSampleExpr(tt.query)

			tt.testFunc(t, p)

			// Verify the updated expression
			syntax.AssertExpressions(t, tt.expectedExpr, actualExpr)
		})
	}
}

// Helper function to parse log selectors for test cases
func mustParseLogSelector(t *testing.T, s string) syntax.LogSelectorExpr {
	expr, err := syntax.ParseLogSelector(s, false)
	if err != nil {
		t.Fatalf("failed to parse log selector %q: %v", s, err)
	}
	return expr
}

// Helper function to parse sample expressions for test cases
func mustParseSampleExpr(t *testing.T, s string) syntax.SampleExpr {
	expr, err := syntax.ParseSampleExpr(s)
	if err != nil {
		t.Fatalf("failed to parse sample expr %q: %v", s, err)
	}
	return expr
}
