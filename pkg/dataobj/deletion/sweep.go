package deletion

import (
	"context"
	"errors"
	"fmt"
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
	// SweepSkipped means that the stored file list was newer than
	// MinSweepInterval, so the sweep did not scan.
	SweepSkipped SweepOutcome = "skipped"
	// SweepScanned means that the sweep scanned and stored a new file list.
	SweepScanned SweepOutcome = "scanned"
	// SweepSuperseded means that the sweep scanned, but a concurrent sweep
	// stored a file list that is not older. The stored list stays.
	SweepSuperseded SweepOutcome = "superseded"
)

// sweepMaxOpenFiles limits the objects that one sweep opens at the same time.
// A worker can run several sweeps at once, so the limit is per sweep.
const sweepMaxOpenFiles = 16

var sweepWriterBackoff = backoff.Config{
	MinBackoff: 100 * time.Millisecond,
	MaxBackoff: 5 * time.Second,
	MaxRetries: 5,
}

// Sweeper records the objects of a tenant in its stored file list, at most
// once per MinSweepInterval.
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

// Sweep reads the stored file list of tenant first. It skips the scan if the
// stored list is newer than MinSweepInterval at now. Otherwise, or if no list
// is stored, it scans the objects of tenant and stores the new list.
//
// Sweep returns an error, and does not scan, if it cannot read the stored
// list. That includes a list that wraps ErrMalformedFileList. Sweep does not
// replace such a list, so an operator must repair or delete it.
//
// A list that a concurrent sweep stored first is not an error. Sweep then
// returns SweepSuperseded.
func (s *Sweeper) Sweep(ctx context.Context, tenant string, now time.Time) (SweepOutcome, error) {
	stored, found, err := s.storedListTime(ctx, tenant)
	if err != nil {
		return "", err
	}
	if found && now.Sub(stored) < MinSweepInterval {
		level.Debug(s.logger).Log("msg", "skipped deletion sweep: stored file list is recent", "tenant", tenant, "created_at", stored.Format(time.RFC3339))
		return SweepSkipped, nil
	}

	list, err := s.scanner.List(ctx, tenant, now)
	if err != nil {
		return "", fmt.Errorf("scanning objects of tenant %q: %w", tenant, err)
	}
	err = s.writer.Write(ctx, list)
	switch {
	case errors.Is(err, ErrSuperseded):
		return SweepSuperseded, nil
	case err != nil:
		return "", fmt.Errorf("storing file list of tenant %q: %w", tenant, err)
	}
	return SweepScanned, nil
}

// storedListTime returns the creation time of the stored file list of
// tenant. found is false if no list is stored.
func (s *Sweeper) storedListTime(ctx context.Context, tenant string) (createdAt time.Time, found bool, err error) {
	r, err := OpenFileList(ctx, s.bucket, tenant)
	if err != nil {
		if s.bucket.IsObjNotFoundErr(err) {
			return time.Time{}, false, nil
		}
		return time.Time{}, false, fmt.Errorf("reading stored file list of tenant %q: %w", tenant, err)
	}
	defer r.Close()
	return r.CreatedAt(), true, nil
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
