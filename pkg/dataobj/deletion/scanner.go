// Package deletion finds the data objects of a tenant that no ToC can reach,
// and records them. A later sweep compares the records to find deletion
// candidates.
package deletion

import (
	"context"
	"errors"
	"fmt"
	"path"
	"slices"
	"strings"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/thanos-io/objstore"

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
	bucket objstore.BucketReader
	logger log.Logger
}

// NewFileListScanner returns a FileListScanner for bucket. bucket is the root
// of the data object storage, which holds the objects/ directory and the
// index storage at [metastore.IndexStoragePrefix].
//
// NewFileListScanner returns an error if bucket does not report modification
// times with [objstore.WithUpdatedAt].
func NewFileListScanner(bucket objstore.BucketReader, logger log.Logger) (*FileListScanner, error) {
	if !slices.Contains(bucket.SupportedIterOptions(), objstore.UpdatedAt) {
		return nil, errors.New("bucket does not report modification times")
	}
	return &FileListScanner{bucket: bucket, logger: logger}, nil
}

// List returns the sorted paths of the log objects and index objects of
// tenant in the tenant directories: objects/tenants/<tenant>/ and
// index/v0/indexes/tenants/<tenant>/. It includes only objects last modified
// at least [MinObjectAge] before now. The paths are relative to the bucket of
// the FileListScanner.
//
// List returns paths, not a [FileList], because a stored file list holds
// only unreachable objects. The caller removes the reachable objects.
//
// List lists the tenant directories only. The path of an object in a tenant
// directory names its tenant. Objects outside the tenant directories, for
// example uncompacted objects in objects/<prefix>/, are not in the list.
//
// List returns an error if a listing fails, or if the bucket does not report
// the modification time of an object.
func (s *FileListScanner) List(ctx context.Context, tenant string, now time.Time) ([]string, error) {
	if err := validateTenant(tenant); err != nil {
		return nil, err
	}

	start := time.Now()
	sc := &scan{bucket: s.bucket, cutoff: now.Add(-MinObjectAge)}
	for _, root := range []string{"objects", path.Join(metastore.IndexStoragePrefix, "indexes")} {
		dir := tenantDir(root, tenant)
		if err := sc.listDir(ctx, dir); err != nil {
			return nil, fmt.Errorf("listing objects in %s: %w", dir, err)
		}
	}

	// A list with no objects can mean a wrong bucket root. A listed count of
	// zero shows this case.
	level.Info(s.logger).Log(
		"msg", "listed objects of tenant",
		"tenant", tenant,
		"cutoff", sc.cutoff.Format(time.RFC3339),
		"listed", sc.listed,
		"too_young", sc.tooYoung,
		"included", len(sc.paths),
		"duration", time.Since(start),
	)

	slices.Sort(sc.paths)
	return sc.paths, nil
}

// tenantDir returns the directory below root that holds the objects of
// tenant.
func tenantDir(root, tenant string) string {
	return tenantsDir(root) + tenant + objstore.DirDelim
}

// tenantsDir returns the directory below root that holds the directories of
// all tenants.
func tenantsDir(root string) string {
	return root + "/tenants/"
}

// scan holds the state of one List call. It is not safe for concurrent use.
type scan struct {
	bucket objstore.BucketReader
	// cutoff is the latest modification time of an object that the scan
	// includes.
	cutoff time.Time

	paths    []string
	listed   int
	tooYoung int
}

// listDir adds each object below dir that was last modified at or before the
// cutoff. It skips keys that end in a slash. Some buckets hold such empty
// placeholder objects for directories, and they are not data objects.
func (sc *scan) listDir(ctx context.Context, dir string) error {
	return sc.bucket.IterWithAttributes(ctx, dir, func(attrs objstore.IterObjectAttributes) error {
		if strings.HasSuffix(attrs.Name, objstore.DirDelim) {
			return nil
		}
		modified, ok := attrs.LastModified()
		if !ok {
			return fmt.Errorf("bucket did not report the modification time of %s", attrs.Name)
		}
		sc.listed++
		if modified.After(sc.cutoff) {
			sc.tooYoung++
			return nil
		}
		sc.paths = append(sc.paths, attrs.Name)
		return nil
	}, objstore.WithRecursiveIter(), objstore.WithUpdatedAt())
}
