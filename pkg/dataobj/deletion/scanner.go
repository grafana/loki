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
// bucket of the Scanner.
//
// Compacted objects have the tenant in their path, so List includes them
// without opening them. Ingest objects have no tenant in their path. List
// opens each of them and includes it only if tenant is its only tenant. List
// logs an ingest object that holds tenant together with other tenants, and
// does not include it.
//
// List does not include ToCs. It returns an error if it cannot open an ingest
// object, or if the bucket does not report the modification time of an
// object.
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
		Scanner: s,
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
	return FileList{Tenant: tenant, CreatedAt: now, Paths: sc.paths}, nil
}

// scan holds the state of one List call.
//
// The walk and the open goroutines update paths and the counters, so mu
// guards them.
type scan struct {
	*Scanner
	tenant string
	// cutoff is the latest modification time of an object that the scan
	// includes.
	cutoff time.Time

	mu          sync.Mutex
	paths       []string
	listed      int
	tooYoung    int
	opened      int
	multiTenant int
}

// walk searches objects/ and <indexPrefix>/indexes/. It adds each compacted
// object of the tenant, and calls ingest for each ingest object of any
// tenant. It skips objects modified after the cutoff.
func (sc *scan) walk(ctx context.Context, ingest func(path string)) error {
	for _, root := range []string{"objects", path.Join(sc.indexPrefix, "indexes")} {
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
// the tenants directory. It lists root one level deep first, so that it skips
// the tenants directory without a recursive listing of the compacted objects
// of every tenant.
func (sc *scan) iterIngestObjects(ctx context.Context, root string, f func(path string)) error {
	skip := tenantsDir(root)
	return sc.bucket.IterWithAttributes(ctx, root+objstore.DirDelim, func(attrs objstore.IterObjectAttributes) error {
		switch {
		case attrs.Name == skip:
			return nil
		case strings.HasSuffix(attrs.Name, objstore.DirDelim):
			return sc.iterFiles(ctx, attrs.Name, f)
		default:
			return sc.visit(attrs, f)
		}
	}, objstore.WithUpdatedAt())
}

func (sc *scan) iterFiles(ctx context.Context, dir string, f func(path string)) error {
	return sc.bucket.IterWithAttributes(ctx, dir, func(attrs objstore.IterObjectAttributes) error {
		return sc.visit(attrs, f)
	}, objstore.WithRecursiveIter(), objstore.WithUpdatedAt())
}

// visit calls f for the object of attrs if it was last modified at or before
// the cutoff.
func (sc *scan) visit(attrs objstore.IterObjectAttributes, f func(path string)) error {
	modified, ok := attrs.LastModified()
	if !ok {
		return fmt.Errorf("bucket did not report the modification time of %s", attrs.Name)
	}

	sc.mu.Lock()
	sc.listed++
	tooYoung := modified.After(sc.cutoff)
	if tooYoung {
		sc.tooYoung++
	}
	sc.mu.Unlock()

	if !tooYoung {
		f(attrs.Name)
	}
	return nil
}

// checkIngestObject opens the ingest object at p and adds it if the tenant is
// its only tenant.
func (sc *scan) checkIngestObject(ctx context.Context, p string) error {
	obj, err := dataobj.FromBucket(ctx, sc.bucket, p, 0)
	if err != nil {
		return fmt.Errorf("opening object %s: %w", p, err)
	}
	tenants := obj.Tenants()

	sc.mu.Lock()
	sc.opened++
	sc.mu.Unlock()

	switch {
	case len(tenants) == 1 && tenants[0] == sc.tenant:
		sc.add(p)
	case slices.Contains(tenants, sc.tenant) || slices.Contains(tenants, ""):
		// Every object is expected to hold a single tenant. A section with the
		// empty tenant holds data of more than one tenant. Logging the object
		// lets an operator find data that the list does not cover.
		level.Warn(sc.logger).Log("msg", "skipped object with more than one tenant", "tenant", sc.tenant, "path", p, "tenants", strings.Join(tenants, ","))
		sc.mu.Lock()
		sc.multiTenant++
		sc.mu.Unlock()
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
	defer sc.mu.Unlock()
	level.Info(sc.logger).Log(
		"msg", "listed objects of tenant",
		"tenant", sc.tenant,
		"cutoff", sc.cutoff.Format(time.RFC3339),
		"listed", sc.listed,
		"too_young", sc.tooYoung,
		"opened", sc.opened,
		"multi_tenant", sc.multiTenant,
		"included", len(sc.paths),
	)
}
