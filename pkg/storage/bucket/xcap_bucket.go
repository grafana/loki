package bucket

import (
	"context"
	"io"

	"github.com/thanos-io/objstore"

	"github.com/grafana/loki/v3/pkg/xcap"
)

// Bucket operation statistic names.
const (
	StatBucketGet        = "bucket_get"
	StatBucketGetRange   = "bucket_get_range"
	StatBucketIter       = "bucket_iter"
	StatBucketExists     = "bucket_exists"
	StatBucketUpload     = "bucket_upload"
	StatBucketAttributes = "bucket_attributes"
)

// Statistics for tracking bucket operation counts.
var (
	statBucketGet        = xcap.NewStatisticInt64(StatBucketGet, xcap.AggregationTypeSum)
	statBucketGetRange   = xcap.NewStatisticInt64(StatBucketGetRange, xcap.AggregationTypeSum)
	statBucketIter       = xcap.NewStatisticInt64(StatBucketIter, xcap.AggregationTypeSum)
	statBucketExists     = xcap.NewStatisticInt64(StatBucketExists, xcap.AggregationTypeSum)
	statBucketUpload     = xcap.NewStatisticInt64(StatBucketUpload, xcap.AggregationTypeSum)
	statBucketAttributes = xcap.NewStatisticInt64(StatBucketAttributes, xcap.AggregationTypeSum)
)

// XcapBucket wraps an objstore.Bucket and records request counts to the xcap
// Region found in the context. If no Region is present in the context, the
// wrapper simply delegates to the underlying bucket without recording.
type XcapBucket struct {
	bkt objstore.Bucket
}

// NewXcapBucket creates a new XcapBucket that wraps the given bucket and records
// request counts to xcap regions found in the context.
func NewXcapBucket(bkt objstore.Bucket) *XcapBucket {
	return &XcapBucket{bkt: bkt}
}

// recordOp records a single operation to the xcap region if present in the context.
func recordOp(ctx context.Context, stat *xcap.StatisticInt64) {
	region := xcap.RegionFromContext(ctx)
	if region == nil {
		return
	}
	region.Record(stat.Observe(1))
}

// Provider returns the underlying bucket provider.
func (b *XcapBucket) Provider() objstore.ObjProvider {
	return b.bkt.Provider()
}

// Close closes the underlying bucket.
func (b *XcapBucket) Close() error {
	return b.bkt.Close()
}

// Iter calls f for each entry in the given directory (not recursive.).
func (b *XcapBucket) Iter(ctx context.Context, dir string, f func(string) error, options ...objstore.IterOption) error {
	recordOp(ctx, statBucketIter)
	return b.bkt.Iter(ctx, dir, f, options...)
}

// IterWithAttributes calls f for each entry in the given directory similar to Iter.
func (b *XcapBucket) IterWithAttributes(ctx context.Context, dir string, f func(objstore.IterObjectAttributes) error, options ...objstore.IterOption) error {
	recordOp(ctx, statBucketIter)
	return b.bkt.IterWithAttributes(ctx, dir, f, options...)
}

// SupportedIterOptions returns a list of supported IterOptions by the underlying provider.
func (b *XcapBucket) SupportedIterOptions() []objstore.IterOptionType {
	return b.bkt.SupportedIterOptions()
}

// Get returns a reader for the given object name.
func (b *XcapBucket) Get(ctx context.Context, name string) (io.ReadCloser, error) {
	recordOp(ctx, statBucketGet)
	return b.bkt.Get(ctx, name)
}

// GetRange returns a new range reader for the given object name and range.
func (b *XcapBucket) GetRange(ctx context.Context, name string, off, length int64) (io.ReadCloser, error) {
	recordOp(ctx, statBucketGetRange)
	return b.bkt.GetRange(ctx, name, off, length)
}

// GetAndReplace an existing object with a new object.
func (b *XcapBucket) GetAndReplace(ctx context.Context, name string, f func(io.ReadCloser) (io.ReadCloser, error)) error {
	// GetAndReplace involves both a Get and an Upload internally
	recordOp(ctx, statBucketGet)
	recordOp(ctx, statBucketUpload)
	return b.bkt.GetAndReplace(ctx, name, f)
}

// Exists checks if the given object exists in the bucket.
func (b *XcapBucket) Exists(ctx context.Context, name string) (bool, error) {
	recordOp(ctx, statBucketExists)
	return b.bkt.Exists(ctx, name)
}

// IsObjNotFoundErr returns true if error means that object is not found.
func (b *XcapBucket) IsObjNotFoundErr(err error) bool {
	return b.bkt.IsObjNotFoundErr(err)
}

// IsAccessDeniedErr returns true if access to object is denied.
func (b *XcapBucket) IsAccessDeniedErr(err error) bool {
	return b.bkt.IsAccessDeniedErr(err)
}

// Attributes returns information about the specified object.
func (b *XcapBucket) Attributes(ctx context.Context, name string) (objstore.ObjectAttributes, error) {
	recordOp(ctx, statBucketAttributes)
	return b.bkt.Attributes(ctx, name)
}

// Upload uploads the contents of the reader as an object into the bucket.
func (b *XcapBucket) Upload(ctx context.Context, name string, r io.Reader) error {
	recordOp(ctx, statBucketUpload)
	return b.bkt.Upload(ctx, name, r)
}

// Delete removes the object with the given name.
func (b *XcapBucket) Delete(ctx context.Context, name string) error {
	return b.bkt.Delete(ctx, name)
}

// Name returns the bucket name for the provider.
func (b *XcapBucket) Name() string {
	return b.bkt.Name()
}

// Ensure XcapBucket implements objstore.Bucket interface.
var _ objstore.Bucket = (*XcapBucket)(nil)
