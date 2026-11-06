// Package deletion finds and records the data objects that hold data of a
// tenant, so that a later step can delete the tenant's data.
package deletion

import (
	"context"
	"errors"
	"fmt"
	"path"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/thanos-io/objstore"
	"golang.org/x/sync/errgroup"

	"github.com/grafana/loki/v3/pkg/dataobj"
)

// MinObjectAge is the minimum age of an object that List returns. Objects are
// immutable, so the age is the time since the last modification.
const MinObjectAge = time.Hour

// Scanner lists the objects of a tenant in a data object bucket.
//
// Scanner is safe for concurrent use.
type Scanner struct {
	bucket       objstore.BucketReader
	indexPrefix  string
	maxOpenFiles int
	logger       log.Logger
}

// NewScanner returns a Scanner for bucket. bucket is the root of the data
// object storage, which holds the objects/ directory. indexPrefix is the path
// of the index storage relative to bucket, for example "index/v0".
//
// maxOpenFiles limits the number of objects that List opens at the same time.
//
// NewScanner returns an error if maxOpenFiles is less than 1, or if bucket
// does not report modification times with [objstore.WithUpdatedAt].
func NewScanner(bucket objstore.BucketReader, indexPrefix string, maxOpenFiles int, logger log.Logger) (*Scanner, error) {
	if maxOpenFiles < 1 {
		return nil, fmt.Errorf("max open files must be at least 1, got %d", maxOpenFiles)
	}
	if !slices.Contains(bucket.SupportedIterOptions(), objstore.UpdatedAt) {
		return nil, errors.New("bucket does not report modification times")
	}
	return &Scanner{
		bucket:       bucket,
		indexPrefix:  strings.Trim(indexPrefix, objstore.DirDelim),
		maxOpenFiles: maxOpenFiles,
		logger:       logger,
	}, nil
}

// List returns the file list of tenant at now. The list holds the sorted
// paths of all log objects and index objects of tenant that were last
// modified at least [MinObjectAge] before now. The paths are relative to the
// bucket of the Scanner. List does not include ToCs.
//
// List finds two kinds of objects:
//
//   - Compacted objects. The compactor writes them to
//     objects/tenants/<tenant>/ and <indexPrefix>/indexes/tenants/<tenant>/.
//     List includes them without opening them.
//   - Ingest objects. The uploader and the index builder write them to
//     objects/<prefix>/ and <indexPrefix>/indexes/<prefix>/, where prefix is
//     the first hex characters of a content hash. Their path holds no tenant,
//     so List opens each of them to read its tenants. List includes an
//     ingest object only if tenant is its only tenant. It logs an ingest
//     object that holds tenant together with other tenants, and does not
//     include it.
//
// List returns an error if it cannot open an ingest object, or if the bucket
// does not report the modification time of an object.
func (s *Scanner) List(ctx context.Context, tenant string, now time.Time) (FileList, error) {
	if err := validateTenant(tenant); err != nil {
		return FileList{}, err
	}

	// A failed walk cancels ctx with the walk error as the cause, and a failed
	// open cancels gctx with the open error as the cause. Either way,
	// context.Cause(gctx) is the first error, not a context error that it
	// caused.
	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(s.maxOpenFiles)

	sc := &scan{
		scanner: s,
		tenant:  tenant,
		cutoff:  now.Add(-MinObjectAge),
		paths:   []string{},
	}
	walkErr := sc.walk(gctx, func(p string) {
		g.Go(func() error { return sc.checkIngestObject(gctx, p) })
	})
	if walkErr != nil {
		cancel(walkErr)
	}
	if err := g.Wait(); err != nil || walkErr != nil {
		return FileList{}, context.Cause(gctx)
	}

	sc.logSummary()
	slices.Sort(sc.paths)
	list := FileList{Tenant: tenant, CreatedAt: now, Paths: sc.paths}
	// A path can appear twice if the index prefix is inside objects/.
	if err := list.validate(); err != nil {
		return FileList{}, fmt.Errorf("scan produced an invalid file list: %w", err)
	}
	return list, nil
}

const metadataPrefetchBytes = 64 << 10

// scan holds the state of one List call.
type scan struct {
	scanner *Scanner
	tenant  string
	// cutoff is the latest modification time of an object that the scan
	// includes.
	cutoff time.Time

	// The walk and the open goroutines add paths, so mu guards paths.
	mu    sync.Mutex
	paths []string

	listed      atomic.Int64
	tooYoung    atomic.Int64
	opened      atomic.Int64
	multiTenant atomic.Int64
}

// walk searches objects/ and <indexPrefix>/indexes/. It adds each compacted
// object of the tenant, and calls ingest for each ingest object of any
// tenant. It skips objects modified after the cutoff.
func (sc *scan) walk(ctx context.Context, ingest func(path string)) error {
	for _, root := range []string{"objects", path.Join(sc.scanner.indexPrefix, "indexes")} {
		tenantDir := tenantsDir(root) + sc.tenant + objstore.DirDelim
		if err := sc.iterFiles(ctx, tenantDir, sc.add); err != nil {
			return fmt.Errorf("listing compacted objects in %s: %w", tenantDir, err)
		}
		if err := sc.iterIngestObjects(ctx, root, ingest); err != nil {
			return fmt.Errorf("listing ingest objects in %s: %w", root, err)
		}
	}
	return nil
}

// tenantsDir returns the directory below root that holds the compacted
// objects of all tenants.
func tenantsDir(root string) string {
	return root + "/tenants/"
}

// iterIngestObjects calls f for each object below root, except the objects in
// the tenants directory.
//
// iterIngestObjects needs a bucket that lists keys in lexical order, and hash
// directories that sort before the tenants directory. Otherwise it misses
// ingest objects without an error. The S3, GCS, Azure and filesystem buckets
// list in lexical order.
//
// It lists root recursively, so that each request returns up to a page of
// objects from many hash directories. It stops at the first key in the
// tenants directory, so that it does not list the compacted objects of every
// tenant. It stops by cancelling the listing context and not by returning an
// error, because the bucket metrics count an error as a failed operation.
func (sc *scan) iterIngestObjects(ctx context.Context, root string, f func(path string)) error {
	listCtx, stop := context.WithCancel(ctx)
	defer stop()

	skip := tenantsDir(root)
	stopped := false
	err := sc.scanner.bucket.IterWithAttributes(listCtx, root+objstore.DirDelim, func(attrs objstore.IterObjectAttributes) error {
		if stopped || strings.HasPrefix(attrs.Name, skip) {
			stopped = true
			stop()
			return listCtx.Err()
		}
		return sc.visit(attrs, f)
	}, objstore.WithRecursiveIter(), objstore.WithUpdatedAt())
	if stopped && ctx.Err() == nil {
		return nil
	}
	return err
}

func (sc *scan) iterFiles(ctx context.Context, dir string, f func(path string)) error {
	return sc.scanner.bucket.IterWithAttributes(ctx, dir, func(attrs objstore.IterObjectAttributes) error {
		return sc.visit(attrs, f)
	}, objstore.WithRecursiveIter(), objstore.WithUpdatedAt())
}

// visit calls f for the object of attrs if it was last modified at or before
// the cutoff. It skips keys that end in a slash. Some buckets hold such empty
// placeholder objects for directories, and they are not data objects.
func (sc *scan) visit(attrs objstore.IterObjectAttributes, f func(path string)) error {
	if strings.HasSuffix(attrs.Name, objstore.DirDelim) {
		return nil
	}
	modified, ok := attrs.LastModified()
	if !ok {
		return fmt.Errorf("bucket did not report the modification time of %s", attrs.Name)
	}
	sc.listed.Add(1)
	if modified.After(sc.cutoff) {
		sc.tooYoung.Add(1)
		return nil
	}
	f(attrs.Name)
	return nil
}

// checkIngestObject opens the ingest object at p and adds it if the tenant is
// its only tenant.
func (sc *scan) checkIngestObject(ctx context.Context, p string) error {
	obj, err := dataobj.FromBucket(ctx, sc.scanner.bucket, p, metadataPrefetchBytes)
	if err != nil {
		return fmt.Errorf("opening object %s: %w", p, err)
	}
	sc.opened.Add(1)
	tenants := obj.Tenants()

	switch {
	case len(tenants) == 1 && tenants[0] == sc.tenant:
		sc.add(p)
	case slices.Contains(tenants, sc.tenant) || slices.Contains(tenants, ""):
		// Every object is expected to hold a single tenant. A section with the
		// empty tenant holds data of more than one tenant. Logging the object
		// lets an operator find data that the list does not cover.
		level.Warn(sc.scanner.logger).Log("msg", "skipped object with more than one tenant", "tenant", sc.tenant, "path", p, "tenants", strings.Join(tenants, ","))
		sc.multiTenant.Add(1)
	}
	return nil
}

func (sc *scan) add(p string) {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	sc.paths = append(sc.paths, p)
}

// logSummary logs the counters of the scan. A list with no objects can mean a
// wrong bucket root or index prefix, and the listed count shows that.
func (sc *scan) logSummary() {
	sc.mu.Lock()
	included := len(sc.paths)
	sc.mu.Unlock()
	level.Info(sc.scanner.logger).Log(
		"msg", "listed objects of tenant",
		"tenant", sc.tenant,
		"cutoff", sc.cutoff.Format(time.RFC3339),
		"listed", sc.listed.Load(),
		"too_young", sc.tooYoung.Load(),
		"opened", sc.opened.Load(),
		"multi_tenant", sc.multiTenant.Load(),
		"included", included,
	)
}
