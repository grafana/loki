package uploader

import (
	"context"
	"errors"
	"io"
	"testing"
	"testing/synctest"
	"time"

	"github.com/go-kit/log"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"
	"github.com/thanos-io/objstore"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/logsobj"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/scratch"
)

func TestUploader_Upload(t *testing.T) {
	t.Run("uploads the object and returns its key", func(t *testing.T) {
		var (
			bucket = objstore.NewInMemBucket()
			u      = New(Config{SHAPrefixSize: 2}, bucket, log.NewNopLogger())
		)
		key, err := u.Upload(t.Context(), newTestObject(t))
		require.NoError(t, err)
		require.Contains(t, bucket.Objects(), key)
		require.Zero(t, testutil.ToFloat64(u.metrics.uploadFailures))
	})

	t.Run("returns a cancellation without uploading or counting a failure when the context is canceled", func(t *testing.T) {
		var (
			bucket = objstore.NewInMemBucket()
			u      = New(Config{SHAPrefixSize: 2}, bucket, log.NewNopLogger())
		)
		ctx, cancel := context.WithCancel(t.Context())
		cancel()

		_, err := u.Upload(ctx, newTestObject(t))
		require.ErrorIs(t, err, context.Canceled)
		require.Empty(t, bucket.Objects())
		require.Zero(t, testutil.ToFloat64(u.metrics.uploadFailures))
	})

	t.Run("returns the cancellation and the bucket error when the context is canceled during a retry", func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()

		var (
			errBucket = errors.New("bucket error")
			bucket    = &failingBucket{Bucket: objstore.NewInMemBucket(), err: errBucket, onUpload: cancel}
			u         = New(Config{SHAPrefixSize: 2}, bucket, log.NewNopLogger())
		)
		_, err := u.Upload(ctx, newTestObject(t))
		require.ErrorIs(t, err, context.Canceled)
		require.ErrorIs(t, err, errBucket)
		require.Equal(t, 1, bucket.uploads)
		require.Zero(t, testutil.ToFloat64(u.metrics.uploadFailures))
	})

	t.Run("counts a failure when every retry fails", func(t *testing.T) {
		obj := newTestObject(t)
		// The backoff between retries sleeps on the fake clock of the bubble.
		synctest.Test(t, func(t *testing.T) {
			var (
				errBucket = errors.New("bucket error")
				bucket    = &failingBucket{Bucket: objstore.NewInMemBucket(), err: errBucket}
				u         = New(Config{SHAPrefixSize: 2}, bucket, log.NewNopLogger())
			)
			_, err := u.Upload(t.Context(), obj)
			require.ErrorIs(t, err, errBucket)
			require.NotErrorIs(t, err, context.Canceled)
			require.Equal(t, 20, bucket.uploads)
			require.Equal(t, float64(1), testutil.ToFloat64(u.metrics.uploadFailures))
		})
	})
}

// newTestObject returns a data object that holds one log line.
func newTestObject(t *testing.T) *dataobj.Object {
	t.Helper()
	b, err := logsobj.NewBuilder(logsobj.BuilderBaseConfig{
		TargetPageSize:          2048,
		TargetObjectSize:        1 << 20,
		TargetSectionSize:       8 << 10,
		BufferSize:              2048 * 8,
		SectionStripeMergeLimit: 2,
	}, scratch.NewMemory(), logsobj.NewBuilderMetrics(), log.NewNopLogger(), nil)
	require.NoError(t, err)

	now := time.Now()
	require.NoError(t, b.Append("tenant", logproto.Stream{
		Labels:  `{app="test"}`,
		Entries: []logproto.Entry{{Timestamp: now, Line: "line"}},
	}, now))

	obj, closer, err := b.Flush()
	require.NoError(t, err)
	t.Cleanup(func() { _ = closer.Close() })
	return obj
}

// failingBucket fails every upload with err.
type failingBucket struct {
	objstore.Bucket
	err     error
	uploads int
	// onUpload, if set, runs after each failed upload.
	onUpload func()
}

func (b *failingBucket) Upload(_ context.Context, _ string, r io.Reader) error {
	b.uploads++
	_, _ = io.Copy(io.Discard, r)
	if b.onUpload != nil {
		b.onUpload()
	}
	return b.err
}
