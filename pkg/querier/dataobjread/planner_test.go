package dataobjread

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"math"
	"testing"
	"time"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
	"github.com/grafana/loki/v3/pkg/dataobj/objtest"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/xcap"

	"github.com/grafana/loki/pkg/push"
)

func TestPlanner_Plan(t *testing.T) {
	fixtureStreams := []logproto.Stream{
		{Labels: `{app="a"}`, Entries: []push.Entry{entry(t, 1, "one")}},
		{Labels: `{app="b"}`, Entries: []push.Entry{entry(t, 1, "two")}},
	}

	// newTestPlanner returns a planner over the fixture's objects, together with the descriptors
	// the metastore resolved for them.
	newTestPlanner := func(t *testing.T, ms metastore.Metastore) (*Planner, metastore.DataobjSectionDescriptors) {
		t.Helper()
		fixture := newObjectsFixture(t, fixtureStreams...)
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

	t.Run("it omits the line from every task when only a pushed line filter reads it", func(t *testing.T) {
		readPlanner, _ := newTestPlanner(t, nil)
		expr, err := syntax.ParseSampleExpr(`sum by (app) (count_over_time({app=~".+"} |= "x" [1m]))`)
		require.NoError(t, err)
		projection, err := NewProjectionPlan(expr, nil)
		require.NoError(t, err)

		query := plainQuery(t)
		query.Projection = projection
		tasks, err := drainTasks(readPlanner.Plan(t.Context(), query))
		require.NoError(t, err)
		require.NotEmpty(t, tasks)
		for _, task := range tasks {
			require.False(t, task.needReturningLine)
		}
	})

	t.Run("it forwards the query's metadata predicates to the metastore for bloom pruning", func(t *testing.T) {
		fixture := newObjectsFixture(t, fixtureStreams...)
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

	for _, test := range []struct {
		name  string
		shard *QueryShard
		want  *streams.HashRange
	}{
		{
			name:  "an unsharded query asks the metastore for no hash range",
			shard: nil,
			want:  nil,
		},
		{
			name:  "a shard count of one restricts no stream, so the planner asks the metastore for no hash range",
			shard: NewQueryShard(powerOfTwoShard(0, 1)),
			want:  nil,
		},
		{
			name:  "the first of two shards asks the metastore for the lower half of the hashes",
			shard: NewQueryShard(powerOfTwoShard(0, 2)),
			want:  &streams.HashRange{From: 0, To: 1<<63 - 1},
		},
		{
			name:  "the last of two shards asks the metastore for the upper half of the hashes",
			shard: NewQueryShard(powerOfTwoShard(1, 2)),
			want:  &streams.HashRange{From: 1 << 63, To: math.MaxUint64},
		},
		{
			name:  "a bounded shard of the whole fingerprint space restricts no stream, so the planner asks the metastore for no hash range",
			shard: NewQueryShard(boundedShard(0, math.MaxUint64)),
			want:  nil,
		},
		{
			name:  "a shard of many shards asks the metastore for the hashes of that shard",
			shard: NewQueryShard(powerOfTwoShard(5, 128)),
			want:  &streams.HashRange{From: 5 << 57, To: 6<<57 - 1},
		},
		{
			name:  "a bounded shard asks the metastore for the hashes of its bounds",
			shard: NewQueryShard(boundedShard(0, ^uint64(0)/2)),
			want:  &streams.HashRange{From: 0, To: ^uint64(0) / 2},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			ms := &fixedMetastore{}
			readPlanner, _ := newTestPlanner(t, ms)

			query := plainQuery(t)
			query.Shard = test.shard
			_, err := drainTasks(readPlanner.Plan(t.Context(), query))
			require.NoError(t, err)

			require.Equal(t, test.want, ms.gotRequest.StreamHashRange)
		})
	}

	t.Run("a stream the metastore lists but the object does not hold fails the query", func(t *testing.T) {
		fixture := newObjectsFixture(t, fixtureStreams...)
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
		fixture := newObjectsFixture(t, fixtureStreams...)
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
		require.NotNil(t, query.Shard.bucketRange(streams.DefaultShardLayout()), "the shard should map to a bucket range")

		tasks, err := drainTasks(readPlanner.Plan(t.Context(), query))
		require.NoError(t, err)
		require.NotEmpty(t, tasks)
		for _, task := range tasks {
			require.NotContains(t, task.streamIDs, int64(9999))
		}
	})

	t.Run("a stream the metastore lists but the object does not hold fails the query when the shard can hold every hash", func(t *testing.T) {
		fixture := newObjectsFixture(t, fixtureStreams...)
		objects := NewOpenObjects(fixture.bucket, objtest.Tenant, DefaultHeadPrefetchBytes, nil)
		t.Cleanup(objects.release)

		corrupted := *fixture.descriptors[0]
		corrupted.StreamIDs = append([]int64{}, corrupted.StreamIDs...)
		corrupted.StreamIDs = append(corrupted.StreamIDs, 9999)

		ms := &fixedMetastore{descriptors: metastore.DataobjSectionDescriptors{&corrupted}}
		readPlanner := NewPlanner(ms, objects, nil)

		query := plainQuery(t)
		query.Shard = NewQueryShard(boundedShard(0, math.MaxUint64))
		require.Nil(t, query.Shard.bucketRange(streams.DefaultShardLayout()), "the shard should map to no bucket range")

		_, err := drainTasks(readPlanner.Plan(t.Context(), query))
		require.EqualError(t, err, fmt.Sprintf("data object %q logs section %d: stream ID 9999 listed by the metastore is missing from the object's streams section", corrupted.ObjectPath, corrupted.SectionIdx))
	})

	for _, test := range []struct {
		name                   string
		buckets                uint32
		wantMissingStreamError bool
	}{
		{
			name:                   "a stream the metastore lists but the object does not hold fails the query when the object has one bucket, which the shard cannot prune",
			buckets:                1,
			wantMissingStreamError: true,
		},
		{
			name:    "a stream the metastore lists but the object does not hold is taken as out of shard when the object has 8 buckets, which the shard prunes",
			buckets: 8,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			layout := streams.NewLayout(streams.MustShardLayoutFromBuckets(test.buckets), []string{"label:app"})
			bucket, path, _ := createTestStoredObject(t, objtest.Tenant, &layout, `{app="a"}`, `{app="b"}`)
			objects := NewOpenObjects(bucket, objtest.Tenant, DefaultHeadPrefetchBytes, nil)
			t.Cleanup(objects.release)

			descriptor := &metastore.DataobjSectionDescriptor{
				SectionKey: metastore.SectionKey{ObjectPath: path, SectionIdx: 0},
				StreamIDs:  []int64{1, 2, 9999},
			}
			readPlanner := NewPlanner(&fixedMetastore{descriptors: metastore.DataobjSectionDescriptors{descriptor}}, objects, nil)

			query := plainQuery(t)
			query.Shard = NewQueryShard(powerOfTwoShard(0, 2))
			_, err := drainTasks(readPlanner.Plan(t.Context(), query))

			if !test.wantMissingStreamError {
				require.NoError(t, err)
				return
			}
			require.EqualError(t, err, fmt.Sprintf("data object %q logs section 0: stream ID 9999 listed by the metastore is missing from the object's streams section", path))
		})
	}

	t.Run("a section the metastore lists with no stream fails the query", func(t *testing.T) {
		fixture := newObjectsFixture(t, fixtureStreams...)
		objects := NewOpenObjects(fixture.bucket, objtest.Tenant, DefaultHeadPrefetchBytes, nil)
		t.Cleanup(objects.release)

		empty := *fixture.descriptors[0]
		empty.StreamIDs = nil

		ms := &fixedMetastore{descriptors: metastore.DataobjSectionDescriptors{&empty}}
		readPlanner := NewPlanner(ms, objects, nil)

		_, err := drainTasks(readPlanner.Plan(t.Context(), plainQuery(t)))
		require.ErrorContains(t, err, "listed no stream")
	})

	t.Run("a section listed twice by the metastore fails the query rather than counting it twice", func(t *testing.T) {
		fixture := newObjectsFixture(t, fixtureStreams...)
		objects := NewOpenObjects(fixture.bucket, objtest.Tenant, DefaultHeadPrefetchBytes, nil)
		t.Cleanup(objects.release)

		repeated := *fixture.descriptors[0]
		ms := &fixedMetastore{descriptors: metastore.DataobjSectionDescriptors{
			fixture.descriptors[0],
			&repeated,
		}}
		readPlanner := NewPlanner(ms, objects, nil)

		_, err := drainTasks(readPlanner.Plan(t.Context(), plainQuery(t)))
		require.ErrorContains(t, err, "was listed twice")
	})

	t.Run("it plans the tasks of a batch before the metastore yields the next batch of the same object", func(t *testing.T) {
		fixture := newObjectsFixture(t, fixtureStreams...)
		objects := NewOpenObjects(fixture.bucket, objtest.Tenant, DefaultHeadPrefetchBytes, nil)
		t.Cleanup(objects.release)

		laterSection := *fixture.descriptors[0]
		laterSection.SectionIdx += 100
		ms := &streamingMetastore{
			batches: []metastore.DataobjSectionDescriptors{fixture.descriptors, {&laterSection}},
			next:    make(chan struct{}),
		}
		ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
		defer cancel()

		it := NewPlanner(ms, objects, nil).Plan(ctx, plainQuery(t))
		require.True(t, it.Next(), "no task arrived before the second batch")
		tasks := []ReadTask{it.At()}
		close(ms.next)

		rest, err := drainTasks(it)
		require.NoError(t, err)
		require.Len(t, append(tasks, rest...), len(fixture.descriptors)+1)
	})

	t.Run("a section that two batches list fails the query rather than counting it twice", func(t *testing.T) {
		fixture := newObjectsFixture(t, fixtureStreams...)
		objects := NewOpenObjects(fixture.bucket, objtest.Tenant, DefaultHeadPrefetchBytes, nil)
		t.Cleanup(objects.release)

		repeated := *fixture.descriptors[0]
		ms := &streamingMetastore{batches: []metastore.DataobjSectionDescriptors{fixture.descriptors, {&repeated}}}

		_, err := drainTasks(NewPlanner(ms, objects, nil).Plan(t.Context(), plainQuery(t)))
		require.ErrorContains(t, err, "was listed twice")
	})

	t.Run("aborting the iterator stops a running resolution and reports no error", func(t *testing.T) {
		fixture := newObjectsFixture(t, fixtureStreams...)
		objects := NewOpenObjects(fixture.bucket, objtest.Tenant, DefaultHeadPrefetchBytes, nil)
		t.Cleanup(objects.release)

		laterSection := *fixture.descriptors[0]
		laterSection.SectionIdx += 100
		ms := &streamingMetastore{
			batches: []metastore.DataobjSectionDescriptors{fixture.descriptors, {&laterSection}},
			next:    make(chan struct{}),
		}

		ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
		defer cancel()

		it := NewPlanner(ms, objects, nil).Plan(ctx, plainQuery(t))
		require.True(t, it.Next())
		it.Abort(nil)

		require.NoError(t, it.Err())
	})

	t.Run("a resolution error surfaces through the iterator", func(t *testing.T) {
		wantErr := errors.New("metastore is down")
		readPlanner, _ := newTestPlanner(t, &fixedMetastore{err: wantErr})

		tasks, err := drainTasks(readPlanner.Plan(t.Context(), plainQuery(t)))
		require.ErrorIs(t, err, wantErr)
		require.Empty(t, tasks)
	})

	t.Run("no task is planned when the access-control filter denies every stream", func(t *testing.T) {
		fixture := newObjectsFixture(t, fixtureStreams...)
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

	t.Run("it reads the objects it plans in a root region of their own", func(t *testing.T) {
		readPlanner, _ := newTestPlanner(t, nil)
		ctx, capture := xcap.NewCapture(t.Context(), nil)

		_, err := drainTasks(readPlanner.Plan(ctx, plainQuery(t)))
		require.NoError(t, err)

		var region *xcap.Region
		for _, r := range capture.Regions() {
			if r.Name() == regionStreamsReader {
				region = r
			}
		}
		require.NotNil(t, region, "the planner opens its objects in the streams-reader region")
		require.True(t, region.ParentID().IsZero(), "the region is a root, so its requests count for the streams-reader component")
		require.Positive(t, xcap.ValueFromRegion[int64](capture, regionStreamsReader, dataobj.StatObjectRequestsGetRange))
		require.Positive(t, xcap.ValueFromRegion[int64](capture, regionStreamsReader, dataobj.StatObjectBytesDownloaded))
	})
}

func TestPlanner_Admits(t *testing.T) {
	const (
		hashInShard    = uint64(0)
		hashOutOfShard = uint64(1) << 63
	)
	shard := NewQueryShard(powerOfTwoShard(0, 2))

	tests := []struct {
		name    string
		shard   *QueryShard
		buckets *shardBucketRange
		hash    uint64
		want    bool
	}{
		{
			name:    "admits a stream of an unsharded query",
			shard:   nil,
			buckets: nil,
			hash:    hashOutOfShard,
			want:    true,
		},
		{
			name:    "admits a stream inside the shard when the read pushed no bucket range down",
			shard:   shard,
			buckets: nil,
			hash:    hashInShard,
			want:    true,
		},
		{
			name:    "rejects a stream outside the shard when the read pushed no bucket range down",
			shard:   shard,
			buckets: nil,
			hash:    hashOutOfShard,
			want:    false,
		},
		{
			name:    "rejects a stream outside the shard when the bucket range is inexact",
			shard:   shard,
			buckets: &shardBucketRange{from: 0, to: 15, exact: false},
			hash:    hashOutOfShard,
			want:    false,
		},
		{
			name:    "admits a stream without checking its hash when the bucket range is exact",
			shard:   shard,
			buckets: &shardBucketRange{from: 0, to: 15, exact: true},
			hash:    hashOutOfShard,
			want:    true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := (&Planner{}).admits(labels.EmptyLabels(), test.hash, test.shard, test.buckets)

			require.Equal(t, test.want, got)
		})
	}
}

// TestPlanner_RecoversPanics asserts a panic on the planner's own goroutine fails the
// query instead of the process. Nothing above the planner can recover it, because it runs on a
// goroutine of its own.
func TestPlanner_RecoversPanics(t *testing.T) {
	fixture := newObjectsFixture(t, logproto.Stream{
		Labels:  `{app="a"}`,
		Entries: []push.Entry{entry(t, 1, "one")},
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

func (m panickingMetastore) SectionsStream(context.Context, metastore.SectionsRequest) iter.Seq2[metastore.DataobjSectionDescriptors, error] {
	panic(m.message)
}

// fixedMetastore yields the descriptors a test supplies as one batch, then err when it is set. A
// test uses it to present the planner with a section list a real metastore would not produce.
// Every other method is nil, so a call the planner should not make panics.
type fixedMetastore struct {
	metastore.Metastore

	descriptors metastore.DataobjSectionDescriptors
	err         error

	// gotRequest records what the planner asked for.
	gotRequest metastore.SectionsRequest
}

func (m *fixedMetastore) SectionsStream(_ context.Context, req metastore.SectionsRequest) iter.Seq2[metastore.DataobjSectionDescriptors, error] {
	m.gotRequest = req
	return func(yield func(metastore.DataobjSectionDescriptors, error) bool) {
		if len(m.descriptors) > 0 && !yield(m.descriptors, nil) {
			return
		}
		if m.err != nil {
			yield(nil, m.err)
		}
	}
}

// streamingMetastore yields the batches a test supplies. When next is set, the stream receives
// from next before each batch after the first. A test closes next to release the later batches.
type streamingMetastore struct {
	metastore.Metastore

	batches []metastore.DataobjSectionDescriptors
	next    chan struct{}
}

func (m *streamingMetastore) SectionsStream(ctx context.Context, _ metastore.SectionsRequest) iter.Seq2[metastore.DataobjSectionDescriptors, error] {
	return func(yield func(metastore.DataobjSectionDescriptors, error) bool) {
		for i, batch := range m.batches {
			if i > 0 && m.next != nil {
				select {
				case <-m.next:
				case <-ctx.Done():
					yield(nil, ctx.Err())
					return
				}
			}
			if !yield(batch, nil) {
				return
			}
		}
	}
}

// drainTasks collects every task the planner produces.
func drainTasks(it *TaskIterator) ([]ReadTask, error) {
	var tasks []ReadTask
	for it.Next() {
		tasks = append(tasks, it.At())
	}
	it.Abort(nil)
	return tasks, it.Err()
}
