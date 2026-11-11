package metastore

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"path"
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
	"github.com/grafana/loki/v3/pkg/dataobj/sections/watermarks"
)

// reachableIndexPrefetchBytes is the size of the first request that
// ReachableObjects makes for an index object. The request usually holds the
// whole metadata, so one request opens the object.
const reachableIndexPrefetchBytes = 64 << 10

// ListTableOfContentsWindows returns the windows that have a ToC directory in
// bucket, sorted. bucket is the index storage, as [NewIndexBucket] returns it.
//
// It skips the legacy shared ToC files at tocs/<window>.toc. No reader uses
// them, because each tenant has its own ToC for each window.
//
// It returns an error for a directory name that is not the name of a window,
// in the form that [TableOfContentsWindowPrefix] writes. A caller looks up the
// ToCs of a window under that form, so it would skip the ToCs of a directory
// with another name, and miss the objects that they reference.
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
		window = window.UTC()
		if want := TableOfContentsWindowPrefix(window); name != want {
			return fmt.Errorf("ToC directory %s is not a window: the window %s has the directory %s", name, window.Format(time.RFC3339), want)
		}
		windows = append(windows, window)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("listing ToC windows: %w", err)
	}
	slices.SortFunc(windows, time.Time.Compare)
	return windows, nil
}

// ReachableObjects returns the sorted paths of the objects that the ToCs of
// tenant make reachable. The paths are relative to bucket, which is the root
// of the data object storage. The objects are:
//
//   - The index objects that a ToC of tenant points to, in any window.
//   - The log objects that those index objects reference.
//
// The result can hold objects of other tenants that share an index object.
// It is a superset of the reachable objects of tenant, so compare it only
// against the objects of tenant.
//
// No query can read an object that ReachableObjects does not return. That
// does not make the object garbage: a new log object stays unreachable until
// an index object references it.
//
// A missing ToC is not an error. ReachableObjects returns an error, and no
// paths, if it cannot read a ToC or an index object. It also returns an error
// for any content that it cannot interpret, for example a section of an
// unknown type, a ToC of another tenant, or a path that is not clean and
// relative. A partial result would make reachable objects look unreachable.
//
// maxOpenFiles limits the index objects that ReachableObjects opens at the
// same time. It must be at least 1.
//
// The result is not a consistent snapshot. ReachableObjects reads each ToC at
// a different time, and index builders and compaction change the ToCs during
// the call. An object can become reachable or unreachable during or after the
// call.
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

	walk := &reachabilityWalk{
		indexBucket: indexBucket,
		tenant:      tenantID,
		reachable:   make(map[string]struct{}),
	}
	return walk.run(ctx, windows, maxOpenFiles)
}

// reachabilityWalk holds the state of one ReachableObjects call. The index
// goroutines add paths, so mu guards reachable. Call run at most once.
type reachabilityWalk struct {
	indexBucket objstore.BucketReader
	tenant      string

	mu        sync.Mutex
	reachable map[string]struct{}
}

// run reads the ToC of the tenant in each window, and the index objects that
// it points to. It opens each index object once, and at most maxOpenFiles of
// them at the same time. It returns the sorted reachable paths.
func (w *reachabilityWalk) run(ctx context.Context, windows []time.Time, maxOpenFiles int) ([]string, error) {
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(maxOpenFiles)
	opened := make(map[string]struct{})
	for _, window := range windows {
		indexPaths, err := w.tocIndexPaths(gctx, window)
		if err != nil {
			// A failed index read cancels gctx, so the ToC read can fail with a
			// context error. Return the error of the index read first, because
			// it is the cause.
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
			w.addAll([]string{IndexStoragePrefix + "/" + indexPath})
			g.Go(func() error { return w.addReferencedObjects(gctx, indexPath) })
		}
	}
	if err := g.Wait(); err != nil {
		return nil, err
	}

	paths := make([]string, 0, len(w.reachable))
	for p := range w.reachable {
		paths = append(paths, p)
	}
	slices.Sort(paths)
	return paths, nil
}

func (w *reachabilityWalk) addAll(paths []string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, p := range paths {
		w.reachable[p] = struct{}{}
	}
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

	// forEachTocPointer reads only the index pointers sections. Check that
	// the ToC holds no other section that could point to index objects.
	for i, section := range toc.Sections() {
		if !indexpointers.CheckSection(section) && !watermarks.CheckSection(section) {
			return nil, fmt.Errorf("ToC %s: section %d has unknown type %s, so the index objects that it points to are unknown", tocPath, i, section.Type)
		}
	}

	var paths []string
	err = forEachTocPointer(ctx, toc, func(pointer indexpointers.IndexPointer) error {
		if err := checkObjectPath(pointer.Path); err != nil {
			return err
		}
		// Every index object is below indexes/. A pointer elsewhere, for
		// example one that holds IndexStoragePrefix, would get the prefix
		// twice and match no object key.
		if !strings.HasPrefix(pointer.Path, "indexes/") {
			return fmt.Errorf("index pointer %q is not in indexes/", pointer.Path)
		}
		paths = append(paths, strings.Clone(pointer.Path))
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("reading index pointers of ToC %s: %w", tocPath, err)
	}
	return paths, nil
}

// addReferencedObjects adds the log objects that the index object at
// indexPath references.
//
// It reads the object paths of both the postings and the stats sections,
// because the format does not promise that one section type lists every log
// object. For example, a legacy index, or a merged index without a stream
// layout, holds postings but no stats.
//
// It reads the sections of every tenant, and sections without a tenant. An
// index object can hold sections of several tenants, and a legacy section
// can have no tenant. A filter by tenant could drop paths that the tenant can
// read. The paths of other tenants only make the result larger.
//
// It returns an error for a section that it does not know. A new section type
// could reference log objects, and skipping it would make them look
// unreachable.
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
		case watermarks.CheckSection(section):
			continue
		default:
			return fmt.Errorf("index object %s: section %d has unknown type %s, so the log objects that it references are unknown", indexPath, i, section.Type)
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
	column, err := objectPathColumn(sec.Columns(), func(c *postings.Column) bool { return c.Type == postings.ColumnTypeObjectPath })
	if err != nil {
		return err
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
	column, err := objectPathColumn(sec.Columns(), func(c *stats.Column) bool { return c.Type == stats.ColumnTypeObjectPath })
	if err != nil {
		return err
	}
	reader := stats.NewReader(stats.ReaderOptions{Columns: []*stats.Column{column}})
	defer reader.Close()
	if err := reader.Open(ctx); err != nil {
		return err
	}
	return w.addObjectPaths(ctx, reader.Read)
}

// objectPathColumn returns the one column of columns that isObjectPath
// accepts. It returns an error if there is no such column, or more than one,
// because reading only one of several columns could miss object paths.
func objectPathColumn[C any](columns []C, isObjectPath func(C) bool) (C, error) {
	var (
		found C
		n     int
	)
	for _, c := range columns {
		if isObjectPath(c) {
			found = c
			n++
		}
	}
	if n != 1 {
		var zero C
		return zero, fmt.Errorf("section has %d object_path columns, want 1", n)
	}
	return found, nil
}

// addObjectPaths adds the paths of the single string column that read
// returns, until read returns io.EOF.
func (w *reachabilityWalk) addObjectPaths(ctx context.Context, read func(context.Context, int) (arrow.RecordBatch, error)) error {
	const batchSize = 1024
	for {
		rec, readErr := read(ctx, batchSize)
		if readErr != nil && !errors.Is(readErr, io.EOF) {
			if rec != nil {
				rec.Release()
			}
			return readErr
		}
		if rec != nil {
			if err := w.addRecordPaths(rec); err != nil {
				return err
			}
		}
		if errors.Is(readErr, io.EOF) {
			return nil
		}
	}
}

// addRecordPaths adds the paths of the object_path column of rec, and
// releases rec.
//
// It returns an error for a null or empty value, and for a path that is not
// clean and relative. Writers never store either, so such a value means a
// corrupt section or a reader bug. Skipping it could drop a reachable object.
func (w *reachabilityWalk) addRecordPaths(rec arrow.RecordBatch) error {
	defer rec.Release()
	column, ok := rec.Column(0).(*array.String)
	if !ok {
		return fmt.Errorf("object_path column has type %s, want string", rec.Column(0).DataType())
	}
	paths := make([]string, 0, column.Len())
	for i := range column.Len() {
		if column.IsNull(i) || column.Value(i) == "" {
			return errors.New("object_path column holds a null or empty value")
		}
		if err := checkObjectPath(column.Value(i)); err != nil {
			return err
		}
		// The values point into the buffers of rec. Clone them so that the
		// result does not keep every batch in memory.
		paths = append(paths, strings.Clone(column.Value(i)))
	}
	w.addAll(paths)
	return nil
}

// checkObjectPath returns an error unless p is a clean, relative path. Callers
// of ReachableObjects compare its paths with object keys as strings, so a path
// such as "/objects/x" or "objects//x" would never match the key of its
// object.
func checkObjectPath(p string) error {
	if p != path.Clean(p) || strings.HasPrefix(p, "/") || p == "." || p == ".." || strings.HasPrefix(p, "../") {
		return fmt.Errorf("object path %q is not a clean relative path", p)
	}
	return nil
}
