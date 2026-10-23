package metastore

import (
	"bytes"
	"testing"

	"github.com/go-kit/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"
)

func TestSectionsDeduplicator(t *testing.T) {
	ctx := t.Context()

	descriptor := func(objectPath string, section int64, rowCount int, size int64, streamIDs ...int64) *DataobjSectionDescriptor {
		return &DataobjSectionDescriptor{
			SectionKey: SectionKey{ObjectPath: objectPath, SectionIdx: section},
			StreamIDs:  streamIDs,
			RowCount:   rowCount,
			Size:       size,
		}
	}
	fromIndex := func(indexPath string, descriptors ...*DataobjSectionDescriptor) []DataobjSectionDescriptorWithIndex {
		sections := make([]DataobjSectionDescriptorWithIndex, 0, len(descriptors))
		for _, descriptor := range descriptors {
			sections = append(sections, DataobjSectionDescriptorWithIndex{descriptor, indexPath})
		}
		return sections
	}
	newDeduplicator := func() (*sectionsDeduplicator, *ObjectMetastoreMetrics, *bytes.Buffer) {
		logs := bytes.NewBuffer(nil)
		metrics := NewObjectMetastoreMetrics(prometheus.NewRegistry())
		return newSectionsDeduplicator(log.NewLogfmtLogger(logs), metrics), metrics, logs
	}
	requireDuplicates := func(t *testing.T, metrics *ObjectMetastoreMetrics, wantIdentical, wantDiverged int) {
		t.Helper()

		require.Equal(t, float64(wantIdentical), testutil.ToFloat64(metrics.duplicateSectionsTotal.WithLabelValues(divergedFalse)))
		require.Equal(t, float64(wantDiverged), testutil.ToFloat64(metrics.duplicateSectionsTotal.WithLabelValues(divergedTrue)))
	}

	t.Run("returns every section of a batch in input order when none repeats", func(t *testing.T) {
		deduplicator, metrics, _ := newDeduplicator()
		batch := []*DataobjSectionDescriptor{descriptor("obj-b", 0, 10, 100, 1), descriptor("obj-a", 1, 10, 100, 1), descriptor("obj-a", 0, 10, 100, 1)}

		kept, err := deduplicator.deduplicate(ctx, fromIndex("index-1", batch...))

		require.NoError(t, err)
		require.Equal(t, batch, kept)
		requireDuplicates(t, metrics, 0, 0)
	})

	t.Run("keeps sections that share an index but belong to different objects", func(t *testing.T) {
		deduplicator, metrics, _ := newDeduplicator()

		kept, err := deduplicator.deduplicate(ctx, fromIndex("index-1", descriptor("obj-a", 0, 10, 100, 1), descriptor("obj-b", 0, 10, 100, 1)))

		require.NoError(t, err)
		require.Len(t, kept, 2)
		requireDuplicates(t, metrics, 0, 0)
	})

	t.Run("returns the first copy and drops an identical copy from a later batch", func(t *testing.T) {
		deduplicator, metrics, logs := newDeduplicator()
		first := descriptor("obj-a", 0, 10, 100, 1, 2)
		second := descriptor("obj-a", 0, 10, 100, 1, 2)

		keptFirst, err := deduplicator.deduplicate(ctx, fromIndex("index-1", first))
		require.NoError(t, err)
		keptSecond, err := deduplicator.deduplicate(ctx, fromIndex("index-2", second))
		require.NoError(t, err)

		require.Len(t, keptFirst, 1)
		require.Same(t, first, keptFirst[0])
		require.Empty(t, keptSecond)
		requireDuplicates(t, metrics, 1, 0)
		require.Contains(t, logs.String(), "kept_index=index-1")
		require.Contains(t, logs.String(), "dropped_index=index-2")
	})

	t.Run("drops an identical copy from the same batch", func(t *testing.T) {
		deduplicator, metrics, _ := newDeduplicator()
		first := descriptor("obj-a", 0, 10, 100, 1)

		kept, err := deduplicator.deduplicate(ctx, fromIndex("index-1", first, descriptor("obj-a", 0, 10, 100, 1)))

		require.NoError(t, err)
		require.Len(t, kept, 1)
		require.Same(t, first, kept[0])
		requireDuplicates(t, metrics, 1, 0)
	})

	t.Run("counts one duplicate for each extra copy across batches", func(t *testing.T) {
		deduplicator, metrics, _ := newDeduplicator()

		for _, indexPath := range []string{"index-1", "index-2", "index-3"} {
			_, err := deduplicator.deduplicate(ctx, fromIndex(indexPath, descriptor("obj-a", 0, 10, 100, 1)))
			require.NoError(t, err)
		}

		requireDuplicates(t, metrics, 2, 0)
	})

	t.Run("drops a copy that lists the same streams in another order", func(t *testing.T) {
		deduplicator, metrics, _ := newDeduplicator()

		_, err := deduplicator.deduplicate(ctx, fromIndex("index-1", descriptor("obj-a", 0, 10, 100, 3, 1, 2)))
		require.NoError(t, err)
		kept, err := deduplicator.deduplicate(ctx, fromIndex("index-2", descriptor("obj-a", 0, 10, 100, 1, 2, 3)))

		require.NoError(t, err)
		require.Empty(t, kept)
		requireDuplicates(t, metrics, 1, 0)
	})

	t.Run("drops a postings copy with a zero row count and size after a copy that has them", func(t *testing.T) {
		deduplicator, metrics, _ := newDeduplicator()

		_, err := deduplicator.deduplicate(ctx, fromIndex("index-1", descriptor("obj-a", 0, 10, 100, 1)))
		require.NoError(t, err)
		kept, err := deduplicator.deduplicate(ctx, fromIndex("index-2", descriptor("obj-a", 0, 0, 0, 1)))

		require.NoError(t, err)
		require.Empty(t, kept)
		requireDuplicates(t, metrics, 1, 0)
	})

	t.Run("drops a copy that has a row count and size after a postings copy with zero", func(t *testing.T) {
		deduplicator, metrics, _ := newDeduplicator()

		_, err := deduplicator.deduplicate(ctx, fromIndex("index-1", descriptor("obj-a", 0, 0, 0, 1)))
		require.NoError(t, err)
		kept, err := deduplicator.deduplicate(ctx, fromIndex("index-2", descriptor("obj-a", 0, 10, 100, 1)))

		require.NoError(t, err)
		require.Empty(t, kept)
		requireDuplicates(t, metrics, 1, 0)
	})

	t.Run("returns an error and counts one diverged duplicate when a later batch lists other streams", func(t *testing.T) {
		deduplicator, metrics, logs := newDeduplicator()

		_, err := deduplicator.deduplicate(ctx, fromIndex("index-1", descriptor("obj-a", 3, 10, 100, 1, 2)))
		require.NoError(t, err)
		kept, err := deduplicator.deduplicate(ctx, fromIndex("index-2", descriptor("obj-a", 3, 10, 100, 1, 2, 3)))

		require.ErrorIs(t, err, errDifferentSectionStatistics)
		require.Nil(t, kept)
		requireDuplicates(t, metrics, 0, 1)
		require.Contains(t, logs.String(), "level=error")
		require.Contains(t, logs.String(), "object=obj-a")
		require.Contains(t, logs.String(), "section=3")
		require.Contains(t, logs.String(), "first_index=index-1")
		require.Contains(t, logs.String(), "second_index=index-2")
	})

	t.Run("returns an error and no section when a copy in the same batch has another row count", func(t *testing.T) {
		deduplicator, metrics, _ := newDeduplicator()

		kept, err := deduplicator.deduplicate(ctx, fromIndex("index-1", descriptor("obj-b", 0, 10, 100, 1), descriptor("obj-a", 0, 10, 100, 1), descriptor("obj-a", 0, 11, 100, 1)))

		require.ErrorIs(t, err, errDifferentSectionStatistics)
		require.Nil(t, kept)
		requireDuplicates(t, metrics, 0, 1)
	})

	t.Run("returns an error when a later batch lists the section with another size", func(t *testing.T) {
		deduplicator, metrics, _ := newDeduplicator()

		_, err := deduplicator.deduplicate(ctx, fromIndex("index-1", descriptor("obj-a", 0, 10, 100, 1)))
		require.NoError(t, err)
		_, err = deduplicator.deduplicate(ctx, fromIndex("index-2", descriptor("obj-a", 0, 10, 200, 1)))

		require.ErrorIs(t, err, errDifferentSectionStatistics)
		requireDuplicates(t, metrics, 0, 1)
	})
}
