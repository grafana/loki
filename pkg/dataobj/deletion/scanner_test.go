package deletion

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	"github.com/thanos-io/objstore"
	"github.com/thanos-io/objstore/providers/filesystem"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/fixtures"
)

func TestNewFileListScanner(t *testing.T) {
	for _, tc := range []struct {
		name         string
		maxOpenFiles int
	}{
		{"returns an error when max open files is zero", 0},
		{"returns an error when max open files is negative", -1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewFileListScanner(newFileBucket(t), "index/v0", tc.maxOpenFiles, log.NewNopLogger())
			require.ErrorContains(t, err, "max open files")
		})
	}

	t.Run("returns an error when the bucket does not report modification times", func(t *testing.T) {
		_, err := NewFileListScanner(objstore.NewInMemBucket(), "index/v0", 2, log.NewNopLogger())
		require.ErrorContains(t, err, "modification times")
	})
}

func TestFileListScanner_List(t *testing.T) {
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
		bucket.put(t, "deletion/a/files.jsonl.gz", notADataObject, old)

		list, err := newTestFileListScanner(t, bucket, "index/v0").List(t.Context(), "a", now)
		require.NoError(t, err)
		require.Equal(t, []string{
			"index/v0/indexes/00/single-a",
			"index/v0/indexes/tenants/a/bb/compacted",
			"objects/00/single-a",
			"objects/tenants/a/aa/compacted",
		}, list.Paths)
	})

	t.Run("logs skipped uncompacted objects that hold the tenant with other tenants and a summary of the scan", func(t *testing.T) {
		bucket := newFileBucket(t)
		bucket.put(t, "objects/01/single-a", objectBytes(t, "a"), old)
		bucket.put(t, "objects/02/multi-a-b", objectBytes(t, "a", "b"), old)
		bucket.put(t, "objects/03/multi-b-c", objectBytes(t, "b", "c"), old)
		bucket.put(t, "objects/04/empty-tenant", objectBytes(t, ""), old)
		bucket.put(t, "objects/05/young", notADataObject, now)

		var logs bytes.Buffer
		scanner, err := NewFileListScanner(bucket, "index/v0", 2, log.NewLogfmtLogger(log.NewSyncWriter(&logs)))
		require.NoError(t, err)

		list, err := scanner.List(t.Context(), "a", now)
		require.NoError(t, err)
		require.Equal(t, []string{"objects/01/single-a"}, list.Paths)
		require.Contains(t, logs.String(), "path=objects/02/multi-a-b")
		require.Contains(t, logs.String(), "path=objects/04/empty-tenant")
		require.NotContains(t, logs.String(), "path=objects/03/multi-b-c")
		require.Contains(t, logs.String(), "listed=5 too_young=1 opened=4 multi_tenant=2 included=1")
	})

	t.Run("returns the tenant and the scan time in the file list", func(t *testing.T) {
		list, err := newTestFileListScanner(t, newFileBucket(t), "index/v0").List(t.Context(), "a", now)
		require.NoError(t, err)
		require.Equal(t, FileList{Tenant: "a", CreatedAt: now}, list)
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

		list, err := newTestFileListScanner(t, bucket, "index/v0").List(t.Context(), "a", now)
		require.NoError(t, err)
		require.Equal(t, []string{
			"objects/00/at-min-age",
			"objects/tenants/a/aa/at-min-age",
		}, list.Paths)
	})

	t.Run("includes an uncompacted object stored directly below the objects directory", func(t *testing.T) {
		bucket := newFileBucket(t)
		bucket.put(t, "objects/stray", objectBytes(t, "a"), old)

		list, err := newTestFileListScanner(t, bucket, "index/v0").List(t.Context(), "a", now)
		require.NoError(t, err)
		require.Equal(t, []string{"objects/stray"}, list.Paths)
	})

	t.Run("finds index objects when the index prefix has leading and trailing slashes", func(t *testing.T) {
		bucket := newFileBucket(t)
		bucket.put(t, "index/v0/indexes/tenants/a/bb/compacted", notADataObject, old)
		bucket.put(t, "index/v0/indexes/00/single-a", objectBytes(t, "a"), old)

		list, err := newTestFileListScanner(t, bucket, "/index/v0/").List(t.Context(), "a", now)
		require.NoError(t, err)
		require.Equal(t, []string{
			"index/v0/indexes/00/single-a",
			"index/v0/indexes/tenants/a/bb/compacted",
		}, list.Paths)
	})

	t.Run("lists each root with one recursive listing and the tenant directory with another", func(t *testing.T) {
		files := newFileBucket(t)
		files.put(t, "objects/00/single-a", objectBytes(t, "a"), old)
		files.put(t, "objects/01/single-b", objectBytes(t, "b"), old)
		files.put(t, "objects/tenants/b/aa/compacted", notADataObject, old)
		bucket := &listRecordingBucket{fileBucket: files}

		_, err := newTestFileListScanner(t, bucket, "index/v0").List(t.Context(), "a", now)
		require.NoError(t, err)
		require.Equal(t, []string{
			"objects/tenants/a/",
			"objects/",
			"index/v0/indexes/tenants/a/",
			"index/v0/indexes/",
		}, bucket.dirs)
	})

	t.Run("stops the listing of uncompacted objects at the first key in the tenants directory", func(t *testing.T) {
		files := newFileBucket(t)
		files.put(t, "objects/00/single-a", objectBytes(t, "a"), old)
		files.put(t, "objects/tenants/b/aa/compacted", notADataObject, old)
		files.put(t, "objects/tenants/c/aa/compacted", notADataObject, old)
		files.put(t, "index/v0/indexes/00/single-a", objectBytes(t, "a"), old)
		files.put(t, "index/v0/indexes/tenants/b/bb/compacted", notADataObject, old)
		files.put(t, "index/v0/indexes/tenants/c/bb/compacted", notADataObject, old)
		bucket := &listRecordingBucket{fileBucket: files}

		_, err := newTestFileListScanner(t, bucket, "index/v0").List(t.Context(), "a", now)
		require.NoError(t, err)
		require.Equal(t, []string{"objects/00/single-a", "objects/tenants/b/aa/compacted"}, bucket.names["objects/"])
		require.Equal(t, []string{"index/v0/indexes/00/single-a", "index/v0/indexes/tenants/b/bb/compacted"}, bucket.names["index/v0/indexes/"])
	})

	t.Run("does not count the stop of the listing of uncompacted objects as a failed bucket operation", func(t *testing.T) {
		files := newFileBucket(t)
		files.put(t, "objects/00/single-a", objectBytes(t, "a"), old)
		files.put(t, "objects/tenants/b/aa/compacted", notADataObject, old)
		reg := prometheus.NewRegistry()
		bucket := objstore.WrapWith(files, objstore.BucketMetrics(reg, "test"))

		list, err := newTestFileListScanner(t, bucket, "index/v0").List(t.Context(), "a", now)
		require.NoError(t, err)
		require.Equal(t, []string{"objects/00/single-a"}, list.Paths)
		require.Zero(t, iterFailures(t, reg))
	})

	t.Run("does not list uncompacted objects that sort after the tenants directory", func(t *testing.T) {
		bucket := newFileBucket(t)
		bucket.put(t, "objects/00/single-a", objectBytes(t, "a"), old)
		bucket.put(t, "objects/tenants/b/aa/compacted", notADataObject, old)
		bucket.put(t, "objects/zz/single-a", objectBytes(t, "a"), old)

		list, err := newTestFileListScanner(t, bucket, "index/v0").List(t.Context(), "a", now)
		require.NoError(t, err)
		require.Equal(t, []string{"objects/00/single-a"}, list.Paths)
	})

	t.Run("skips directory placeholder objects in both listings", func(t *testing.T) {
		files := newFileBucket(t)
		files.put(t, "objects/00/single-a", objectBytes(t, "a"), old)
		files.put(t, "objects/tenants/a/aa/compacted", notADataObject, old)
		bucket := &placeholderBucket{fileBucket: files, modified: old, placeholders: map[string]string{
			"objects/":           "objects/00/",
			"objects/tenants/a/": "objects/tenants/a/aa/",
		}}

		list, err := newTestFileListScanner(t, bucket, "index/v0").List(t.Context(), "a", now)
		require.NoError(t, err)
		require.Equal(t, []string{"objects/00/single-a", "objects/tenants/a/aa/compacted"}, list.Paths)
	})

	t.Run("returns an error when the index prefix makes the scan list an object twice", func(t *testing.T) {
		bucket := newFileBucket(t)
		bucket.put(t, "objects/indexes/00/single-a", objectBytes(t, "a"), old)

		_, err := newTestFileListScanner(t, bucket, "objects").List(t.Context(), "a", now)
		require.ErrorContains(t, err, "invalid file list")
	})

	t.Run("returns an error naming the object when an uncompacted object cannot be opened", func(t *testing.T) {
		bucket := newFileBucket(t)
		bucket.put(t, "objects/00/single-a", objectBytes(t, "a"), old)
		bucket.put(t, "objects/01/corrupt", notADataObject, old)

		_, err := newTestFileListScanner(t, bucket, "index/v0").List(t.Context(), "a", now)
		require.ErrorContains(t, err, "objects/01/corrupt")
	})

	t.Run("returns the open error and not a context error when an open fails during the listing", func(t *testing.T) {
		files := newFileBucket(t)
		files.put(t, "objects/01/corrupt", notADataObject, old)
		bucket := &blockingBucket{fileBucket: files, blockAfter: "objects/01/corrupt"}

		_, err := newTestFileListScanner(t, bucket, "index/v0").List(t.Context(), "a", now)
		require.ErrorContains(t, err, "opening object objects/01/corrupt")
		require.NotErrorIs(t, err, context.Canceled)
	})

	t.Run("returns an error naming the object when the bucket does not report its modification time", func(t *testing.T) {
		files := newFileBucket(t)
		files.put(t, "objects/00/single-a", objectBytes(t, "a"), old)
		files.put(t, "objects/01/no-time", objectBytes(t, "a"), old)
		bucket := &noModTimeBucket{fileBucket: files, name: "objects/01/no-time"}

		_, err := newTestFileListScanner(t, bucket, "index/v0").List(t.Context(), "a", now)
		require.ErrorContains(t, err, "modification time of objects/01/no-time")
	})

	t.Run("returns the listing error and not a context error when a listing fails while an open runs", func(t *testing.T) {
		files := newFileBucket(t)
		files.put(t, "objects/00/blocked", objectBytes(t, "a"), old)
		files.put(t, "objects/01/fails", objectBytes(t, "a"), old)
		bucket := &listFailsDuringOpenBucket{
			fileBucket: files,
			blockOpen:  "objects/00/blocked",
			failAt:     "objects/01/fails",
			opening:    make(chan struct{}),
		}

		_, err := newTestFileListScanner(t, bucket, "index/v0").List(t.Context(), "a", now)
		require.ErrorIs(t, err, errListing)
		require.ErrorContains(t, err, "listing uncompacted objects in objects")
		require.NotErrorIs(t, err, context.Canceled)
	})

	for _, tc := range []struct {
		name    string
		noTime  string
		wantErr string
	}{
		{
			name:    "returns an error when the listing of the compacted log objects of the tenant fails",
			noTime:  "objects/tenants/a/aa/compacted",
			wantErr: "listing compacted objects in objects/tenants/a/",
		},
		{
			name:    "returns an error when the listing of the compacted index objects of the tenant fails",
			noTime:  "index/v0/indexes/tenants/a/bb/compacted",
			wantErr: "listing compacted objects in index/v0/indexes/tenants/a/",
		},
		{
			name:    "returns an error when the listing of the uncompacted index objects fails",
			noTime:  "index/v0/indexes/00/single-a",
			wantErr: "listing uncompacted objects in index/v0/indexes",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			files := newFileBucket(t)
			files.put(t, "objects/00/single-a", objectBytes(t, "a"), old)
			files.put(t, "objects/tenants/a/aa/compacted", notADataObject, old)
			files.put(t, "index/v0/indexes/tenants/a/bb/compacted", notADataObject, old)
			files.put(t, "index/v0/indexes/00/single-a", objectBytes(t, "a"), old)
			bucket := &noModTimeBucket{fileBucket: files, name: tc.noTime}

			list, err := newTestFileListScanner(t, bucket, "index/v0").List(t.Context(), "a", now)
			require.ErrorContains(t, err, tc.wantErr)
			require.Empty(t, list.Paths)
		})
	}

	t.Run("returns the context error when the context is cancelled", func(t *testing.T) {
		bucket := newFileBucket(t)
		bucket.put(t, "objects/00/single-a", objectBytes(t, "a"), old)
		ctx, cancel := context.WithCancel(t.Context())
		cancel()

		_, err := newTestFileListScanner(t, bucket, "index/v0").List(ctx, "a", now)
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
			_, err := newTestFileListScanner(t, newFileBucket(t), "index/v0").List(t.Context(), tc.tenant, now)
			require.ErrorContains(t, err, "tenant")
		})
	}
}

func newTestFileListScanner(t *testing.T, bucket objstore.BucketReader, indexPrefix string) *FileListScanner {
	t.Helper()
	scanner, err := NewFileListScanner(bucket, indexPrefix, 2, log.NewNopLogger())
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

// listRecordingBucket records the directory of each listing, and the names
// that each listing gives to its callback.
//
// listRecordingBucket is not safe for concurrent use.
type listRecordingBucket struct {
	fileBucket
	dirs  []string
	names map[string][]string
}

func (b *listRecordingBucket) IterWithAttributes(ctx context.Context, dir string, f func(objstore.IterObjectAttributes) error, options ...objstore.IterOption) error {
	b.dirs = append(b.dirs, dir)
	if b.names == nil {
		b.names = map[string][]string{}
	}
	return b.fileBucket.IterWithAttributes(ctx, dir, func(attrs objstore.IterObjectAttributes) error {
		b.names[dir] = append(b.names[dir], attrs.Name)
		return f(attrs)
	}, options...)
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

// iterFailures returns the number of failed iter operations that the bucket
// metrics in reg counted.
func iterFailures(t *testing.T, reg *prometheus.Registry) float64 {
	t.Helper()
	families, err := reg.Gather()
	require.NoError(t, err)
	for _, family := range families {
		if family.GetName() != "objstore_bucket_operation_failures_total" {
			continue
		}
		for _, m := range family.GetMetric() {
			for _, l := range m.GetLabel() {
				if l.GetName() == "operation" && l.GetValue() == objstore.OpIter {
					return m.GetCounter().GetValue()
				}
			}
		}
	}
	return 0
}

var errListing = errors.New("listing failed")

// listFailsDuringOpenBucket blocks the reads of the object blockOpen until
// the context of the read ends. After the first such read starts, the listing
// fails with errListing when it reaches the object failAt.
type listFailsDuringOpenBucket struct {
	fileBucket
	blockOpen string
	failAt    string
	opening   chan struct{}
	once      sync.Once
}

func (b *listFailsDuringOpenBucket) Get(ctx context.Context, name string) (io.ReadCloser, error) {
	if name == b.blockOpen {
		return nil, b.block(ctx)
	}
	return b.fileBucket.Get(ctx, name)
}

func (b *listFailsDuringOpenBucket) GetRange(ctx context.Context, name string, off, length int64) (io.ReadCloser, error) {
	if name == b.blockOpen {
		return nil, b.block(ctx)
	}
	return b.fileBucket.GetRange(ctx, name, off, length)
}

func (b *listFailsDuringOpenBucket) block(ctx context.Context) error {
	b.once.Do(func() { close(b.opening) })
	<-ctx.Done()
	return ctx.Err()
}

func (b *listFailsDuringOpenBucket) IterWithAttributes(ctx context.Context, dir string, f func(objstore.IterObjectAttributes) error, options ...objstore.IterOption) error {
	return b.fileBucket.IterWithAttributes(ctx, dir, func(attrs objstore.IterObjectAttributes) error {
		if attrs.Name == b.failAt {
			<-b.opening
			return errListing
		}
		return f(attrs)
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
