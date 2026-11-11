package metastore

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/grafana/dskit/tenant"
	"github.com/thanos-io/objstore"
	"golang.org/x/sync/errgroup"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/indexpointers"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/postings"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/stats"
)

// reachableIndexPrefetchBytes is the number of bytes that ReachableObjects
// reads from the start of an index object to read its metadata.
const reachableIndexPrefetchBytes = 64 << 10

// ListTableOfContentsWindows returns the windows that have a ToC directory in
// bucket, sorted. bucket is the index storage, as [NewIndexBucket] returns it.
//
// It skips the legacy shared ToC files at tocs/<window>.toc. Nothing reads
// them since ToCs are per tenant. It returns an error for a directory name
// that is not a window, because a caller that skips a window can miss the
// objects that its ToCs reference.
func ListTableOfContentsWindows(ctx context.Context, bucket objstore.BucketReader) ([]time.Time, error) {
	var windows []time.Time
	err := bucket.Iter(ctx, TocPrefix, func(name string) error {
		dir, ok := strings.CutSuffix(strings.TrimPrefix(name, TocPrefix), objstore.DirDelim)
		if !ok {
			return nil
		}
		window, err := time.Parse(time.RFC3339, strings.ReplaceAll(dir, "_", ":"))
		if err != nil {
			return fmt.Errorf("ToC directory %s is not a window: %w", name, err)
		}
		windows = append(windows, window.UTC())
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("listing ToC windows: %w", err)
	}
	slices.SortFunc(windows, time.Time.Compare)
	return windows, nil
}

// ReachableObjects returns the sorted paths of the objects of tenant that the
// metastore can reach. The paths are relative to bucket, which is the root of
// the data object storage. The objects are:
//
//   - The index objects that the ToC of tenant points to, in any window.
//   - The log objects that those index objects reference.
//
// An object that ReachableObjects does not return is garbage: no query can
// read it.
//
// ReachableObjects returns an error, and no paths, if it cannot read a ToC or
// an index object. A partial result would make reachable objects look like
// garbage. maxOpenFiles limits the index objects that it opens at the same
// time, and must be at least 1.
//
// The result is a snapshot. Compaction keeps changing the ToCs, so an object
// can become reachable or unreachable right after the call.
func ReachableObjects(ctx context.Context, bucket objstore.Bucket, tenantID string, maxOpenFiles int) ([]string, error) {
	if tenantID == "" {
		return nil, errors.New("tenant must not be empty")
	}
	if err := tenant.ValidTenantID(tenantID); err != nil {
		return nil, fmt.Errorf("invalid tenant %q: %w", tenantID, err)
	}
	if maxOpenFiles < 1 {
		return nil, fmt.Errorf("max open files must be at least 1, got %d", maxOpenFiles)
	}
	indexBucket := NewIndexBucket(bucket)

	windows, err := ListTableOfContentsWindows(ctx, indexBucket)
	if err != nil {
		return nil, err
	}

	walk := &reachabilityWalk{indexBucket: indexBucket, tenant: tenantID, reachable: make(map[string]struct{})}
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(maxOpenFiles)
	opened := make(map[string]struct{})
	for _, window := range windows {
		indexPaths, err := walk.tocIndexPaths(gctx, window)
		if err != nil {
			// A failed index read cancels gctx, which then fails the ToC read.
			// Return the error of the index read, because it is the cause.
			if waitErr := g.Wait(); waitErr != nil {
				return nil, waitErr
			}
			return nil, err
		}
		for _, indexPath := range indexPaths {
			if _, ok := opened[indexPath]; ok {
				continue
			}
			opened[indexPath] = struct{}{}
			walk.add(IndexStoragePrefix + "/" + indexPath)
			g.Go(func() error { return walk.addReferencedObjects(gctx, indexPath) })
		}
	}
	if err := g.Wait(); err != nil {
		return nil, err
	}

	paths := make([]string, 0, len(walk.reachable))
	for path := range walk.reachable {
		paths = append(paths, path)
	}
	slices.Sort(paths)
	return paths, nil
}

// reachabilityWalk holds the state of one ReachableObjects call. The index
// goroutines add paths, so mu guards reachable.
type reachabilityWalk struct {
	indexBucket objstore.BucketReader
	tenant      string

	mu        sync.Mutex
	reachable map[string]struct{}
}

func (w *reachabilityWalk) add(path string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.reachable[path] = struct{}{}
}

// tocIndexPaths returns the index object paths in the ToC of the tenant for
// window. The paths are relative to the index storage. A missing ToC has no
// paths.
//
// It reads the whole ToC with one request. A ToC is rewritten in place, so
// reads of several ranges could mix two versions.
func (w *reachabilityWalk) tocIndexPaths(ctx context.Context, window time.Time) ([]string, error) {
	tocPath := TableOfContentsPath(w.tenant, window)
	r, err := w.indexBucket.Get(ctx, tocPath)
	if err != nil {
		if w.indexBucket.IsObjNotFoundErr(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading ToC %s: %w", tocPath, err)
	}
	defer r.Close()
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("reading ToC %s: %w", tocPath, err)
	}
	toc, err := dataobj.FromReaderAt(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("decoding ToC %s: %w", tocPath, err)
	}
	if err := checkTenant(toc, w.tenant); err != nil {
		return nil, fmt.Errorf("ToC %s: %w", tocPath, err)
	}

	var paths []string
	err = forEachTocPointer(ctx, toc, func(pointer indexpointers.IndexPointer) error {
		paths = append(paths, strings.Clone(pointer.Path))
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("reading index pointers of ToC %s: %w", tocPath, err)
	}
	return paths, nil
}

// addReferencedObjects adds the log objects that the index object at
// indexPath references. It reads the object paths of the postings sections
// and the stats sections. A merged index can hold postings without stats, so
// neither section alone lists every log object.
//
// It reads the sections of every tenant. A path of another tenant is not in
// the objects of tenant, so it changes nothing, and reading it is the safe
// choice for a section of mixed tenants.
func (w *reachabilityWalk) addReferencedObjects(ctx context.Context, indexPath string) error {
	obj, err := dataobj.FromBucket(ctx, w.indexBucket, indexPath, reachableIndexPrefetchBytes)
	if err != nil {
		return fmt.Errorf("opening index object %s: %w", indexPath, err)
	}

	for i, section := range obj.Sections() {
		var err error
		switch {
		case postings.CheckSection(section):
			err = w.addPostingsObjectPaths(ctx, section)
		case stats.CheckSection(section):
			err = w.addStatsObjectPaths(ctx, section)
		default:
			continue
		}
		if err != nil {
			return fmt.Errorf("reading object paths of section %d of index object %s: %w", i, indexPath, err)
		}
	}
	return nil
}

func (w *reachabilityWalk) addPostingsObjectPaths(ctx context.Context, section *dataobj.Section) error {
	sec, err := postings.Open(ctx, section)
	if err != nil {
		return err
	}
	var column *postings.Column
	for _, c := range sec.Columns() {
		if c.Type == postings.ColumnTypeObjectPath {
			column = c
		}
	}
	if column == nil {
		return errors.New("postings section has no object_path column")
	}
	reader := postings.NewReader(postings.ReaderOptions{Columns: []*postings.Column{column}})
	defer reader.Close()
	if err := reader.Open(ctx); err != nil {
		return err
	}
	return w.addObjectPaths(ctx, reader.Read)
}

func (w *reachabilityWalk) addStatsObjectPaths(ctx context.Context, section *dataobj.Section) error {
	sec, err := stats.Open(ctx, section)
	if err != nil {
		return err
	}
	var column *stats.Column
	for _, c := range sec.Columns() {
		if c.Type == stats.ColumnTypeObjectPath {
			column = c
		}
	}
	if column == nil {
		return errors.New("stats section has no object_path column")
	}
	reader := stats.NewReader(stats.ReaderOptions{Columns: []*stats.Column{column}})
	defer reader.Close()
	if err := reader.Open(ctx); err != nil {
		return err
	}
	return w.addObjectPaths(ctx, reader.Read)
}

// addObjectPaths adds the paths of the single string column that read
// returns, until read returns io.EOF.
func (w *reachabilityWalk) addObjectPaths(ctx context.Context, read func(context.Context, int) (arrow.RecordBatch, error)) error {
	const batchSize = 1024
	var batch []string
	for {
		rec, readErr := read(ctx, batchSize)
		if readErr != nil && !errors.Is(readErr, io.EOF) {
			if rec != nil {
				rec.Release()
			}
			return readErr
		}
		if rec != nil {
			column, ok := rec.Column(0).(*array.String)
			if !ok {
				rec.Release()
				return fmt.Errorf("object_path column has type %s, want string", rec.Column(0).DataType())
			}
			batch = batch[:0]
			for i := range column.Len() {
				if column.IsNull(i) || column.Value(i) == "" {
					continue
				}
				// The values point into the buffers of rec. Clone them so that
				// the result does not keep every batch in memory.
				batch = append(batch, strings.Clone(column.Value(i)))
			}
			rec.Release()
			w.mu.Lock()
			for _, path := range batch {
				w.reachable[path] = struct{}{}
			}
			w.mu.Unlock()
		}
		if errors.Is(readErr, io.EOF) {
			return nil
		}
	}
}
