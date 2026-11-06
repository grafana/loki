package deletion

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"time"
	"unicode/utf8"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/backoff"
	"github.com/grafana/dskit/tenant"
	"github.com/thanos-io/objstore"

	"github.com/grafana/loki/v3/pkg/storage/bucket"
)

// FileList is the list of objects that hold data of a tenant at CreatedAt.
//
// When two writers store a file list for the same tenant, the list with the
// later CreatedAt wins.
type FileList struct {
	Tenant    string
	CreatedAt time.Time
	// Paths are relative to the root of the data object storage. They are not
	// empty, are valid UTF-8, and are in strictly increasing order. JSON
	// replaces invalid UTF-8, so a stored path would not match the object key.
	Paths []string
}

// Path returns the object storage path of the file list, relative to the root
// of the data object storage.
func (l FileList) Path() string {
	return fileListPath(l.Tenant)
}

func (l FileList) header() fileListHeader {
	return fileListHeader{Version: fileListVersion, Tenant: l.Tenant, CreatedAt: l.CreatedAt}
}

func (l FileList) validate() error {
	if err := l.header().validate(); err != nil {
		return fmt.Errorf("invalid file list: %w", err)
	}
	for i, p := range l.Paths {
		if p == "" {
			return errors.New("file list paths must not be empty")
		}
		if !utf8.ValidString(p) {
			return fmt.Errorf("file list path %q is not valid UTF-8", p)
		}
		if i > 0 && p <= l.Paths[i-1] {
			return fmt.Errorf("file list paths must be in strictly increasing order: %q is not after %q", p, l.Paths[i-1])
		}
	}
	return nil
}

// canBeReplacedBy returns nil if list may replace the stored list with the
// header h. It returns an error that wraps ErrSuperseded if the stored list
// is not older than list. The caller checks that h holds the tenant of list.
func (h fileListHeader) canBeReplacedBy(list FileList) error {
	if !list.CreatedAt.After(h.CreatedAt) {
		return fmt.Errorf("%w: stored file list created at %s", ErrSuperseded, h.CreatedAt.Format(time.RFC3339Nano))
	}
	return nil
}

func validateTenant(id string) error {
	if id == "" {
		return errors.New("tenant must not be empty")
	}
	if err := tenant.ValidTenantID(id); err != nil {
		return fmt.Errorf("invalid tenant %q: %w", id, err)
	}
	return nil
}

// ErrSuperseded means that the stored file list of the tenant has the same or
// a later creation time than the list to write.
var ErrSuperseded = errors.New("stored file list is not older than the file list to write")

// FileListWriter stores file lists in a data object bucket.
//
// FileListWriter is safe for concurrent use.
type FileListWriter struct {
	bucket     objstore.Bucket
	backoffCfg backoff.Config
	logger     log.Logger
}

// NewFileListWriter returns a FileListWriter for bucket. bucket is the root of
// the data object storage. The writer retries a failed write with
// backoffCfg. A backoffCfg with MaxRetries 0 retries until the context ends.
//
// bucket must support conditional writes through GetAndReplace. A bucket that
// ignores the write condition can replace a newer list with an older one.
func NewFileListWriter(bucket objstore.Bucket, backoffCfg backoff.Config, logger log.Logger) *FileListWriter {
	return &FileListWriter{bucket: bucket, backoffCfg: backoffCfg, logger: logger}
}

// Write stores list at list.Path() if the bucket holds no file list there, or
// holds an older one.
//
// Write uses a conditional write, so an older list never replaces a list from
// a concurrent writer. One gap remains: the S3 bucket sends no condition when
// no list is stored yet. Two concurrent first writes to S3 both succeed, and
// the last one wins.
//
// If the stored list is not older than list, Write leaves it unchanged and
// returns an error that wraps ErrSuperseded. The stored list can be the list
// that an earlier attempt of the same call wrote. That attempt lost its
// response. The stored state is correct in that case too, so Write does not
// tell the two cases apart.
//
// Write reads only the header of the stored list. It retries a failed write
// with the backoff config of the writer. It does not retry if the stored
// header is malformed, holds another tenant, or has an unknown version. The
// error then wraps ErrMalformedFileList.
func (w *FileListWriter) Write(ctx context.Context, list FileList) error {
	if err := list.validate(); err != nil {
		return err
	}

	encoded, err := encodeFileList(list)
	if err != nil {
		return fmt.Errorf("encoding file list: %w", err)
	}

	logger := log.With(w.logger, "tenant", list.Tenant, "path", list.Path())

	b := backoff.New(ctx, w.backoffCfg)
	for b.Ongoing() {
		err = w.bucket.GetAndReplace(ctx, list.Path(), func(existing io.ReadCloser) (io.ReadCloser, error) {
			if existing != nil {
				defer existing.Close()
				dec, err := newFileListDecoder(existing, list.Tenant)
				if err != nil {
					return nil, err
				}
				if err := dec.header.canBeReplacedBy(list); err != nil {
					return nil, err
				}
			}
			// The body must report its size. Without a size, minio uploads in
			// parts and drops the If-Match condition of the write.
			return objstore.NopCloserWithSize(bytes.NewReader(encoded)), nil
		})

		switch {
		case err == nil:
			level.Info(logger).Log("msg", "wrote file list", "paths", len(list.Paths), "bytes", len(encoded))
			return nil
		case errors.Is(err, ErrSuperseded):
			level.Info(logger).Log("msg", "skipped file list write: stored file list is not older", "err", err)
			return err
		case errors.Is(err, ErrMalformedFileList):
			level.Error(logger).Log("msg", "file list write failed unrecoverably", "err", err)
			return err
		case ctx.Err() != nil:
			return fmt.Errorf("writing file list %s: %w", list.Path(), context.Cause(ctx))
		case bucket.IsConditionalWriteConflict(err):
			level.Debug(logger).Log("msg", "file list write lost a conditional write, retrying", "attempt", b.NumRetries()+1)
		default:
			level.Warn(logger).Log("msg", "file list write failed, retrying", "attempt", b.NumRetries()+1, "err", err)
		}
		b.Wait()
	}

	if ctx.Err() != nil {
		return fmt.Errorf("writing file list %s: %w", list.Path(), context.Cause(ctx))
	}
	err = fmt.Errorf("writing file list %s: gave up after %d attempts: %w", list.Path(), b.NumRetries(), err)
	level.Error(logger).Log("msg", "file list write failed, gave up", "err", err)
	return err
}
