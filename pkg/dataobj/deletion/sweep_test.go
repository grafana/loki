package deletion

import (
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/stretchr/testify/require"
	"github.com/thanos-io/objstore"
)

func TestSweeper_Sweep(t *testing.T) {
	notADataObject := []byte("not a data object")
	now := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)
	old := now.Add(-2 * time.Hour)

	newSweeper := func(t *testing.T, bucket objstore.Bucket) *Sweeper {
		t.Helper()
		sweeper, err := NewSweeper(bucket, log.NewNopLogger())
		require.NoError(t, err)
		return sweeper
	}

	t.Run("scans and stores the list when no list is stored", func(t *testing.T) {
		bucket := newFileBucket(t)
		bucket.put(t, "objects/00/single-a", objectBytes(t, "a"), old)

		outcome, err := newSweeper(t, bucket).Sweep(t.Context(), "a", now)
		require.NoError(t, err)
		require.Equal(t, SweepScanned, outcome)
		require.Equal(t, FileList{Tenant: "a", CreatedAt: now, Paths: []string{"objects/00/single-a"}}, readFileList(t, bucket, "a"))
	})

	t.Run("skips the scan and keeps the stored list when it is newer than MinSweepInterval", func(t *testing.T) {
		files := newFileBucket(t)
		files.put(t, "objects/00/single-a", objectBytes(t, "a"), old)
		stored := FileList{Tenant: "a", CreatedAt: now.Add(-MinSweepInterval + time.Second), Paths: []string{"objects/00/stored"}}
		storeFileList(t, files, stored)
		bucket := &listRecordingBucket{fileBucket: files}

		outcome, err := newSweeper(t, bucket).Sweep(t.Context(), "a", now)
		require.NoError(t, err)
		require.Equal(t, SweepSkipped, outcome)
		require.Empty(t, bucket.dirs)
		require.Equal(t, stored, readFileList(t, files, "a"))
	})

	t.Run("scans and replaces a stored list that is MinSweepInterval old", func(t *testing.T) {
		bucket := newFileBucket(t)
		bucket.put(t, "objects/00/single-a", objectBytes(t, "a"), old)
		storeFileList(t, bucket, FileList{Tenant: "a", CreatedAt: now.Add(-MinSweepInterval)})

		outcome, err := newSweeper(t, bucket).Sweep(t.Context(), "a", now)
		require.NoError(t, err)
		require.Equal(t, SweepScanned, outcome)
		require.Equal(t, FileList{Tenant: "a", CreatedAt: now, Paths: []string{"objects/00/single-a"}}, readFileList(t, bucket, "a"))
	})

	t.Run("returns SweepSuperseded and keeps a newer list that a concurrent sweep stored", func(t *testing.T) {
		files := newFileBucket(t)
		files.put(t, "objects/00/single-a", objectBytes(t, "a"), old)
		newer := FileList{Tenant: "a", CreatedAt: now.Add(time.Minute), Paths: []string{"objects/00/newer"}}
		bucket := &conflictBucket{Bucket: files, conflicts: 1, concurrentWrite: func() { storeFileList(t, files, newer) }}

		outcome, err := newSweeper(t, bucket).Sweep(t.Context(), "a", now)
		require.NoError(t, err)
		require.Equal(t, SweepSuperseded, outcome)
		require.Equal(t, newer, readFileList(t, files, "a"))
	})

	t.Run("returns ErrMalformedFileList without a scan and keeps a malformed stored list", func(t *testing.T) {
		files := newFileBucket(t)
		upload(t, files, fileListPath("a"), []byte("not gzip"))
		bucket := &listRecordingBucket{fileBucket: files}

		_, err := newSweeper(t, bucket).Sweep(t.Context(), "a", now)
		require.ErrorIs(t, err, ErrMalformedFileList)
		require.Empty(t, bucket.dirs)
		require.Equal(t, "not gzip", readRaw(t, files, fileListPath("a")))
	})

	t.Run("returns an error and stores no list when the scan fails", func(t *testing.T) {
		bucket := newFileBucket(t)
		bucket.put(t, "objects/00/corrupt", notADataObject, old)

		_, err := newSweeper(t, bucket).Sweep(t.Context(), "a", now)
		require.ErrorContains(t, err, "objects/00/corrupt")
		exists, err := bucket.Exists(t.Context(), fileListPath("a"))
		require.NoError(t, err)
		require.False(t, exists)
	})
}

func TestListCompactedTenants(t *testing.T) {
	t.Run("returns the tenants of the compacted log and index directories", func(t *testing.T) {
		bucket := newFileBucket(t)
		upload(t, bucket, "objects/tenants/a/aa/compacted", []byte("x"))
		upload(t, bucket, "objects/tenants/b/aa/compacted", []byte("x"))
		upload(t, bucket, "index/v0/indexes/tenants/b/bb/compacted", []byte("x"))
		upload(t, bucket, "index/v0/indexes/tenants/c/bb/compacted", []byte("x"))
		upload(t, bucket, "objects/00/uncompacted", []byte("x"))
		upload(t, bucket, "objects/tenants/stray-file", []byte("x"))

		tenants, err := ListCompactedTenants(t.Context(), bucket, log.NewNopLogger())
		require.NoError(t, err)
		require.Equal(t, map[string]struct{}{"a": {}, "b": {}, "c": {}}, tenants)
	})

	t.Run("skips a directory name that is not a valid tenant", func(t *testing.T) {
		bucket := newFileBucket(t)
		upload(t, bucket, "objects/tenants/a/aa/compacted", []byte("x"))
		upload(t, bucket, "objects/tenants/not a tenant/aa/compacted", []byte("x"))

		tenants, err := ListCompactedTenants(t.Context(), bucket, log.NewNopLogger())
		require.NoError(t, err)
		require.Equal(t, map[string]struct{}{"a": {}}, tenants)
	})

	t.Run("returns no tenants when no compacted objects exist", func(t *testing.T) {
		tenants, err := ListCompactedTenants(t.Context(), newFileBucket(t), log.NewNopLogger())
		require.NoError(t, err)
		require.Empty(t, tenants)
	})
}
