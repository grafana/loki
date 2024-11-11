package bucket

import (
	"context"
	"io"
	"slices"
	"strings"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/pkg/errors"
	"github.com/thanos-io/objstore"

	"github.com/grafana/loki/v3/pkg/storage/chunk/client"
)

type ObjectClientAdapter struct {
	bucket, hedgedBucket objstore.Bucket
	logger               log.Logger
	isRetryableErr       func(err error) bool
}

func NewObjectClientAdapter(bucket, hedgedBucket objstore.Bucket, logger log.Logger, opts ...ClientOptions) *ObjectClientAdapter {
	if hedgedBucket == nil {
		hedgedBucket = bucket
	}

	o := &ObjectClientAdapter{
		bucket:       bucket,
		hedgedBucket: hedgedBucket,
		logger:       log.With(logger, "component", "bucket_to_object_client_adapter"),
		// default to no retryable errors. Override with WithRetryableErrFunc
		isRetryableErr: func(_ error) bool {
			return false
		},
	}

	for _, opt := range opts {
		opt(o)
	}

	return o
}

type ClientOptions func(*ObjectClientAdapter)

func WithRetryableErrFunc(f func(err error) bool) ClientOptions {
	return func(o *ObjectClientAdapter) {
		o.isRetryableErr = f
	}
}

func (o *ObjectClientAdapter) Stop() {
}

// ObjectExists checks if a given objectKey exists in the bucket
func (o *ObjectClientAdapter) ObjectExists(ctx context.Context, objectKey string) (bool, error) {
	return o.bucket.Exists(ctx, objectKey)
}

// GetAttributes returns the attributes of the specified object key from the configured bucket.
func (o *ObjectClientAdapter) GetAttributes(ctx context.Context, objectKey string) (client.ObjectAttributes, error) {
	attr := client.ObjectAttributes{}
	thanosAttr, err := o.hedgedBucket.Attributes(ctx, objectKey)
	if err != nil {
		return attr, err
	}

	attr.Size = thanosAttr.Size
	return attr, nil
}

// PutObject puts the specified bytes into the configured bucket at the provided key
func (o *ObjectClientAdapter) PutObject(ctx context.Context, objectKey string, object io.Reader) error {
	return o.bucket.Upload(ctx, objectKey, object)
}

// GetObject returns a reader and the size for the specified object key from the configured bucket.
// size is set to -1 if it cannot be succefully determined, it is up to the caller to check this value before using it.
func (o *ObjectClientAdapter) GetObject(ctx context.Context, objectKey string) (io.ReadCloser, int64, error) {
	reader, err := o.hedgedBucket.Get(ctx, objectKey)
	if err != nil {
		return nil, 0, err
	}

	size, err := objstore.TryToGetSize(reader)
	if err != nil {
		size = -1
		level.Warn(o.logger).Log("msg", "failed to get size of object", "err", err)
	}

	return reader, size, err
}

func (o *ObjectClientAdapter) GetObjectRange(ctx context.Context, objectKey string, offset, length int64) (io.ReadCloser, error) {
	return o.hedgedBucket.GetRange(ctx, objectKey, offset, length)
}

// List objects with given prefix.
func (o *ObjectClientAdapter) List(ctx context.Context, prefix, delimiter string) ([]client.StorageObject, []client.StorageCommonPrefix, error) {
	var storageObjects []client.StorageObject
	var commonPrefixes []client.StorageCommonPrefix
	var iterParams []objstore.IterOption

	// If delimiter is empty we want to list all files
	if delimiter == "" {
		iterParams = append(iterParams, objstore.WithRecursiveIter())
	}

	supportsUpdatedAt := slices.Contains(o.bucket.SupportedIterOptions(), objstore.UpdatedAt)
	if supportsUpdatedAt {
		iterParams = append(iterParams, objstore.WithUpdatedAt())
	}

	err := o.bucket.IterWithAttributes(ctx, prefix, func(attrs objstore.IterObjectAttributes) error {
		// CommonPrefixes are keys that have the prefix and have the delimiter
		// as a suffix
		objectKey := attrs.Name
		if delimiter != "" && strings.HasSuffix(objectKey, delimiter) {
			commonPrefixes = append(commonPrefixes, client.StorageCommonPrefix(objectKey))
			return nil
		}

		lastModified, ok := attrs.LastModified()
		if supportsUpdatedAt && !ok {
			return errors.Errorf("failed to get lastModified for %s", objectKey)
		}
		// Some providers do not support supports UpdatedAt option. For those we need
		// to make an additional request to get the last modified time.
		if !supportsUpdatedAt {
			attr, err := o.bucket.Attributes(ctx, objectKey)
			if err != nil {
				return errors.Wrapf(err, "failed to get attributes for %s", objectKey)
			}
			lastModified = attr.LastModified
		}

		storageObjects = append(storageObjects, client.StorageObject{
			Key:        objectKey,
			ModifiedAt: lastModified,
		})

		return nil
	}, iterParams...)
	if err != nil {
		return nil, nil, err
	}

	return storageObjects, commonPrefixes, nil
}

// DeleteObject deletes the specified object key from the configured bucket.
func (o *ObjectClientAdapter) DeleteObject(ctx context.Context, objectKey string) error {
	return o.bucket.Delete(ctx, objectKey)
}

// IsObjectNotFoundErr returns true if error means that object is not found. Relevant to GetObject and DeleteObject operations.
func (o *ObjectClientAdapter) IsObjectNotFoundErr(err error) bool {
	return o.bucket.IsObjNotFoundErr(err)
}

// IsRetryableErr returns true if the request failed due to some retryable server-side scenario
func (o *ObjectClientAdapter) IsRetryableErr(err error) bool {
	return o.isRetryableErr(err)
}
