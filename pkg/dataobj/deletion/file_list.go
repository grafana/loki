package deletion

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/backoff"
	"github.com/grafana/dskit/tenant"
	"github.com/thanos-io/objstore"

	"github.com/grafana/loki/v3/pkg/storage/bucket"
)

// FileList is the list of objects that hold data of a tenant at CreatedAt.
//
// FileList is stored as JSON. When two writers store a file list for the same
// tenant, the list with the later CreatedAt wins.
type FileList struct {
	Tenant    string    `json:"tenant"`
	CreatedAt time.Time `json:"created_at"`
	// Paths are sorted and relative to the root of the data object storage.
	Paths []string `json:"paths"`
}

// Path returns the object storage path of the file list, relative to the root
// of the data object storage.
func (l FileList) Path() string {
	return "deletion/" + l.Tenant + "/files.json"
}

func (l FileList) validate() error {
	if err := validateTenant(l.Tenant); err != nil {
		return err
	}
	if l.CreatedAt.IsZero() {
		return errors.New("file list creation time must be set")
	}
	return nil
}

// checkReplaces returns nil if l may replace stored. It returns
// errAlreadyStored if stored is the same list as l, and an error that wraps
// ErrSuperseded if stored is a different list that is not older than l.
func (l FileList) checkReplaces(stored FileList) error {
	if stored.Tenant != l.Tenant {
		return fmt.Errorf("%w: stored file list holds tenant %q, want %q", errUnrecoverable, stored.Tenant, l.Tenant)
	}
	if stored.CreatedAt.Equal(l.CreatedAt) && slices.Equal(stored.Paths, l.Paths) {
		return errAlreadyStored
	}
	if !l.CreatedAt.After(stored.CreatedAt) {
		return fmt.Errorf("%w: stored file list created at %s", ErrSuperseded, stored.CreatedAt.Format(time.RFC3339Nano))
	}
	return nil
}

// decodeFileList decodes a stored file list. It rejects unknown fields, so
// that a writer never replaces a list in a format that it does not know.
func decodeFileList(data []byte) (FileList, error) {
	if len(data) == 0 {
		return FileList{}, fmt.Errorf("%w: stored file list is empty", errUnrecoverable)
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var list FileList
	if err := dec.Decode(&list); err != nil {
		return FileList{}, fmt.Errorf("%w: decoding stored file list: %w", errUnrecoverable, err)
	}
	return list, nil
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

// ErrSuperseded means that the stored file list of the tenant is a different
// list, and its creation time is the same as or later than the creation time
// of the list to write.
var ErrSuperseded = errors.New("stored file list is not older than the file list to write")

var (
	// errUnrecoverable marks a write error that retrying cannot fix.
	errUnrecoverable = errors.New("unrecoverable file list error")
	// errAlreadyStored means that the stored file list is the list to write,
	// for example because a previous attempt wrote it and lost the response.
	errAlreadyStored = errors.New("file list is already stored")
)

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

// Write stores list at list.Path() if no file list is stored there, or if the
// stored file list is older.
//
// Write uses a conditional write, so an older list never replaces a list from
// a concurrent writer. One gap remains: the S3 bucket sends no condition when
// no list is stored yet. Two concurrent first writes to S3 both succeed, and
// the last one wins.
//
// If the stored file list is a different list that is not older than list,
// Write leaves it unchanged and returns an error that wraps ErrSuperseded. If
// the stored file list is equal to list, Write returns nil.
//
// Write retries a failed write with the backoff config of the writer. It does
// not retry if the stored file list is empty, malformed, or belongs to another
// tenant.
func (w *FileListWriter) Write(ctx context.Context, list FileList) error {
	if err := list.validate(); err != nil {
		return err
	}

	encoded, err := json.Marshal(list)
	if err != nil {
		return fmt.Errorf("encoding file list: %w", err)
	}

	logger := log.With(w.logger, "tenant", list.Tenant, "path", list.Path())

	b := backoff.New(ctx, w.backoffCfg)
	for b.Ongoing() {
		err = w.bucket.GetAndReplace(ctx, list.Path(), func(existing io.ReadCloser) (io.ReadCloser, error) {
			if existing != nil {
				defer existing.Close()
				if err := checkStored(existing, list); err != nil {
					return nil, err
				}
			}
			return io.NopCloser(bytes.NewReader(encoded)), nil
		})

		switch {
		case err == nil:
			level.Info(logger).Log("msg", "wrote file list", "paths", len(list.Paths))
			return nil
		case errors.Is(err, errAlreadyStored):
			level.Info(logger).Log("msg", "file list is already stored")
			return nil
		case errors.Is(err, ErrSuperseded):
			level.Info(logger).Log("msg", "skipped file list write: stored file list is not older", "err", err)
			return err
		case errors.Is(err, errUnrecoverable):
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

// checkStored returns nil if list may replace the stored file list read from
// r. It reads r in full before it decodes, so that a read error stays
// retryable.
func checkStored(r io.Reader, list FileList) error {
	data, err := io.ReadAll(r)
	if err != nil {
		return fmt.Errorf("reading stored file list: %w", err)
	}
	stored, err := decodeFileList(data)
	if err != nil {
		return err
	}
	return list.checkReplaces(stored)
}
