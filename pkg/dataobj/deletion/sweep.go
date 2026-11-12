package deletion

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"path"
	"strings"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/backoff"
	"github.com/thanos-io/objstore"

	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
)

// MinSweepInterval is the minimum age of a stored file list that makes a
// sweep scan again.
//
// The planner starts a sweep for each tenant every hour, and each start is a
// little late. A minimum of 23 hours keeps the daily scan of a tenant at the
// same hour. A minimum of 24 hours would move it one hour later each day.
const MinSweepInterval = 23 * time.Hour

// SweepOutcome reports what a sweep did.
type SweepOutcome string

const (
	// SweepSkipped means that the stored file list was less than
	// MinSweepInterval old, so the sweep did not scan.
	SweepSkipped SweepOutcome = "skipped"
	// SweepScanned means that the sweep scanned and stored a new file list.
	SweepScanned SweepOutcome = "scanned"
	// SweepSuperseded means that the sweep scanned, but did not store its list
	// or log deletion candidates. The stored list was not older than the new
	// list, or it changed or was removed during the sweep.
	SweepSuperseded SweepOutcome = "superseded"
)

// errStoredListChanged means that the stored file list changed after the
// sweep read its creation time.
var errStoredListChanged = errors.New("stored file list changed during the sweep")

// sweepMaxOpenFiles limits the objects that one sweep opens at the same time.
// A worker can run several sweeps at once, so the limit is per sweep.
const sweepMaxOpenFiles = 16

var sweepWriterBackoff = backoff.Config{
	MinBackoff: 100 * time.Millisecond,
	MaxBackoff: 5 * time.Second,
	MaxRetries: 5,
}

// Sweeper finds the objects of a tenant that no ToC can reach. It stores them
// in the file list of the tenant, at most once per MinSweepInterval. It logs
// a summary and examples of the deletion candidates: the objects that were
// also in the previous file list. It deletes nothing.
//
// Sweeper is safe for concurrent use.
type Sweeper struct {
	bucket  objstore.Bucket
	scanner *FileListScanner
	writer  *FileListWriter
	logger  log.Logger
}

// NewSweeper returns a Sweeper for bucket. bucket is the root of the data
// object storage. It must report modification times and support conditional
// writes. See [NewFileListScanner] and [NewFileListWriter].
func NewSweeper(bucket objstore.Bucket, logger log.Logger) (*Sweeper, error) {
	scanner, err := NewFileListScanner(bucket, sweepMaxOpenFiles, logger)
	if err != nil {
		return nil, err
	}
	return &Sweeper{
		bucket:  bucket,
		scanner: scanner,
		writer:  NewFileListWriter(bucket, sweepWriterBackoff, logger),
		logger:  logger,
	}, nil
}

// Sweep sweeps tenant at now:
//
//  1. It reads the header of the stored file list. If the stored list is less
//     than MinSweepInterval old, Sweep returns SweepSkipped.
//  2. It scans the objects of tenant, and then finds the objects that a ToC
//     of tenant can reach. The scanned objects that no ToC can reach form
//     the new list.
//  3. It compares the new list with the stored list, if the stored list also
//     holds unreachable objects. The objects in both are the deletion
//     candidates.
//  4. It stores the new list.
//  5. It logs a summary and examples of the deletion candidates. It does not
//     delete them.
//
// Sweep reads the ToCs after the scan. If it read them first, an object that
// a writer indexed during the scan would look unreachable.
//
// The scan skips objects younger than MinObjectAge. So the new list holds no
// object that a writer stores during the sweep.
//
// One unreachable result is not enough to delete an object. The ToCs change
// during the sweep. Also, a query can read an index object for some time
// after a ToC stops listing it. An object is a candidate only if two sweeps,
// at least MinSweepInterval apart, find it unreachable.
//
// A stored list in the older format holds every object of tenant, not the
// unreachable ones. Sweep stores the new list, but finds no candidates from
// such a list. Comparing it would make every object that is unreachable now
// a candidate after one sweep.
//
// Sweep stores the new list before it logs the candidates, so it logs
// candidates only for a list that it stored.
//
// Sweep returns SweepSuperseded, and logs no candidates, if the stored list
// is not older than the new list when Sweep writes. It also returns
// SweepSuperseded if the stored list held unreachable objects and changed
// during the sweep.
//
// Two overlapping sweeps can still both store a list and both log
// candidates. Both find their candidates from the same stored list.
//
// Sweep returns an error, and does not scan, if it cannot read the stored
// list. That includes a list that wraps ErrMalformedFileList. Sweep does not
// replace such a list, so an operator must repair or delete it.
func (s *Sweeper) Sweep(ctx context.Context, tenant string, now time.Time) (SweepOutcome, error) {
	logger := log.With(s.logger, "tenant", tenant)
	level.Info(logger).Log("msg", "deletion sweep running")
	previous, err := s.readStoredHeader(ctx, tenant)
	if err != nil {
		return "", err
	}
	if previous != nil && now.Sub(previous.CreatedAt) < MinSweepInterval {
		level.Debug(logger).Log("msg", "skipped deletion sweep: stored file list is recent", "created_at", previous.CreatedAt.Format(time.RFC3339))
		return SweepSkipped, nil
	}

	scanned, err := s.scanner.List(ctx, tenant, now)
	if err != nil {
		return "", fmt.Errorf("scanning objects of tenant %q: %w", tenant, err)
	}
	reachable, err := metastore.ReachableObjects(ctx, s.bucket, tenant, sweepMaxOpenFiles)
	if err != nil {
		return "", fmt.Errorf("finding reachable objects of tenant %q: %w", tenant, err)
	}
	list := FileList{Tenant: tenant, CreatedAt: now, Paths: unreachable(scanned.Paths, reachable)}

	// cands is nil if the sweep did not compare with a stored list. A stored
	// list with no shared objects gives empty candidates, which a dry run
	// logs.
	var cands *candidates
	switch {
	case previous == nil:
		level.Info(logger).Log("msg", "deletion sweep found no candidates: no file list is stored, so this list is the first")
	case !previous.holdsUnreachableObjects():
		level.Info(logger).Log("msg", "deletion sweep found no candidates: stored file list holds every object, not the unreachable ones", "previous_list_created_at", previous.CreatedAt.Format(time.RFC3339))
	default:
		found, err := s.findStoredCandidates(ctx, *previous, list.Paths)
		switch {
		case errors.Is(err, errStoredListChanged):
			level.Info(logger).Log("msg", "deletion sweep superseded: stored file list changed during the sweep")
			return SweepSuperseded, nil
		case err != nil:
			return "", err
		}
		cands = &found
	}

	err = s.writer.Write(ctx, list)
	switch {
	case errors.Is(err, ErrSuperseded):
		return SweepSuperseded, nil
	case err != nil:
		return "", fmt.Errorf("storing file list of tenant %q: %w", tenant, err)
	}

	level.Info(logger).Log(
		"msg", "deletion sweep stored unreachable objects",
		"scanned", len(scanned.Paths),
		"reachable", len(scanned.Paths)-len(list.Paths),
		"unreachable", len(list.Paths),
	)
	if cands != nil {
		s.logDryRun(ctx, logger, previous.CreatedAt, list, *cands)
	}
	return SweepScanned, nil
}

// readStoredHeader returns the header of the stored file list of tenant, or
// nil if no list is stored.
func (s *Sweeper) readStoredHeader(ctx context.Context, tenant string) (*fileListHeader, error) {
	r, err := OpenFileList(ctx, s.bucket, tenant)
	if err != nil {
		if s.bucket.IsObjNotFoundErr(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading stored file list of tenant %q: %w", tenant, err)
	}
	defer r.Close()
	header := r.dec.header
	return &header, nil
}

// findStoredCandidates compares paths with the stored file list of the
// tenant of previous. It returns errStoredListChanged if the stored list no
// longer has the header previous.
//
// It opens the stored list again instead of keeping it open during the scan.
// A scan can take many minutes, and a long read can time out.
func (s *Sweeper) findStoredCandidates(ctx context.Context, previous fileListHeader, paths []string) (candidates, error) {
	r, err := OpenFileList(ctx, s.bucket, previous.Tenant)
	if err != nil {
		if s.bucket.IsObjNotFoundErr(err) {
			return candidates{}, errStoredListChanged
		}
		return candidates{}, fmt.Errorf("reading stored file list of tenant %q: %w", previous.Tenant, err)
	}
	defer r.Close()
	if !r.dec.header.equal(previous) {
		return candidates{}, errStoredListChanged
	}
	cands, err := findCandidates(r.Paths(), paths)
	if err != nil {
		return candidates{}, fmt.Errorf("comparing with stored file list of tenant %q: %w", previous.Tenant, err)
	}
	return cands, nil
}

// logDryRun logs a summary of the deletion candidates that a sweep would
// delete. It also logs one line for each example candidate, with its last
// modification time and size. An operator uses these lines to check the
// candidates by hand.
//
// If logDryRun cannot read the attributes of an example, it logs the error
// instead. The list is already stored, so the error does not fail the sweep.
func (s *Sweeper) logDryRun(ctx context.Context, logger log.Logger, previousCreatedAt time.Time, list FileList, cands candidates) {
	level.Info(logger).Log(
		"msg", "deletion sweep dry run: would delete objects",
		"previous_list_created_at", previousCreatedAt.Format(time.RFC3339),
		"list_created_at", list.CreatedAt.Format(time.RFC3339),
		"previous_list_paths", cands.previousPaths,
		"list_paths", len(list.Paths),
		"candidates", cands.count(),
		"log_objects", len(cands.logObjects),
		"index_objects", len(cands.indexObjects),
	)

	logObjects, indexObjects := cands.examples(rand.New(rand.NewPCG(rand.Uint64(), rand.Uint64())))
	s.logExamples(ctx, logger, "index", indexObjects)
	s.logExamples(ctx, logger, "log", logObjects)
}

// logExamples logs one line for each path, with its last modification time
// and size.
func (s *Sweeper) logExamples(ctx context.Context, logger log.Logger, kind string, paths []string) {
	for _, p := range paths {
		attrs, err := s.bucket.Attributes(ctx, p)
		if err != nil {
			level.Warn(logger).Log("msg", "deletion sweep dry run: candidate", "kind", kind, "path", p, "err", err)
			continue
		}
		level.Info(logger).Log(
			"msg", "deletion sweep dry run: candidate",
			"kind", kind,
			"path", p,
			"last_modified", attrs.LastModified.UTC().Format(time.RFC3339),
			"size", attrs.Size,
		)
	}
}

// ListCompactedTenants returns the tenants that have a directory of compacted
// log objects or compacted index objects in bucket. bucket is the root of the
// data object storage.
//
// It lists the directory names only, so a tenant whose compacted objects were
// all deleted can still appear. It skips a directory name that is not a valid
// tenant ID, and logs it.
func ListCompactedTenants(ctx context.Context, bucket objstore.BucketReader, logger log.Logger) (map[string]struct{}, error) {
	tenants := make(map[string]struct{})
	for _, root := range []string{"objects", path.Join(metastore.IndexStoragePrefix, "indexes")} {
		dir := tenantsDir(root)
		err := bucket.Iter(ctx, dir, func(name string) error {
			if !strings.HasSuffix(name, objstore.DirDelim) {
				return nil
			}
			tenant := strings.TrimSuffix(strings.TrimPrefix(name, dir), objstore.DirDelim)
			if err := validateTenant(tenant); err != nil {
				level.Warn(logger).Log("msg", "skipped tenants directory entry that is not a valid tenant", "dir", name, "err", err)
				return nil
			}
			tenants[tenant] = struct{}{}
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("listing tenants in %s: %w", dir, err)
		}
	}
	return tenants, nil
}
