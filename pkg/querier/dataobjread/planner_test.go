package dataobjread

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
	"github.com/grafana/loki/v3/pkg/dataobj/objtest"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/syntax"

	"github.com/grafana/loki/pkg/push"
)

func TestPlanner_Plan(t *testing.T) {
	fixtureStreams := []logproto.Stream{
		{Labels: `{app="a"}`, Entries: []push.Entry{entry(1, "one")}},
		{Labels: `{app="b"}`, Entries: []push.Entry{entry(1, "two")}},
	}

	// newTestPlanner returns a planner over the fixture's objects, together with the descriptors
	// the metastore resolved for them.
	newTestPlanner := func(t *testing.T, ms metastore.Metastore) (*Planner, metastore.DataobjSectionDescriptors) {
		t.Helper()
		fixture := newObjectsFixture(t, "", fixtureStreams...)
		objects := NewOpenObjects(fixture.bucket, objtest.Tenant, DefaultHeadPrefetchBytes, nil)
		t.Cleanup(objects.release)

		if ms == nil {
			ms = &fixedMetastore{descriptors: fixture.descriptors}
		}
		return NewPlanner(ms, objects, nil), fixture.descriptors
	}

	plainQuery := func(t *testing.T) QueryParams {
		t.Helper()
		expr, err := syntax.ParseSampleExpr(`sum by (app) (count_over_time({app=~".+"}[1m]))`)
		require.NoError(t, err)
		projection, err := NewProjectionPlan(expr, nil)
		require.NoError(t, err)
		return QueryParams{
			Start:      at(0),
			End:        at(100),
			Matchers:   syntax.MustParseLogSelector(`{app=~".+"}`, true).Matchers(),
			Projection: projection,
		}
	}

	t.Run("it plans one task per resolved logs section", func(t *testing.T) {
		readPlanner, descriptors := newTestPlanner(t, nil)
		tasks, err := drainTasks(readPlanner.Plan(t.Context(), plainQuery(t)))
		require.NoError(t, err)
		require.Len(t, tasks, len(descriptors))
	})

	t.Run("it forwards the query's metadata predicates to the metastore for bloom pruning", func(t *testing.T) {
		fixture := newObjectsFixture(t, "", fixtureStreams...)
		objects := NewOpenObjects(fixture.bucket, objtest.Tenant, DefaultHeadPrefetchBytes, nil)
		t.Cleanup(objects.release)

		ms := &fixedMetastore{descriptors: fixture.descriptors}
		readPlanner := NewPlanner(ms, objects, nil)

		expr, err := syntax.ParseSampleExpr(`sum by (app) (count_over_time({app=~".+"} | level="error" [1m]))`)
		require.NoError(t, err)
		projection, err := NewProjectionPlan(expr, nil)
		require.NoError(t, err)

		query := plainQuery(t)
		query.Projection = projection
		_, err = drainTasks(readPlanner.Plan(t.Context(), query))
		require.NoError(t, err)

		require.Len(t, ms.gotRequest.Predicates, 1)
		require.Equal(t, `level="error"`, ms.gotRequest.Predicates[0].String())
	})

	t.Run("a stream the metastore lists but the object does not hold fails the query", func(t *testing.T) {
		fixture := newObjectsFixture(t, "", fixtureStreams...)
		objects := NewOpenObjects(fixture.bucket, objtest.Tenant, DefaultHeadPrefetchBytes, nil)
		t.Cleanup(objects.release)

		// Claim a stream ID no object holds, which is the index and the object disagreeing.
		corrupted := *fixture.descriptors[0]
		corrupted.StreamIDs = append([]int64{}, corrupted.StreamIDs...)
		corrupted.StreamIDs = append(corrupted.StreamIDs, 9999)

		ms := &fixedMetastore{descriptors: metastore.DataobjSectionDescriptors{&corrupted}}
		readPlanner := NewPlanner(ms, objects, nil)

		_, err := drainTasks(readPlanner.Plan(t.Context(), plainQuery(t)))
		require.ErrorContains(t, err, "listed by the metastore is missing")
	})

	t.Run("a stream missing from a shard bucket-pruned read is taken as out of shard rather than an error", func(t *testing.T) {
		fixture := newObjectsFixture(t, "", fixtureStreams...)
		objects := NewOpenObjects(fixture.bucket, objtest.Tenant, DefaultHeadPrefetchBytes, nil)
		t.Cleanup(objects.release)

		corrupted := *fixture.descriptors[0]
		corrupted.StreamIDs = append([]int64{}, corrupted.StreamIDs...)
		corrupted.StreamIDs = append(corrupted.StreamIDs, 9999)

		ms := &fixedMetastore{descriptors: metastore.DataobjSectionDescriptors{&corrupted}}
		readPlanner := NewPlanner(ms, objects, nil)

		// Two shards, so the read is bucket-pruned and a listed stream the pruned read did not
		// return is taken as out of shard.
		query := plainQuery(t)
		query.Shard = NewQueryShard(powerOfTwoShard(0, 2))
		require.NotNil(t, query.Shard.buckets, "the shard should map to a bucket range")

		tasks, err := drainTasks(readPlanner.Plan(t.Context(), query))
		require.NoError(t, err)
		require.NotEmpty(t, tasks)
		for _, task := range tasks {
			require.NotContains(t, task.streamIDs, int64(9999))
		}
	})

	t.Run("a section the metastore lists with no stream fails the query", func(t *testing.T) {
		fixture := newObjectsFixture(t, "", fixtureStreams...)
		objects := NewOpenObjects(fixture.bucket, objtest.Tenant, DefaultHeadPrefetchBytes, nil)
		t.Cleanup(objects.release)

		empty := *fixture.descriptors[0]
		empty.StreamIDs = nil

		ms := &fixedMetastore{descriptors: metastore.DataobjSectionDescriptors{&empty}}
		readPlanner := NewPlanner(ms, objects, nil)

		_, err := drainTasks(readPlanner.Plan(t.Context(), plainQuery(t)))
		require.ErrorContains(t, err, "listed no stream")
	})

	t.Run("a resolution error surfaces through the iterator", func(t *testing.T) {
		wantErr := errors.New("metastore is down")
		readPlanner, _ := newTestPlanner(t, &fixedMetastore{err: wantErr})

		tasks, err := drainTasks(readPlanner.Plan(t.Context(), plainQuery(t)))
		require.ErrorIs(t, err, wantErr)
		require.Empty(t, tasks)
	})

	t.Run("no task is planned when the access-control filter denies every stream", func(t *testing.T) {
		fixture := newObjectsFixture(t, "", fixtureStreams...)
		objects := NewOpenObjects(fixture.bucket, objtest.Tenant, DefaultHeadPrefetchBytes, nil)
		t.Cleanup(objects.release)

		ms := &fixedMetastore{descriptors: fixture.descriptors}
		readPlanner := NewPlanner(ms, objects, denyEverythingFilterer{})

		tasks, err := drainTasks(readPlanner.Plan(t.Context(), plainQuery(t)))
		require.NoError(t, err)
		require.Empty(t, tasks)
	})

	t.Run("resolving no section plans no task and reports no error", func(t *testing.T) {
		readPlanner, _ := newTestPlanner(t, &fixedMetastore{})

		tasks, err := drainTasks(readPlanner.Plan(t.Context(), plainQuery(t)))
		require.NoError(t, err)
		require.Empty(t, tasks)
	})
}

// TestPlanner_RecoversPanics asserts a panic on the planner's own goroutine fails the
// query instead of the process. Nothing above the planner can recover it, because it runs on a
// goroutine of its own.
func TestPlanner_RecoversPanics(t *testing.T) {
	fixture := newObjectsFixture(t, "", logproto.Stream{
		Labels:  `{app="a"}`,
		Entries: []push.Entry{entry(1, "one")},
	})
	objects := NewOpenObjects(fixture.bucket, objtest.Tenant, DefaultHeadPrefetchBytes, nil)
	t.Cleanup(objects.release)

	expr, err := syntax.ParseSampleExpr(`count_over_time({app=~".+"}[1m])`)
	require.NoError(t, err)
	projection, err := NewProjectionPlan(expr, nil)
	require.NoError(t, err)

	readPlanner := NewPlanner(panickingMetastore{message: "metastore exploded"}, objects, nil)
	tasks, err := drainTasks(readPlanner.Plan(t.Context(), QueryParams{
		Start:      at(0),
		End:        at(100),
		Matchers:   syntax.MustParseLogSelector(`{app=~".+"}`, true).Matchers(),
		Projection: projection,
	}))
	require.ErrorContains(t, err, "metastore exploded")
	require.Empty(t, tasks)
}

// panickingMetastore panics instead of resolving, to drive the planner goroutine's panic
// recovery.
type panickingMetastore struct {
	metastore.Metastore

	message string
}

func (m panickingMetastore) Sections(context.Context, metastore.SectionsRequest) (metastore.SectionsResponse, error) {
	panic(m.message)
}

// fixedMetastore resolves to descriptors a test supplies, so a test can present the planner with
// a section list a real metastore would not produce. Every other method is nil, so a call the
// planner should not make panics rather than returning an empty answer.
type fixedMetastore struct {
	metastore.Metastore

	descriptors metastore.DataobjSectionDescriptors
	err         error

	// gotRequest records what the planner asked for.
	gotRequest metastore.SectionsRequest
}

func (m *fixedMetastore) Sections(_ context.Context, req metastore.SectionsRequest) (metastore.SectionsResponse, error) {
	m.gotRequest = req
	if m.err != nil {
		return metastore.SectionsResponse{}, m.err
	}
	return metastore.SectionsResponse{Sections: m.descriptors}, nil
}

// drainTasks collects every task the planner produces.
func drainTasks(it *TaskIterator) ([]readTask, error) {
	var tasks []readTask
	for it.Next() {
		tasks = append(tasks, it.At())
	}
	it.Abort(nil)
	return tasks, it.Err()
}
