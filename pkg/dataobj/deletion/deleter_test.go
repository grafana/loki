package deletion

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/go-kit/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"
)

func TestDeleteObjects(t *testing.T) {
	data := []byte("x")
	indexPath := "index/v0/indexes/tenants/a/00/orphan-index"
	logPath := "objects/tenants/a/01/orphan"

	newLogger := func() (log.Logger, *bytes.Buffer) {
		var logs bytes.Buffer
		return log.With(log.NewLogfmtLogger(log.NewSyncWriter(&logs)), "tenant", "a"), &logs
	}
	requireCounts := func(t *testing.T, metrics *SweepMetrics, deletedLog, deletedIndex, failedLog, failedIndex float64) {
		t.Helper()
		require.Equal(t, deletedLog, testutil.ToFloat64(metrics.deletedObjects.WithLabelValues(kindLog)))
		require.Equal(t, deletedIndex, testutil.ToFloat64(metrics.deletedObjects.WithLabelValues(kindIndex)))
		require.Equal(t, failedLog, testutil.ToFloat64(metrics.failedDeletes.WithLabelValues(kindLog)))
		require.Equal(t, failedIndex, testutil.ToFloat64(metrics.failedDeletes.WithLabelValues(kindIndex)))
	}

	t.Run("deletes every path, keeps other objects, and counts the deletes by kind", func(t *testing.T) {
		bucket := newFileBucket(t)
		upload(t, bucket, indexPath, data)
		upload(t, bucket, logPath, data)
		upload(t, bucket, "objects/tenants/a/02/other", data)
		metrics := NewSweepMetrics(prometheus.NewRegistry())
		logger, logs := newLogger()

		err := DeleteObjects(t.Context(), bucket, []string{indexPath, logPath}, metrics, logger)
		require.NoError(t, err)
		requireExists(t, bucket, false, indexPath, logPath)
		requireExists(t, bucket, true, "objects/tenants/a/02/other")
		requireCounts(t, metrics, 1, 1, 0, 0)
		require.Contains(t, logs.String(), `level=info tenant=a msg="deletion sweep deleted candidates" candidates=2 deleted_log_objects=1 deleted_index_objects=1 failed_log_objects=0 failed_index_objects=0`+"\n")
	})

	t.Run("counts a path that is already gone as deleted", func(t *testing.T) {
		bucket := newFileBucket(t)
		metrics := NewSweepMetrics(prometheus.NewRegistry())
		logger, logs := newLogger()

		err := DeleteObjects(t.Context(), bucket, []string{logPath}, metrics, logger)
		require.NoError(t, err)
		requireCounts(t, metrics, 1, 0, 0, 0)
		require.Contains(t, logs.String(), "deleted_log_objects=1 deleted_index_objects=0 failed_log_objects=0 failed_index_objects=0\n")
	})

	t.Run("counts and logs a failed delete, deletes the other paths, logs the summary at warn, and returns nil", func(t *testing.T) {
		files := newFileBucket(t)
		upload(t, files, indexPath, data)
		upload(t, files, logPath, data)
		bucket := &failingDeleteBucket{fileBucket: files, errs: map[string]error{indexPath: errors.New("delete failed")}}
		metrics := NewSweepMetrics(prometheus.NewRegistry())
		logger, logs := newLogger()

		err := DeleteObjects(t.Context(), bucket, []string{indexPath, logPath}, metrics, logger)
		require.NoError(t, err)
		requireExists(t, files, false, logPath)
		requireExists(t, files, true, indexPath)
		requireCounts(t, metrics, 1, 0, 0, 1)
		require.Contains(t, logs.String(), `level=warn tenant=a msg="deletion sweep deleted candidates" candidates=2 deleted_log_objects=1 deleted_index_objects=0 failed_log_objects=0 failed_index_objects=1`+"\n")
		require.Contains(t, logs.String(), `level=warn tenant=a msg="deletion sweep cannot delete candidate" kind=index path=`+indexPath+` err="delete failed"`)
	})

	t.Run("logs only the first five failed deletes and counts every failed delete", func(t *testing.T) {
		files := newFileBucket(t)
		errs := make(map[string]error)
		var paths []string
		for i := range 8 {
			p := fmt.Sprintf("objects/tenants/a/aa/log-%d", i)
			upload(t, files, p, data)
			paths = append(paths, p)
			errs[p] = errors.New("delete failed")
		}
		bucket := &failingDeleteBucket{fileBucket: files, errs: errs}
		metrics := NewSweepMetrics(prometheus.NewRegistry())
		logger, logs := newLogger()

		err := DeleteObjects(t.Context(), bucket, paths, metrics, logger)
		require.NoError(t, err)
		requireCounts(t, metrics, 0, 0, 8, 0)
		require.Equal(t, 5, strings.Count(logs.String(), `msg="deletion sweep cannot delete candidate"`))
	})

	t.Run("returns the context error and counts no delete when the context ends during the deletes", func(t *testing.T) {
		files := newFileBucket(t)
		upload(t, files, indexPath, data)
		upload(t, files, logPath, data)
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		bucket := &cancellingDeleteBucket{fileBucket: files, cancel: cancel}
		metrics := NewSweepMetrics(prometheus.NewRegistry())

		err := DeleteObjects(ctx, bucket, []string{indexPath, logPath}, metrics, log.NewNopLogger())
		require.ErrorIs(t, err, context.Canceled)
		requireExists(t, files, true, indexPath, logPath)
		requireCounts(t, metrics, 0, 0, 0, 0)
	})

	t.Run("returns an error and deletes nothing when the metrics are nil", func(t *testing.T) {
		bucket := newFileBucket(t)
		upload(t, bucket, logPath, data)

		err := DeleteObjects(t.Context(), bucket, []string{logPath}, nil, log.NewNopLogger())
		require.ErrorContains(t, err, "metrics are required")
		requireExists(t, bucket, true, logPath)
	})

	t.Run("logs nothing when there are no paths", func(t *testing.T) {
		metrics := NewSweepMetrics(prometheus.NewRegistry())
		logger, logs := newLogger()

		err := DeleteObjects(t.Context(), newFileBucket(t), nil, metrics, logger)
		require.NoError(t, err)
		requireCounts(t, metrics, 0, 0, 0, 0)
		require.Empty(t, logs.String())
	})
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
