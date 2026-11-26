package store

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/thanos-io/objstore"

	"github.com/grafana/loki/v3/pkg/logline/format"
)

func TestStore_GetIndex_ReturnsIndexData(t *testing.T) {
	bucket := objstore.NewInMemBucket()
	s := newTestStore(t, bucket)

	now := time.Now().UTC()
	indexContent := "binary index data"
	meta := Meta{
		Date: "2026-02-25", Hash: "abc1230000000000", Version: "v3",
		MinLogTs: now.Add(-1 * time.Hour), MaxLogTs: now,
		MinRecordTs: now.Add(-1 * time.Hour), MaxRecordTs: now,
		IndexHeader: &format.HeaderInfo{},
		SizeBytes:   int64(len(indexContent)),
	}
	err := s.PutIndex(context.Background(), strings.NewReader(indexContent), meta)
	require.NoError(t, err)

	rc, err := s.GetIndex(context.Background(), meta.ID())
	require.NoError(t, err)
	defer rc.Close()

	data, err := io.ReadAll(rc)
	require.NoError(t, err)
	require.Equal(t, indexContent, string(data))
}

func TestStore_GetIndex_NotFound(t *testing.T) {
	bucket := objstore.NewInMemBucket()
	s := newTestStore(t, bucket)

	_, err := s.GetIndex(context.Background(), "2026-02-25/1234567890abcdef")
	require.Error(t, err)
}

func TestStore_GetIndex_InvalidID(t *testing.T) {
	bucket := objstore.NewInMemBucket()
	s := newTestStore(t, bucket)

	_, err := s.GetIndex(context.Background(), "invalid-no-slash")
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid index id")
}

func TestStore_GetMeta_ReturnsPopulatedMeta(t *testing.T) {
	bucket := objstore.NewInMemBucket()
	s := newTestStore(t, bucket)

	now := time.Now().UTC().Truncate(time.Second)
	meta := Meta{
		Date: "2026-02-25", Hash: "deadbeef00000000", Version: "v3",
		MinLogTs: now.Add(-2 * time.Hour), MaxLogTs: now.Add(-1 * time.Hour),
		MinRecordTs: now.Add(-2 * time.Hour), MaxRecordTs: now.Add(-1 * time.Hour),
		CompactedFrom: []string{"2026-02-25/0000000000000001"},
		IndexHeader:   &format.HeaderInfo{},
		SizeBytes:     int64(len("data")),
	}
	err := s.PutIndex(context.Background(), strings.NewReader("data"), meta)
	require.NoError(t, err)

	got, err := s.GetMeta(context.Background(), meta.ID())
	require.NoError(t, err)
	// Path is the source of truth for ID; hash comes from meta.json.
	require.Equal(t, "2026-02-25", got.Date)
	require.Equal(t, "deadbeef00000000", got.Hash)
	require.Equal(t, "v3", got.Version)
	require.True(t, meta.MinLogTs.Equal(got.MinLogTs))
	require.True(t, meta.MaxLogTs.Equal(got.MaxLogTs))
	require.Equal(t, meta.CompactedFrom, got.CompactedFrom)
}

func TestStore_GetMeta_UsesPathStorageIDAndPreservesHash(t *testing.T) {
	bucket := objstore.NewInMemBucket()
	s := newTestStore(t, bucket)

	now := time.Now().UTC().Truncate(time.Second)
	meta := Meta{
		Date:        "2026-02-25",
		StorageID:   "builder-123",
		Hash:        "deadbeef00000000",
		Version:     "v3",
		MinLogTs:    now.Add(-2 * time.Hour),
		MaxLogTs:    now.Add(-1 * time.Hour),
		MinRecordTs: now.Add(-2 * time.Hour),
		MaxRecordTs: now.Add(-1 * time.Hour),
		IndexHeader: &format.HeaderInfo{},
		SizeBytes:   int64(len("data")),
	}
	err := s.PutIndex(context.Background(), strings.NewReader("data"), meta)
	require.NoError(t, err)

	got, err := s.GetMeta(context.Background(), meta.ID())
	require.NoError(t, err)
	require.Equal(t, "2026-02-25", got.Date)
	require.Equal(t, "builder-123", got.StorageID)
	require.Equal(t, "deadbeef00000000", got.Hash)
}

func TestStore_GetMeta_NotFound(t *testing.T) {
	bucket := objstore.NewInMemBucket()
	s := newTestStore(t, bucket)

	_, err := s.GetMeta(context.Background(), "2026-02-25/fedcba9876543210")
	require.Error(t, err)
}

func TestStore_GetMeta_InvalidID(t *testing.T) {
	bucket := objstore.NewInMemBucket()
	s := newTestStore(t, bucket)

	_, err := s.GetMeta(context.Background(), "noslash")
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid index id")
}

func TestStore_GetMeta_InvalidID_RejectsSlashInStorageID(t *testing.T) {
	bucket := objstore.NewInMemBucket()
	s := newTestStore(t, bucket)

	_, err := s.GetMeta(context.Background(), "2026-02-25/a/b")
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid storage id")
}

func TestStore_GetIndex_PathTraversal(t *testing.T) {
	bucket := objstore.NewInMemBucket()
	s := newTestStore(t, bucket)
	_, err := s.GetIndex(context.Background(), "../../etc/passwd")
	require.Error(t, err)
	require.Contains(t, err.Error(), "bad date")
}

func TestStore_GetMeta_PopulatesSizeBytes(t *testing.T) {
	bucket := objstore.NewInMemBucket()
	s := newTestStore(t, bucket)

	now := time.Now().UTC()
	indexContent := "some index bytes"
	meta := Meta{
		Date: "2026-02-25", Hash: "aabbccdd00000000", Version: "v3",
		MinLogTs: now.Add(-1 * time.Hour), MaxLogTs: now,
		MinRecordTs: now.Add(-1 * time.Hour), MaxRecordTs: now,
		IndexHeader: &format.HeaderInfo{},
		SizeBytes:   int64(len(indexContent)),
	}
	err := s.PutIndex(context.Background(), strings.NewReader(indexContent), meta)
	require.NoError(t, err)

	got, err := s.GetMeta(context.Background(), meta.ID())
	require.NoError(t, err)
	require.Equal(t, int64(len(indexContent)), got.SizeBytes)
}

func TestStore_GetMeta_UsesSizeBytesFromMeta(t *testing.T) {
	bucket := objstore.NewInMemBucket()
	s := newTestStore(t, bucket)

	now := time.Now().UTC()
	indexContent := "some index bytes"
	meta := Meta{
		Date: "2026-02-25", Hash: "aabbccdd00000001", Version: "v3",
		MinLogTs: now.Add(-1 * time.Hour), MaxLogTs: now,
		MinRecordTs: now.Add(-1 * time.Hour), MaxRecordTs: now,
		IndexHeader: &format.HeaderInfo{},
		SizeBytes:   int64(len(indexContent)),
	}
	err := s.PutIndex(context.Background(), strings.NewReader(indexContent), meta)
	require.NoError(t, err)

	got, err := s.GetMeta(context.Background(), meta.ID())
	require.NoError(t, err)
	require.Equal(t, int64(len(indexContent)), got.SizeBytes)
}

func TestStore_GetMeta_MismatchedJSONID_ReturnsError(t *testing.T) {
	bucket := objstore.NewInMemBucket()
	s := newTestStore(t, bucket)

	now := time.Now().UTC().Truncate(time.Second)
	meta := Meta{
		Date:        "2026-02-25",
		StorageID:   "builder-123",
		Hash:        "deadbeef00000000",
		Version:     "v3",
		MinLogTs:    now.Add(-2 * time.Hour),
		MaxLogTs:    now.Add(-1 * time.Hour),
		MinRecordTs: now.Add(-2 * time.Hour),
		MaxRecordTs: now.Add(-1 * time.Hour),
		IndexHeader: &format.HeaderInfo{},
		SizeBytes:   int64(len("data")),
	}
	err := s.PutIndex(context.Background(), strings.NewReader("data"), meta)
	require.NoError(t, err)

	meta.StorageID = "other-id"
	metaBytes, err := json.Marshal(meta)
	require.NoError(t, err)
	require.NoError(t, bucket.Upload(context.Background(), "2026-02-25/builder-123/meta.json", strings.NewReader(string(metaBytes))))

	_, err = s.GetMeta(context.Background(), "2026-02-25/builder-123")
	require.Error(t, err)
	require.Contains(t, err.Error(), "mismatched")
}
