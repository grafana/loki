package deletion

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/stretchr/testify/require"
	"github.com/thanos-io/objstore"
	"github.com/thanos-io/objstore/providers/filesystem"
)

func TestNewFileListScanner(t *testing.T) {
	t.Run("returns an error when the bucket does not report modification times", func(t *testing.T) {
		_, err := NewFileListScanner(noUpdatedAtBucket{fileBucket: newFileBucket(t)}, log.NewNopLogger())
		require.ErrorContains(t, err, "modification times")
	})
}

func TestFileListScanner_List(t *testing.T) {
	data := []byte("x")
	now := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)
	old := now.Add(-2 * time.Hour)

	t.Run("returns the objects in the log and index directories of the tenant and skips all other objects", func(t *testing.T) {
		bucket := newFileBucket(t)
		bucket.put(t, "objects/tenants/a/aa/log", data, old)
		bucket.put(t, "objects/tenants/a/bb/log", data, old)
		bucket.put(t, "objects/tenants/ab/aa/other-tenant", data, old)
		bucket.put(t, "objects/tenants/b/aa/other-tenant", data, old)
		bucket.put(t, "index/v0/indexes/tenants/a/cc/index", data, old)
		bucket.put(t, "index/v0/indexes/tenants/b/cc/other-tenant", data, old)
		bucket.put(t, "objects/00/uncompacted", data, old)
		bucket.put(t, "index/v0/indexes/00/uncompacted", data, old)
		bucket.put(t, "index/v0/tocs/2026-11-04T00_00_00Z/a/toc.toc", data, old)
		bucket.put(t, "deletion/a/files.jsonl.gz", data, old)

		list, err := newTestFileListScanner(t, bucket).List(t.Context(), "a", now)
		require.NoError(t, err)
		require.Equal(t, []string{
			"index/v0/indexes/tenants/a/cc/index",
			"objects/tenants/a/aa/log",
			"objects/tenants/a/bb/log",
		}, list)
	})

	t.Run("returns no paths when the tenant has no objects", func(t *testing.T) {
		list, err := newTestFileListScanner(t, newFileBucket(t)).List(t.Context(), "a", now)
		require.NoError(t, err)
		require.Empty(t, list)
	})

	t.Run("excludes objects modified less than MinObjectAge before now", func(t *testing.T) {
		bucket := newFileBucket(t)
		atMinAge := now.Add(-MinObjectAge)
		young := atMinAge.Add(time.Second)
		bucket.put(t, "objects/tenants/a/aa/at-min-age", data, atMinAge)
		bucket.put(t, "objects/tenants/a/aa/young", data, young)
		bucket.put(t, "index/v0/indexes/tenants/a/bb/young", data, young)

		list, err := newTestFileListScanner(t, bucket).List(t.Context(), "a", now)
		require.NoError(t, err)
		require.Equal(t, []string{"objects/tenants/a/aa/at-min-age"}, list)
	})

	t.Run("logs the counts of the scan", func(t *testing.T) {
		bucket := newFileBucket(t)
		bucket.put(t, "objects/tenants/a/aa/old", data, old)
		bucket.put(t, "objects/tenants/a/aa/young", data, now)
		var logs bytes.Buffer
		scanner, err := NewFileListScanner(bucket, log.NewLogfmtLogger(log.NewSyncWriter(&logs)))
		require.NoError(t, err)

		_, err = scanner.List(t.Context(), "a", now)
		require.NoError(t, err)
		require.Contains(t, logs.String(), `msg="listed objects of tenant" tenant=a cutoff=2026-11-04T11:00:00Z listed=2 too_young=1 included=1 duration=`)
	})

	t.Run("lists only the log and index directories of the tenant", func(t *testing.T) {
		files := newFileBucket(t)
		files.put(t, "objects/tenants/a/aa/log", data, old)
		files.put(t, "objects/00/uncompacted", data, old)
		bucket := &listRecordingBucket{fileBucket: files}

		_, err := newTestFileListScanner(t, bucket).List(t.Context(), "a", now)
		require.NoError(t, err)
		require.Equal(t, []string{"objects/tenants/a/", "index/v0/indexes/tenants/a/"}, bucket.dirs)
	})

	t.Run("skips directory placeholder objects", func(t *testing.T) {
		files := newFileBucket(t)
		files.put(t, "objects/tenants/a/aa/log", data, old)
		bucket := &placeholderBucket{fileBucket: files, modified: old, placeholders: map[string]string{"objects/tenants/a/": "objects/tenants/a/aa/"}}

		list, err := newTestFileListScanner(t, bucket).List(t.Context(), "a", now)
		require.NoError(t, err)
		require.Equal(t, []string{"objects/tenants/a/aa/log"}, list)
	})

	t.Run("returns an error naming the directory when a listing fails", func(t *testing.T) {
		files := newFileBucket(t)
		files.put(t, "objects/tenants/a/aa/log", data, old)
		bucket := &noModTimeBucket{fileBucket: files, name: "objects/tenants/a/aa/log"}

		_, err := newTestFileListScanner(t, bucket).List(t.Context(), "a", now)
		require.ErrorContains(t, err, "listing objects in objects/tenants/a/")
		require.ErrorContains(t, err, "modification time of objects/tenants/a/aa/log")
	})

	t.Run("returns the context error when the context is cancelled", func(t *testing.T) {
		bucket := newFileBucket(t)
		bucket.put(t, "objects/tenants/a/aa/log", data, old)
		ctx, cancel := context.WithCancel(t.Context())
		cancel()

		_, err := newTestFileListScanner(t, bucket).List(ctx, "a", now)
		require.ErrorIs(t, err, context.Canceled)
	})

	for _, tc := range []struct {
		name   string
		tenant string
	}{
		{"returns an error for an empty tenant", ""},
		{"returns an error for a current directory tenant", "."},
		{"returns an error for a parent directory tenant", ".."},
		{"returns an error for a tenant with a slash", "a/aa"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := newTestFileListScanner(t, newFileBucket(t)).List(t.Context(), tc.tenant, now)
			require.ErrorContains(t, err, "tenant")
		})
	}
}

func newTestFileListScanner(t *testing.T, bucket objstore.BucketReader) *FileListScanner {
	t.Helper()
	scanner, err := NewFileListScanner(bucket, log.NewNopLogger())
	require.NoError(t, err)
	return scanner
}

// fileBucket is a filesystem bucket whose objects have a modification time
// that the test chooses.
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

// listRecordingBucket records the directory of each listing.
//
// listRecordingBucket is not safe for concurrent use.
type listRecordingBucket struct {
	fileBucket
	dirs []string
}

func (b *listRecordingBucket) IterWithAttributes(ctx context.Context, dir string, f func(objstore.IterObjectAttributes) error, options ...objstore.IterOption) error {
	b.dirs = append(b.dirs, dir)
	return b.fileBucket.IterWithAttributes(ctx, dir, f, options...)
}

// placeholderBucket adds an empty placeholder object to the listing of each
// directory in placeholders. The callback gets it before the other objects.
// The filesystem bucket cannot hold keys that end in a slash.
type placeholderBucket struct {
	fileBucket
	placeholders map[string]string
	modified     time.Time
}

func (b *placeholderBucket) IterWithAttributes(ctx context.Context, dir string, f func(objstore.IterObjectAttributes) error, options ...objstore.IterOption) error {
	if name, ok := b.placeholders[dir]; ok {
		attrs := objstore.IterObjectAttributes{Name: name}
		attrs.SetLastModified(b.modified)
		if err := f(attrs); err != nil {
			return err
		}
	}
	return b.fileBucket.IterWithAttributes(ctx, dir, f, options...)
}

// noUpdatedAtBucket does not support the UpdatedAt iter option, like the Swift
// and OSS buckets.
type noUpdatedAtBucket struct {
	fileBucket
}

func (noUpdatedAtBucket) SupportedIterOptions() []objstore.IterOptionType {
	return []objstore.IterOptionType{objstore.Recursive}
}

// noModTimeBucket supports the UpdatedAt iter option, but reports no
// modification time for the object name.
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
