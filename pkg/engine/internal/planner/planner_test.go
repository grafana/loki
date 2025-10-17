package planner

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
	"github.com/grafana/loki/v3/pkg/engine/internal/planner/logical"
	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/storage/chunk/cache/resultscache"
)

type TestQuery struct {
	statement      string
	start, end     time.Time
	step, interval time.Duration
	direction      logproto.Direction
	limit          uint32
}

// Direction implements logql.Params.
func (q *TestQuery) Direction() logproto.Direction {
	return q.direction
}

// End implements logql.Params.
func (q *TestQuery) End() time.Time {
	return q.end
}

// Start implements logql.Params.
func (q *TestQuery) Start() time.Time {
	return q.start
}

// Limit implements logql.Params.
func (q *TestQuery) Limit() uint32 {
	return q.limit
}

// QueryString implements logql.Params.
func (q *TestQuery) QueryString() string {
	return q.statement
}

// GetExpression implements logql.Params.
func (q *TestQuery) GetExpression() syntax.Expr {
	return syntax.MustParseExpr(q.statement)
}

// CachingOptions implements logql.Params.
func (q *TestQuery) CachingOptions() resultscache.CachingOptions {
	panic("unimplemented")
}

// GetStoreChunks implements logql.Params.
func (q *TestQuery) GetStoreChunks() *logproto.ChunkRefGroup {
	panic("unimplemented")
}

// Interval implements logql.Params.
func (q *TestQuery) Interval() time.Duration {
	panic("unimplemented")
}

// Shards implements logql.Params.
func (q *TestQuery) Shards() []string {
	return []string{"0_of_1"} // 0_of_1 == noShard
}

// Step implements logql.Params.
func (q *TestQuery) Step() time.Duration {
	return q.step
}

var _ logql.Params = (*TestQuery)(nil)

type TestMetastore struct{}

// DataObjects implements metastore.Metastore.
func (t *TestMetastore) DataObjects(_ context.Context, _ time.Time, _ time.Time, _ ...*labels.Matcher) ([]string, error) {
	panic("unimplemented")
}

// Labels implements metastore.Metastore.
func (t *TestMetastore) Labels(_ context.Context, _ time.Time, _ time.Time, _ ...*labels.Matcher) ([]string, error) {
	panic("unimplemented")
}

// Sections implements metastore.Metastore.
func (t *TestMetastore) Sections(_ context.Context, start time.Time, end time.Time, _ []*labels.Matcher, _ []*labels.Matcher) ([]*metastore.DataobjSectionDescriptor, error) {
	dur := end.Sub(start)
	return []*metastore.DataobjSectionDescriptor{
		{
			SectionKey: metastore.SectionKey{
				ObjectPath: "objects/00/0000000000.dataobj",
				SectionIdx: 0,
			},
			StreamIDs: []int64{1, 3, 5, 7, 9},
			RowCount:  1000,
			Size:      1 << 10,
			Start:     start,
			End:       end.Add(dur / -2),
		},
		{
			SectionKey: metastore.SectionKey{
				ObjectPath: "objects/00/0000000000.dataobj",
				SectionIdx: 1,
			},
			StreamIDs: []int64{1, 3, 5, 7, 9},
			RowCount:  1000,
			Size:      1 << 10,
			Start:     end.Add(dur / -2),
			End:       end,
		},
	}, nil
}

// StreamIDs implements metastore.Metastore.
func (t *TestMetastore) StreamIDs(_ context.Context, _ time.Time, _ time.Time, _ ...*labels.Matcher) ([]string, [][]int64, []int, error) {
	panic("unimplemented")
}

// Streams implements metastore.Metastore.
func (t *TestMetastore) Streams(_ context.Context, _ time.Time, _ time.Time, _ ...*labels.Matcher) ([]*labels.Labels, error) {
	panic("unimplemented")
}

// Values implements metastore.Metastore.
func (t *TestMetastore) Values(_ context.Context, _ time.Time, _ time.Time, _ ...*labels.Matcher) ([]string, error) {
	panic("unimplemented")
}

var _ metastore.Metastore = (*TestMetastore)(nil)

func TestFullQueryPlanning(t *testing.T) {
	metastore := &TestMetastore{}
	testCases := []struct {
		comment  string
		query    string
		expected string
	}{
		{
			comment: "limited query",
			query:   `{app="foo"}`,
			expected: `
Limit offset=0 limit=1000
└── TopK sort_by=builtin.timestamp ascending=false nulls_first=false k=1000
    └── Parallelize
        └── TopK sort_by=builtin.timestamp ascending=false nulls_first=false k=1000
            └── Compat src=metadata dst=metadata collision=label
                └── ScanSet num_targets=2 predicate[0]=GTE(builtin.timestamp, 2025-01-01T00:00:00Z) predicate[1]=LT(builtin.timestamp, 2025-01-01T01:00:00Z)
                        ├── @target type=ScanTypeDataObject location=objects/00/0000000000.dataobj streams=5 section_id=1 projections=()
                        └── @target type=ScanTypeDataObject location=objects/00/0000000000.dataobj streams=5 section_id=0 projections=()
				`,
		},
		{
			comment: "filter query",
			query:   `{app="foo"} | label_foo="bar" |= "baz"`,
			expected: `
Limit offset=0 limit=1000
└── TopK sort_by=builtin.timestamp ascending=false nulls_first=false k=1000
    └── Parallelize
        └── TopK sort_by=builtin.timestamp ascending=false nulls_first=false k=1000
            └── Filter predicate[0]=EQ(ambiguous.label_foo, "bar")
                └── Compat src=metadata dst=metadata collision=label
                    └── ScanSet num_targets=2 predicate[0]=GTE(builtin.timestamp, 2025-01-01T00:00:00Z) predicate[1]=LT(builtin.timestamp, 2025-01-01T01:00:00Z) predicate[2]=MATCH_STR(builtin.message, "baz")
                            ├── @target type=ScanTypeDataObject location=objects/00/0000000000.dataobj streams=5 section_id=1 projections=()
                            └── @target type=ScanTypeDataObject location=objects/00/0000000000.dataobj streams=5 section_id=0 projections=()
	`,
		},
		{
			comment: "drop columns",
			query:   `{app="foo"} | drop service_name,__error__`,
			expected: `
Limit offset=0 limit=1000
└── TopK sort_by=builtin.timestamp ascending=false nulls_first=false k=1000
    └── Parallelize
        └── TopK sort_by=builtin.timestamp ascending=false nulls_first=false k=1000
            └── Projection all=true drop=(ambiguous.service_name, ambiguous.__error__)
                └── Compat src=metadata dst=metadata collision=label
                    └── ScanSet num_targets=2 predicate[0]=GTE(builtin.timestamp, 2025-01-01T00:00:00Z) predicate[1]=LT(builtin.timestamp, 2025-01-01T01:00:00Z)
                            ├── @target type=ScanTypeDataObject location=objects/00/0000000000.dataobj streams=5 section_id=1 projections=()
                            └── @target type=ScanTypeDataObject location=objects/00/0000000000.dataobj streams=5 section_id=0 projections=()
						`,
		},
		{
			comment: "unwrap",
			query:   `sum by (bar) (sum_over_time({app="foo"} | unwrap duration(request_duration)[1m]))`,
			expected: `
VectorAggregation
└── RangeAggregation operation=sum start=2025-01-01T00:00:00Z end=2025-01-01T01:00:00Z step=0s range=1m0s partition_by=(ambiguous.bar)
    └── Parallelize
        └── Projection all=true expand=(CAST_DURATION(ambiguous.request_duration))
            └── Compat src=metadata dst=metadata collision=label
                └── ScanSet num_targets=2 predicate[0]=GTE(builtin.timestamp, 2024-12-31T23:59:00Z) predicate[1]=LT(builtin.timestamp, 2025-01-01T01:00:00Z)
                        ├── @target type=ScanTypeDataObject location=objects/00/0000000000.dataobj streams=5 section_id=1 projections=()
                        └── @target type=ScanTypeDataObject location=objects/00/0000000000.dataobj streams=5 section_id=0 projections=()
						`,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.query, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			t.Cleanup(cancel)

			q := &TestQuery{
				statement: tc.query,
				start:     time.Date(2025, time.January, 1, 0, 0, 0, 0, time.UTC),
				end:       time.Date(2025, time.January, 1, 1, 0, 0, 0, time.UTC),
				interval:  5 * time.Minute,
				limit:     1000,
				direction: logproto.BACKWARD,
			}

			logicalPlan, err := logical.BuildPlan(q)
			require.NoError(t, err)

			catalog := physical.NewMetastoreCatalog(ctx, metastore)
			planner := physical.NewPlanner(physical.NewContext(q.Start(), q.End()), catalog)

			plan, err := planner.Build(logicalPlan)
			require.NoError(t, err)
			plan, err = planner.Optimize(plan)
			require.NoError(t, err)

			actual := physical.PrintAsTree(plan)
			require.Equal(t, strings.TrimSpace(tc.expected), strings.TrimSpace(actual))
		})
	}
}
