package storegateway

import (
	"bytes"
	"context"
	"encoding/json"
	"path"
	"testing"
	"time"

	"github.com/cortexproject/cortex/pkg/storage/bucket"
	"github.com/cortexproject/cortex/pkg/storage/tsdb/bucketindex"
	"github.com/go-kit/log"
	"github.com/oklog/ulid"
	"github.com/prometheus/client_golang/prometheus"
	promtest "github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thanos-io/thanos/pkg/block"
	"github.com/thanos-io/thanos/pkg/block/metadata"
	"github.com/thanos-io/thanos/pkg/extprom"
	"github.com/thanos-io/thanos/pkg/objstore"

	tsdb_testutil "github.com/grafana/loki/pkg/storage/tsdb/testutil"
)

func TestIgnoreDeletionMarkFilter_Filter(t *testing.T) {
	testIgnoreDeletionMarkFilter(t, false)
}

func TestIgnoreDeletionMarkFilter_FilterWithBucketIndex(t *testing.T) {
	testIgnoreDeletionMarkFilter(t, true)
}

func testIgnoreDeletionMarkFilter(t *testing.T, bucketIndexEnabled bool) {
	const userID = "user-1"

	now := time.Now()
	ctx := context.Background()
	logger := log.NewNopLogger()

	// Create a bucket backed by filesystem.
	bkt, _ := tsdb_testutil.PrepareFilesystemBucket(t)
	bkt = bucketindex.BucketWithGlobalMarkers(bkt)
	userBkt := bucket.NewUserBucketClient(userID, bkt, nil)

	shouldFetch := &metadata.DeletionMark{
		ID:           ulid.MustNew(1, nil),
		DeletionTime: now.Add(-15 * time.Hour).Unix(),
		Version:      1,
	}

	shouldIgnore := &metadata.DeletionMark{
		ID:           ulid.MustNew(2, nil),
		DeletionTime: now.Add(-60 * time.Hour).Unix(),
		Version:      1,
	}

	var buf bytes.Buffer
	require.NoError(t, json.NewEncoder(&buf).Encode(&shouldFetch))
	require.NoError(t, userBkt.Upload(ctx, path.Join(shouldFetch.ID.String(), metadata.DeletionMarkFilename), &buf))
	require.NoError(t, json.NewEncoder(&buf).Encode(&shouldIgnore))
	require.NoError(t, userBkt.Upload(ctx, path.Join(shouldIgnore.ID.String(), metadata.DeletionMarkFilename), &buf))
	require.NoError(t, userBkt.Upload(ctx, path.Join(ulid.MustNew(3, nil).String(), metadata.DeletionMarkFilename), bytes.NewBufferString("not a valid deletion-mark.json")))

	// Create the bucket index if required.
	var idx *bucketindex.Index
	if bucketIndexEnabled {
		var err error

		u := bucketindex.NewUpdater(bkt, userID, nil, logger)
		idx, _, err = u.UpdateIndex(ctx, nil)
		require.NoError(t, err)
		require.NoError(t, bucketindex.WriteIndex(ctx, bkt, userID, nil, idx))
	}

	inputMetas := map[ulid.ULID]*metadata.Meta{
		ulid.MustNew(1, nil): {},
		ulid.MustNew(2, nil): {},
		ulid.MustNew(3, nil): {},
		ulid.MustNew(4, nil): {},
	}

	expectedMetas := map[ulid.ULID]*metadata.Meta{
		ulid.MustNew(1, nil): {},
		ulid.MustNew(3, nil): {},
		ulid.MustNew(4, nil): {},
	}

	expectedDeletionMarks := map[ulid.ULID]*metadata.DeletionMark{
		ulid.MustNew(1, nil): shouldFetch,
		ulid.MustNew(2, nil): shouldIgnore,
	}

	synced := extprom.NewTxGaugeVec(nil, prometheus.GaugeOpts{Name: "synced"}, []string{"state"})
	f := NewIgnoreDeletionMarkFilter(logger, objstore.WithNoopInstr(userBkt), 48*time.Hour, 32)

	if bucketIndexEnabled {
		require.NoError(t, f.FilterWithBucketIndex(ctx, inputMetas, idx, synced))
	} else {
		require.NoError(t, f.Filter(ctx, inputMetas, synced))
	}

	assert.Equal(t, 1.0, promtest.ToFloat64(synced.WithLabelValues(block.MarkedForDeletionMeta)))
	assert.Equal(t, expectedMetas, inputMetas)
	assert.Equal(t, expectedDeletionMarks, f.DeletionMarkBlocks())
}
