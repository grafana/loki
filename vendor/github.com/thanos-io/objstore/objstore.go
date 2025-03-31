// Copyright (c) The Thanos Authors.
// Licensed under the Apache License 2.0.

package objstore

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/efficientgo/core/logerrcapture"
	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"golang.org/x/sync/errgroup"
)

type ObjProvider string

const (
	MEMORY     ObjProvider = "MEMORY"
	FILESYSTEM ObjProvider = "FILESYSTEM"
	GCS        ObjProvider = "GCS"
	S3         ObjProvider = "S3"
	AZURE      ObjProvider = "AZURE"
	SWIFT      ObjProvider = "SWIFT"
	COS        ObjProvider = "COS"
	ALIYUNOSS  ObjProvider = "ALIYUNOSS"
	BOS        ObjProvider = "BOS"
	OCI        ObjProvider = "OCI"
	OBS        ObjProvider = "OBS"
)

const (
	OpIter       = "iter"
	OpGet        = "get"
	OpGetRange   = "get_range"
	OpExists     = "exists"
	OpUpload     = "upload"
	OpDelete     = "delete"
	OpAttributes = "attributes"
)

// Bucket provides read and write access to an object storage bucket.
// NOTE: We assume strong consistency for write-read flow.
type Bucket interface {
	io.Closer
	BucketReader

	Provider() ObjProvider

	// Upload the contents of the reader as an object into the bucket.
	// Upload should be idempotent.
	Upload(ctx context.Context, name string, r io.Reader) error

	// GetAndReplace an existing object with a new object
	// If the previous object is created or updated before the new object is uploaded, then the call will fail with an error.
	// The existing reader will be nil in the case it did not previously exist.
	GetAndReplace(ctx context.Context, name string, f func(existing io.Reader) (io.Reader, error)) error

	// Delete removes the object with the given name.
	// If object does not exist in the moment of deletion, Delete should throw error.
	Delete(ctx context.Context, name string) error

	// Name returns the bucket name for the provider.
	Name() string
}

// InstrumentedBucket is a Bucket with optional instrumentation control on reader.
type InstrumentedBucket interface {
	Bucket

	// WithExpectedErrs allows to specify a filter that marks certain errors as expected, so it will not increment
	// objstore_bucket_operation_failures_total metric.
	WithExpectedErrs(IsOpFailureExpectedFunc) Bucket

	// ReaderWithExpectedErrs allows to specify a filter that marks certain errors as expected, so it will not increment
	// objstore_bucket_operation_failures_total metric.
	// TODO(bwplotka): Remove this when moved to Go 1.14 and replace with InstrumentedBucketReader.
	ReaderWithExpectedErrs(IsOpFailureExpectedFunc) BucketReader
}

// BucketReader provides read access to an object storage bucket.
type BucketReader interface {
	// Iter calls f for each entry in the given directory (not recursive.). The argument to f is the full
	// object name including the prefix of the inspected directory.

	// Entries are passed to function in sorted order.
	Iter(ctx context.Context, dir string, f func(name string) error, options ...IterOption) error

	// IterWithAttributes calls f for each entry in the given directory similar to Iter.
	// In addition to Name, it also includes requested object attributes in the argument to f.
	//
	// Attributes can be requested using IterOption.
	// Not all IterOptions are supported by all providers, requesting for an unsupported option will fail with ErrOptionNotSupported.
	IterWithAttributes(ctx context.Context, dir string, f func(attrs IterObjectAttributes) error, options ...IterOption) error

	// SupportedIterOptions returns a list of supported IterOptions by the underlying provider.
	SupportedIterOptions() []IterOptionType

	// Get returns a reader for the given object name.
	Get(ctx context.Context, name string) (io.ReadCloser, error)

	// GetRange returns a new range reader for the given object name and range.
	GetRange(ctx context.Context, name string, off, length int64) (io.ReadCloser, error)

	// Exists checks if the given object exists in the bucket.
	Exists(ctx context.Context, name string) (bool, error)

	// IsObjNotFoundErr returns true if error means that object is not found. Relevant to Get operations.
	IsObjNotFoundErr(err error) bool

	// IsAccessDeniedErr returns true if access to object is denied.
	IsAccessDeniedErr(err error) bool

	// Attributes returns information about the specified object.
	Attributes(ctx context.Context, name string) (ObjectAttributes, error)
}

// InstrumentedBucketReader is a BucketReader with optional instrumentation control.
type InstrumentedBucketReader interface {
	BucketReader

	// ReaderWithExpectedErrs allows to specify a filter that marks certain errors as expected, so it will not increment
	// objstore_bucket_operation_failures_total metric.
	ReaderWithExpectedErrs(IsOpFailureExpectedFunc) BucketReader
}

var ErrOptionNotSupported = errors.New("iter option is not supported")

// IterOptionType is used for type-safe option support checking.
type IterOptionType int

const (
	Recursive IterOptionType = iota
	UpdatedAt
)

// IterOption configures the provided params.
type IterOption struct {
	Type  IterOptionType
	Apply func(params *IterParams)
}

// WithRecursiveIter is an option that can be applied to Iter() to recursively list objects
// in the bucket.
func WithRecursiveIter() IterOption {
	return IterOption{
		Type: Recursive,
		Apply: func(params *IterParams) {
			params.Recursive = true
		},
	}
}

// WithUpdatedAt is an option that can be applied to Iter() to
// include the last modified time in the attributes.
// NB: Prefixes may not report last modified time.
// This option is currently supported for the azure, s3, bos, gcs and filesystem providers.
func WithUpdatedAt() IterOption {
	return IterOption{
		Type: UpdatedAt,
		Apply: func(params *IterParams) {
			params.LastModified = true
		},
	}
}

// IterParams holds the Iter() parameters and is used by objstore clients implementations.
type IterParams struct {
	Recursive    bool
	LastModified bool
}

func ValidateIterOptions(supportedOptions []IterOptionType, options ...IterOption) error {
	for _, opt := range options {
		if !slices.Contains(supportedOptions, opt.Type) {
			return fmt.Errorf("%w: %v", ErrOptionNotSupported, opt.Type)
		}
	}

	return nil
}

func ApplyIterOptions(options ...IterOption) IterParams {
	out := IterParams{}
	for _, opt := range options {
		opt.Apply(&out)
	}
	return out
}

// DownloadOption configures the provided params.
type DownloadOption func(params *downloadParams)

// downloadParams holds the DownloadDir() parameters and is used by objstore clients implementations.
type downloadParams struct {
	concurrency  int
	ignoredPaths []string
}

// WithDownloadIgnoredPaths is an option to set the paths to not be downloaded.
func WithDownloadIgnoredPaths(ignoredPaths ...string) DownloadOption {
	return func(params *downloadParams) {
		params.ignoredPaths = ignoredPaths
	}
}

// WithFetchConcurrency is an option to set the concurrency of the download operation.
func WithFetchConcurrency(concurrency int) DownloadOption {
	return func(params *downloadParams) {
		params.concurrency = concurrency
	}
}

func applyDownloadOptions(options ...DownloadOption) downloadParams {
	out := downloadParams{
		concurrency: 1,
	}
	for _, opt := range options {
		opt(&out)
	}
	return out
}

// UploadOption configures the provided params.
type UploadOption func(params *uploadParams)

// uploadParams holds the UploadDir() parameters and is used by objstore clients implementations.
type uploadParams struct {
	concurrency int
}

// WithUploadConcurrency is an option to set the concurrency of the upload operation.
func WithUploadConcurrency(concurrency int) UploadOption {
	return func(params *uploadParams) {
		params.concurrency = concurrency
	}
}

func applyUploadOptions(options ...UploadOption) uploadParams {
	out := uploadParams{
		concurrency: 1,
	}
	for _, opt := range options {
		opt(&out)
	}
	return out
}

type ObjectAttributes struct {
	// Size is the object size in bytes.
	Size int64 `json:"size"`

	// LastModified is the timestamp the object was last modified.
	LastModified time.Time `json:"last_modified"`
}

type IterObjectAttributes struct {
	Name         string
	lastModified time.Time
}

func (i *IterObjectAttributes) SetLastModified(t time.Time) {
	i.lastModified = t
}

// LastModified returns the timestamp the object was last modified. Returns false if the timestamp is not available.
func (i *IterObjectAttributes) LastModified() (time.Time, bool) {
	return i.lastModified, !i.lastModified.IsZero()
}

// TryToGetSize tries to get upfront size from reader.
// Some implementations may return only size of unread data in the reader, so it's best to call this method before
// doing any reading.
//
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
	case *bytes.Reader:
		// Returns length of unread data only.
		return int64(f.Len()), nil
	case *strings.Reader:
		return f.Size(), nil
	case ObjectSizer:
		return f.ObjectSize()
	case *io.LimitedReader:
		return f.N, nil
	}
	return 0, errors.Errorf("unsupported type of io.Reader: %T", r)
}

// ObjectSizer can return size of object.
type ObjectSizer interface {
	// ObjectSize returns the size of the object in bytes, or error if it is not available.
	ObjectSize() (int64, error)
}

type nopCloserWithObjectSize struct{ io.Reader }

func (nopCloserWithObjectSize) Close() error                 { return nil }
func (n nopCloserWithObjectSize) ObjectSize() (int64, error) { return TryToGetSize(n.Reader) }

// NopCloserWithSize returns a ReadCloser with a no-op Close method wrapping
// the provided Reader r. Returned ReadCloser also implements Size method.
func NopCloserWithSize(r io.Reader) io.ReadCloser {
	return nopCloserWithObjectSize{r}
}

// UploadDir uploads all files in srcdir to the bucket with into a top-level directory
// named dstdir. It is a caller responsibility to clean partial upload in case of failure.
func UploadDir(ctx context.Context, logger log.Logger, bkt Bucket, srcdir, dstdir string, options ...UploadOption) error {
	df, err := os.Stat(srcdir)
	opts := applyUploadOptions(options...)

	// The derived Context is canceled the first time a function passed to Go returns a non-nil error or the first
	// time Wait returns, whichever occurs first.
	g, ctx := errgroup.WithContext(ctx)
	g.SetLimit(opts.concurrency)

	if err != nil {
		return errors.Wrap(err, "stat dir")
	}
	if !df.IsDir() {
		return errors.Errorf("%s is not a directory", srcdir)
	}
	err = filepath.WalkDir(srcdir, func(src string, d fs.DirEntry, err error) error {
		g.Go(func() error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			srcRel, err := filepath.Rel(srcdir, src)
			if err != nil {
				return errors.Wrap(err, "getting relative path")
			}

			dst := path.Join(dstdir, filepath.ToSlash(srcRel))
			return UploadFile(ctx, logger, bkt, src, dst)
		})

		return nil
	})

	if err == nil {
		err = g.Wait()
	}

	return err
}

// UploadFile uploads the file with the given name to the bucket.
// It is a caller responsibility to clean partial upload in case of failure.
func UploadFile(ctx context.Context, logger log.Logger, bkt Bucket, src, dst string) error {
	r, err := os.Open(filepath.Clean(src))
	if err != nil {
		return errors.Wrapf(err, "open file %s", src)
	}
	defer logerrcapture.Do(logger, r.Close, "close file %s", src)

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
	defer logerrcapture.Do(logger, rc.Close, "close block's file reader")

	f, err := os.Create(dst)
	if err != nil {
		return errors.Wrapf(err, "create file %s", dst)
	}
	defer func() {
		if err != nil {
			if rerr := os.Remove(dst); rerr != nil {
				level.Warn(logger).Log("msg", "failed to remove partially downloaded file", "file", dst, "err", rerr)
			}
		}
	}()
	defer logerrcapture.Do(logger, f.Close, "close block's output file")

	if _, err = io.Copy(f, rc); err != nil {
		return errors.Wrapf(err, "copy object to file %s", src)
	}
	return nil
}

// DownloadDir downloads all object found in the directory into the local directory.
func DownloadDir(ctx context.Context, logger log.Logger, bkt BucketReader, originalSrc, src, dst string, options ...DownloadOption) error {
	if err := os.MkdirAll(dst, 0750); err != nil {
		return errors.Wrap(err, "create dir")
	}
	opts := applyDownloadOptions(options...)

	// The derived Context is canceled the first time a function passed to Go returns a non-nil error or the first
	// time Wait returns, whichever occurs first.
	g, ctx := errgroup.WithContext(ctx)
	g.SetLimit(opts.concurrency)

	var downloadedFiles []string
	var m sync.Mutex

	err := bkt.Iter(ctx, src, func(name string) error {
		g.Go(func() error {
			dst := filepath.Join(dst, filepath.Base(name))
			if strings.HasSuffix(name, DirDelim) {
				if err := DownloadDir(ctx, logger, bkt, originalSrc, name, dst, options...); err != nil {
					return err
				}
				m.Lock()
				downloadedFiles = append(downloadedFiles, dst)
				m.Unlock()
				return nil
			}
			for _, ignoredPath := range opts.ignoredPaths {
				if ignoredPath == strings.TrimPrefix(name, string(originalSrc)+DirDelim) {
					level.Debug(logger).Log("msg", "not downloading again because a provided path matches this one", "file", name)
					return nil
				}
			}
			if err := DownloadFile(ctx, logger, bkt, name, dst); err != nil {
				return err
			}

			m.Lock()
			downloadedFiles = append(downloadedFiles, dst)
			m.Unlock()
			return nil
		})
		return nil
	})

	if err == nil {
		err = g.Wait()
	}

	if err != nil {
		downloadedFiles = append(downloadedFiles, dst) // Last, clean up the root dst directory.
		// Best-effort cleanup if the download failed.
		for _, f := range downloadedFiles {
			if rerr := os.RemoveAll(f); rerr != nil {
				level.Warn(logger).Log("msg", "failed to remove file on partial dir download error", "file", f, "err", rerr)
			}
		}
		return err
	}

	return nil
}

// IsOpFailureExpectedFunc allows to mark certain errors as expected, so they will not increment objstore_bucket_operation_failures_total metric.
type IsOpFailureExpectedFunc func(error) bool

var _ InstrumentedBucket = &metricBucket{}

func BucketMetrics(reg prometheus.Registerer, name string) *Metrics {
	return &Metrics{
		isOpFailureExpected: func(err error) bool { return false },
		ops: promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
			Name:        "objstore_bucket_operations_total",
			Help:        "Total number of all attempted operations against a bucket.",
			ConstLabels: prometheus.Labels{"bucket": name},
		}, []string{"operation"}),

		opsFailures: promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
			Name:        "objstore_bucket_operation_failures_total",
			Help:        "Total number of operations against a bucket that failed, but were not expected to fail in certain way from caller perspective. Those errors have to be investigated.",
			ConstLabels: prometheus.Labels{"bucket": name},
		}, []string{"operation"}),

		opsFetchedBytes: promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
			Name:        "objstore_bucket_operation_fetched_bytes_total",
			Help:        "Total number of bytes fetched from bucket, per operation.",
			ConstLabels: prometheus.Labels{"bucket": name},
		}, []string{"operation"}),

		opsTransferredBytes: promauto.With(reg).NewHistogramVec(prometheus.HistogramOpts{
			Name:        "objstore_bucket_operation_transferred_bytes",
			Help:        "Number of bytes transferred from/to bucket per operation.",
			ConstLabels: prometheus.Labels{"bucket": name},
			Buckets:     prometheus.ExponentialBuckets(2<<14, 2, 16), // 32KiB, 64KiB, ... 1GiB
			// Use factor=2 for native histograms, which gives similar buckets as the original exponential buckets.
			NativeHistogramBucketFactor:     2,
			NativeHistogramMaxBucketNumber:  100,
			NativeHistogramMinResetDuration: 1 * time.Hour,
		}, []string{"operation"}),

		opsDuration: promauto.With(reg).NewHistogramVec(prometheus.HistogramOpts{
			Name:        "objstore_bucket_operation_duration_seconds",
			Help:        "Duration of successful operations against the bucket per operation - iter operations include time spent on each callback.",
			ConstLabels: prometheus.Labels{"bucket": name},
			Buckets:     []float64{0.001, 0.01, 0.1, 0.3, 0.6, 1, 3, 6, 9, 20, 30, 60, 90, 120},
			// Use the recommended defaults for native histograms with 10% growth factor.
			NativeHistogramBucketFactor:     1.1,
			NativeHistogramMaxBucketNumber:  100,
			NativeHistogramMinResetDuration: 1 * time.Hour,
		}, []string{"operation"}),

		lastSuccessfulUploadTime: promauto.With(reg).NewGauge(prometheus.GaugeOpts{
			Name:        "objstore_bucket_last_successful_upload_time",
			Help:        "Second timestamp of the last successful upload to the bucket.",
			ConstLabels: prometheus.Labels{"bucket": name},
		}),
	}
}

// WrapWithMetrics takes a bucket and registers metrics with the given registry for
// operations run against the bucket.
func WrapWithMetrics(b Bucket, reg prometheus.Registerer, name string) *metricBucket {
	metrics := BucketMetrics(reg, name)
	return wrapWithMetrics(b, metrics)
}

// WrapWith takes a `bucket` and `metrics` that returns instrumented bucket.
// Similar to WrapWithMetrics, but `metrics` can be passed separately as an argument.
func WrapWith(b Bucket, metrics *Metrics) *metricBucket {
	return wrapWithMetrics(b, metrics)
}

func wrapWithMetrics(b Bucket, metrics *Metrics) *metricBucket {
	bkt := &metricBucket{
		bkt:     b,
		metrics: metrics,
	}

	for _, op := range []string{
		OpIter,
		OpGet,
		OpGetRange,
		OpExists,
		OpUpload,
		OpDelete,
		OpAttributes,
	} {
		bkt.metrics.ops.WithLabelValues(op)
		bkt.metrics.opsFailures.WithLabelValues(op)
		bkt.metrics.opsDuration.WithLabelValues(op)
		bkt.metrics.opsFetchedBytes.WithLabelValues(op)
	}

	// fetched bytes only relevant for get, getrange and upload
	for _, op := range []string{
		OpGet,
		OpGetRange,
		OpUpload,
	} {
		bkt.metrics.opsTransferredBytes.WithLabelValues(op)
	}
	return bkt
}

type Metrics struct {
	ops                 *prometheus.CounterVec
	opsFailures         *prometheus.CounterVec
	isOpFailureExpected IsOpFailureExpectedFunc

	opsFetchedBytes          *prometheus.CounterVec
	opsTransferredBytes      *prometheus.HistogramVec
	opsDuration              *prometheus.HistogramVec
	lastSuccessfulUploadTime prometheus.Gauge
}

type metricBucket struct {
	bkt     Bucket
	metrics *Metrics
}

func (b *metricBucket) Provider() ObjProvider {
	return b.bkt.Provider()
}

func (b *metricBucket) WithExpectedErrs(fn IsOpFailureExpectedFunc) Bucket {
	return &metricBucket{
		bkt: b.bkt,
		metrics: &Metrics{
			ops:                      b.metrics.ops,
			opsFailures:              b.metrics.opsFailures,
			opsFetchedBytes:          b.metrics.opsFetchedBytes,
			opsTransferredBytes:      b.metrics.opsTransferredBytes,
			isOpFailureExpected:      fn,
			opsDuration:              b.metrics.opsDuration,
			lastSuccessfulUploadTime: b.metrics.lastSuccessfulUploadTime,
		},
	}
}

func (b *metricBucket) ReaderWithExpectedErrs(fn IsOpFailureExpectedFunc) BucketReader {
	return b.WithExpectedErrs(fn)
}

func (b *metricBucket) Iter(ctx context.Context, dir string, f func(string) error, options ...IterOption) error {
	const op = OpIter
	b.metrics.ops.WithLabelValues(op).Inc()

	timer := prometheus.NewTimer(b.metrics.opsDuration.WithLabelValues(op))
	defer timer.ObserveDuration()

	err := b.bkt.Iter(ctx, dir, f, options...)
	if err != nil {
		if !b.metrics.isOpFailureExpected(err) && ctx.Err() != context.Canceled {
			b.metrics.opsFailures.WithLabelValues(op).Inc()
		}
	}
	return err
}

func (b *metricBucket) IterWithAttributes(ctx context.Context, dir string, f func(IterObjectAttributes) error, options ...IterOption) error {
	const op = OpIter
	b.metrics.ops.WithLabelValues(op).Inc()

	timer := prometheus.NewTimer(b.metrics.opsDuration.WithLabelValues(op))
	defer timer.ObserveDuration()

	err := b.bkt.IterWithAttributes(ctx, dir, f, options...)
	if err != nil {
		if !b.metrics.isOpFailureExpected(err) && ctx.Err() != context.Canceled {
			b.metrics.opsFailures.WithLabelValues(op).Inc()
		}
	}

	return err
}

func (b *metricBucket) SupportedIterOptions() []IterOptionType {
	return b.bkt.SupportedIterOptions()
}

func (b *metricBucket) Attributes(ctx context.Context, name string) (ObjectAttributes, error) {
	const op = OpAttributes
	b.metrics.ops.WithLabelValues(op).Inc()

	start := time.Now()
	attrs, err := b.bkt.Attributes(ctx, name)
	if err != nil {
		if !b.metrics.isOpFailureExpected(err) && ctx.Err() != context.Canceled {
			b.metrics.opsFailures.WithLabelValues(op).Inc()
		}
		return attrs, err
	}
	b.metrics.opsDuration.WithLabelValues(op).Observe(time.Since(start).Seconds())
	return attrs, nil
}

func (b *metricBucket) Get(ctx context.Context, name string) (io.ReadCloser, error) {
	const op = OpGet
	b.metrics.ops.WithLabelValues(op).Inc()

	start := time.Now()

	rc, err := b.bkt.Get(ctx, name)
	if err != nil {
		if !b.metrics.isOpFailureExpected(err) && ctx.Err() != context.Canceled {
			b.metrics.opsFailures.WithLabelValues(op).Inc()
		}
		b.metrics.opsDuration.WithLabelValues(op).Observe(time.Since(start).Seconds())
		return nil, err
	}
	return newTimingReader(
		start,
		rc,
		true,
		op,
		b.metrics.opsDuration,
		b.metrics.opsFailures,
		b.metrics.isOpFailureExpected,
		b.metrics.opsFetchedBytes,
		b.metrics.opsTransferredBytes,
	), nil
}

func (b *metricBucket) GetRange(ctx context.Context, name string, off, length int64) (io.ReadCloser, error) {
	const op = OpGetRange
	b.metrics.ops.WithLabelValues(op).Inc()

	start := time.Now()

	rc, err := b.bkt.GetRange(ctx, name, off, length)
	if err != nil {
		if !b.metrics.isOpFailureExpected(err) && ctx.Err() != context.Canceled {
			b.metrics.opsFailures.WithLabelValues(op).Inc()
		}
		b.metrics.opsDuration.WithLabelValues(op).Observe(time.Since(start).Seconds())
		return nil, err
	}
	return newTimingReader(
		start,
		rc,
		true,
		op,
		b.metrics.opsDuration,
		b.metrics.opsFailures,
		b.metrics.isOpFailureExpected,
		b.metrics.opsFetchedBytes,
		b.metrics.opsTransferredBytes,
	), nil
}

func (b *metricBucket) GetAndReplace(ctx context.Context, name string, f func(io.Reader) (io.Reader, error)) error {
	return b.bkt.GetAndReplace(ctx, name, f)
}

func (b *metricBucket) Exists(ctx context.Context, name string) (bool, error) {
	const op = OpExists
	b.metrics.ops.WithLabelValues(op).Inc()

	start := time.Now()
	ok, err := b.bkt.Exists(ctx, name)
	if err != nil {
		if !b.metrics.isOpFailureExpected(err) && ctx.Err() != context.Canceled {
			b.metrics.opsFailures.WithLabelValues(op).Inc()
		}
		return false, err
	}
	b.metrics.opsDuration.WithLabelValues(op).Observe(time.Since(start).Seconds())
	return ok, nil
}

func (b *metricBucket) Upload(ctx context.Context, name string, r io.Reader) error {
	const op = OpUpload
	b.metrics.ops.WithLabelValues(op).Inc()

	start := time.Now()

	trc := newTimingReader(
		start,
		r,
		false,
		op,
		b.metrics.opsDuration,
		b.metrics.opsFailures,
		b.metrics.isOpFailureExpected,
		nil,
		b.metrics.opsTransferredBytes,
	)
	defer trc.Close()
	err := b.bkt.Upload(ctx, name, trc)
	if err != nil {
		if !b.metrics.isOpFailureExpected(err) && ctx.Err() != context.Canceled {
			b.metrics.opsFailures.WithLabelValues(op).Inc()
		}
		return err
	}
	b.metrics.lastSuccessfulUploadTime.SetToCurrentTime()

	return nil
}

func (b *metricBucket) Delete(ctx context.Context, name string) error {
	const op = OpDelete
	b.metrics.ops.WithLabelValues(op).Inc()

	start := time.Now()
	if err := b.bkt.Delete(ctx, name); err != nil {
		if !b.metrics.isOpFailureExpected(err) && ctx.Err() != context.Canceled {
			b.metrics.opsFailures.WithLabelValues(op).Inc()
		}
		return err
	}
	b.metrics.opsDuration.WithLabelValues(op).Observe(time.Since(start).Seconds())

	return nil
}

func (b *metricBucket) IsObjNotFoundErr(err error) bool {
	return b.bkt.IsObjNotFoundErr(err)
}

func (b *metricBucket) IsAccessDeniedErr(err error) bool {
	return b.bkt.IsAccessDeniedErr(err)
}

func (b *metricBucket) Close() error {
	return b.bkt.Close()
}

func (b *metricBucket) Name() string {
	return b.bkt.Name()
}

type timingReader struct {
	io.Reader

	// closeReader holds whether the wrapper io.Reader should be closed when
	// Close() is called on the timingReader.
	closeReader bool

	objSize    int64
	objSizeErr error

	alreadyGotErr bool

	start             time.Time
	op                string
	readBytes         int64
	duration          *prometheus.HistogramVec
	failed            *prometheus.CounterVec
	isFailureExpected IsOpFailureExpectedFunc
	fetchedBytes      *prometheus.CounterVec
	transferredBytes  *prometheus.HistogramVec
}

func newTimingReader(start time.Time, r io.Reader, closeReader bool, op string, dur *prometheus.HistogramVec, failed *prometheus.CounterVec, isFailureExpected IsOpFailureExpectedFunc, fetchedBytes *prometheus.CounterVec, transferredBytes *prometheus.HistogramVec) io.ReadCloser {
	// Initialize the metrics with 0.
	dur.WithLabelValues(op)
	failed.WithLabelValues(op)
	objSize, objSizeErr := TryToGetSize(r)

	trc := timingReader{
		Reader:            r,
		closeReader:       closeReader,
		objSize:           objSize,
		objSizeErr:        objSizeErr,
		start:             start,
		op:                op,
		duration:          dur,
		failed:            failed,
		isFailureExpected: isFailureExpected,
		fetchedBytes:      fetchedBytes,
		transferredBytes:  transferredBytes,
		readBytes:         0,
	}

	_, isSeeker := r.(io.Seeker)
	_, isReaderAt := r.(io.ReaderAt)
	if isSeeker && isReaderAt {
		// The assumption is that in most cases when io.ReaderAt() is implemented then
		// io.Seeker is implemented too (e.g. os.File).
		return &timingReaderSeekerReaderAt{timingReaderSeeker: timingReaderSeeker{timingReader: trc}}
	}
	if isSeeker {
		return &timingReaderSeeker{timingReader: trc}
	}
	if _, isWriterTo := r.(io.WriterTo); isWriterTo {
		return &timingReaderWriterTo{timingReader: trc}
	}

	return &trc
}

func (r *timingReader) ObjectSize() (int64, error) {
	return r.objSize, r.objSizeErr
}

func (r *timingReader) Close() error {
	var closeErr error

	// Call the wrapped reader if it implements Close(), only if we've been asked to close it.
	if closer, ok := r.Reader.(io.Closer); r.closeReader && ok {
		closeErr = closer.Close()

		if !r.alreadyGotErr && closeErr != nil {
			r.failed.WithLabelValues(r.op).Inc()
			r.alreadyGotErr = true
		}
	}

	// Track duration and transferred bytes only if no error occurred.
	if !r.alreadyGotErr {
		r.duration.WithLabelValues(r.op).Observe(time.Since(r.start).Seconds())
		r.transferredBytes.WithLabelValues(r.op).Observe(float64(r.readBytes))

		// Trick to tracking metrics multiple times in case Close() gets called again.
		r.alreadyGotErr = true
	}

	return closeErr
}

func (r *timingReader) Read(b []byte) (n int, err error) {
	n, err = r.Reader.Read(b)
	r.updateMetrics(n, err)
	return n, err
}

func (r *timingReader) updateMetrics(n int, err error) {
	if r.fetchedBytes != nil {
		r.fetchedBytes.WithLabelValues(r.op).Add(float64(n))
	}
	r.readBytes += int64(n)

	// Report metric just once.
	if !r.alreadyGotErr && err != nil && err != io.EOF {
		if !r.isFailureExpected(err) && !errors.Is(err, context.Canceled) {
			r.failed.WithLabelValues(r.op).Inc()
		}
		r.alreadyGotErr = true
	}
}

type timingReaderSeeker struct {
	timingReader
}

func (rsc *timingReaderSeeker) Seek(offset int64, whence int) (int64, error) {
	return (rsc.Reader).(io.Seeker).Seek(offset, whence)
}

type timingReaderSeekerReaderAt struct {
	timingReaderSeeker
}

func (rsc *timingReaderSeekerReaderAt) ReadAt(p []byte, off int64) (int, error) {
	return (rsc.Reader).(io.ReaderAt).ReadAt(p, off)
}

type timingReaderWriterTo struct {
	timingReader
}

func (t *timingReaderWriterTo) WriteTo(w io.Writer) (n int64, err error) {
	n, err = (t.Reader).(io.WriterTo).WriteTo(w)
	t.timingReader.updateMetrics(int(n), err)
	return n, err
}

type ObjectSizerReadCloser struct {
	io.ReadCloser
	Size func() (int64, error)
}

// ObjectSize implement ObjectSizer.
func (o ObjectSizerReadCloser) ObjectSize() (int64, error) {
	if o.Size == nil {
		return 0, errors.New("unknown size")
	}

	return o.Size()
}
