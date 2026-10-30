package dataobjread

import (
	"bytes"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"
	"github.com/thanos-io/objstore"

	"github.com/grafana/loki/v4/pkg/dataobj/fixtures"
	"github.com/grafana/loki/v4/pkg/dataobj/sections/streams"
)

// defaultStoredLayout is the layout that a test object records when the test does not depend on it.
var defaultStoredLayout = streams.NewLayout(streams.DefaultShardLayout(), nil)

func TestOpenObjects(t *testing.T) {
	const fixtureTenant = "fixture-tenant"

	t.Run("a section read of one object is shared by every caller that asks for it", func(t *testing.T) {
		bucket, path, _ := createTestStoredObject(t, fixtureTenant, &defaultStoredLayout, `{app="a"}`)
		objects := NewOpenObjects(bucket, fixtureTenant, DefaultHeadPrefetchBytes, nil)
		t.Cleanup(objects.release)

		first, err := objects.get(t.Context(), path)
		require.NoError(t, err)
		second, err := objects.get(t.Context(), path)
		require.NoError(t, err)
		require.Same(t, first, second)
	})

	t.Run("a tenant's logs section keeps the index it has among every tenant's sections", func(t *testing.T) {
		const otherTenant = "other-tenant"

		theirs := fixtures.NewLogsFixtureBuilder(t)
		theirs.ForStream(`{app="other"}`).Entry(1, "{}", "theirs")

		mine := fixtures.NewLogsFixtureBuilder(t)
		mine.ForStream(`{app="a"}`).Entry(1, "{}", "one")

		bucket := objstore.NewInMemBucket()
		t.Cleanup(func() { require.NoError(t, bucket.Close()) })

		// The other tenant's logs section is written first, so it takes logs-relative index 0 and
		// this tenant's takes 1. A reader that numbered each tenant's sections from zero would
		// read the wrong section.
		path := fixtures.StoredDataObject(t, bucket,
			fixtures.StreamsSection(t, otherTenant, theirs.Streams()),
			fixtures.LogsSection(t, otherTenant, theirs.Logs()),
			fixtures.StreamsSection(t, fixtureTenant, mine.Streams()),
			fixtures.LogsSection(t, fixtureTenant, mine.Logs()),
		)

		objects := NewOpenObjects(bucket, fixtureTenant, DefaultHeadPrefetchBytes, nil)
		t.Cleanup(objects.release)

		object, err := objects.get(t.Context(), path)
		require.NoError(t, err)

		require.Len(t, object.tenant.Logs, 1, "only the queried tenant's logs section belongs to the set")
		require.Contains(t, object.tenant.Logs, 1, "the tenant's own section sits at index 1, after the other tenant's")

		section, err := object.logsSection(t.Context(), 1)
		require.NoError(t, err)
		require.NotNil(t, section)
	})

	t.Run("a logs-relative index the object does not hold fails, because the index and the object disagree", func(t *testing.T) {
		object, _ := createAndOpenTestStoredObject(t, fixtureTenant, &defaultStoredLayout, `{app="a"}`)

		_, err := object.logsSection(t.Context(), 999)
		require.ErrorContains(t, err, "holds no logs section 999")
	})

	t.Run("opening an object that is not in the bucket fails", func(t *testing.T) {
		bucket, _, _ := createTestStoredObject(t, fixtureTenant, &defaultStoredLayout, `{app="a"}`)
		objects := NewOpenObjects(bucket, fixtureTenant, DefaultHeadPrefetchBytes, nil)
		t.Cleanup(objects.release)

		_, err := objects.get(t.Context(), "objects/does-not-exist")
		require.Error(t, err)
	})
}

func TestOpenObject_StreamLabels(t *testing.T) {
	const fixtureTenant = "fixture-tenant"

	// Three streams, so a read can be asked for a subset and a bucket range can prune one.
	fixtureStreams := []string{`{app="a"}`, `{app="b"}`, `{app="c"}`}

	t.Run("it decodes only the stream IDs it was asked for", func(t *testing.T) {
		object, ids := createAndOpenTestStoredObject(t, fixtureTenant, &defaultStoredLayout, fixtureStreams...)

		decoded, err := object.streamLabels(t.Context(), ids[:1], nil)
		require.NoError(t, err)
		require.Len(t, decoded, 1)
		require.Contains(t, decoded, ids[0])
	})

	t.Run("an empty set of stream IDs reads nothing", func(t *testing.T) {
		object, _ := createAndOpenTestStoredObject(t, fixtureTenant, &defaultStoredLayout, fixtureStreams...)

		decoded, err := object.streamLabels(t.Context(), nil, nil)
		require.NoError(t, err)
		require.Empty(t, decoded)
	})

	t.Run("a bucket range covering every bucket keeps every stream", func(t *testing.T) {
		object, ids := createAndOpenTestStoredObject(t, fixtureTenant, &defaultStoredLayout, fixtureStreams...)

		decoded, err := object.streamLabels(t.Context(), ids, &shardBucketRange{from: 0, to: streams.DefaultShardLayout().Buckets() - 1})
		require.NoError(t, err)
		require.Len(t, decoded, len(ids))
	})

	t.Run("a bucket range keeps only the streams whose bucket falls inside it", func(t *testing.T) {
		object, ids := createAndOpenTestStoredObject(t, fixtureTenant, &defaultStoredLayout, fixtureStreams...)

		all, err := object.streamLabels(t.Context(), ids, nil)
		require.NoError(t, err)

		bucketOf := func(id int64) uint32 {
			return streams.DefaultShardLayout().BucketFromHash(labels.StableHash(all[id]))
		}

		// Prune to the bucket of one stream, then assert nothing outside that bucket survives.
		target := ids[0]
		bucket := bucketOf(target)

		// With every fixture stream in one bucket the pruned read would return all of them and
		// assert nothing, so the fixture has to spread across at least two buckets.
		require.True(t, slices.ContainsFunc(ids, func(id int64) bool { return bucketOf(id) != bucket }),
			"the fixture streams share one bucket: pick labels that hash into different ones")

		decoded, err := object.streamLabels(t.Context(), ids, &shardBucketRange{from: bucket, to: bucket})
		require.NoError(t, err)
		require.Contains(t, decoded, target)
		require.Less(t, len(decoded), len(all), "the pruned read must drop the streams outside the bucket")
		for id, streamLabels := range decoded {
			require.Equal(t, bucket, streams.DefaultShardLayout().BucketFromHash(labels.StableHash(streamLabels)), "stream %d is outside the pruned bucket", id)
		}
	})
}

func TestOpenObject_ShardLayout(t *testing.T) {
	const fixtureTenant = "fixture-tenant"

	fixtureStreams := []string{`{app="a"}`, `{app="b"}`}
	recordedLayout := func(buckets uint32) *streams.Layout {
		layout := streams.NewLayout(streams.MustShardLayoutFromBuckets(buckets), []string{"label:app"})
		return &layout
	}

	for _, buckets := range []uint32{8, 128} {
		t.Run(fmt.Sprintf("returns the shard layout of %d buckets that the streams section records", buckets), func(t *testing.T) {
			object, _ := createAndOpenTestStoredObject(t, fixtureTenant, recordedLayout(buckets), fixtureStreams...)

			layout, err := object.shardLayout(t.Context())
			require.NoError(t, err)
			require.Equal(t, buckets, layout.Buckets())
		})
	}

	t.Run("returns the default shard layout for a legacy object whose streams section records none", func(t *testing.T) {
		data, err := os.ReadFile(filepath.Join("..", "..", "dataobj", "testdata", "legacy", "legacy-app.dataobj"))
		require.NoError(t, err)
		bucket := objstore.NewInMemBucket()
		t.Cleanup(func() { require.NoError(t, bucket.Close()) })
		require.NoError(t, bucket.Upload(t.Context(), "legacy-app.dataobj", bytes.NewReader(data)))

		layout, err := openStoredObject(t, bucket, "T", "legacy-app.dataobj").shardLayout(t.Context())
		require.NoError(t, err)
		require.Equal(t, streams.DefaultShardLayout(), layout)
	})

	t.Run("returns the default shard layout when no section of the object records a layout", func(t *testing.T) {
		object, _ := createAndOpenTestStoredObject(t, fixtureTenant, nil, fixtureStreams...)

		layout, err := object.shardLayout(t.Context())
		require.NoError(t, err)
		require.Equal(t, streams.DefaultShardLayout(), layout)
	})

	t.Run("returns an error when the object holds no streams section for the tenant", func(t *testing.T) {
		bucket, path, _ := createTestStoredObject(t, fixtureTenant, recordedLayout(8), fixtureStreams...)

		_, err := openStoredObject(t, bucket, "other-tenant", path).shardLayout(t.Context())
		require.EqualError(t, err, fmt.Sprintf("data object %q holds no streams section for the tenant", path))
	})
}

func TestOpenObject_ShardBucketRange(t *testing.T) {
	const fixtureTenant = "fixture-tenant"

	fixtureStreams := []string{`{app="a"}`, `{app="b"}`}

	for _, test := range []struct {
		name    string
		buckets uint32
		want    *shardBucketRange
	}{
		{name: "maps shard 3 of 64 to one inexact bucket in an object of 8 buckets", buckets: 8, want: &shardBucketRange{from: 0, to: 0, exact: false}},
		{name: "maps shard 3 of 64 to one inexact bucket in an object of 32 buckets", buckets: 32, want: &shardBucketRange{from: 1, to: 1, exact: false}},
		{name: "maps shard 3 of 64 to two exact buckets in an object of 128 buckets", buckets: 128, want: &shardBucketRange{from: 6, to: 7, exact: true}},
	} {
		t.Run(test.name, func(t *testing.T) {
			layout := streams.NewLayout(streams.MustShardLayoutFromBuckets(test.buckets), []string{"label:app"})
			object, _ := createAndOpenTestStoredObject(t, fixtureTenant, &layout, fixtureStreams...)

			got, err := object.shardBucketRange(t.Context(), NewQueryShard(powerOfTwoShard(3, 64)))
			require.NoError(t, err)
			require.Equal(t, test.want, got)
		})
	}

	for _, test := range []struct {
		name  string
		shard *QueryShard
	}{
		{name: "returns no range and does not read the layout for an unsharded query", shard: nil},
		{name: "returns no range and does not read the layout for a shard count of one", shard: NewQueryShard(powerOfTwoShard(0, 1))},
		{name: "returns no range and does not read the layout for a bounded shard of the whole fingerprint space", shard: NewQueryShard(boundedShard(0, math.MaxUint64))},
	} {
		t.Run(test.name, func(t *testing.T) {
			object, _ := createAndOpenTestStoredObject(t, fixtureTenant, &defaultStoredLayout, fixtureStreams...)

			got, err := object.shardBucketRange(t.Context(), test.shard)

			require.NoError(t, err)
			require.Nil(t, got)
			require.Nil(t, object.streamsSection, "the streams section records the layout, so reading the layout opens it")
		})
	}
}

// createTestStoredObject writes one data object holding the given streams for tenant, to a fresh
// in-memory bucket. It returns the bucket, the object's path, and the stream ID the fixture gave
// each stream, in the order the streams were named.
//
// The streams section records layout. If layout is nil, it records none, and no logs section
// records a layout either.
//
// The bucket carries no index, so the object is reachable by path but resolves through no
// metastore. Use [newObjectsFixture] for a test that needs resolution.
func createTestStoredObject(t *testing.T, tenant string, layout *streams.Layout, streamLabels ...string) (objstore.Bucket, string, []int64) {
	t.Helper()

	var (
		fixtureOpts []fixtures.LogFixtureOpt
		streamsOpts = []fixtures.StreamsSectionOpt{fixtures.WithoutStreamsLayout()}
	)
	if layout != nil {
		fixtureOpts = append(fixtureOpts, fixtures.WithLayout(*layout))
		streamsOpts = []fixtures.StreamsSectionOpt{fixtures.WithStreamsLayout(*layout)}
	}

	rows := fixtures.NewLogsFixtureBuilder(t, fixtureOpts...)
	for i, stream := range streamLabels {
		rows.ForStream(stream).Entry(1, "{}", fmt.Sprintf("line %d", i))
	}

	bucket := objstore.NewInMemBucket()
	t.Cleanup(func() { require.NoError(t, bucket.Close()) })

	path := fixtures.StoredDataObject(t, bucket,
		fixtures.StreamsSection(t, tenant, rows.Streams(), streamsOpts...),
		fixtures.LogsSection(t, tenant, rows.Logs()),
	)

	ids := make([]int64, 0, len(streamLabels))
	for _, stream := range rows.Streams() {
		ids = append(ids, stream.ID)
	}
	return bucket, path, ids
}

// createAndOpenTestStoredObject writes the object [createTestStoredObject] does and opens it for tenant.
func createAndOpenTestStoredObject(t *testing.T, tenant string, layout *streams.Layout, streamLabels ...string) (*openObject, []int64) {
	t.Helper()

	bucket, path, ids := createTestStoredObject(t, tenant, layout, streamLabels...)
	return openStoredObject(t, bucket, tenant, path), ids
}

// openStoredObject opens the object at path in bucket for tenant.
func openStoredObject(t *testing.T, bucket objstore.Bucket, tenant, path string) *openObject {
	t.Helper()

	objects := NewOpenObjects(bucket, tenant, DefaultHeadPrefetchBytes, nil)
	t.Cleanup(objects.release)

	object, err := objects.get(t.Context(), path)
	require.NoError(t, err)
	return object
}
