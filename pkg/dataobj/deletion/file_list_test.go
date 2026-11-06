package deletion

import (
	"bytes"
	"compress/gzip"
	"context"
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

func TestFileList_Path(t *testing.T) {
	t.Run("returns the gzip JSON Lines key in the deletion directory of the tenant", func(t *testing.T) {
		require.Equal(t, "deletion/a/files.jsonl.gz", FileList{Tenant: "a"}.Path())
	})
}

func TestEncodeFileList(t *testing.T) {
	t.Run("encodes a gzip-compressed JSON Lines stream with the header first", func(t *testing.T) {
		list := FileList{
			Tenant:    "a",
			CreatedAt: time.Date(2026, 11, 4, 12, 0, 0, 500, time.UTC),
			Paths:     []string{"index/v0/indexes/00/x", "objects/tenants/a/aa/y"},
		}

		encoded, err := encodeFileList(list)
		require.NoError(t, err)
		gz, err := gzip.NewReader(bytes.NewReader(encoded))
		require.NoError(t, err)
		text, err := io.ReadAll(gz)
		require.NoError(t, err)
		require.Equal(t, `{"version":1,"tenant":"a","created_at":"2026-11-04T12:00:00.0000005Z"}
{"path":"index/v0/indexes/00/x"}
{"path":"objects/tenants/a/aa/y"}
`, string(text))
	})
}

func TestOpenFileList(t *testing.T) {
	header := `{"version":1,"tenant":"a","created_at":"2026-11-04T12:00:00.0000005Z"}` + "\n"
	path := fileListPath("a")

	t.Run("reads the header and the paths of a stored list", func(t *testing.T) {
		bucket := objstore.NewInMemBucket()
		upload(t, bucket, path, gzipText(t, header+`{"path":"objects/00/x"}`+"\n"+`{"path":"objects/01/y"}`+"\n"))

		r := openFileList(t, bucket, "a")
		require.Equal(t, "a", r.Tenant())
		require.Equal(t, time.Date(2026, 11, 4, 12, 0, 0, 500, time.UTC), r.CreatedAt())
		require.Equal(t, []string{"objects/00/x", "objects/01/y"}, collectPaths(t, r))
	})

	t.Run("reads no paths from a list with only a header", func(t *testing.T) {
		bucket := objstore.NewInMemBucket()
		upload(t, bucket, path, gzipText(t, header))

		require.Empty(t, collectPaths(t, openFileList(t, bucket, "a")))
	})

	t.Run("resumes after the last yielded path when the paths are iterated again after a break", func(t *testing.T) {
		bucket := objstore.NewInMemBucket()
		upload(t, bucket, path, gzipText(t, header+`{"path":"objects/00/x"}`+"\n"+`{"path":"objects/01/y"}`+"\n"))
		r := openFileList(t, bucket, "a")

		for p, err := range r.Paths() {
			require.NoError(t, err)
			require.Equal(t, "objects/00/x", p)
			break
		}
		require.Equal(t, []string{"objects/01/y"}, collectPaths(t, r))
	})

	t.Run("returns the not-found error of the bucket when no list is stored", func(t *testing.T) {
		bucket := objstore.NewInMemBucket()

		_, err := OpenFileList(t.Context(), bucket, "a")
		require.True(t, bucket.IsObjNotFoundErr(err))
	})

	for _, tc := range []struct {
		name   string
		tenant string
	}{
		{"returns an invalid tenant error for an empty tenant", ""},
		{"returns an invalid tenant error for a parent directory tenant", ".."},
		{"returns an invalid tenant error for a tenant with a slash", "a/b"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := OpenFileList(t.Context(), objstore.NewInMemBucket(), tc.tenant)
			require.ErrorContains(t, err, "tenant")
			require.NotErrorIs(t, err, ErrMalformedFileList)
		})
	}

	for _, tc := range []struct {
		name string
		data []byte
	}{
		{"returns ErrMalformedFileList for an empty object", []byte{}},
		{"returns ErrMalformedFileList for an object that is not gzip", []byte(header)},
		{"returns ErrMalformedFileList for a header with an unknown field", gzipText(t, `{"version":1,"tenant":"a","created_at":"2026-11-04T12:00:00Z","extra":2}`+"\n")},
		{"returns ErrMalformedFileList for a header with an unknown version", gzipText(t, `{"version":2,"tenant":"a","created_at":"2026-11-04T12:00:00Z"}`+"\n")},
		{"returns ErrMalformedFileList for a header without a version", gzipText(t, `{"tenant":"a","created_at":"2026-11-04T12:00:00Z"}`+"\n")},
		{"returns ErrMalformedFileList for a path record before the header", gzipText(t, `{"path":"objects/00/x"}`+"\n"+header)},
		{"returns ErrMalformedFileList for a header without a creation time", gzipText(t, `{"version":1,"tenant":"a"}`+"\n")},
		{"returns ErrMalformedFileList for a header that holds another tenant", gzipText(t, `{"version":1,"tenant":"b","created_at":"2026-11-04T12:00:00Z"}`+"\n")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bucket := objstore.NewInMemBucket()
			upload(t, bucket, path, tc.data)

			_, err := OpenFileList(t.Context(), bucket, "a")
			require.ErrorIs(t, err, ErrMalformedFileList)
		})
	}

	valid := header + `{"path":"objects/01/y"}` + "\n"
	for _, tc := range []struct {
		name string
		data []byte
	}{
		{"yields ErrMalformedFileList after the valid paths for a path out of order", gzipText(t, valid+`{"path":"objects/00/x"}`+"\n")},
		{"yields ErrMalformedFileList after the valid paths for a duplicate path", gzipText(t, valid+`{"path":"objects/01/y"}`+"\n")},
		{"yields ErrMalformedFileList after the valid paths for an empty path", gzipText(t, valid+`{"path":""}`+"\n")},
		{"yields ErrMalformedFileList after the valid paths for a path record with an unknown field", gzipText(t, valid+`{"path":"objects/02/z","size":1}`+"\n")},
		{"yields ErrMalformedFileList after the valid paths for data after the gzip stream", append(gzipText(t, valid), []byte("garbage")...)},
		{"yields ErrMalformedFileList after the valid paths for a second gzip stream", append(gzipText(t, valid), gzipText(t, `{"path":"objects/02/z"}`+"\n")...)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bucket := objstore.NewInMemBucket()
			upload(t, bucket, path, tc.data)

			paths, err := readUntilError(openFileList(t, bucket, "a"))
			require.Equal(t, []string{"objects/01/y"}, paths)
			require.ErrorIs(t, err, ErrMalformedFileList)
		})
	}

	t.Run("yields the same error again when the paths are iterated after a malformed record", func(t *testing.T) {
		bucket := objstore.NewInMemBucket()
		upload(t, bucket, path, gzipText(t, valid+`{"path":"objects/00/x"}`+"\n"+`{"path":"objects/02/z"}`+"\n"))
		r := openFileList(t, bucket, "a")

		_, firstErr := readUntilError(r)
		require.ErrorIs(t, firstErr, ErrMalformedFileList)
		paths, secondErr := readUntilError(r)
		require.Empty(t, paths)
		require.Equal(t, firstErr, secondErr)
	})

	t.Run("yields an error at the end of a stream without its gzip trailer", func(t *testing.T) {
		bucket := objstore.NewInMemBucket()
		data := gzipText(t, header+`{"path":"objects/00/x"}`+"\n")
		upload(t, bucket, path, data[:len(data)-4])

		_, err := readUntilError(openFileList(t, bucket, "a"))
		require.ErrorIs(t, err, io.ErrUnexpectedEOF)
		require.ErrorIs(t, err, ErrMalformedFileList)
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
		require.Equal(t, list, readFileList(t, bucket, "a"))
	})

	t.Run("gives the bucket a body that reports its size", func(t *testing.T) {
		bucket := &sizeCheckingBucket{Bucket: objstore.NewInMemBucket()}

		require.NoError(t, newTestWriter(bucket).Write(t.Context(), list))
		require.NoError(t, bucket.sizeErr)
		require.Positive(t, bucket.size)
	})

	t.Run("replaces a stored list with an earlier creation time", func(t *testing.T) {
		bucket := objstore.NewInMemBucket()
		storeFileList(t, bucket, olderList)

		require.NoError(t, newTestWriter(bucket).Write(t.Context(), list))
		require.Equal(t, list, readFileList(t, bucket, "a"))
	})

	t.Run("returns ErrSuperseded and keeps a stored list equal to the list", func(t *testing.T) {
		bucket := objstore.NewInMemBucket()
		storeFileList(t, bucket, list)

		require.ErrorIs(t, newTestWriter(bucket).Write(t.Context(), list), ErrSuperseded)
		require.Equal(t, list, readFileList(t, bucket, "a"))
	})

	t.Run("replaces an older stored list with malformed paths because it reads only the header", func(t *testing.T) {
		bucket := objstore.NewInMemBucket()
		upload(t, bucket, list.Path(), gzipText(t, `{"version":1,"tenant":"a","created_at":"2026-11-04T11:00:00Z"}`+"\n"+`not json`+"\n"))

		require.NoError(t, newTestWriter(bucket).Write(t.Context(), list))
		require.Equal(t, list, readFileList(t, bucket, "a"))
	})

	t.Run("returns ErrSuperseded and keeps a different stored list with the same creation time", func(t *testing.T) {
		bucket := objstore.NewInMemBucket()
		stored := FileList{Tenant: "a", CreatedAt: createdAt, Paths: []string{"objects/04/same"}}
		storeFileList(t, bucket, stored)

		require.ErrorIs(t, newTestWriter(bucket).Write(t.Context(), list), ErrSuperseded)
		require.Equal(t, stored, readFileList(t, bucket, "a"))
	})

	t.Run("returns ErrSuperseded and keeps a stored list with a later creation time", func(t *testing.T) {
		bucket := objstore.NewInMemBucket()
		storeFileList(t, bucket, newerList)

		require.ErrorIs(t, newTestWriter(bucket).Write(t.Context(), list), ErrSuperseded)
		require.Equal(t, newerList, readFileList(t, bucket, "a"))
	})

	t.Run("retries after a conflict and keeps a newer list from the competing writer", func(t *testing.T) {
		inner := objstore.NewInMemBucket()
		bucket := &conflictBucket{Bucket: inner, conflicts: 1, concurrentWrite: func() { storeFileList(t, inner, newerList) }}

		require.ErrorIs(t, newTestWriter(bucket).Write(t.Context(), list), ErrSuperseded)
		require.Equal(t, 2, bucket.calls)
		require.Equal(t, newerList, readFileList(t, inner, "a"))
	})

	t.Run("retries after a conflict and replaces an older list from the competing writer", func(t *testing.T) {
		inner := objstore.NewInMemBucket()
		bucket := &conflictBucket{Bucket: inner, conflicts: 1, concurrentWrite: func() { storeFileList(t, inner, olderList) }}

		require.NoError(t, newTestWriter(bucket).Write(t.Context(), list))
		require.Equal(t, 2, bucket.calls)
		require.Equal(t, list, readFileList(t, inner, "a"))
	})

	t.Run("returns an error after the last attempt when every attempt conflicts", func(t *testing.T) {
		bucket := &conflictBucket{Bucket: objstore.NewInMemBucket(), conflicts: 100}

		err := newTestWriter(bucket).Write(t.Context(), list)
		require.ErrorIs(t, err, errConditionalWriteConflict)
		require.Equal(t, testBackoffConfig.MaxRetries, bucket.calls)
	})

	t.Run("returns the context error without a retry when the context ends during an attempt", func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		bucket := &conflictBucket{Bucket: objstore.NewInMemBucket(), conflicts: 100, concurrentWrite: cancel}

		err := newTestWriter(bucket).Write(ctx, list)
		require.ErrorIs(t, err, context.Canceled)
		require.Equal(t, 1, bucket.calls)
	})

	t.Run("retries after a bucket error and writes the list", func(t *testing.T) {
		inner := objstore.NewInMemBucket()
		bucket := &flakyBucket{Bucket: inner, failures: 1}

		require.NoError(t, newTestWriter(bucket).Write(t.Context(), list))
		require.Equal(t, 2, bucket.calls)
		require.Equal(t, list, readFileList(t, inner, "a"))
	})

	t.Run("retries after a read of the stored header fails partway and writes the list", func(t *testing.T) {
		inner := objstore.NewInMemBucket()
		storeFileList(t, inner, olderList)
		bucket := &flakyBucket{Bucket: inner, failedReads: 1}

		require.NoError(t, newTestWriter(bucket).Write(t.Context(), list))
		require.Equal(t, 2, bucket.calls)
		require.Equal(t, list, readFileList(t, inner, "a"))
	})

	t.Run("returns ErrSuperseded and keeps the list when a previous attempt wrote it and lost the response", func(t *testing.T) {
		inner := objstore.NewInMemBucket()
		bucket := &flakyBucket{Bucket: inner, lostResponses: 1}

		require.ErrorIs(t, newTestWriter(bucket).Write(t.Context(), list), ErrSuperseded)
		require.Equal(t, 2, bucket.calls)
		require.Equal(t, list, readFileList(t, inner, "a"))
	})

	t.Run("returns ErrMalformedFileList without a retry and keeps a malformed stored list", func(t *testing.T) {
		inner := objstore.NewInMemBucket()
		upload(t, inner, list.Path(), []byte("not json"))
		bucket := &flakyBucket{Bucket: inner}

		err := newTestWriter(bucket).Write(t.Context(), list)
		require.ErrorIs(t, err, ErrMalformedFileList)
		require.Equal(t, 1, bucket.calls)
		require.Equal(t, "not json", readRaw(t, inner, list.Path()))
	})

	t.Run("returns ErrMalformedFileList without a retry when the stored list holds another tenant", func(t *testing.T) {
		inner := objstore.NewInMemBucket()
		other, err := encodeFileList(FileList{Tenant: "b", CreatedAt: olderList.CreatedAt})
		require.NoError(t, err)
		upload(t, inner, list.Path(), other)
		bucket := &flakyBucket{Bucket: inner}

		require.ErrorIs(t, newTestWriter(bucket).Write(t.Context(), list), ErrMalformedFileList)
		require.Equal(t, 1, bucket.calls)
	})

	t.Run("returns ErrMalformedFileList without a retry when the stored list has an unknown version", func(t *testing.T) {
		inner := objstore.NewInMemBucket()
		upload(t, inner, list.Path(), gzipText(t, `{"version":2,"tenant":"a","created_at":"2026-11-04T11:00:00Z"}`+"\n"))
		bucket := &flakyBucket{Bucket: inner}

		require.ErrorIs(t, newTestWriter(bucket).Write(t.Context(), list), ErrMalformedFileList)
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

	for _, tc := range []struct {
		name string
		list FileList
	}{
		{"returns an error without an attempt for an empty tenant", FileList{CreatedAt: createdAt}},
		{"returns an error without an attempt for an unsafe tenant", FileList{Tenant: "..", CreatedAt: createdAt}},
		{"returns an error without an attempt for a tenant with a slash", FileList{Tenant: "a/b", CreatedAt: createdAt}},
		{"returns an error without an attempt for a zero creation time", FileList{Tenant: "a"}},
		{"returns an error without an attempt for unsorted paths", FileList{Tenant: "a", CreatedAt: createdAt, Paths: []string{"objects/01/y", "objects/00/x"}}},
		{"returns an error without an attempt for duplicate paths", FileList{Tenant: "a", CreatedAt: createdAt, Paths: []string{"objects/00/x", "objects/00/x"}}},
		{"returns an error without an attempt for an empty path", FileList{Tenant: "a", CreatedAt: createdAt, Paths: []string{""}}},
		{"returns an error without an attempt for a path that is not valid UTF-8", FileList{Tenant: "a", CreatedAt: createdAt, Paths: []string{"objects/00/\xff"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bucket := &flakyBucket{Bucket: objstore.NewInMemBucket()}

			require.Error(t, newTestWriter(bucket).Write(t.Context(), tc.list))
			require.Equal(t, 0, bucket.calls)
		})
	}
}

var testBackoffConfig = backoff.Config{MinBackoff: time.Millisecond, MaxBackoff: time.Millisecond, MaxRetries: 3}

func newTestWriter(bucket objstore.Bucket) *FileListWriter {
	return NewFileListWriter(bucket, testBackoffConfig, log.NewNopLogger())
}

// storeFileList stores list at list.Path() in the stored format.
func storeFileList(t *testing.T, bucket objstore.Bucket, list FileList) {
	t.Helper()
	data, err := encodeFileList(list)
	require.NoError(t, err)
	upload(t, bucket, list.Path(), data)
}

func readFileList(t *testing.T, bucket objstore.Bucket, tenant string) FileList {
	t.Helper()
	r := openFileList(t, bucket, tenant)
	return FileList{Tenant: r.Tenant(), CreatedAt: r.CreatedAt(), Paths: collectPaths(t, r)}
}

// openFileList opens the stored list of tenant and closes it at the end of
// the test.
func openFileList(t *testing.T, bucket objstore.BucketReader, tenant string) *FileListReader {
	t.Helper()
	r, err := OpenFileList(t.Context(), bucket, tenant)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, r.Close()) })
	return r
}

func collectPaths(t *testing.T, r *FileListReader) []string {
	t.Helper()
	paths := []string{}
	for p, err := range r.Paths() {
		require.NoError(t, err)
		paths = append(paths, p)
	}
	return paths
}

// readUntilError returns the paths that r yields before its first error, and
// the error.
func readUntilError(r *FileListReader) ([]string, error) {
	var paths []string
	for p, err := range r.Paths() {
		if err != nil {
			return paths, err
		}
		paths = append(paths, p)
	}
	return paths, nil
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

func gzipText(t *testing.T, text string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	_, err := gz.Write([]byte(text))
	require.NoError(t, err)
	require.NoError(t, gz.Close())
	return buf.Bytes()
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

// sizeCheckingBucket records the size that objstore.TryToGetSize reads from
// the body of the last write. The S3 bucket needs that size to keep the
// write condition.
type sizeCheckingBucket struct {
	objstore.Bucket
	size    int64
	sizeErr error
}

func (b *sizeCheckingBucket) GetAndReplace(ctx context.Context, name string, fn func(io.ReadCloser) (io.ReadCloser, error)) error {
	return b.Bucket.GetAndReplace(ctx, name, func(existing io.ReadCloser) (io.ReadCloser, error) {
		body, err := fn(existing)
		if err != nil {
			return nil, err
		}
		b.size, b.sizeErr = objstore.TryToGetSize(body)
		return body, nil
	})
}

// flakyBucket fails GetAndReplace calls in three ways, in this order:
//
//   - The first failures calls return an error and do not call fn.
//   - The next failedReads calls give fn the first half of the stored object,
//     followed by a read error.
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
		r, err := b.Get(ctx, name)
		if err != nil {
			return err
		}
		data, err := io.ReadAll(r)
		r.Close()
		if err != nil {
			return err
		}
		_, err = fn(io.NopCloser(io.MultiReader(bytes.NewReader(data[:len(data)/2]), failingReader{})))
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
