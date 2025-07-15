package bucket

import (
	"context"
	"io"
	"strings"

	"github.com/thanos-io/objstore"
)

type PrefixedBucketClient struct {
	bucket objstore.Bucket
	prefix string
}

// NewPrefixedBucketClient returns a new PrefixedBucketClient.
func NewPrefixedBucketClient(bucket objstore.Bucket, prefix string) *PrefixedBucketClient {
	return &PrefixedBucketClient{
		bucket: bucket,
		prefix: prefix,
	}
}

func (b *PrefixedBucketClient) fullName(name string) string {
	return b.prefix + objstore.DirDelim + name
}

// Close implements io.Closer
func (b *PrefixedBucketClient) Close() error {
	return b.bucket.Close()
}

// Upload the contents of the reader as an object into the bucket.
func (b *PrefixedBucketClient) Upload(ctx context.Context, name string, r io.Reader) (err error) {
	err = b.bucket.Upload(ctx, b.fullName(name), r)
	return
}

// GetAndReplace is a helper function that gets an object from the bucket and replaces it with a new reader.
func (b *PrefixedBucketClient) GetAndReplace(ctx context.Context, name string, fn func(existing io.Reader) (io.Reader, error)) error {
	return b.bucket.GetAndReplace(ctx, b.fullName(name), fn)
}

// Delete removes the object with the given name.
func (b *PrefixedBucketClient) Delete(ctx context.Context, name string) error {
	return b.bucket.Delete(ctx, b.fullName(name))
}

// Name returns the bucket name for the provider.
func (b *PrefixedBucketClient) Name() string { return b.bucket.Name() }

// SupportedIterOptions returns a list of supported IterOptions by the underlying provider.
func (b *PrefixedBucketClient) SupportedIterOptions() []objstore.IterOptionType {
	return b.bucket.SupportedIterOptions()
}

// Iter calls f for each entry in the given directory (not recursive.). The argument to f is the full
// object name including the prefix of the inspected directory. The configured prefix will be stripped
// before supplied function is applied.
func (b *PrefixedBucketClient) Iter(ctx context.Context, dir string, f func(string) error, options ...objstore.IterOption) error {
	return b.bucket.Iter(ctx, b.fullName(dir), func(s string) error {
		return f(strings.TrimPrefix(s, b.prefix+objstore.DirDelim))
	}, options...)
}

// IterWithAttributes calls f for each entry in the given directory similar to Iter.
// In addition to Name, it also includes requested object attributes in the argument to f.
//
// Attributes can be requested using IterOption.
// Not all IterOptions are supported by all providers, requesting for an unsupported option will fail with ErrOptionNotSupported.
func (b *PrefixedBucketClient) IterWithAttributes(ctx context.Context, dir string, f func(attrs objstore.IterObjectAttributes) error, options ...objstore.IterOption) error {
	return b.bucket.IterWithAttributes(ctx, b.fullName(dir), func(attrs objstore.IterObjectAttributes) error {
		attrs.Name = strings.TrimPrefix(attrs.Name, b.prefix+objstore.DirDelim)
		return f(attrs)
	}, options...)
}

// Get returns a reader for the given object name.
func (b *PrefixedBucketClient) Get(ctx context.Context, name string) (io.ReadCloser, error) {
	return b.bucket.Get(ctx, b.fullName(name))
}

// GetRange returns a new range reader for the given object name and range.
func (b *PrefixedBucketClient) GetRange(ctx context.Context, name string, off, length int64) (io.ReadCloser, error) {
	return b.bucket.GetRange(ctx, b.fullName(name), off, length)
}

// Exists checks if the given object exists in the bucket.
func (b *PrefixedBucketClient) Exists(ctx context.Context, name string) (bool, error) {
	return b.bucket.Exists(ctx, b.fullName(name))
}

// IsObjNotFoundErr returns true if error means that object is not found. Relevant to Get operations.
func (b *PrefixedBucketClient) IsObjNotFoundErr(err error) bool {
	return b.bucket.IsObjNotFoundErr(err)
}

// Attributes returns attributes of the specified object.
func (b *PrefixedBucketClient) Attributes(ctx context.Context, name string) (objstore.ObjectAttributes, error) {
	return b.bucket.Attributes(ctx, b.fullName(name))
}

// WithExpectedErrs allows to specify a filter that marks certain errors as expected, so it will not increment
// thanos_objstore_bucket_operation_failures_total metric.
func (b *PrefixedBucketClient) ReaderWithExpectedErrs(fn objstore.IsOpFailureExpectedFunc) objstore.BucketReader {
	return b.WithExpectedErrs(fn)
}

// IsAccessDeniedErr returns true if access to object is denied.
func (b *PrefixedBucketClient) IsAccessDeniedErr(err error) bool {
	return b.bucket.IsAccessDeniedErr(err)
}

// Provider returns the provider of the bucket.
func (b *PrefixedBucketClient) Provider() objstore.ObjProvider {
	return b.bucket.Provider()
}

// ReaderWithExpectedErrs allows to specify a filter that marks certain errors as expected, so it will not increment
// thanos_objstore_bucket_operation_failures_total metric.
func (b *PrefixedBucketClient) WithExpectedErrs(fn objstore.IsOpFailureExpectedFunc) objstore.Bucket {
	if ib, ok := b.bucket.(objstore.InstrumentedBucket); ok {
		return &PrefixedBucketClient{
			bucket: ib.WithExpectedErrs(fn),
			prefix: b.prefix,
		}
	}
	return b
}
