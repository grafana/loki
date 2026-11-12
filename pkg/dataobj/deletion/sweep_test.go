package deletion

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"
	"github.com/thanos-io/objstore"

	"github.com/grafana/loki/v3/pkg/dataobj/index/indexobj"
	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
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
	newLoggedSweeper := func(t *testing.T, bucket objstore.Bucket) (*Sweeper, *bytes.Buffer) {
		t.Helper()
		var logs bytes.Buffer
		sweeper, err := NewSweeper(bucket, log.NewLogfmtLogger(log.NewSyncWriter(&logs)))
		require.NoError(t, err)
		return sweeper, &logs
	}
	newDeletingSweeper := func(t *testing.T, bucket objstore.Bucket) (*Sweeper, *SweepMetrics, *bytes.Buffer) {
		t.Helper()
		var logs bytes.Buffer
		metrics := NewSweepMetrics(prometheus.NewRegistry())
		sweeper, err := NewDeletingSweeper(bucket, metrics, log.NewLogfmtLogger(log.NewSyncWriter(&logs)))
		require.NoError(t, err)
		return sweeper, metrics, &logs
	}
	stale := now.Add(-MinSweepInterval)
	requireExists := func(t *testing.T, bucket objstore.BucketReader, want bool, paths ...string) {
		t.Helper()
		for _, p := range paths {
			exists, err := bucket.Exists(t.Context(), p)
			require.NoError(t, err)
			require.Equal(t, want, exists, p)
		}
	}
	// storeAnyReachableIndex stores a reachable index object, so that the scan
	// finds a reachable object. Sweep fails when no scanned object is
	// reachable.
	storeAnyReachableIndex := func(t *testing.T, bucket fileBucket) {
		t.Helper()
		storeReachableIndex(t, bucket, "a", "indexes/tenants/a/ff/index", []string{"objects/tenants/a/ff/not-stored"}, old)
	}

	t.Run("stores only the scanned objects that the ToCs of the tenant cannot reach", func(t *testing.T) {
		bucket := newFileBucket(t)
		bucket.put(t, "objects/tenants/a/00/reachable", notADataObject, old)
		bucket.put(t, "objects/tenants/a/01/unreachable", notADataObject, old)
		storeReachableIndex(t, bucket, "a", "indexes/tenants/a/aa/index", []string{"objects/tenants/a/00/reachable"}, old)

		outcome, err := newSweeper(t, bucket).Sweep(t.Context(), "a", now)
		require.NoError(t, err)
		require.Equal(t, SweepScanned, outcome)
		require.Equal(t, FileList{Tenant: "a", CreatedAt: now, Paths: []string{"objects/tenants/a/01/unreachable"}}, readFileList(t, bucket, "a"))
	})

	t.Run("stores the list and logs the missing index objects when a ToC points to an index object that no longer exists", func(t *testing.T) {
		bucket := newFileBucket(t)
		bucket.put(t, "objects/tenants/a/00/reachable", notADataObject, old)
		bucket.put(t, "objects/tenants/a/01/only-in-expired-index", notADataObject, old)
		storeReachableIndex(t, bucket, "a", "indexes/tenants/a/aa/index", []string{"objects/tenants/a/00/reachable"}, old)
		storeReachableIndex(t, bucket, "a", "indexes/53/expired", []string{"objects/tenants/a/01/only-in-expired-index"}, old)
		require.NoError(t, bucket.Delete(t.Context(), "index/v0/indexes/53/expired"))
		sweeper, logs := newLoggedSweeper(t, bucket)

		outcome, err := sweeper.Sweep(t.Context(), "a", now)
		require.NoError(t, err)
		require.Equal(t, SweepScanned, outcome)
		require.Contains(t, logs.String(), "scanned=3 reachable=2 unreachable=1 missing_index_objects=1")
		require.Equal(t, []string{"objects/tenants/a/01/only-in-expired-index"}, readFileList(t, bucket, "a").Paths)
	})

	t.Run("logs the objects that are unreachable in both the stored list and the scan as deletion candidates and deletes nothing", func(t *testing.T) {
		bucket := newFileBucket(t)
		bucket.put(t, "index/v0/indexes/tenants/a/00/orphan-index", notADataObject, old)
		bucket.put(t, "objects/tenants/a/00/reachable-now", notADataObject, old)
		bucket.put(t, "objects/tenants/a/01/orphan", notADataObject, old)
		bucket.put(t, "objects/tenants/a/02/new-orphan", notADataObject, old)
		storeReachableIndex(t, bucket, "a", "indexes/tenants/a/aa/index", []string{"objects/tenants/a/00/reachable-now"}, old)
		storeFileList(t, bucket, FileList{Tenant: "a", CreatedAt: stale, Paths: []string{
			"index/v0/indexes/tenants/a/00/orphan-index",
			"objects/tenants/a/00/reachable-now",
			"objects/tenants/a/01/orphan",
			"objects/tenants/a/09/gone",
		}})
		sweeper, logs := newLoggedSweeper(t, bucket)

		outcome, err := sweeper.Sweep(t.Context(), "a", now)
		require.NoError(t, err)
		require.Equal(t, SweepScanned, outcome)
		require.Contains(t, logs.String(), `msg="deletion sweep dry run: would delete objects"`)
		require.Contains(t, logs.String(), "previous_list_created_at="+stale.Format(time.RFC3339)+" list_created_at="+now.Format(time.RFC3339)+" previous_list_paths=4 list_paths=3 candidates=2 log_objects=1 index_objects=1\n")
		lastModified := old.Format(time.RFC3339)
		require.Contains(t, logs.String(), `tenant=a msg="deletion sweep stored unreachable objects" scanned=5 reachable=2 unreachable=3 missing_index_objects=0`)
		require.Contains(t, logs.String(), `tenant=a msg="deletion sweep dry run: candidate" kind=index path=index/v0/indexes/tenants/a/00/orphan-index last_modified=`+lastModified+" size=")
		require.Contains(t, logs.String(), `tenant=a msg="deletion sweep dry run: candidate" kind=log path=objects/tenants/a/01/orphan last_modified=`+lastModified+" size=")
		require.NotContains(t, logs.String(), "path=objects/tenants/a/00/reachable-now last_modified")
		require.NotContains(t, logs.String(), "path=objects/tenants/a/02/new-orphan last_modified")
		requireExists(t, bucket, true, "index/v0/indexes/tenants/a/00/orphan-index", "objects/tenants/a/01/orphan", "objects/tenants/a/02/new-orphan")
		require.Equal(t, FileList{Tenant: "a", CreatedAt: now, Paths: []string{
			"index/v0/indexes/tenants/a/00/orphan-index",
			"objects/tenants/a/01/orphan",
			"objects/tenants/a/02/new-orphan",
		}}, readFileList(t, bucket, "a"))
	})

	// storeCandidates stores an index candidate, a log candidate, an object
	// that is reachable now, and a new orphan. It returns the candidates.
	storeCandidates := func(t *testing.T, bucket fileBucket) []string {
		t.Helper()
		bucket.put(t, "index/v0/indexes/tenants/a/00/orphan-index", notADataObject, old)
		bucket.put(t, "objects/tenants/a/00/reachable-now", notADataObject, old)
		bucket.put(t, "objects/tenants/a/01/orphan", notADataObject, old)
		bucket.put(t, "objects/tenants/a/02/new-orphan", notADataObject, old)
		storeReachableIndex(t, bucket, "a", "indexes/tenants/a/aa/index", []string{"objects/tenants/a/00/reachable-now"}, old)
		storeFileList(t, bucket, FileList{Tenant: "a", CreatedAt: stale, Paths: []string{
			"index/v0/indexes/tenants/a/00/orphan-index",
			"objects/tenants/a/00/reachable-now",
			"objects/tenants/a/01/orphan",
		}})
		return []string{"index/v0/indexes/tenants/a/00/orphan-index", "objects/tenants/a/01/orphan"}
	}
	requireCounts := func(t *testing.T, metrics *SweepMetrics, deletedLog, deletedIndex, failedLog, failedIndex float64) {
		t.Helper()
		require.Equal(t, deletedLog, testutil.ToFloat64(metrics.deletedObjects.WithLabelValues(kindLog)))
		require.Equal(t, deletedIndex, testutil.ToFloat64(metrics.deletedObjects.WithLabelValues(kindIndex)))
		require.Equal(t, failedLog, testutil.ToFloat64(metrics.failedDeletes.WithLabelValues(kindLog)))
		require.Equal(t, failedIndex, testutil.ToFloat64(metrics.failedDeletes.WithLabelValues(kindIndex)))
	}

	t.Run("deletes the index and log candidates and keeps the reachable objects and the new orphans", func(t *testing.T) {
		bucket := newFileBucket(t)
		cands := storeCandidates(t, bucket)
		sweeper, metrics, logs := newDeletingSweeper(t, bucket)

		outcome, err := sweeper.Sweep(t.Context(), "a", now)
		require.NoError(t, err)
		require.Equal(t, SweepScanned, outcome)
		requireExists(t, bucket, false, cands...)
		requireExists(t, bucket, true, "objects/tenants/a/00/reachable-now", "objects/tenants/a/02/new-orphan", "index/v0/indexes/tenants/a/aa/index")
		requireCounts(t, metrics, 1, 1, 0, 0)
		require.Contains(t, logs.String(), `tenant=a msg="deletion sweep deleted candidates" previous_list_created_at=`+stale.Format(time.RFC3339)+" list_created_at="+now.Format(time.RFC3339)+" candidates=2 deleted_log_objects=1 deleted_index_objects=1 failed_log_objects=0 failed_index_objects=0\n")
		require.NotContains(t, logs.String(), "dry run")
		require.Equal(t, FileList{Tenant: "a", CreatedAt: now, Paths: []string{
			"index/v0/indexes/tenants/a/00/orphan-index",
			"objects/tenants/a/01/orphan",
			"objects/tenants/a/02/new-orphan",
		}}, readFileList(t, bucket, "a"))
	})

	t.Run("deletes nothing when no list is stored", func(t *testing.T) {
		bucket := newFileBucket(t)
		storeAnyReachableIndex(t, bucket)
		bucket.put(t, "objects/tenants/a/00/orphan", notADataObject, old)
		sweeper, metrics, logs := newDeletingSweeper(t, bucket)

		outcome, err := sweeper.Sweep(t.Context(), "a", now)
		require.NoError(t, err)
		require.Equal(t, SweepScanned, outcome)
		requireExists(t, bucket, true, "objects/tenants/a/00/orphan")
		requireCounts(t, metrics, 0, 0, 0, 0)
		require.NotContains(t, logs.String(), "deleted candidates")
	})

	t.Run("deletes nothing and returns SweepSuperseded when a concurrent sweep stores its list first", func(t *testing.T) {
		files := newFileBucket(t)
		cands := storeCandidates(t, files)
		newer := FileList{Tenant: "a", CreatedAt: now.Add(time.Minute), Paths: []string{"objects/tenants/a/00/newer"}}
		bucket := &conflictBucket{Bucket: files, conflicts: 1, concurrentWrite: func() { storeFileList(t, files, newer) }}
		sweeper, metrics, _ := newDeletingSweeper(t, bucket)

		outcome, err := sweeper.Sweep(t.Context(), "a", now)
		require.NoError(t, err)
		require.Equal(t, SweepSuperseded, outcome)
		requireExists(t, files, true, cands...)
		requireCounts(t, metrics, 0, 0, 0, 0)
	})

	t.Run("deletes nothing and returns SweepSuperseded when the stored list changes during the scan", func(t *testing.T) {
		files := newFileBucket(t)
		cands := storeCandidates(t, files)
		replacement := FileList{Tenant: "a", CreatedAt: now.Add(-time.Minute), Paths: cands}
		bucket := &onFirstListingBucket{fileBucket: files, hook: func() { storeFileList(t, files, replacement) }}
		sweeper, metrics, _ := newDeletingSweeper(t, bucket)

		outcome, err := sweeper.Sweep(t.Context(), "a", now)
		require.NoError(t, err)
		require.Equal(t, SweepSuperseded, outcome)
		requireExists(t, files, true, cands...)
		requireCounts(t, metrics, 0, 0, 0, 0)
	})

	t.Run("deletes nothing and returns the write error when the list cannot be stored", func(t *testing.T) {
		files := newFileBucket(t)
		cands := storeCandidates(t, files)
		bucket := &malformedOnReplaceBucket{fileBucket: files}
		sweeper, metrics, _ := newDeletingSweeper(t, bucket)

		_, err := sweeper.Sweep(t.Context(), "a", now)
		require.ErrorContains(t, err, "storing file list")
		requireExists(t, files, true, cands...)
		requireCounts(t, metrics, 0, 0, 0, 0)
	})

	t.Run("counts a candidate that is already gone as deleted", func(t *testing.T) {
		files := newFileBucket(t)
		storeCandidates(t, files)
		bucket := &failingDeleteBucket{fileBucket: files, errs: map[string]error{
			"objects/tenants/a/01/orphan": &os.PathError{Op: "remove", Path: "objects/tenants/a/01/orphan", Err: os.ErrNotExist},
		}}
		sweeper, metrics, logs := newDeletingSweeper(t, bucket)

		outcome, err := sweeper.Sweep(t.Context(), "a", now)
		require.NoError(t, err)
		require.Equal(t, SweepScanned, outcome)
		requireCounts(t, metrics, 1, 1, 0, 0)
		require.Contains(t, logs.String(), "deleted_log_objects=1 deleted_index_objects=1 failed_log_objects=0 failed_index_objects=0\n")
	})

	t.Run("counts and logs a failed delete, deletes the other candidates, and returns SweepScanned", func(t *testing.T) {
		files := newFileBucket(t)
		storeCandidates(t, files)
		bucket := &failingDeleteBucket{fileBucket: files, errs: map[string]error{
			"index/v0/indexes/tenants/a/00/orphan-index": errors.New("delete failed"),
		}}
		sweeper, metrics, logs := newDeletingSweeper(t, bucket)

		outcome, err := sweeper.Sweep(t.Context(), "a", now)
		require.NoError(t, err)
		require.Equal(t, SweepScanned, outcome)
		requireExists(t, files, false, "objects/tenants/a/01/orphan")
		requireExists(t, files, true, "index/v0/indexes/tenants/a/00/orphan-index")
		requireCounts(t, metrics, 1, 0, 0, 1)
		require.Contains(t, logs.String(), "deleted_log_objects=1 deleted_index_objects=0 failed_log_objects=0 failed_index_objects=1\n")
		require.Contains(t, logs.String(), `level=warn tenant=a msg="deletion sweep cannot delete candidate" kind=index path=index/v0/indexes/tenants/a/00/orphan-index err="delete failed"`)
		require.Contains(t, readFileList(t, files, "a").Paths, "index/v0/indexes/tenants/a/00/orphan-index")
	})

	t.Run("logs only the first five failed deletes and counts every failed delete", func(t *testing.T) {
		files := newFileBucket(t)
		storeAnyReachableIndex(t, files)
		errs := make(map[string]error)
		var paths []string
		for i := range 8 {
			p := fmt.Sprintf("objects/tenants/a/aa/log-%d", i)
			files.put(t, p, notADataObject, old)
			paths = append(paths, p)
			errs[p] = errors.New("delete failed")
		}
		storeFileList(t, files, FileList{Tenant: "a", CreatedAt: stale, Paths: paths})
		bucket := &failingDeleteBucket{fileBucket: files, errs: errs}
		sweeper, metrics, logs := newDeletingSweeper(t, bucket)

		outcome, err := sweeper.Sweep(t.Context(), "a", now)
		require.NoError(t, err)
		require.Equal(t, SweepScanned, outcome)
		requireCounts(t, metrics, 0, 0, 8, 0)
		require.Equal(t, 5, strings.Count(logs.String(), `msg="deletion sweep cannot delete candidate"`))
	})

	t.Run("returns the context error and keeps the new list when the context ends during the deletes", func(t *testing.T) {
		files := newFileBucket(t)
		cands := storeCandidates(t, files)
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		bucket := &cancellingDeleteBucket{fileBucket: files, cancel: cancel}
		sweeper, metrics, _ := newDeletingSweeper(t, bucket)

		_, err := sweeper.Sweep(ctx, "a", now)
		require.ErrorIs(t, err, context.Canceled)
		requireExists(t, files, true, cands...)
		requireCounts(t, metrics, 0, 0, 0, 0)
		require.Equal(t, now, readFileList(t, files, "a").CreatedAt)
	})

	t.Run("excludes reachable compacted log and index objects and keeps a compacted index that no ToC reaches", func(t *testing.T) {
		bucket := newFileBucket(t)
		bucket.put(t, "objects/tenants/a/aa/compacted-log", notADataObject, old)
		bucket.put(t, "index/v0/indexes/tenants/a/bb/orphan-index", notADataObject, old)
		storeReachableIndex(t, bucket, "a", "indexes/tenants/a/bb/index", []string{"objects/tenants/a/aa/compacted-log"}, old)

		_, err := newSweeper(t, bucket).Sweep(t.Context(), "a", now)
		require.NoError(t, err)
		require.Equal(t, []string{"index/v0/indexes/tenants/a/bb/orphan-index"}, readFileList(t, bucket, "a").Paths)
	})

	t.Run("logs five examples of each kind and counts every candidate when there are more", func(t *testing.T) {
		bucket := newFileBucket(t)
		storeAnyReachableIndex(t, bucket)
		var paths []string
		for i := range 8 {
			p := fmt.Sprintf("objects/tenants/a/aa/log-%d", i)
			bucket.put(t, p, notADataObject, old)
			paths = append(paths, p)
			p = fmt.Sprintf("index/v0/indexes/tenants/a/bb/index-%d", i)
			bucket.put(t, p, notADataObject, old)
			paths = append(paths, p)
		}
		slices.Sort(paths)
		storeFileList(t, bucket, FileList{Tenant: "a", CreatedAt: stale, Paths: paths})
		sweeper, logs := newLoggedSweeper(t, bucket)

		_, err := sweeper.Sweep(t.Context(), "a", now)
		require.NoError(t, err)
		require.Contains(t, logs.String(), "candidates=16 log_objects=8 index_objects=8\n")
		require.Equal(t, 5, strings.Count(logs.String(), `msg="deletion sweep dry run: candidate" kind=log`))
		require.Equal(t, 5, strings.Count(logs.String(), `msg="deletion sweep dry run: candidate" kind=index`))
	})

	t.Run("logs every candidate whose attributes cannot be read with the error and stores the list", func(t *testing.T) {
		files := newFileBucket(t)
		storeAnyReachableIndex(t, files)
		files.put(t, "objects/tenants/a/00/orphan", notADataObject, old)
		files.put(t, "objects/tenants/a/01/orphan", notADataObject, old)
		storeFileList(t, files, FileList{Tenant: "a", CreatedAt: stale, Paths: []string{"objects/tenants/a/00/orphan", "objects/tenants/a/01/orphan"}})
		bucket := &failingAttributesBucket{fileBucket: files}
		sweeper, logs := newLoggedSweeper(t, bucket)

		outcome, err := sweeper.Sweep(t.Context(), "a", now)
		require.NoError(t, err)
		require.Equal(t, SweepScanned, outcome)
		require.Contains(t, logs.String(), `level=warn tenant=a msg="deletion sweep dry run: candidate" kind=log path=objects/tenants/a/00/orphan err="attributes failed"`)
		require.Contains(t, logs.String(), `level=warn tenant=a msg="deletion sweep dry run: candidate" kind=log path=objects/tenants/a/01/orphan err="attributes failed"`)
		require.Equal(t, now, readFileList(t, files, "a").CreatedAt)
	})

	t.Run("returns the write error and logs no deletion candidates when the list cannot be stored", func(t *testing.T) {
		files := newFileBucket(t)
		storeAnyReachableIndex(t, files)
		files.put(t, "objects/tenants/a/00/orphan", notADataObject, old)
		storeFileList(t, files, FileList{Tenant: "a", CreatedAt: stale, Paths: []string{"objects/tenants/a/00/orphan"}})
		bucket := &malformedOnReplaceBucket{fileBucket: files}
		sweeper, logs := newLoggedSweeper(t, bucket)

		outcome, err := sweeper.Sweep(t.Context(), "a", now)
		require.ErrorIs(t, err, ErrMalformedFileList)
		require.ErrorContains(t, err, "storing file list")
		require.Empty(t, outcome)
		require.NotContains(t, logs.String(), "dry run")
		require.Equal(t, stale, readFileList(t, files, "a").CreatedAt)
	})

	t.Run("logs no deletion candidates and says why when no list is stored", func(t *testing.T) {
		bucket := newFileBucket(t)
		storeAnyReachableIndex(t, bucket)
		bucket.put(t, "objects/tenants/a/00/orphan", notADataObject, old)
		sweeper, logs := newLoggedSweeper(t, bucket)

		_, err := sweeper.Sweep(t.Context(), "a", now)
		require.NoError(t, err)
		require.NotContains(t, logs.String(), "dry run")
		require.Contains(t, logs.String(), "no file list is stored, so this list is the first")
		require.True(t, strings.HasPrefix(logs.String(), `level=info tenant=a msg="deletion sweep running"`+"\n"))
	})

	t.Run("logs an empty dry run when the stored list shares no object with the new list", func(t *testing.T) {
		bucket := newFileBucket(t)
		storeAnyReachableIndex(t, bucket)
		bucket.put(t, "objects/tenants/a/00/orphan", notADataObject, old)
		storeFileList(t, bucket, FileList{Tenant: "a", CreatedAt: stale, Paths: []string{"objects/tenants/a/09/gone"}})
		sweeper, logs := newLoggedSweeper(t, bucket)

		_, err := sweeper.Sweep(t.Context(), "a", now)
		require.NoError(t, err)
		require.Contains(t, logs.String(), "candidates=0 log_objects=0 index_objects=0\n")
		require.NotContains(t, logs.String(), "dry run: candidate")
	})

	t.Run("returns ErrMalformedFileList without a write when the stored list is malformed at the second read", func(t *testing.T) {
		files := newFileBucket(t)
		storeAnyReachableIndex(t, files)
		files.put(t, "objects/tenants/a/00/orphan", notADataObject, old)
		storeFileList(t, files, FileList{Tenant: "a", CreatedAt: stale, Paths: []string{"objects/tenants/a/00/orphan"}})
		bucket := &onFirstListingBucket{fileBucket: files, hook: func() { upload(t, files, fileListPath("a"), []byte("not gzip")) }}

		outcome, err := newSweeper(t, bucket).Sweep(t.Context(), "a", now)
		require.ErrorIs(t, err, ErrMalformedFileList)
		require.Empty(t, outcome)
		require.Equal(t, "not gzip", readRaw(t, files, fileListPath("a")))
	})

	t.Run("returns an error and stores no list when the tenant has objects but no ToC reaches any object", func(t *testing.T) {
		bucket := newFileBucket(t)
		bucket.put(t, "objects/tenants/a/00/orphan", notADataObject, old)
		bucket.put(t, "index/v0/indexes/tenants/a/00/orphan-index", notADataObject, old)
		storeFileList(t, bucket, FileList{Tenant: "a", CreatedAt: stale, Paths: []string{"objects/tenants/a/00/orphan"}})
		sweeper, logs := newLoggedSweeper(t, bucket)

		_, err := sweeper.Sweep(t.Context(), "a", now)
		require.ErrorContains(t, err, `no ToC of tenant "a" reaches any of the 2 scanned objects, and none of the 0 reachable objects is among them`)
		require.NotContains(t, logs.String(), "dry run")
		require.Equal(t, stale, readFileList(t, bucket, "a").CreatedAt)
	})

	t.Run("returns an error and stores no list when the ToCs reach only objects outside the tenant directories", func(t *testing.T) {
		bucket := newFileBucket(t)
		bucket.put(t, "objects/tenants/a/00/orphan", notADataObject, old)
		storeReachableIndex(t, bucket, "a", "indexes/00/uncompacted-index", []string{"objects/00/uncompacted"}, old)

		_, err := newSweeper(t, bucket).Sweep(t.Context(), "a", now)
		require.ErrorContains(t, err, `no ToC of tenant "a" reaches any of the 1 scanned objects, and none of the 2 reachable objects is among them`)
		exists, err := bucket.Exists(t.Context(), fileListPath("a"))
		require.NoError(t, err)
		require.False(t, exists)
	})

	t.Run("returns an error and stores no list when the ToCs cannot be read", func(t *testing.T) {
		bucket := newFileBucket(t)
		bucket.put(t, "objects/tenants/a/00/orphan", notADataObject, old)
		upload(t, bucket, "index/v0/"+metastore.TableOfContentsPath("a", now.Truncate(metastore.MetastoreWindowSize)), notADataObject)

		_, err := newSweeper(t, bucket).Sweep(t.Context(), "a", now)
		require.ErrorContains(t, err, "finding reachable objects")
		exists, err := bucket.Exists(t.Context(), fileListPath("a"))
		require.NoError(t, err)
		require.False(t, exists)
	})

	t.Run("returns SweepSuperseded and logs no deletion candidates when a concurrent sweep stores its list first", func(t *testing.T) {
		files := newFileBucket(t)
		storeAnyReachableIndex(t, files)
		files.put(t, "objects/tenants/a/00/orphan", notADataObject, old)
		storeFileList(t, files, FileList{Tenant: "a", CreatedAt: stale, Paths: []string{"objects/tenants/a/00/orphan"}})
		newer := FileList{Tenant: "a", CreatedAt: now.Add(time.Minute), Paths: []string{"objects/tenants/a/00/newer"}}
		bucket := &conflictBucket{Bucket: files, conflicts: 1, concurrentWrite: func() { storeFileList(t, files, newer) }}
		sweeper, logs := newLoggedSweeper(t, bucket)

		outcome, err := sweeper.Sweep(t.Context(), "a", now)
		require.NoError(t, err)
		require.Equal(t, SweepSuperseded, outcome)
		require.NotContains(t, logs.String(), "dry run")
		require.Equal(t, newer, readFileList(t, files, "a"))
	})

	t.Run("returns SweepSuperseded without a write when the stored list changes during the scan", func(t *testing.T) {
		files := newFileBucket(t)
		storeAnyReachableIndex(t, files)
		files.put(t, "objects/tenants/a/00/orphan", notADataObject, old)
		storeFileList(t, files, FileList{Tenant: "a", CreatedAt: stale, Paths: []string{"objects/tenants/a/00/orphan"}})
		replacement := FileList{Tenant: "a", CreatedAt: now.Add(-time.Minute), Paths: []string{"objects/tenants/a/00/replacement"}}
		bucket := &onFirstListingBucket{fileBucket: files, hook: func() { storeFileList(t, files, replacement) }}
		sweeper, logs := newLoggedSweeper(t, bucket)

		outcome, err := sweeper.Sweep(t.Context(), "a", now)
		require.NoError(t, err)
		require.Equal(t, SweepSuperseded, outcome)
		require.NotContains(t, logs.String(), "dry run")
		require.Equal(t, replacement, readFileList(t, files, "a"))
	})

	t.Run("returns SweepSuperseded without a write when the stored list is removed during the scan", func(t *testing.T) {
		files := newFileBucket(t)
		storeAnyReachableIndex(t, files)
		files.put(t, "objects/tenants/a/00/orphan", notADataObject, old)
		storeFileList(t, files, FileList{Tenant: "a", CreatedAt: stale, Paths: []string{"objects/tenants/a/00/orphan"}})
		bucket := &onFirstListingBucket{fileBucket: files, hook: func() { require.NoError(t, files.Delete(t.Context(), fileListPath("a"))) }}
		sweeper, logs := newLoggedSweeper(t, bucket)

		outcome, err := sweeper.Sweep(t.Context(), "a", now)
		require.NoError(t, err)
		require.Equal(t, SweepSuperseded, outcome)
		require.NotContains(t, logs.String(), "dry run")
		exists, err := files.Exists(t.Context(), fileListPath("a"))
		require.NoError(t, err)
		require.False(t, exists)
	})

	t.Run("returns an error, logs an error, and stores no list when the stored paths are malformed", func(t *testing.T) {
		bucket := newFileBucket(t)
		storeAnyReachableIndex(t, bucket)
		bucket.put(t, "objects/tenants/a/00/orphan", notADataObject, old)
		storedData := gzipText(t, `{"version":2,"tenant":"a","created_at":"`+stale.Format(time.RFC3339)+`"}`+"\n"+`not json`+"\n")
		upload(t, bucket, fileListPath("a"), storedData)

		sweeper, logs := newLoggedSweeper(t, bucket)

		_, err := sweeper.Sweep(t.Context(), "a", now)
		require.ErrorIs(t, err, ErrMalformedFileList)
		require.Contains(t, logs.String(), `level=error tenant=a msg="deletion sweep cannot continue: stored file list is malformed, repair or delete it"`)
		require.Equal(t, string(storedData), readRaw(t, bucket, fileListPath("a")))
	})

	t.Run("skips the scan and keeps the stored list when it is newer than MinSweepInterval", func(t *testing.T) {
		files := newFileBucket(t)
		files.put(t, "objects/tenants/a/00/single-a", notADataObject, old)
		stored := FileList{Tenant: "a", CreatedAt: now.Add(-MinSweepInterval + time.Second), Paths: []string{"objects/tenants/a/00/stored"}}
		storeFileList(t, files, stored)
		bucket := &failingIterBucket{listRecordingBucket: listRecordingBucket{fileBucket: files}}

		outcome, err := newSweeper(t, bucket).Sweep(t.Context(), "a", now)
		require.NoError(t, err)
		require.Equal(t, SweepSkipped, outcome)
		require.Empty(t, bucket.dirs)
		require.Equal(t, stored, readFileList(t, files, "a"))
	})

	t.Run("scans and replaces a stored list that is MinSweepInterval old", func(t *testing.T) {
		bucket := newFileBucket(t)
		storeAnyReachableIndex(t, bucket)
		bucket.put(t, "objects/tenants/a/00/single-a", notADataObject, old)
		storeFileList(t, bucket, FileList{Tenant: "a", CreatedAt: now.Add(-MinSweepInterval)})

		outcome, err := newSweeper(t, bucket).Sweep(t.Context(), "a", now)
		require.NoError(t, err)
		require.Equal(t, SweepScanned, outcome)
		require.Equal(t, FileList{Tenant: "a", CreatedAt: now, Paths: []string{"objects/tenants/a/00/single-a"}}, readFileList(t, bucket, "a"))
	})

	t.Run("returns ErrMalformedFileList without a scan and keeps a stored version 1 list", func(t *testing.T) {
		files := newFileBucket(t)
		stored := gzipText(t, `{"version":1,"tenant":"a","created_at":"`+stale.Format(time.RFC3339)+`"}`+"\n"+`{"path":"objects/tenants/a/00/x"}`+"\n")
		upload(t, files, fileListPath("a"), stored)
		bucket := &listRecordingBucket{fileBucket: files}

		_, err := newSweeper(t, bucket).Sweep(t.Context(), "a", now)
		require.ErrorIs(t, err, ErrMalformedFileList)
		require.Empty(t, bucket.dirs)
		require.Equal(t, string(stored), readRaw(t, files, fileListPath("a")))
	})

	t.Run("returns ErrMalformedFileList without a scan, logs an error, and keeps a malformed stored list", func(t *testing.T) {
		files := newFileBucket(t)
		upload(t, files, fileListPath("a"), []byte("not gzip"))
		bucket := &listRecordingBucket{fileBucket: files}
		sweeper, logs := newLoggedSweeper(t, bucket)

		_, err := sweeper.Sweep(t.Context(), "a", now)
		require.ErrorIs(t, err, ErrMalformedFileList)
		require.Empty(t, bucket.dirs)
		require.Contains(t, logs.String(), `level=error tenant=a msg="deletion sweep cannot continue: stored file list is malformed, repair or delete it" path=deletion/a/files.jsonl.gz`)
		require.Equal(t, "not gzip", readRaw(t, files, fileListPath("a")))
	})
}

func TestNewDeletingSweeper(t *testing.T) {
	t.Run("returns an error when the metrics are nil", func(t *testing.T) {
		_, err := NewDeletingSweeper(newFileBucket(t), nil, log.NewNopLogger())
		require.ErrorContains(t, err, "metrics are required")
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

// storeReachableIndex stores an index object of tenant at indexPath in the
// index storage of bucket, with a stats section that references logPaths. It
// adds the index object to the ToC of tenant, so that the index object and
// logPaths are reachable. The index object was last modified at modified.
func storeReachableIndex(t *testing.T, bucket fileBucket, tenant, indexPath string, logPaths []string, modified time.Time) {
	t.Helper()
	builder, err := indexobj.NewBuilder(tenant, metastore.DefaultTocBuilderConfig, nil, indexobj.NewBuilderMetrics(nil))
	require.NoError(t, err)
	require.NoError(t, builder.SetLayout(streams.NewLayout(streams.DefaultShardLayout(), nil)))
	for _, p := range logPaths {
		require.NoError(t, builder.AppendStat(p, 0, 0, nil, modified, modified, 1, 1))
	}
	obj, closer, err := builder.Flush()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, closer.Close()) })
	r, err := obj.Reader(t.Context())
	require.NoError(t, err)
	defer r.Close()
	data, err := io.ReadAll(r)
	require.NoError(t, err)
	bucket.put(t, metastore.IndexStoragePrefix+"/"+indexPath, data, modified)

	toc := metastore.NewTableOfContentsWriter(metastore.NewIndexBucket(bucket), metastore.DefaultTocWriterBackoffConfig, metastore.DefaultTocBuilderConfig, log.NewNopLogger(), metastore.NewTocWriterMetrics(nil), indexobj.NewBuilderMetrics(nil))
	require.NoError(t, toc.WriteEntry(t.Context(), tenant, metastore.TableOfContentsEntry{
		Path:      indexPath,
		StartTime: modified,
		EndTime:   modified.Add(time.Minute),
	}))
}

// failingAttributesBucket fails every Attributes call.
type failingAttributesBucket struct {
	fileBucket
}

func (failingAttributesBucket) Attributes(context.Context, string) (objstore.ObjectAttributes, error) {
	return objstore.ObjectAttributes{}, errors.New("attributes failed")
}

// malformedOnReplaceBucket gives the GetAndReplace callback a stored object
// that is not a file list, so that every write of a file list fails.
type malformedOnReplaceBucket struct {
	fileBucket
}

func (*malformedOnReplaceBucket) GetAndReplace(_ context.Context, _ string, fn func(io.ReadCloser) (io.ReadCloser, error)) error {
	_, err := fn(io.NopCloser(strings.NewReader("not gzip")))
	return err
}

// failingIterBucket fails every Iter call, which the ToC walk uses to list
// the ToC windows. A sweep that skips its scan must not list them.
type failingIterBucket struct {
	listRecordingBucket
}

func (failingIterBucket) Iter(context.Context, string, func(string) error, ...objstore.IterOption) error {
	return errors.New("iter failed")
}

// onFirstListingBucket runs hook before its first IterWithAttributes call,
// which is during the scan of a sweep.
type onFirstListingBucket struct {
	fileBucket
	hook func()
	once sync.Once
}

func (b *onFirstListingBucket) IterWithAttributes(ctx context.Context, dir string, f func(objstore.IterObjectAttributes) error, options ...objstore.IterOption) error {
	b.once.Do(b.hook)
	return b.fileBucket.IterWithAttributes(ctx, dir, f, options...)
}

// failingDeleteBucket returns the error in errs for a Delete of a path in
// errs, and keeps the object. It deletes other paths.
type failingDeleteBucket struct {
	fileBucket
	errs map[string]error
}

func (b *failingDeleteBucket) Delete(ctx context.Context, name string) error {
	if err, ok := b.errs[name]; ok {
		return err
	}
	return b.fileBucket.Delete(ctx, name)
}

// cancellingDeleteBucket calls cancel on every Delete, and then fails the
// Delete with the context error. It keeps the object.
type cancellingDeleteBucket struct {
	fileBucket
	cancel context.CancelFunc
}

func (b *cancellingDeleteBucket) Delete(ctx context.Context, _ string) error {
	b.cancel()
	return ctx.Err()
}
