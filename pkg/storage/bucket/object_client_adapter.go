package bucket

import (
	"context"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/thanos-io/objstore"

	"github.com/grafana/loki/v3/pkg/storage/chunk/client"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client/aws"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client/gcp"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client/hedging"
)

type ObjectClientAdapter struct {
	bucket, hedgedBucket objstore.Bucket
	logger               log.Logger
	supportsUpdatedAt    bool
	isRetryableErr       func(err error) bool
}

func NewObjectClient(ctx context.Context, backend string, cfg ConfigWithNamedStores, component string, hedgingCfg hedging.Config, disableRetries bool, logger log.Logger) (*ObjectClientAdapter, error) {
	var (
		storeType = backend
		storeCfg  = cfg.Config
	)

	if st, ok := cfg.NamedStores.LookupStoreType(backend); ok {
		storeType = st
		// override config with values from named store config
		if err := cfg.NamedStores.OverrideConfig(&storeCfg, backend); err != nil {
			return nil, err
		}
	}

	if disableRetries {
		if err := storeCfg.disableRetries(storeType); err != nil {
			return nil, fmt.Errorf("create bucket: %w", err)
		}
	}

	bucket, err := NewClient(ctx, storeType, storeCfg, component, logger)
	if err != nil {
		return nil, fmt.Errorf("create bucket: %w", err)
	}

	hedgedBucket := bucket
	if hedgingCfg.At != 0 {
		hedgedTrasport, err := hedgingCfg.RoundTripperWithRegisterer(nil, prometheus.WrapRegistererWithPrefix("loki_", prometheus.DefaultRegisterer))
		if err != nil {
			return nil, fmt.Errorf("create hedged transport: %w", err)
		}

		if err := storeCfg.configureTransport(storeType, hedgedTrasport); err != nil {
			return nil, fmt.Errorf("create hedged bucket: %w", err)
		}

		hedgedBucket, err = NewClient(ctx, storeType, storeCfg, component, logger)
		if err != nil {
			return nil, fmt.Errorf("create hedged bucket: %w", err)
		}
	}

	o := &ObjectClientAdapter{
		bucket:            bucket,
		hedgedBucket:      hedgedBucket,
		logger:            log.With(logger, "component", "bucket_to_object_client_adapter"),
		supportsUpdatedAt: slices.Contains(bucket.SupportedIterOptions(), objstore.UpdatedAt),
		// default to no retryable errors. Override with WithRetryableErrFunc
		isRetryableErr: func(_ error) bool {
			return false
		},
	}

	switch storeType {
	case GCS:
		o.isRetryableErr = gcp.IsRetryableErr
	case S3:
		o.isRetryableErr = aws.IsRetryableErr
	}

	return o, nil
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

	if o.supportsUpdatedAt {
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
		if o.supportsUpdatedAt && !ok {
			return errors.Errorf("failed to get lastModified for %s", objectKey)
		}
		// Some providers do not support supports UpdatedAt option. For those we need
		// to make an additional request to get the last modified time.
		if !o.supportsUpdatedAt {
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
