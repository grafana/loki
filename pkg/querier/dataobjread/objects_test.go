package dataobjread

import (
	"fmt"
	"slices"
	"testing"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"
	"github.com/thanos-io/objstore"

	"github.com/grafana/loki/v3/pkg/dataobj/fixtures"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
)

func TestOpenObjects(t *testing.T) {
	const fixtureTenant = "fixture-tenant"

	t.Run("a section read of one object is shared by every caller that asks for it", func(t *testing.T) {
		bucket, path, _ := createTestStoredObject(t, fixtureTenant, `{app="a"}`)
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
		object, _ := createAndOpenTestStoredObject(t, fixtureTenant, `{app="a"}`)

		_, err := object.logsSection(t.Context(), 999)
		require.ErrorContains(t, err, "holds no logs section 999")
	})

	t.Run("opening an object that is not in the bucket fails", func(t *testing.T) {
		bucket, _, _ := createTestStoredObject(t, fixtureTenant, `{app="a"}`)
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
		object, ids := createAndOpenTestStoredObject(t, fixtureTenant, fixtureStreams...)

		decoded, err := object.streamLabels(t.Context(), ids[:1], nil)
		require.NoError(t, err)
		require.Len(t, decoded, 1)
		require.Contains(t, decoded, ids[0])
	})

	t.Run("an empty set of stream IDs reads nothing", func(t *testing.T) {
		object, _ := createAndOpenTestStoredObject(t, fixtureTenant, fixtureStreams...)

		decoded, err := object.streamLabels(t.Context(), nil, nil)
		require.NoError(t, err)
		require.Empty(t, decoded)
	})

	t.Run("a bucket range covering every bucket keeps every stream", func(t *testing.T) {
		object, ids := createAndOpenTestStoredObject(t, fixtureTenant, fixtureStreams...)

		decoded, err := object.streamLabels(t.Context(), ids, &shardBucketRange{from: 0, to: streams.ShardFactor - 1})
		require.NoError(t, err)
		require.Len(t, decoded, len(ids))
	})

	t.Run("a bucket range keeps only the streams whose bucket falls inside it", func(t *testing.T) {
		object, ids := createAndOpenTestStoredObject(t, fixtureTenant, fixtureStreams...)

		all, err := object.streamLabels(t.Context(), ids, nil)
		require.NoError(t, err)

		bucketOf := func(id int64) uint32 {
			return streams.ShardBucketFromHash(labels.StableHash(all[id]))
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
			require.Equal(t, bucket, streams.ShardBucketFromHash(labels.StableHash(streamLabels)), "stream %d is outside the pruned bucket", id)
		}
	})
}

// createTestStoredObject writes one data object holding the given streams for tenant, to a fresh
// in-memory bucket. It returns the bucket, the object's path, and the stream ID the fixture gave
// each stream, in the order the streams were named.
//
// The bucket carries no index, so the object is reachable by path but resolves through no
// metastore. Use [newObjectsFixture] for a test that needs resolution.
func createTestStoredObject(t *testing.T, tenant string, streamLabels ...string) (objstore.Bucket, string, []int64) {
	t.Helper()

	rows := fixtures.NewLogsFixtureBuilder(t)
	for i, stream := range streamLabels {
		rows.ForStream(stream).Entry(1, "{}", fmt.Sprintf("line %d", i))
	}

	bucket := objstore.NewInMemBucket()
	t.Cleanup(func() { require.NoError(t, bucket.Close()) })

	path := fixtures.StoredDataObject(t, bucket,
		fixtures.StreamsSection(t, tenant, rows.Streams()),
		fixtures.LogsSection(t, tenant, rows.Logs()),
	)

	ids := make([]int64, 0, len(streamLabels))
	for _, stream := range rows.Streams() {
		ids = append(ids, stream.ID)
	}
	return bucket, path, ids
}

// createAndOpenTestStoredObject writes the object [newTestStoredObject] does and opens it for tenant.
func createAndOpenTestStoredObject(t *testing.T, tenant string, streamLabels ...string) (*openObject, []int64) {
	t.Helper()

	bucket, path, ids := createTestStoredObject(t, tenant, streamLabels...)
	objects := NewOpenObjects(bucket, tenant, DefaultHeadPrefetchBytes, nil)
	t.Cleanup(objects.release)

	object, err := objects.get(t.Context(), path)
	require.NoError(t, err)
	return object, ids
}
