package dataobj

import (
	"context"
	"errors"
	"io"

	"github.com/thanos-io/objstore"

	"github.com/grafana/loki/v3/pkg/xcap"
)

// NewInstrumentedBucketReader returns bucket wrapped to record its object-store
// requests. Every Attributes, Get and GetRange call records one request in the
// [xcap.Region] of the call's context. The reader records the request before it
// issues it, so a request that never returns still counts.
//
// The reader also records one failure when a request fails with an unexpected
// error. The error can come from the call, or from reading the body that Get or
// GetRange returned. The reader does not count these errors as failures:
//   - an error that says the object does not exist, because the object store
//     answered;
//   - an error the caller causes, which is a context error returned while the
//     context of the call is done, such as a canceled request;
//   - the end of the body.
//
// When the caller closes a body, the reader also records the bytes it delivered
// in [StatObjectBytesDownloaded].
//
// Other methods, such as Iter and Exists, pass through and record nothing.
func NewInstrumentedBucketReader(bucket objstore.BucketReader) objstore.BucketReader {
	// Do not wrap twice, or requests count twice. A decorator around an instrumented reader hides it
	// from this check.
	if instrumented, ok := bucket.(*instrumentedBucketReader); ok {
		return instrumented
	}
	return &instrumentedBucketReader{BucketReader: bucket}
}

type instrumentedBucketReader struct {
	objstore.BucketReader
}

func (r *instrumentedBucketReader) Attributes(ctx context.Context, name string) (objstore.ObjectAttributes, error) {
	r.trackRequest(ctx, StatObjectRequestsAttributes)

	attrs, err := r.BucketReader.Attributes(ctx, name)
	if err != nil {
		r.trackFailure(ctx, StatObjectRequestFailuresAttributes, err)
		return objstore.ObjectAttributes{}, err
	}

	return attrs, err
}

func (r *instrumentedBucketReader) Get(ctx context.Context, name string) (io.ReadCloser, error) {
	r.trackRequest(ctx, StatObjectRequestsGet)

	rc, err := r.BucketReader.Get(ctx, name)
	if err != nil {
		r.trackFailure(ctx, StatObjectRequestFailuresGet, err)
		return nil, err
	}

	return newInstrumentedReadCloser(ctx, rc, r, StatObjectRequestFailuresGet), nil
}

func (r *instrumentedBucketReader) GetRange(ctx context.Context, name string, offset, length int64) (io.ReadCloser, error) {
	r.trackRequest(ctx, StatObjectRequestsGetRange)

	rc, err := r.BucketReader.GetRange(ctx, name, offset, length)
	if err != nil {
		r.trackFailure(ctx, StatObjectRequestFailuresGetRange, err)
		return nil, err
	}

	return newInstrumentedReadCloser(ctx, rc, r, StatObjectRequestFailuresGetRange), nil
}

// trackRequest records one request in the region of ctx. It does nothing when
// ctx carries no region.
func (r *instrumentedBucketReader) trackRequest(ctx context.Context, requests *xcap.StatisticInt64) {
	xcap.RegionFromContext(ctx).Record(requests.Observe(1))
}

// trackFailure records one failure in the region of ctx unless err is nil, a
// not-found error, or a context error returned while ctx is done. It reports
// whether err counts as a failure, and records nothing when ctx carries no region.
func (r *instrumentedBucketReader) trackFailure(ctx context.Context, failures *xcap.StatisticInt64, err error) bool {
	if err == nil || r.BucketReader.IsObjNotFoundErr(err) {
		return false
	}
	if ctx.Err() != nil && (errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)) {
		return false
	}
	xcap.RegionFromContext(ctx).Record(failures.Observe(1))
	return true
}

// instrumentedReadCloser wraps an [io.ReadCloser] returned by an object-store
// request and counts the bytes read from it. When the body is closed, it records
// the total in [StatObjectBytesDownloaded], in the region of the request's
// context if any. The total reflects the bytes transferred from storage, not the
// requested range size.
//
// The body also records one failure of its request when a read fails with an
// unexpected error.
type instrumentedReadCloser struct {
	inner  io.ReadCloser
	region *xcap.Region
	n      int64

	// ctx, reader and failures let Read record the failure. failureTracked is true once
	// Read has recorded it.
	ctx            context.Context
	reader         *instrumentedBucketReader
	failures       *xcap.StatisticInt64
	failureTracked bool
}

func newInstrumentedReadCloser(ctx context.Context, rc io.ReadCloser, reader *instrumentedBucketReader, failures *xcap.StatisticInt64) *instrumentedReadCloser {
	return &instrumentedReadCloser{
		inner:    rc,
		region:   xcap.RegionFromContext(ctx),
		ctx:      ctx,
		reader:   reader,
		failures: failures,
	}
}

func (rc *instrumentedReadCloser) Read(p []byte) (int, error) {
	n, err := rc.inner.Read(p)
	rc.n += int64(n)
	if err != nil && err != io.EOF && !rc.failureTracked {
		rc.failureTracked = rc.reader.trackFailure(rc.ctx, rc.failures, err)
	}
	return n, err
}

func (rc *instrumentedReadCloser) Close() error {
	if rc.n > 0 {
		rc.region.Record(StatObjectBytesDownloaded.Observe(rc.n))
		rc.n = 0
	}
	return rc.inner.Close()
}
