package deletion

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/stretchr/testify/require"
	"github.com/thanos-io/objstore"
	"github.com/thanos-io/objstore/providers/filesystem"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/fixtures"
)

func TestNewScanner(t *testing.T) {
	t.Run("returns an error when max open files is less than one", func(t *testing.T) {
		for _, maxOpenFiles := range []int{0, -1} {
			_, err := NewScanner(newFileBucket(t), "index/v0", maxOpenFiles, log.NewNopLogger())
			require.ErrorContains(t, err, "max open files")
		}
	})

	t.Run("returns an error when the bucket does not report modification times", func(t *testing.T) {
		_, err := NewScanner(objstore.NewInMemBucket(), "index/v0", 2, log.NewNopLogger())
		require.ErrorContains(t, err, "modification times")
	})
}

func TestScanner_List(t *testing.T) {
	// The tests store invalid bytes in each object that List must not open. If
	// List opens one, it returns an error and the test fails.
	notADataObject := []byte("not a data object")
	now := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)
	old := now.Add(-2 * time.Hour)

	t.Run("returns single-tenant objects of the tenant and skips all other objects", func(t *testing.T) {
		bucket := newFileBucket(t)
		bucket.put(t, "objects/tenants/a/aa/compacted", notADataObject, old)
		bucket.put(t, "objects/tenants/ab/aa/compacted", notADataObject, old)
		bucket.put(t, "objects/tenants/b/aa/compacted", notADataObject, old)
		bucket.put(t, "index/v0/indexes/tenants/a/bb/compacted", notADataObject, old)
		bucket.put(t, "index/v0/indexes/tenants/b/bb/compacted", notADataObject, old)
		bucket.put(t, "objects/00/single-a", objectBytes(t, "a"), old)
		bucket.put(t, "objects/01/single-b", objectBytes(t, "b"), old)
		bucket.put(t, "objects/02/multi-a-b", objectBytes(t, "a", "b"), old)
		bucket.put(t, "objects/03/multi-b-c", objectBytes(t, "b", "c"), old)
		bucket.put(t, "objects/04/empty-tenant", objectBytes(t, ""), old)
		bucket.put(t, "index/v0/indexes/00/single-a", objectBytes(t, "a"), old)
		bucket.put(t, "index/v0/indexes/01/multi-a-b", objectBytes(t, "a", "b"), old)
		bucket.put(t, "index/v0/tocs/2026-11-04T00_00_00Z/a/toc.toc", notADataObject, old)
		bucket.put(t, "deletion/a/files.json", notADataObject, old)

		list, err := newTestScanner(t, bucket, "index/v0").List(t.Context(), "a", now)
		require.NoError(t, err)
		require.Equal(t, []string{
			"index/v0/indexes/00/single-a",
			"index/v0/indexes/tenants/a/bb/compacted",
			"objects/00/single-a",
			"objects/tenants/a/aa/compacted",
		}, list.Paths)
	})

	t.Run("logs skipped ingest objects that hold the tenant with other tenants", func(t *testing.T) {
		bucket := newFileBucket(t)
		bucket.put(t, "objects/02/multi-a-b", objectBytes(t, "a", "b"), old)
		bucket.put(t, "objects/03/multi-b-c", objectBytes(t, "b", "c"), old)
		bucket.put(t, "objects/04/empty-tenant", objectBytes(t, ""), old)

		var logs bytes.Buffer
		scanner, err := NewScanner(bucket, "index/v0", 2, log.NewLogfmtLogger(log.NewSyncWriter(&logs)))
		require.NoError(t, err)

		list, err := scanner.List(t.Context(), "a", now)
		require.NoError(t, err)
		require.Empty(t, list.Paths)
		require.Contains(t, logs.String(), "path=objects/02/multi-a-b")
		require.Contains(t, logs.String(), "path=objects/04/empty-tenant")
		require.NotContains(t, logs.String(), "path=objects/03/multi-b-c")
	})

	t.Run("returns the tenant and the scan time in the file list", func(t *testing.T) {
		list, err := newTestScanner(t, newFileBucket(t), "index/v0").List(t.Context(), "a", now)
		require.NoError(t, err)
		require.Equal(t, FileList{Tenant: "a", CreatedAt: now, Paths: []string{}}, list)
	})

	t.Run("excludes objects modified less than MinObjectAge before now and does not open them", func(t *testing.T) {
		bucket := newFileBucket(t)
		atMinAge := now.Add(-MinObjectAge)
		young := atMinAge.Add(time.Second)
		bucket.put(t, "objects/tenants/a/aa/at-min-age", notADataObject, atMinAge)
		bucket.put(t, "objects/tenants/a/aa/young", notADataObject, young)
		bucket.put(t, "index/v0/indexes/tenants/a/bb/young", notADataObject, young)
		bucket.put(t, "objects/00/at-min-age", objectBytes(t, "a"), atMinAge)
		bucket.put(t, "objects/01/young", notADataObject, young)
		bucket.put(t, "objects/young-stray", notADataObject, young)
		bucket.put(t, "index/v0/indexes/00/young", notADataObject, young)

		list, err := newTestScanner(t, bucket, "index/v0").List(t.Context(), "a", now)
		require.NoError(t, err)
		require.Equal(t, []string{
			"objects/00/at-min-age",
			"objects/tenants/a/aa/at-min-age",
		}, list.Paths)
	})

	t.Run("includes an ingest object stored directly below the objects directory", func(t *testing.T) {
		bucket := newFileBucket(t)
		bucket.put(t, "objects/stray", objectBytes(t, "a"), old)

		list, err := newTestScanner(t, bucket, "index/v0").List(t.Context(), "a", now)
		require.NoError(t, err)
		require.Equal(t, []string{"objects/stray"}, list.Paths)
	})

	t.Run("finds index objects when the index prefix has leading and trailing slashes", func(t *testing.T) {
		bucket := newFileBucket(t)
		bucket.put(t, "index/v0/indexes/tenants/a/bb/compacted", notADataObject, old)
		bucket.put(t, "index/v0/indexes/00/single-a", objectBytes(t, "a"), old)

		list, err := newTestScanner(t, bucket, "/index/v0/").List(t.Context(), "a", now)
		require.NoError(t, err)
		require.Equal(t, []string{
			"index/v0/indexes/00/single-a",
			"index/v0/indexes/tenants/a/bb/compacted",
		}, list.Paths)
	})

	t.Run("returns an error naming the object when an ingest object cannot be opened", func(t *testing.T) {
		bucket := newFileBucket(t)
		bucket.put(t, "objects/00/single-a", objectBytes(t, "a"), old)
		bucket.put(t, "objects/01/corrupt", notADataObject, old)

		_, err := newTestScanner(t, bucket, "index/v0").List(t.Context(), "a", now)
		require.ErrorContains(t, err, "objects/01/corrupt")
	})

	t.Run("returns the open error and not a context error when an open fails during the walk", func(t *testing.T) {
		files := newFileBucket(t)
		files.put(t, "objects/01/corrupt", notADataObject, old)
		bucket := &blockingBucket{fileBucket: files, blockAfter: "objects/01/corrupt"}

		_, err := newTestScanner(t, bucket, "index/v0").List(t.Context(), "a", now)
		require.ErrorContains(t, err, "opening object objects/01/corrupt")
		require.NotErrorIs(t, err, context.Canceled)
	})

	t.Run("returns an error naming the object when the bucket does not report its modification time", func(t *testing.T) {
		files := newFileBucket(t)
		files.put(t, "objects/00/single-a", objectBytes(t, "a"), old)
		files.put(t, "objects/01/no-time", objectBytes(t, "a"), old)
		bucket := &noModTimeBucket{fileBucket: files, name: "objects/01/no-time"}

		_, err := newTestScanner(t, bucket, "index/v0").List(t.Context(), "a", now)
		require.ErrorContains(t, err, "modification time of objects/01/no-time")
	})

	t.Run("returns the context error when the context is cancelled", func(t *testing.T) {
		bucket := newFileBucket(t)
		bucket.put(t, "objects/00/single-a", objectBytes(t, "a"), old)
		ctx, cancel := context.WithCancel(t.Context())
		cancel()

		_, err := newTestScanner(t, bucket, "index/v0").List(ctx, "a", now)
		require.ErrorIs(t, err, context.Canceled)
	})

	t.Run("returns an error for an empty or unsafe tenant", func(t *testing.T) {
		scanner := newTestScanner(t, newFileBucket(t), "index/v0")
		for _, tenant := range []string{"", ".", "..", "a/aa"} {
			_, err := scanner.List(t.Context(), tenant, now)
			require.Error(t, err, "tenant %q", tenant)
		}
	})
}

func newTestScanner(t *testing.T, bucket objstore.BucketReader, indexPrefix string) *Scanner {
	t.Helper()
	scanner, err := NewScanner(bucket, indexPrefix, 2, log.NewNopLogger())
	require.NoError(t, err)
	return scanner
}

// fileBucket is a filesystem bucket whose objects have a modification time
// that the test chooses. The in-memory bucket does not report modification
// times.
type fileBucket struct {
	*filesystem.Bucket
	root string
}

func newFileBucket(t *testing.T) fileBucket {
	t.Helper()
	root := t.TempDir()
	bucket, err := filesystem.NewBucket(root)
	require.NoError(t, err)
	return fileBucket{Bucket: bucket, root: root}
}

func (b fileBucket) put(t *testing.T, path string, data []byte, modified time.Time) {
	t.Helper()
	upload(t, b, path, data)
	require.NoError(t, os.Chtimes(filepath.Join(b.root, path), modified, modified))
}

// blockingBucket blocks a listing after it reports the object blockAfter,
// until the context of the listing ends.
type blockingBucket struct {
	fileBucket
	blockAfter string
}

func (b *blockingBucket) IterWithAttributes(ctx context.Context, dir string, f func(objstore.IterObjectAttributes) error, options ...objstore.IterOption) error {
	return b.fileBucket.IterWithAttributes(ctx, dir, func(attrs objstore.IterObjectAttributes) error {
		if err := f(attrs); err != nil {
			return err
		}
		if attrs.Name == b.blockAfter {
			<-ctx.Done()
			return ctx.Err()
		}
		return nil
	}, options...)
}

// noModTimeBucket reports no modification time for the object name.
type noModTimeBucket struct {
	fileBucket
	name string
}

func (b *noModTimeBucket) IterWithAttributes(ctx context.Context, dir string, f func(objstore.IterObjectAttributes) error, options ...objstore.IterOption) error {
	return b.fileBucket.IterWithAttributes(ctx, dir, func(attrs objstore.IterObjectAttributes) error {
		if attrs.Name == b.name {
			attrs = objstore.IterObjectAttributes{Name: attrs.Name}
		}
		return f(attrs)
	}, options...)
}

func upload(t *testing.T, bucket objstore.Bucket, path string, data []byte) {
	t.Helper()
	require.NoError(t, bucket.Upload(t.Context(), path, bytes.NewReader(data)))
}

// objectBytes returns the encoded data object with one streams section for
// each tenant in tenants.
func objectBytes(t *testing.T, tenants ...string) []byte {
	t.Helper()

	lr := fixtures.NewLogsFixtureBuilder(t)
	lr.ForStream(`{app="foo"}`).Entry(10, `{}`, "hello")

	sections := make([]dataobj.SectionBuilder, 0, len(tenants))
	for _, tenant := range tenants {
		sections = append(sections, fixtures.StreamsSection(t, tenant, lr.Streams()))
	}
	obj, closer := fixtures.DataObject(t, sections...)
	t.Cleanup(func() { require.NoError(t, closer.Close()) })

	r, err := obj.Reader(t.Context())
	require.NoError(t, err)
	defer r.Close()
	data, err := io.ReadAll(r)
	require.NoError(t, err)
	return data
}
