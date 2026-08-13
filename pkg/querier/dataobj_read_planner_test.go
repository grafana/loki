package querier

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/grafana/dskit/user"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"
	"github.com/thanos-io/objstore"

	"github.com/grafana/loki/pkg/push"

	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/logs"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
)

func TestPlanProjectionsAndPredicates(t *testing.T) {
	var (
		sid = logs.ColumnTypeStreamID
		ts  = logs.ColumnTypeTimestamp
	)

	// Pushed predicates are described by a stable string: an equality is page-prunable
	// (MetadataMatcherRowPredicate), any other matcher is a MetadataFilterRowPredicate whose closure is
	// not comparable, so it is identified by its key alone.
	eq := func(key, value string) string { return "eq:" + key + "=" + value }
	filter := func(key string) string { return "filter:" + key }
	predDesc := func(p logs.RowPredicate) string {
		switch pp := p.(type) {
		case logs.MetadataMatcherRowPredicate:
			return eq(pp.Key, pp.Value)
		case logs.MetadataFilterRowPredicate:
			return filter(pp.Key)
		default:
			return fmt.Sprintf("%T", p)
		}
	}

	cases := map[string]struct {
		query string
		// streamLabels are the stream-label names seen in any of the matched streams.
		streamLabels   []string
		wantColumns    []logs.ColumnType
		wantMetadata   []string // projected metadata column names; empty means "all metadata" (ColumnTypeMetadata in columns)
		wantPredicates []string
	}{
		// Bare range aggregation: output carries the full label set, so all metadata is projected.
		"bare count reads all metadata": {
			query:        `count_over_time({app="x"}[5m])`,
			streamLabels: []string{"app"},
			wantColumns:  []logs.ColumnType{sid, ts, logs.ColumnTypeMetadata},
		},
		"bare bytes reads all metadata and the line": {
			query:        `bytes_over_time({app="x"}[5m])`,
			streamLabels: []string{"app"},
			wantColumns:  []logs.ColumnType{sid, ts, logs.ColumnTypeMetadata, logs.ColumnTypeMessage},
		},
		// sum dismisses every label, so only the metadata a filter/unwrap references is projected.
		"sum with a metadata equality projects and page-prunes that key": {
			query:          `sum(count_over_time({app="x"} | trace_id="t"[5m]))`,
			streamLabels:   []string{"app"},
			wantColumns:    []logs.ColumnType{sid, ts},
			wantMetadata:   []string{"trace_id"},
			wantPredicates: []string{eq("trace_id", "t")},
		},
		// The outer sum reduces the output to {}, so no metadata surfaces and none is projected — unlike
		// the bare count_over_time above, which reads all metadata.
		"sum of a bare count_over_time projects no metadata": {
			query:        `sum(count_over_time({app="x"}[5m]))`,
			streamLabels: []string{"app"},
			wantColumns:  []logs.ColumnType{sid, ts},
		},
		"sum by a metadata key projects the grouped and referenced keys": {
			query:          `sum by (pod) (count_over_time({app="x"} | trace_id="t"[5m]))`,
			streamLabels:   []string{"app"},
			wantColumns:    []logs.ColumnType{sid, ts},
			wantMetadata:   []string{"pod", "trace_id"},
			wantPredicates: []string{eq("trace_id", "t")},
		},
		"sum by a stream label projects that key (a harmless no-op column)": {
			query:        `sum by (namespace) (count_over_time({app="x"}[5m]))`,
			streamLabels: []string{"app", "namespace"},
			wantColumns:  []logs.ColumnType{sid, ts},
			wantMetadata: []string{"namespace"},
		},
		// `without` keeps arbitrary labels, so all metadata is projected.
		"sum without reads all metadata": {
			query:        `sum without (pod) (count_over_time({app="x"}[5m]))`,
			streamLabels: []string{"app"},
			wantColumns:  []logs.ColumnType{sid, ts, logs.ColumnTypeMetadata},
		},
		// sum of a non-additive range op still reduces to {} via the extractor, so a subset is safe.
		"sum of max_over_time projects only the unwrap key": {
			query:        `sum(max_over_time({app="x"} | unwrap duration[5m]))`,
			streamLabels: []string{"app"},
			wantColumns:  []logs.ColumnType{sid, ts},
			wantMetadata: []string{"duration"},
		},
		"sum with a line filter needs the line but a metadata subset": {
			query:          `sum(count_over_time({app="x"} |= "boom" | trace_id="t"[5m]))`,
			streamLabels:   []string{"app"},
			wantColumns:    []logs.ColumnType{sid, ts, logs.ColumnTypeMessage},
			wantMetadata:   []string{"trace_id"},
			wantPredicates: []string{eq("trace_id", "t")},
		},
		// A key that is a stream label for the matched streams is not pushed down; it is still projected.
		"stream-label equality under sum is not pushed down": {
			query:        `sum(count_over_time({app="x"} | cluster="y"[5m]))`,
			streamLabels: []string{"app", "cluster"},
			wantColumns:  []logs.ColumnType{sid, ts},
			wantMetadata: []string{"cluster"},
		},
		// Ambiguous name: cluster is a stream label here but could be metadata elsewhere. Projected,
		// not pushed down.
		"ambiguous name under sum is projected, not pushed down": {
			query:        `sum(count_over_time({app="x"} | pod="p"[5m]))`,
			streamLabels: []string{"app", "pod"},
			wantColumns:  []logs.ColumnType{sid, ts},
			wantMetadata: []string{"pod"},
		},
		"an AND of metadata equalities under sum pushes both down": {
			query:          `sum(count_over_time({app="x"} | trace_id="t" and span_id="s"[5m]))`,
			streamLabels:   []string{"app"},
			wantColumns:    []logs.ColumnType{sid, ts},
			wantMetadata:   []string{"span_id", "trace_id"},
			wantPredicates: []string{eq("trace_id", "t"), eq("span_id", "s")},
		},
		// A parser derives labels from the line: read all metadata and the line, push nothing down.
		"a parser reads all metadata and the line": {
			query:        `sum(count_over_time({app="x"} | logfmt | code="500"[5m]))`,
			streamLabels: []string{"app"},
			wantColumns:  []logs.ColumnType{sid, ts, logs.ColumnTypeMetadata, logs.ColumnTypeMessage},
		},
		// A matcher in the stream selector `{}` is a stream-label matcher for section resolution, not a
		// pipeline filter. planProjectionsAndPredicates never inspects the selector, so a metadata key placed there
		// is neither projected nor pushed down; metadata filtering must be in the pipeline.
		"a metadata matcher in the stream selector is ignored": {
			query:        `sum(count_over_time({app="x", pod="p"}[5m]))`,
			streamLabels: []string{"app"},
			wantColumns:  []logs.ColumnType{sid, ts},
		},
		// line_format and label_format build values from a template whose fields cannot be enumerated
		// statically (stream labels, any metadata key, or parsed fields), so they read all metadata and
		// the line and push nothing down.
		"line_format reads all metadata and the line": {
			query:        `sum(count_over_time({app="x"} | line_format "{{.app}}-{{.pod}}"[5m]))`,
			streamLabels: []string{"app"},
			wantColumns:  []logs.ColumnType{sid, ts, logs.ColumnTypeMetadata, logs.ColumnTypeMessage},
		},
		"label_format reads all metadata and the line": {
			query:        `sum(count_over_time({app="x"} | label_format out="{{.pod}}"[5m]))`,
			streamLabels: []string{"app"},
			wantColumns:  []logs.ColumnType{sid, ts, logs.ColumnTypeMetadata, logs.ColumnTypeMessage},
		},
		// Any metadata matcher is pushed once the key is not a stream label: an equality prunes pages;
		// negation and regex become a filter predicate that can't prune but still lets the reader read the
		// message and other secondary columns only for matching rows. A section missing the column is
		// reduced by the reader against the empty value (absent metadata reads as ""), so a matcher that
		// matches "" (e.g. `!=`, `!~`) is kept there rather than dropped, which is why it is safe to push.
		"a regex-match metadata filter is pushed as a filter predicate": {
			query:          `sum(count_over_time({app="x"} | trace_id=~"t.*"[5m]))`,
			streamLabels:   []string{"app"},
			wantColumns:    []logs.ColumnType{sid, ts},
			wantMetadata:   []string{"trace_id"},
			wantPredicates: []string{filter("trace_id")},
		},
		"a not-equal metadata filter is pushed as a filter predicate": {
			query:          `sum(count_over_time({app="x"} | trace_id!="t"[5m]))`,
			streamLabels:   []string{"app"},
			wantColumns:    []logs.ColumnType{sid, ts},
			wantMetadata:   []string{"trace_id"},
			wantPredicates: []string{filter("trace_id")},
		},
		"a regex-not-match metadata filter is pushed as a filter predicate": {
			query:          `sum(count_over_time({app="x"} | trace_id!~"t.*"[5m]))`,
			streamLabels:   []string{"app"},
			wantColumns:    []logs.ColumnType{sid, ts},
			wantMetadata:   []string{"trace_id"},
			wantPredicates: []string{filter("trace_id")},
		},
		"a not-empty metadata filter is pushed as a filter predicate": {
			query:          `sum(count_over_time({app="x"} | trace_id!=""[5m]))`,
			streamLabels:   []string{"app"},
			wantColumns:    []logs.ColumnType{sid, ts},
			wantMetadata:   []string{"trace_id"},
			wantPredicates: []string{filter("trace_id")},
		},
		"an equality-to-empty metadata filter is pushed as an equality predicate": {
			query:          `sum(count_over_time({app="x"} | trace_id=""[5m]))`,
			streamLabels:   []string{"app"},
			wantColumns:    []logs.ColumnType{sid, ts},
			wantMetadata:   []string{"trace_id"},
			wantPredicates: []string{eq("trace_id", "")},
		},
		// An OR cannot be pushed (either branch may match), but both keys are projected.
		"an OR of metadata equalities projects both, pushes neither": {
			query:        `sum(count_over_time({app="x"} | trace_id="t" or span_id="s"[5m]))`,
			streamLabels: []string{"app"},
			wantColumns:  []logs.ColumnType{sid, ts},
			wantMetadata: []string{"span_id", "trace_id"},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			expr, err := syntax.ParseSampleExpr(tc.query)
			require.NoError(t, err)

			streamLabelSet := map[string]struct{}{}
			for _, n := range tc.streamLabels {
				streamLabelSet[n] = struct{}{}
			}

			columns, metadataKeys, predicates := planProjectionsAndPredicates(expr, streamLabelSet)

			var gotPredicates []string
			for _, p := range predicates {
				gotPredicates = append(gotPredicates, predDesc(p))
			}

			require.Equal(t, tc.wantColumns, columns)
			require.ElementsMatch(t, tc.wantMetadata, metadataKeys)
			require.ElementsMatch(t, tc.wantPredicates, gotPredicates)
		})
	}
}

// TestDataObjReadPlanner_Plan checks that Plan turns the metastore's resolved sections into one read
// task per section — grouped by object, with the projection and time range set and the shard filter
// applied so a section with no surviving streams is dropped. It drives a real metastore over real data
// objects; a tiny section size is used where several logs sections in one object are needed.
func TestDataObjReadPlanner_Plan(t *testing.T) {
	ctx := user.InjectOrgID(context.Background(), dataObjTestTenant)
	start, end := time.Unix(0, 0), time.Unix(100, 0)

	// A sum reduces the output to {}, so the projection is just stream_id + timestamp with no metadata
	// and no pushed-down predicates: this test focuses on the section/stream wiring, not the projection.
	expr, err := syntax.ParseSampleExpr(`sum(count_over_time({cluster="test"}[5m]))`)
	require.NoError(t, err)
	matchers := []*labels.Matcher{labels.MustNewMatcher(labels.MatchEqual, "cluster", "test")}

	plan := func(bucket objstore.Bucket, ms metastore.Metastore, shard *logql.Shard) []dataObjReadTask {
		return drainTaskIterator(t, newDataObjReadPlanner(ms, newDataObjCache(bucket, dataObjTestTenant)).
			plan(ctx, start, end, matchers, shard, expr))
	}

	// fpByLabels maps each app's stream-label string to its fingerprint (the StableHash of the labels).
	fpByLabels := func(apps ...string) map[string]uint64 {
		m := make(map[string]uint64, len(apps))
		for _, app := range apps {
			l := labels.FromStrings("app", app, "cluster", "test")
			m[l.String()] = labels.StableHash(l)
		}
		return m
	}

	// assertTasks checks every task's projection and time range, and that the union of the streams
	// across all tasks matches wantFPs (label string -> fingerprint) with consistent per-stream
	// fingerprints. It is independent of task order and of how streams split across sections.
	assertTasks := func(t *testing.T, tasks []dataObjReadTask, wantFPs map[string]uint64) {
		t.Helper()
		got := map[string]uint64{}
		for _, task := range tasks {
			require.Equal(t, []logs.ColumnType{logs.ColumnTypeStreamID, logs.ColumnTypeTimestamp}, task.projectedColumns)
			require.Empty(t, task.projectedMetadata)
			require.Empty(t, task.predicates)
			require.Equal(t, start, task.start)
			require.Equal(t, end, task.end)
			require.Len(t, task.labels, len(task.streamIDs))
			require.Len(t, task.fingerprints, len(task.streamIDs))
			for _, id := range task.streamIDs {
				lset := task.labels[id]
				require.Equal(t, labels.StableHash(lset), task.fingerprints[id])
				got[lset.String()] = task.fingerprints[id]
			}
		}
		require.Equal(t, wantFPs, got)
	}

	t.Run("single object, single section", func(t *testing.T) {
		bucket := objstore.NewInMemBucket()
		ms := newTestDataObjMetastore(ctx, t, bucket, testSectionSize, [][]logproto.Stream{planTestStreams("a", "b")})

		tasks := plan(bucket, ms, nil)
		require.Len(t, tasks, 1)
		assertTasks(t, tasks, fpByLabels("a", "b"))
	})

	t.Run("one object, multiple sections", func(t *testing.T) {
		bucket := objstore.NewInMemBucket()
		// A tiny section size forces several logs sections in the object, so Plan must emit a task per
		// section while reading the object's streams only once.
		ms := newTestDataObjMetastore(ctx, t, bucket, 1, [][]logproto.Stream{planTestStreams("a", "b", "c", "d")})

		tasks := plan(bucket, ms, nil)
		require.Greater(t, len(tasks), 1, "a tiny section size should split the object into several sections")
		for _, task := range tasks {
			require.Equal(t, tasks[0].object, task.object, "every task must read the same object")
		}
		assertTasks(t, tasks, fpByLabels("a", "b", "c", "d"))
	})

	t.Run("multiple objects", func(t *testing.T) {
		bucket := objstore.NewInMemBucket()
		ms := newTestDataObjMetastore(ctx, t, bucket, testSectionSize, [][]logproto.Stream{planTestStreams("a", "b"), planTestStreams("c", "d")})

		tasks := plan(bucket, ms, nil)
		objects := map[string]struct{}{}
		for _, task := range tasks {
			objects[task.object] = struct{}{}
		}
		require.Len(t, objects, 2, "tasks must span both objects")
		assertTasks(t, tasks, fpByLabels("a", "b", "c", "d"))
	})

	t.Run("sharding filters streams and drops empty sections", func(t *testing.T) {
		bucket := objstore.NewInMemBucket()
		apps := []string{"a", "b", "c", "d", "e", "f", "g", "h"}
		// One logs section per stream, so a section whose only stream is filtered out yields no task.
		ms := newTestDataObjMetastore(ctx, t, bucket, 1, [][]logproto.Stream{planTestStreams(apps...)})

		shards, _, err := logql.ParseShards([]string{"0_of_2"})
		require.NoError(t, err)
		shard := shards[0].Ptr()

		want := map[string]uint64{}
		for _, app := range apps {
			l := labels.FromStrings("app", app, "cluster", "test")
			if shard.Match(model.Fingerprint(labels.StableHash(l))) {
				want[l.String()] = labels.StableHash(l)
			}
		}
		require.NotEmpty(t, want, "test data must have streams inside shard 0")
		require.Less(t, len(want), len(apps), "test data must have streams outside shard 0")

		tasks := plan(bucket, ms, shard)
		require.Len(t, tasks, len(want), "one task per surviving single-stream section")
		assertTasks(t, tasks, want)
	})
}

// TestPlanSectionRead checks that planSectionRead fails when the metastore lists a stream ID the object's
// streams section did not resolve, rather than silently dropping it (which would under-count the query).
func TestPlanSectionRead(t *testing.T) {
	expr, err := syntax.ParseSampleExpr(`sum(count_over_time({app="a"}[5m]))`)
	require.NoError(t, err)
	q := readQuery{expr: expr, start: time.Unix(0, 0), end: time.Unix(100, 0)}
	idLabels := map[streamID]labels.Labels{1: labels.FromStrings("app", "a")}

	t.Run("all listed streams resolved", func(t *testing.T) {
		desc := &metastore.DataobjSectionDescriptor{
			SectionKey: metastore.SectionKey{ObjectPath: "obj", SectionIdx: 0},
			StreamIDs:  []int64{1},
		}
		task, ok, err := planSectionRead(desc, idLabels, q)
		require.NoError(t, err)
		require.True(t, ok)
		require.Equal(t, []streamID{1}, task.streamIDs)
	})

	t.Run("listed stream missing from the streams section errors", func(t *testing.T) {
		desc := &metastore.DataobjSectionDescriptor{
			SectionKey: metastore.SectionKey{ObjectPath: "obj", SectionIdx: 0},
			StreamIDs:  []int64{1, 2}, // 2 is not in idLabels
		}
		_, ok, err := planSectionRead(desc, idLabels, q)
		require.False(t, ok)
		require.ErrorContains(t, err, "missing from the object's streams section")
	})
}

// planTestStreams builds one single-entry stream per app, all sharing cluster="test".
func planTestStreams(apps ...string) []logproto.Stream {
	out := make([]logproto.Stream, 0, len(apps))
	for _, app := range apps {
		out = append(out, logproto.Stream{
			Labels:  labels.FromStrings("app", app, "cluster", "test").String(),
			Entries: []push.Entry{{Timestamp: time.Unix(1, 0), Line: "x"}},
		})
	}
	return out
}

func TestDataObjReadTask_RowPredicates(t *testing.T) {
	start := time.Unix(100, 0)
	end := time.Unix(200, 0)

	timeRange := logs.TimeRangeRowPredicate{
		StartTime:    start,
		EndTime:      end,
		IncludeStart: true,
		IncludeEnd:   false,
	}

	tests := map[string]struct {
		predicates []logs.RowPredicate
		want       []logs.RowPredicate
	}{
		"no metadata predicates keeps only the half-open time window": {
			predicates: nil,
			want:       []logs.RowPredicate{timeRange},
		},
		"metadata predicates follow the time window in order": {
			predicates: []logs.RowPredicate{
				logs.MetadataMatcherRowPredicate{Key: "pod", Value: "a"},
				logs.MetadataMatcherRowPredicate{Key: "level", Value: "error"},
			},
			want: []logs.RowPredicate{
				timeRange,
				logs.MetadataMatcherRowPredicate{Key: "pod", Value: "a"},
				logs.MetadataMatcherRowPredicate{Key: "level", Value: "error"},
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			task := dataObjReadTask{start: start, end: end, predicates: tc.predicates}
			require.Equal(t, tc.want, task.rowPredicates())
		})
	}
}

func TestDataObjReadTask_RecordBatch(t *testing.T) {
	a := labels.FromStrings("app", "a")
	b := labels.FromStrings("app", "b")
	md1 := labels.FromStrings("trace_id", "x")
	md2 := labels.FromStrings("trace_id", "y")

	task := dataObjReadTask{
		object:       "obj-1",
		section:      3,
		fingerprints: map[streamID]uint64{1: 111, 2: 222},
		labels:       map[streamID]labels.Labels{1: a, 2: b},
	}

	t.Run("maps records to log records", func(t *testing.T) {
		got, err := task.recordBatch([]logs.Record{
			{StreamID: 1, Timestamp: time.Unix(0, 100), Line: []byte("l1"), Metadata: md1},
			{StreamID: 2, Timestamp: time.Unix(0, 200), Line: []byte("l2"), Metadata: md2},
		})
		require.NoError(t, err)
		require.Equal(t, []dataObjLogRecord{
			{fingerprint: 111, streamLabels: a, timestamp: 100, line: []byte("l1"), metadata: md1},
			{fingerprint: 222, streamLabels: b, timestamp: 200, line: []byte("l2"), metadata: md2},
		}, got)
	})

	t.Run("errors on an unplanned stream ID", func(t *testing.T) {
		got, err := task.recordBatch([]logs.Record{
			{StreamID: 1, Timestamp: time.Unix(0, 100), Line: []byte("l1"), Metadata: md1},
			{StreamID: 9, Timestamp: time.Unix(0, 200), Line: []byte("l9"), Metadata: md2},
		})
		require.Nil(t, got)
		require.ErrorContains(t, err, "unexpected stream ID 9")
		require.ErrorContains(t, err, "obj-1")
	})
}
