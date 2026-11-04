package deletion

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/backoff"
	"github.com/minio/minio-go/v7"
	pkgerrors "github.com/pkg/errors"
	"github.com/stretchr/testify/require"
	"github.com/thanos-io/objstore"
)

func TestFileList_JSON(t *testing.T) {
	list := FileList{
		Tenant:    "a",
		CreatedAt: time.Date(2026, 11, 4, 12, 0, 0, 500, time.UTC),
		Paths:     []string{"index/v0/indexes/00/x", "objects/tenants/a/aa/y"},
	}
	stored := `{"tenant":"a","created_at":"2026-11-04T12:00:00.0000005Z","paths":["index/v0/indexes/00/x","objects/tenants/a/aa/y"]}`

	t.Run("encodes to the stored format", func(t *testing.T) {
		encoded, err := json.Marshal(list)
		require.NoError(t, err)
		require.JSONEq(t, stored, string(encoded))
	})

	t.Run("decodes the stored format", func(t *testing.T) {
		decoded, err := decodeFileList([]byte(stored))
		require.NoError(t, err)
		require.Equal(t, list, decoded)
	})

	t.Run("returns an unrecoverable error for a stored list with an unknown field", func(t *testing.T) {
		_, err := decodeFileList([]byte(`{"tenant":"a","created_at":"2026-11-04T12:00:00Z","paths":[],"version":2}`))
		require.ErrorIs(t, err, errUnrecoverable)
	})

	t.Run("returns an unrecoverable error for an empty stored list", func(t *testing.T) {
		_, err := decodeFileList(nil)
		require.ErrorIs(t, err, errUnrecoverable)
		require.ErrorContains(t, err, "empty")
	})
}

func TestFileListWriter_Write(t *testing.T) {
	createdAt := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)
	list := FileList{Tenant: "a", CreatedAt: createdAt, Paths: []string{"objects/00/x", "objects/01/y"}}
	olderList := FileList{Tenant: "a", CreatedAt: createdAt.Add(-time.Hour), Paths: []string{"objects/02/old"}}
	newerList := FileList{Tenant: "a", CreatedAt: createdAt.Add(time.Hour), Paths: []string{"objects/03/new"}}

	t.Run("writes the list when no list is stored", func(t *testing.T) {
		bucket := objstore.NewInMemBucket()

		require.NoError(t, newTestWriter(bucket).Write(t.Context(), list))
		require.Equal(t, list, readFileList(t, bucket, "deletion/a/files.json"))
	})

	t.Run("replaces a stored list with an earlier creation time", func(t *testing.T) {
		bucket := objstore.NewInMemBucket()
		storeFileList(t, bucket, list.Path(), olderList)

		require.NoError(t, newTestWriter(bucket).Write(t.Context(), list))
		require.Equal(t, list, readFileList(t, bucket, list.Path()))
	})

	t.Run("returns nil when the stored list is equal to the list", func(t *testing.T) {
		bucket := objstore.NewInMemBucket()
		storeFileList(t, bucket, list.Path(), list)

		require.NoError(t, newTestWriter(bucket).Write(t.Context(), list))
		require.Equal(t, list, readFileList(t, bucket, list.Path()))
	})

	t.Run("returns ErrSuperseded and keeps a different stored list with the same creation time", func(t *testing.T) {
		bucket := objstore.NewInMemBucket()
		stored := FileList{Tenant: "a", CreatedAt: createdAt, Paths: []string{"objects/04/same"}}
		storeFileList(t, bucket, list.Path(), stored)

		require.ErrorIs(t, newTestWriter(bucket).Write(t.Context(), list), ErrSuperseded)
		require.Equal(t, stored, readFileList(t, bucket, list.Path()))
	})

	t.Run("returns ErrSuperseded and keeps a stored list with a later creation time", func(t *testing.T) {
		bucket := objstore.NewInMemBucket()
		storeFileList(t, bucket, list.Path(), newerList)

		require.ErrorIs(t, newTestWriter(bucket).Write(t.Context(), list), ErrSuperseded)
		require.Equal(t, newerList, readFileList(t, bucket, list.Path()))
	})

	t.Run("retries after a conflict and keeps a newer list from the competing writer", func(t *testing.T) {
		inner := objstore.NewInMemBucket()
		bucket := &conflictBucket{Bucket: inner, conflicts: 1, concurrentWrite: func() { storeFileList(t, inner, list.Path(), newerList) }}

		require.ErrorIs(t, newTestWriter(bucket).Write(t.Context(), list), ErrSuperseded)
		require.Equal(t, 2, bucket.calls)
		require.Equal(t, newerList, readFileList(t, inner, list.Path()))
	})

	t.Run("retries after a conflict and replaces an older list from the competing writer", func(t *testing.T) {
		inner := objstore.NewInMemBucket()
		bucket := &conflictBucket{Bucket: inner, conflicts: 1, concurrentWrite: func() { storeFileList(t, inner, list.Path(), olderList) }}

		require.NoError(t, newTestWriter(bucket).Write(t.Context(), list))
		require.Equal(t, 2, bucket.calls)
		require.Equal(t, list, readFileList(t, inner, list.Path()))
	})

	t.Run("returns an error after the last attempt when every attempt conflicts", func(t *testing.T) {
		bucket := &conflictBucket{Bucket: objstore.NewInMemBucket(), conflicts: 100}

		err := newTestWriter(bucket).Write(t.Context(), list)
		require.ErrorIs(t, err, errConditionalWriteConflict)
		require.Equal(t, testBackoffConfig.MaxRetries, bucket.calls)
	})

	t.Run("retries after a bucket error and writes the list", func(t *testing.T) {
		inner := objstore.NewInMemBucket()
		bucket := &flakyBucket{Bucket: inner, failures: 1}

		require.NoError(t, newTestWriter(bucket).Write(t.Context(), list))
		require.Equal(t, 2, bucket.calls)
		require.Equal(t, list, readFileList(t, inner, list.Path()))
	})

	t.Run("retries after a failed read of the stored list and writes the list", func(t *testing.T) {
		inner := objstore.NewInMemBucket()
		storeFileList(t, inner, list.Path(), olderList)
		bucket := &flakyBucket{Bucket: inner, failedReads: 1}

		require.NoError(t, newTestWriter(bucket).Write(t.Context(), list))
		require.Equal(t, 2, bucket.calls)
		require.Equal(t, list, readFileList(t, inner, list.Path()))
	})

	t.Run("returns nil when a previous attempt wrote the list and lost the response", func(t *testing.T) {
		inner := objstore.NewInMemBucket()
		bucket := &flakyBucket{Bucket: inner, lostResponses: 1}

		require.NoError(t, newTestWriter(bucket).Write(t.Context(), list))
		require.Equal(t, 2, bucket.calls)
		require.Equal(t, list, readFileList(t, inner, list.Path()))
	})

	t.Run("returns an unrecoverable error without a retry and keeps a malformed stored list", func(t *testing.T) {
		inner := objstore.NewInMemBucket()
		upload(t, inner, list.Path(), []byte("not json"))
		bucket := &flakyBucket{Bucket: inner}

		err := newTestWriter(bucket).Write(t.Context(), list)
		require.ErrorIs(t, err, errUnrecoverable)
		require.Equal(t, 1, bucket.calls)
		require.Equal(t, "not json", readRaw(t, inner, list.Path()))
	})

	t.Run("returns an unrecoverable error without a retry when the stored list holds another tenant", func(t *testing.T) {
		inner := objstore.NewInMemBucket()
		storeFileList(t, inner, list.Path(), FileList{Tenant: "b", CreatedAt: olderList.CreatedAt})
		bucket := &flakyBucket{Bucket: inner}

		err := newTestWriter(bucket).Write(t.Context(), list)
		require.ErrorIs(t, err, errUnrecoverable)
		require.Equal(t, 1, bucket.calls)
	})

	t.Run("returns the context error without an attempt when the context is cancelled", func(t *testing.T) {
		bucket := &flakyBucket{Bucket: objstore.NewInMemBucket()}
		ctx, cancel := context.WithCancel(t.Context())
		cancel()

		err := newTestWriter(bucket).Write(ctx, list)
		require.ErrorIs(t, err, context.Canceled)
		require.Equal(t, 0, bucket.calls)
	})

	t.Run("returns an error without an attempt for an invalid list", func(t *testing.T) {
		for name, invalid := range map[string]FileList{
			"empty tenant":       {CreatedAt: createdAt},
			"unsafe tenant":      {Tenant: "..", CreatedAt: createdAt},
			"tenant with slash":  {Tenant: "a/b", CreatedAt: createdAt},
			"zero creation time": {Tenant: "a"},
		} {
			bucket := &flakyBucket{Bucket: objstore.NewInMemBucket()}

			require.Error(t, newTestWriter(bucket).Write(t.Context(), invalid), name)
			require.Equal(t, 0, bucket.calls, name)
		}
	})
}

var testBackoffConfig = backoff.Config{MinBackoff: time.Millisecond, MaxBackoff: time.Millisecond, MaxRetries: 3}

func newTestWriter(bucket objstore.Bucket) *FileListWriter {
	return NewFileListWriter(bucket, testBackoffConfig, log.NewNopLogger())
}

func storeFileList(t *testing.T, bucket objstore.Bucket, path string, list FileList) {
	t.Helper()
	data, err := json.Marshal(list)
	require.NoError(t, err)
	upload(t, bucket, path, data)
}

func readFileList(t *testing.T, bucket objstore.Bucket, path string) FileList {
	t.Helper()
	list, err := decodeFileList([]byte(readRaw(t, bucket, path)))
	require.NoError(t, err)
	return list
}

func readRaw(t *testing.T, bucket objstore.Bucket, path string) string {
	t.Helper()
	r, err := bucket.Get(t.Context(), path)
	require.NoError(t, err)
	defer r.Close()
	data, err := io.ReadAll(r)
	require.NoError(t, err)
	return string(data)
}

// errConditionalWriteConflict is the error that the S3 bucket returns when a
// conditional write loses to another writer.
var errConditionalWriteConflict = pkgerrors.Wrap(minio.ErrorResponse{Code: "PreconditionFailed", StatusCode: http.StatusPreconditionFailed}, "upload s3 object")

// conflictBucket simulates conditional writes that lose to another writer,
// because the in-memory bucket does not check the write condition.
//
// The first conflicts calls to GetAndReplace read the stored object and call
// fn, as a real bucket does. They then run concurrentWrite, if set, and
// return errConditionalWriteConflict instead of the result of fn. Later calls
// pass through.
//
// conflictBucket is not safe for concurrent use.
type conflictBucket struct {
	objstore.Bucket
	concurrentWrite func()
	conflicts       int
	calls           int
}

func (b *conflictBucket) GetAndReplace(ctx context.Context, name string, fn func(io.ReadCloser) (io.ReadCloser, error)) error {
	b.calls++
	if b.calls > b.conflicts {
		return b.Bucket.GetAndReplace(ctx, name, fn)
	}

	existing, err := b.Get(ctx, name)
	if err != nil && !b.IsObjNotFoundErr(err) {
		return err
	}
	replacement, err := fn(existing)
	if err != nil {
		return err
	}
	replacement.Close()

	if b.concurrentWrite != nil {
		b.concurrentWrite()
	}
	return errConditionalWriteConflict
}

// flakyBucket fails GetAndReplace calls in three ways, in this order:
//
//   - The first failures calls return an error and do not call fn.
//   - The next failedReads calls give fn a stored object whose read fails.
//   - The next lostResponses calls write the object and then return an error.
//
// Later calls pass through. calls counts all GetAndReplace calls.
//
// flakyBucket is not safe for concurrent use.
type flakyBucket struct {
	objstore.Bucket
	failures      int
	failedReads   int
	lostResponses int
	calls         int
}

var errFlaky = errors.New("flaky bucket error")

func (b *flakyBucket) GetAndReplace(ctx context.Context, name string, fn func(io.ReadCloser) (io.ReadCloser, error)) error {
	b.calls++
	switch {
	case b.calls <= b.failures:
		return errFlaky
	case b.calls <= b.failures+b.failedReads:
		_, err := fn(io.NopCloser(failingReader{}))
		return err
	case b.calls <= b.failures+b.failedReads+b.lostResponses:
		if err := b.Bucket.GetAndReplace(ctx, name, fn); err != nil {
			return err
		}
		return errFlaky
	default:
		return b.Bucket.GetAndReplace(ctx, name, fn)
	}
}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, errFlaky }
