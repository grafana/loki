// Copyright (c) The Thanos Authors.
// Licensed under the Apache License 2.0.

package objstore

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/thanos-io/thanos/pkg/runutil"
)

// Bucket provides read and write access to an object storage bucket.
// NOTE: We assume strong consistency for write-read flow.
type Bucket interface {
	io.Closer
	BucketReader

	// Upload the contents of the reader as an object into the bucket.
	// Upload should be idempotent.
	Upload(ctx context.Context, name string, r io.Reader) error

	// Delete removes the object with the given name.
	// If object does not exists in the moment of deletion, Delete should throw error.
	Delete(ctx context.Context, name string) error

	// Name returns the bucket name for the provider.
	Name() string
}

// InstrumentedBucket is a Bucket with optional instrumentation control on reader.
type InstrumentedBucket interface {
	Bucket

	// WithExpectedErrs allows to specify a filter that marks certain errors as expected, so it will not increment
	// thanos_objstore_bucket_operation_failures_total metric.
	WithExpectedErrs(IsOpFailureExpectedFunc) Bucket

	// ReaderWithExpectedErrs allows to specify a filter that marks certain errors as expected, so it will not increment
	// thanos_objstore_bucket_operation_failures_total metric.
	// TODO(bwplotka): Remove this when moved to Go 1.14 and replace with InstrumentedBucketReader.
	ReaderWithExpectedErrs(IsOpFailureExpectedFunc) BucketReader
}

// BucketReader provides read access to an object storage bucket.
type BucketReader interface {
	// Iter calls f for each entry in the given directory (not recursive.). The argument to f is the full
	// object name including the prefix of the inspected directory.
	Iter(ctx context.Context, dir string, f func(string) error) error

	// Get returns a reader for the given object name.
	Get(ctx context.Context, name string) (io.ReadCloser, error)

	// GetRange returns a new range reader for the given object name and range.
	GetRange(ctx context.Context, name string, off, length int64) (io.ReadCloser, error)

	// Exists checks if the given object exists in the bucket.
	Exists(ctx context.Context, name string) (bool, error)

	// IsObjNotFoundErr returns true if error means that object is not found. Relevant to Get operations.
	IsObjNotFoundErr(err error) bool

	// Attributes returns information about the specified object.
	Attributes(ctx context.Context, name string) (ObjectAttributes, error)
}

// InstrumentedBucket is a BucketReader with optional instrumentation control.
type InstrumentedBucketReader interface {
	BucketReader

	// ReaderWithExpectedErrs allows to specify a filter that marks certain errors as expected, so it will not increment
	// thanos_objstore_bucket_operation_failures_total metric.
	ReaderWithExpectedErrs(IsOpFailureExpectedFunc) BucketReader
}

type ObjectAttributes struct {
	// Size is the object size in bytes.
	Size int64 `json:"size"`

	// LastModified is the timestamp the object was last modified.
	LastModified time.Time `json:"last_modified"`
}

// TryToGetSize tries to get upfront size from reader.
// TODO(https://github.com/thanos-io/thanos/issues/678): Remove guessing length when minio provider will support multipart upload without this.
func TryToGetSize(r io.Reader) (int64, error) {
	switch f := r.(type) {
	case *os.File:
		fileInfo, err := f.Stat()
		if err != nil {
			return 0, errors.Wrap(err, "os.File.Stat()")
		}
		return fileInfo.Size(), nil
	case *bytes.Buffer:
		return int64(f.Len()), nil
	case *strings.Reader:
		return f.Size(), nil
	}
	return 0, errors.New("unsupported type of io.Reader")
}

// UploadDir uploads all files in srcdir to the bucket with into a top-level directory
// named dstdir. It is a caller responsibility to clean partial upload in case of failure.
func UploadDir(ctx context.Context, logger log.Logger, bkt Bucket, srcdir, dstdir string) error {
	df, err := os.Stat(srcdir)
	if err != nil {
		return errors.Wrap(err, "stat dir")
	}
	if !df.IsDir() {
		return errors.Errorf("%s is not a directory", srcdir)
	}
	return filepath.Walk(srcdir, func(src string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if fi.IsDir() {
			return nil
		}
		dst := filepath.Join(dstdir, strings.TrimPrefix(src, srcdir))

		return UploadFile(ctx, logger, bkt, src, dst)
	})
}

// UploadFile uploads the file with the given name to the bucket.
// It is a caller responsibility to clean partial upload in case of failure.
func UploadFile(ctx context.Context, logger log.Logger, bkt Bucket, src, dst string) error {
	r, err := os.Open(src)
	if err != nil {
		return errors.Wrapf(err, "open file %s", src)
	}
	defer runutil.CloseWithLogOnErr(logger, r, "close file %s", src)

	if err := bkt.Upload(ctx, dst, r); err != nil {
		return errors.Wrapf(err, "upload file %s as %s", src, dst)
	}
	level.Debug(logger).Log("msg", "uploaded file", "from", src, "dst", dst, "bucket", bkt.Name())
	return nil
}

// DirDelim is the delimiter used to model a directory structure in an object store bucket.
const DirDelim = "/"

// DownloadFile downloads the src file from the bucket to dst. If dst is an existing
// directory, a file with the same name as the source is created in dst.
// If destination file is already existing, download file will overwrite it.
func DownloadFile(ctx context.Context, logger log.Logger, bkt BucketReader, src, dst string) (err error) {
	if fi, err := os.Stat(dst); err == nil {
		if fi.IsDir() {
			dst = filepath.Join(dst, filepath.Base(src))
		}
	} else if !os.IsNotExist(err) {
		return err
	}

	rc, err := bkt.Get(ctx, src)
	if err != nil {
		return errors.Wrapf(err, "get file %s", src)
	}
	defer runutil.CloseWithLogOnErr(logger, rc, "download block's file reader")

	f, err := os.Create(dst)
	if err != nil {
		return errors.Wrap(err, "create file")
	}
	defer func() {
		if err != nil {
			if rerr := os.Remove(dst); rerr != nil {
				level.Warn(logger).Log("msg", "failed to remove partially downloaded file", "file", dst, "err", rerr)
			}
		}
	}()
	defer runutil.CloseWithLogOnErr(logger, f, "download block's output file")

	if _, err = io.Copy(f, rc); err != nil {
		return errors.Wrap(err, "copy object to file")
	}
	return nil
}

// DownloadDir downloads all object found in the directory into the local directory.
func DownloadDir(ctx context.Context, logger log.Logger, bkt BucketReader, src, dst string) error {
	if err := os.MkdirAll(dst, 0777); err != nil {
		return errors.Wrap(err, "create dir")
	}

	var downloadedFiles []string
	if err := bkt.Iter(ctx, src, func(name string) error {
		if strings.HasSuffix(name, DirDelim) {
			return DownloadDir(ctx, logger, bkt, name, filepath.Join(dst, filepath.Base(name)))
		}
		if err := DownloadFile(ctx, logger, bkt, name, dst); err != nil {
			return err
		}

		downloadedFiles = append(downloadedFiles, dst)
		return nil
	}); err != nil {
		// Best-effort cleanup if the download failed.
		for _, f := range downloadedFiles {
			if rerr := os.Remove(f); rerr != nil {
				level.Warn(logger).Log("msg", "failed to remove file on partial dir download error", "file", f, "err", rerr)
			}
		}
		return err
	}

	return nil
}

const (
	iterOp       = "iter"
	getOp        = "get"
	getRangeOp   = "get_range"
	existsOp     = "exists"
	uploadOp     = "upload"
	deleteOp     = "delete"
	attributesOp = "attributes"
)

// IsOpFailureExpectedFunc allows to mark certain errors as expected, so they will not increment thanos_objstore_bucket_operation_failures_total metric.
type IsOpFailureExpectedFunc func(error) bool

var _ InstrumentedBucket = &metricBucket{}

// BucketWithMetrics takes a bucket and registers metrics with the given registry for
// operations run against the bucket.
func BucketWithMetrics(name string, b Bucket, reg prometheus.Registerer) *metricBucket {
	bkt := &metricBucket{
		bkt:                 b,
		isOpFailureExpected: func(err error) bool { return false },
		ops: promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
			Name:        "thanos_objstore_bucket_operations_total",
			Help:        "Total number of all attempted operations against a bucket.",
			ConstLabels: prometheus.Labels{"bucket": name},
		}, []string{"operation"}),

		opsFailures: promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
			Name:        "thanos_objstore_bucket_operation_failures_total",
			Help:        "Total number of operations against a bucket that failed, but were not expected to fail in certain way from caller perspective. Those errors have to be investigated.",
			ConstLabels: prometheus.Labels{"bucket": name},
		}, []string{"operation"}),

		opsDuration: promauto.With(reg).NewHistogramVec(prometheus.HistogramOpts{
			Name:        "thanos_objstore_bucket_operation_duration_seconds",
			Help:        "Duration of successful operations against the bucket",
			ConstLabels: prometheus.Labels{"bucket": name},
			Buckets:     []float64{0.001, 0.01, 0.1, 0.3, 0.6, 1, 3, 6, 9, 20, 30, 60, 90, 120},
		}, []string{"operation"}),
		lastSuccessfulUploadTime: promauto.With(reg).NewGaugeVec(prometheus.GaugeOpts{
			Name: "thanos_objstore_bucket_last_successful_upload_time",
			Help: "Second timestamp of the last successful upload to the bucket.",
		}, []string{"bucket"}),
	}
	for _, op := range []string{
		iterOp,
		getOp,
		getRangeOp,
		existsOp,
		uploadOp,
		deleteOp,
		attributesOp,
	} {
		bkt.ops.WithLabelValues(op)
		bkt.opsFailures.WithLabelValues(op)
		bkt.opsDuration.WithLabelValues(op)
	}
	bkt.lastSuccessfulUploadTime.WithLabelValues(b.Name())
	return bkt
}

type metricBucket struct {
	bkt Bucket

	ops                 *prometheus.CounterVec
	opsFailures         *prometheus.CounterVec
	isOpFailureExpected IsOpFailureExpectedFunc

	opsDuration              *prometheus.HistogramVec
	lastSuccessfulUploadTime *prometheus.GaugeVec
}

func (b *metricBucket) WithExpectedErrs(fn IsOpFailureExpectedFunc) Bucket {
	return &metricBucket{
		bkt:                      b.bkt,
		ops:                      b.ops,
		opsFailures:              b.opsFailures,
		isOpFailureExpected:      fn,
		opsDuration:              b.opsDuration,
		lastSuccessfulUploadTime: b.lastSuccessfulUploadTime,
	}
}

func (b *metricBucket) ReaderWithExpectedErrs(fn IsOpFailureExpectedFunc) BucketReader {
	return b.WithExpectedErrs(fn)
}

func (b *metricBucket) Iter(ctx context.Context, dir string, f func(name string) error) error {
	const op = iterOp
	b.ops.WithLabelValues(op).Inc()

	err := b.bkt.Iter(ctx, dir, f)
	if err != nil && !b.isOpFailureExpected(err) {
		b.opsFailures.WithLabelValues(op).Inc()
	}
	return err
}

func (b *metricBucket) Attributes(ctx context.Context, name string) (ObjectAttributes, error) {
	const op = attributesOp
	b.ops.WithLabelValues(op).Inc()

	start := time.Now()
	attrs, err := b.bkt.Attributes(ctx, name)
	if err != nil {
		if !b.isOpFailureExpected(err) {
			b.opsFailures.WithLabelValues(op).Inc()
		}
		return attrs, err
	}
	b.opsDuration.WithLabelValues(op).Observe(time.Since(start).Seconds())
	return attrs, nil
}

func (b *metricBucket) Get(ctx context.Context, name string) (io.ReadCloser, error) {
	const op = getOp
	b.ops.WithLabelValues(op).Inc()

	rc, err := b.bkt.Get(ctx, name)
	if err != nil {
		if !b.isOpFailureExpected(err) {
			b.opsFailures.WithLabelValues(op).Inc()
		}
		return nil, err
	}
	return newTimingReadCloser(
		rc,
		op,
		b.opsDuration,
		b.opsFailures,
		b.isOpFailureExpected,
	), nil
}

func (b *metricBucket) GetRange(ctx context.Context, name string, off, length int64) (io.ReadCloser, error) {
	const op = getRangeOp
	b.ops.WithLabelValues(op).Inc()

	rc, err := b.bkt.GetRange(ctx, name, off, length)
	if err != nil {
		if !b.isOpFailureExpected(err) {
			b.opsFailures.WithLabelValues(op).Inc()
		}
		return nil, err
	}
	return newTimingReadCloser(
		rc,
		op,
		b.opsDuration,
		b.opsFailures,
		b.isOpFailureExpected,
	), nil
}

func (b *metricBucket) Exists(ctx context.Context, name string) (bool, error) {
	const op = existsOp
	b.ops.WithLabelValues(op).Inc()

	start := time.Now()
	ok, err := b.bkt.Exists(ctx, name)
	if err != nil {
		if !b.isOpFailureExpected(err) {
			b.opsFailures.WithLabelValues(op).Inc()
		}
		return false, err
	}
	b.opsDuration.WithLabelValues(op).Observe(time.Since(start).Seconds())
	return ok, nil
}

func (b *metricBucket) Upload(ctx context.Context, name string, r io.Reader) error {
	const op = uploadOp
	b.ops.WithLabelValues(op).Inc()

	start := time.Now()
	if err := b.bkt.Upload(ctx, name, r); err != nil {
		if !b.isOpFailureExpected(err) {
			b.opsFailures.WithLabelValues(op).Inc()
		}
		return err
	}
	b.lastSuccessfulUploadTime.WithLabelValues(b.bkt.Name()).SetToCurrentTime()
	b.opsDuration.WithLabelValues(op).Observe(time.Since(start).Seconds())
	return nil
}

func (b *metricBucket) Delete(ctx context.Context, name string) error {
	const op = deleteOp
	b.ops.WithLabelValues(op).Inc()

	start := time.Now()
	if err := b.bkt.Delete(ctx, name); err != nil {
		if !b.isOpFailureExpected(err) {
			b.opsFailures.WithLabelValues(op).Inc()
		}
		return err
	}
	b.opsDuration.WithLabelValues(op).Observe(time.Since(start).Seconds())

	return nil
}

func (b *metricBucket) IsObjNotFoundErr(err error) bool {
	return b.bkt.IsObjNotFoundErr(err)
}

func (b *metricBucket) Close() error {
	return b.bkt.Close()
}

func (b *metricBucket) Name() string {
	return b.bkt.Name()
}

type timingReadCloser struct {
	io.ReadCloser

	alreadyGotErr bool

	start             time.Time
	op                string
	duration          *prometheus.HistogramVec
	failed            *prometheus.CounterVec
	isFailureExpected IsOpFailureExpectedFunc
}

func newTimingReadCloser(rc io.ReadCloser, op string, dur *prometheus.HistogramVec, failed *prometheus.CounterVec, isFailureExpected IsOpFailureExpectedFunc) *timingReadCloser {
	// Initialize the metrics with 0.
	dur.WithLabelValues(op)
	failed.WithLabelValues(op)
	return &timingReadCloser{
		ReadCloser:        rc,
		start:             time.Now(),
		op:                op,
		duration:          dur,
		failed:            failed,
		isFailureExpected: isFailureExpected,
	}
}

func (rc *timingReadCloser) Close() error {
	err := rc.ReadCloser.Close()
	if !rc.alreadyGotErr && err != nil {
		rc.failed.WithLabelValues(rc.op).Inc()
	}
	if !rc.alreadyGotErr && err == nil {
		rc.duration.WithLabelValues(rc.op).Observe(time.Since(rc.start).Seconds())
		rc.alreadyGotErr = true
	}
	return err
}

func (rc *timingReadCloser) Read(b []byte) (n int, err error) {
	n, err = rc.ReadCloser.Read(b)
	// Report metric just once.
	if !rc.alreadyGotErr && err != nil && err != io.EOF {
		if !rc.isFailureExpected(err) {
			rc.failed.WithLabelValues(rc.op).Inc()
		}
		rc.alreadyGotErr = true
	}
	return n, err
}
