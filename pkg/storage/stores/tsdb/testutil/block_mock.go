package testutil

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/oklog/ulid"
	"github.com/prometheus/prometheus/tsdb"
	"github.com/stretchr/testify/require"
	"github.com/thanos-io/thanos/pkg/block/metadata"
	"github.com/thanos-io/thanos/pkg/objstore"
)

func MockStorageBlock(t testing.TB, bucket objstore.Bucket, userID string, minT, maxT int64) tsdb.BlockMeta {
	// Generate a block ID whose timestamp matches the maxT (for simplicity we assume it
	// has been compacted and shipped in zero time, even if not realistic).
	id := ulid.MustNew(uint64(maxT), rand.Reader)

	meta := tsdb.BlockMeta{
		Version: 1,
		ULID:    id,
		MinTime: minT,
		MaxTime: maxT,
		Compaction: tsdb.BlockMetaCompaction{
			Level:   1,
			Sources: []ulid.ULID{id},
		},
	}

	metaContent, err := json.Marshal(meta)
	if err != nil {
		panic("failed to marshal mocked block meta")
	}

	metaContentReader := strings.NewReader(string(metaContent))
	metaPath := fmt.Sprintf("%s/%s/meta.json", userID, id.String())
	require.NoError(t, bucket.Upload(context.Background(), metaPath, metaContentReader))

	// Upload an empty index, just to make sure the meta.json is not the only object in the block location.
	indexPath := fmt.Sprintf("%s/%s/index", userID, id.String())
	require.NoError(t, bucket.Upload(context.Background(), indexPath, strings.NewReader("")))

	return meta
}

func MockStorageDeletionMark(t testing.TB, bucket objstore.Bucket, userID string, meta tsdb.BlockMeta) *metadata.DeletionMark {
	mark := metadata.DeletionMark{
		ID:           meta.ULID,
		DeletionTime: time.Now().Add(-time.Minute).Unix(),
		Version:      metadata.DeletionMarkVersion1,
	}

	markContent, err := json.Marshal(mark)
	if err != nil {
		panic("failed to marshal mocked block meta")
	}

	markContentReader := strings.NewReader(string(markContent))
	markPath := fmt.Sprintf("%s/%s/%s", userID, meta.ULID.String(), metadata.DeletionMarkFilename)
	require.NoError(t, bucket.Upload(context.Background(), markPath, markContentReader))

	return &mark
}
