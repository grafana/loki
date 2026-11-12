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
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/thanos-io/objstore"
	"go.uber.org/atomic"
	"golang.org/x/sync/errgroup"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
)

// MinObjectAge is the minimum age of an object that List returns. Objects are
// immutable, so the age is the time since the last modification.
//
// List excludes newly written objects because a ToC might not reference them
// yet. A later step could then select such an object for deletion by mistake.
const MinObjectAge = time.Hour

// FileListScanner lists the objects of a tenant in a data object bucket.
//
// FileListScanner is safe for concurrent use.
type FileListScanner struct {
	bucket       objstore.BucketReader
	maxOpenFiles int
	logger       log.Logger
}

// NewFileListScanner returns a FileListScanner for bucket. bucket is the root
// of the data object storage, which holds the objects/ directory and the
// index storage at [metastore.IndexStoragePrefix].
//
// maxOpenFiles limits the number of objects that List opens at the same time.
//
// NewFileListScanner returns an error if maxOpenFiles is less than 1, or if
// bucket does not report modification times with [objstore.WithUpdatedAt].
func NewFileListScanner(bucket objstore.BucketReader, maxOpenFiles int, logger log.Logger) (*FileListScanner, error) {
	if maxOpenFiles < 1 {
		return nil, fmt.Errorf("max open files must be at least 1, got %d", maxOpenFiles)
	}
	if !slices.Contains(bucket.SupportedIterOptions(), objstore.UpdatedAt) {
		return nil, errors.New("bucket does not report modification times")
	}
	return &FileListScanner{
		bucket:       bucket,
		maxOpenFiles: maxOpenFiles,
		logger:       logger,
	}, nil
}

// List returns the file list of tenant at now. The list holds the sorted
// paths of the log objects and index objects of tenant. It includes only
// objects last modified at least [MinObjectAge] before now. The paths are
// relative to the bucket of the FileListScanner. List does not include ToCs.
//
// List finds two kinds of objects:
//
//   - Compacted objects. The compactor writes them to
//     objects/tenants/<tenant>/ and index/v0/indexes/tenants/<tenant>/.
//     List includes them without opening them.
//   - Uncompacted objects. The uploader and the index builder write them to
//     objects/<prefix>/ and index/v0/indexes/<prefix>/. The prefix is the
//     first hex characters of a content hash. Their path holds no tenant,
//     so List opens each of them to read its tenants. List includes an
//     uncompacted object only if tenant is its only tenant.
//
// List does not include an uncompacted object that might hold tenant together
// with other tenants. Such an object names tenant with other tenants, or has a
// section with the empty tenant. List logs each such object if the scan
// succeeds.
//
// List returns an error if it cannot open an uncompacted object, or if the
// bucket does not report the modification time of an object.
func (s *FileListScanner) List(ctx context.Context, tenant string, now time.Time) (FileList, error) {
	if err := validateTenant(tenant); err != nil {
		return FileList{}, err
	}

	cutoff := now.Add(-MinObjectAge)
	sc := &scan{
		bucket:       s.bucket,
		maxOpenFiles: s.maxOpenFiles,
		tenant:       tenant,
		cutoff:       cutoff,
	}
	paths, stats, err := sc.run(ctx)
	if err != nil {
		return FileList{}, err
	}

	for _, obj := range stats.MultiTenant {
		// The log lets an operator find data that the list does not cover.
		level.Warn(s.logger).Log("msg", "skipped object with more than one tenant", "tenant", tenant, "path", obj.Path, "tenants", strings.Join(obj.Tenants, ","))
	}
	// A list with no objects can mean a wrong bucket root. A listed count of
	// zero shows this case.
	level.Info(s.logger).Log(
		"msg", "listed objects of tenant",
		"tenant", tenant,
		"cutoff", cutoff.Format(time.RFC3339),
		"listed", stats.Listed,
		"too_young", stats.TooYoung,
		"opened", stats.Opened,
		"multi_tenant", len(stats.MultiTenant),
		"included", len(paths),
	)

	slices.Sort(paths)
	list := FileList{Tenant: tenant, CreatedAt: now, Paths: paths}
	// Write rejects an invalid list, for example a key that is not valid
	// UTF-8. Checking here reports the error at the scan that caused it.
	if err := list.validate(); err != nil {
		return FileList{}, fmt.Errorf("scan produced an invalid file list: %w", err)
	}
	return list, nil
}

const metadataPrefetchBytes = 64 << 10

// scanStats describes the objects that a scan saw.
type scanStats struct {
	// Listed counts the objects that the scan listed. It includes uncompacted
	// objects of any tenant, and objects of any age.
	Listed int64
	// TooYoung counts the listed objects modified after the cutoff.
	TooYoung int64
	// Opened counts the uncompacted objects that the scan opened.
	Opened int64
	// MultiTenant holds the uncompacted objects that might hold the tenant
	// together with other tenants. The scan does not include them.
	MultiTenant []multiTenantObject
}

type multiTenantObject struct {
	Path    string
	Tenants []string
}

// scan finds the objects of one tenant that were last modified at or before
// the cutoff.
//
// Call run at most once. run does not reset the paths and the counters, so a
// second call adds each object again.
type scan struct {
	bucket       objstore.BucketReader
	maxOpenFiles int
	tenant       string
	cutoff       time.Time

	// run adds compacted paths while the open goroutines add uncompacted paths
	// and multiTenant, so resultsMu guards paths and multiTenant.
	resultsMu   sync.Mutex
	paths       []string
	multiTenant []multiTenantObject

	listed   atomic.Int64
	tooYoung atomic.Int64
	opened   atomic.Int64
}

// run returns the unsorted paths of the objects of the tenant, and the stats
// of the scan.
//
// run lists objects/ and index/v0/indexes/, and skips objects modified
// after the cutoff. It adds each compacted object of the tenant. It opens each
// uncompacted object of any tenant to read its tenants.
//
// run opens at most maxOpenFiles uncompacted objects at the same time. If a
// listing or an open fails, run cancels the other work and returns the first
// error.
func (sc *scan) run(ctx context.Context) ([]string, scanStats, error) {
	// A failed listing cancels ctx with the listing error as the cause. A
	// failed open cancels gctx with the open error as the cause. Either way,
	// context.Cause(gctx) is the first error, not a context error that it
	// caused.
	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(sc.maxOpenFiles)

	check := func(p string) {
		g.Go(func() error { return sc.checkUncompactedObject(gctx, p) })
	}
	var listErr error
	for _, root := range []string{"objects", path.Join(metastore.IndexStoragePrefix, "indexes")} {
		tenantDir := tenantsDir(root) + sc.tenant + objstore.DirDelim
		if err := sc.iterFiles(gctx, tenantDir, sc.add); err != nil {
			listErr = fmt.Errorf("listing compacted objects in %s: %w", tenantDir, err)
			break
		}
		if err := sc.iterUncompactedObjects(gctx, root, check); err != nil {
			listErr = fmt.Errorf("listing uncompacted objects in %s: %w", root, err)
			break
		}
	}
	if listErr != nil {
		cancel(listErr)
	}
	if err := g.Wait(); err != nil || listErr != nil {
		return nil, scanStats{}, context.Cause(gctx)
	}

	stats := scanStats{
		Listed:      sc.listed.Load(),
		TooYoung:    sc.tooYoung.Load(),
		Opened:      sc.opened.Load(),
		MultiTenant: sc.multiTenant,
	}
	return sc.paths, stats, nil
}

// tenantsDir returns the directory below root that holds the compacted
// objects of all tenants.
func tenantsDir(root string) string {
	return root + "/tenants/"
}

// iterUncompactedObjects calls f for each object below root, except the
// objects in the tenants directory.
//
// iterUncompactedObjects needs a bucket that lists keys in lexical order, and
// hash directories that sort before the tenants directory. Otherwise it misses
// uncompacted objects without an error. The S3, GCS, Azure and filesystem
// buckets list in lexical order.
//
// It lists root recursively, so that each request returns up to a page of
// objects from many hash directories. It stops at the first key in the
// tenants directory, so that it does not list the compacted objects of every
// tenant. It stops by cancelling the listing context, not by returning an
// error. The bucket metrics count an error as a failed operation.
func (sc *scan) iterUncompactedObjects(ctx context.Context, root string, f func(path string)) error {
	listCtx, stop := context.WithCancel(ctx)
	defer stop()

	skip := tenantsDir(root)
	stopped := false
	err := sc.bucket.IterWithAttributes(listCtx, root+objstore.DirDelim, func(attrs objstore.IterObjectAttributes) error {
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
	return sc.bucket.IterWithAttributes(ctx, dir, func(attrs objstore.IterObjectAttributes) error {
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
	sc.listed.Inc()
	if modified.After(sc.cutoff) {
		sc.tooYoung.Inc()
		return nil
	}
	f(attrs.Name)
	return nil
}

// checkUncompactedObject opens the uncompacted object at p. It adds the
// object if the tenant is its only tenant. It records the object as
// multi-tenant if the object might hold the tenant together with other
// tenants.
func (sc *scan) checkUncompactedObject(ctx context.Context, p string) error {
	obj, err := dataobj.FromBucket(ctx, sc.bucket, p, metadataPrefetchBytes)
	if err != nil {
		return fmt.Errorf("opening object %s: %w", p, err)
	}
	sc.opened.Inc()
	tenants := obj.Tenants()

	switch {
	case len(tenants) == 1 && tenants[0] == sc.tenant:
		sc.add(p)
	case slices.Contains(tenants, sc.tenant) || slices.Contains(tenants, ""):
		// Objects should hold one tenant. A section with the empty tenant holds
		// data of more than one tenant, and that data can include the tenant.
		sc.addMultiTenant(multiTenantObject{Path: p, Tenants: tenants})
	}
	return nil
}

func (sc *scan) add(p string) {
	sc.resultsMu.Lock()
	defer sc.resultsMu.Unlock()
	sc.paths = append(sc.paths, p)
}

func (sc *scan) addMultiTenant(obj multiTenantObject) {
	sc.resultsMu.Lock()
	defer sc.resultsMu.Unlock()
	sc.multiTenant = append(sc.multiTenant, obj)
}
