package bucket

import (
	"context"
	"io"
	"strings"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/pkg/errors"

	"github.com/grafana/loki/v3/pkg/storage/chunk/client"
	"github.com/thanos-io/objstore"
)

type ObjectClientAdapter struct {
	bucket, hedgedBucket objstore.Bucket
	logger               log.Logger
	isRetryableErr       func(err error) bool
}

func NewObjectClientAdapter(bucket, hedgedBucket objstore.Bucket, logger log.Logger) *ObjectClientAdapter {
	if hedgedBucket == nil {
		hedgedBucket = bucket
	}

	return &ObjectClientAdapter{
		bucket:       bucket,
		hedgedBucket: hedgedBucket,
		logger:       logger,
		// default to no retryable errors. Override with WithRetryableErr
		isRetryableErr: func(err error) bool {
			return false
		},
	}
}

func WithRetryableErrFunc(f func(err error) bool) func(*ObjectClientAdapter) {
	return func(o *ObjectClientAdapter) {
		o.isRetryableErr = f
	}
}

func (t *ObjectClientAdapter) Stop() {
}

// ObjectExists checks if a given objectKey exists in the GCS bucket
func (s *ObjectClientAdapter) ObjectExists(ctx context.Context, objectKey string) (bool, error) {
	return s.bucket.Exists(ctx, objectKey)
}

// GetAttributes returns the attributes of the specified object key from the configured GCS bucket.
func (s *ObjectClientAdapter) GetAttributes(ctx context.Context, objectKey string) (client.ObjectAttributes, error) {
	attr := client.ObjectAttributes{}
	thanosAttr, err := s.hedgedBucket.Attributes(ctx, objectKey)
	if err != nil {
		return attr, err
	}

	attr.Size = thanosAttr.Size
	return attr, nil
}

// PutObject puts the specified bytes into the configured GCS bucket at the provided key
func (s *ObjectClientAdapter) PutObject(ctx context.Context, objectKey string, object io.Reader) error {
	return s.bucket.Upload(ctx, objectKey, object)
}

// GetObject returns a reader and the size for the specified object key from the configured GCS bucket.
// size is set to -1 if it cannot be succefully determined, it is up to the caller to check this value before using it.
func (s *ObjectClientAdapter) GetObject(ctx context.Context, objectKey string) (io.ReadCloser, int64, error) {
	reader, err := s.hedgedBucket.Get(ctx, objectKey)
	if err != nil {
		return nil, 0, err
	}

	size, err := objstore.TryToGetSize(reader)
	if err != nil {
		size = -1
		level.Warn(s.logger).Log("msg", "failed to get size of object", "err", err)
	}

	return reader, size, err
}

func (s *ObjectClientAdapter) GetObjectRange(ctx context.Context, objectKey string, offset, length int64) (io.ReadCloser, error) {
	return s.hedgedBucket.GetRange(ctx, objectKey, offset, length)
}

// List objects with given prefix.
func (s *ObjectClientAdapter) List(ctx context.Context, prefix, delimiter string) ([]client.StorageObject, []client.StorageCommonPrefix, error) {
	var storageObjects []client.StorageObject
	var commonPrefixes []client.StorageCommonPrefix
	var iterParams []objstore.IterOption

	// If delimiter is empty we want to list all files
	if delimiter == "" {
		iterParams = append(iterParams, objstore.WithRecursiveIter)
	}

	err := s.bucket.Iter(ctx, prefix, func(objectKey string) error {
		// CommonPrefixes are keys that have the prefix and have the delimiter
		// as a suffix
		if delimiter != "" && strings.HasSuffix(objectKey, delimiter) {
			commonPrefixes = append(commonPrefixes, client.StorageCommonPrefix(objectKey))
			return nil
		}

		// TODO: remove this once thanos support IterWithAttributes
		attr, err := s.bucket.Attributes(ctx, objectKey)
		if err != nil {
			return errors.Wrapf(err, "failed to get attributes for %s", objectKey)
		}

		storageObjects = append(storageObjects, client.StorageObject{
			Key:        objectKey,
			ModifiedAt: attr.LastModified,
		})

		return nil
	}, iterParams...)
	if err != nil {
		return nil, nil, err
	}

	return storageObjects, commonPrefixes, nil
}

// DeleteObject deletes the specified object key from the configured GCS bucket.
func (s *ObjectClientAdapter) DeleteObject(ctx context.Context, objectKey string) error {
	return s.bucket.Delete(ctx, objectKey)
}

// IsObjectNotFoundErr returns true if error means that object is not found. Relevant to GetObject and DeleteObject operations.
func (s *ObjectClientAdapter) IsObjectNotFoundErr(err error) bool {
	return s.bucket.IsObjNotFoundErr(err)
}

// IsRetryableErr returns true if the request failed due to some retryable server-side scenario
func (s *ObjectClientAdapter) IsRetryableErr(err error) bool {
	return s.isRetryableErr(err)
}
