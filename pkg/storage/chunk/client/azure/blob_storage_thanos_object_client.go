package azure

import (
	"context"
	"io"
	"strings"

	"github.com/go-kit/log"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/thanos-io/objstore"

	"github.com/grafana/loki/v3/pkg/storage/bucket"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client/hedging"
)

type BlobStorageThanosObjectClient struct {
	client       objstore.Bucket
	hedgedClient objstore.Bucket
}

// NewBlobStorageObjectClient makes a new BlobStorage-backed ObjectClient.
func NewBlobStorageThanosObjectClient(ctx context.Context, cfg bucket.Config, component string, logger log.Logger, hedgingCfg hedging.Config, reg prometheus.Registerer) (*BlobStorageThanosObjectClient, error) {
	client, err := newBlobStorageThanosObjClient(ctx, cfg, component, logger, false, hedgingCfg, prometheus.WrapRegistererWith(prometheus.Labels{"hedging": "false"}, reg))
	if err != nil {
		return nil, err
	}
	hedgedClient, err := newBlobStorageThanosObjClient(ctx, cfg, component, logger, true, hedgingCfg, prometheus.WrapRegistererWith(prometheus.Labels{"hedging": "true"}, reg))
	if err != nil {
		return nil, err
	}
	return &BlobStorageThanosObjectClient{
		client:       client,
		hedgedClient: hedgedClient,
	}, nil
}

func newBlobStorageThanosObjClient(ctx context.Context, cfg bucket.Config, component string, logger log.Logger, hedging bool, hedgingCfg hedging.Config, reg prometheus.Registerer) (objstore.Bucket, error) {
	if hedging {
		hedgedTrasport, err := hedgingCfg.RoundTripperWithRegisterer(nil, reg)
		if err != nil {
			return nil, err
		}

		cfg.Azure.HTTP.Transport = hedgedTrasport
	}

	return bucket.NewClient(ctx, cfg, component, logger, reg)
}

// Stop fulfills the chunk.ObjectClient interface
func (s *BlobStorageThanosObjectClient) Stop() {}

// ObjectExists checks if a given objectKey exists in the AWS bucket
func (s *BlobStorageThanosObjectClient) ObjectExists(ctx context.Context, objectKey string) (bool, error) {
	return s.hedgedClient.Exists(ctx, objectKey)
}

// PutObject into the store
func (s *BlobStorageThanosObjectClient) PutObject(ctx context.Context, objectKey string, object io.ReadSeeker) error {
	return s.client.Upload(ctx, objectKey, object)
}

// DeleteObject deletes the specified objectKey from the appropriate BlobStorage bucket
func (s *BlobStorageThanosObjectClient) DeleteObject(ctx context.Context, objectKey string) error {
	return s.client.Delete(ctx, objectKey)
}

// GetObject returns a reader and the size for the specified object key from the configured BlobStorage bucket.
func (s *BlobStorageThanosObjectClient) GetObject(ctx context.Context, objectKey string) (io.ReadCloser, int64, error) {
	reader, err := s.hedgedClient.Get(ctx, objectKey)
	if err != nil {
		return nil, 0, err
	}

	attr, err := s.hedgedClient.Attributes(ctx, objectKey)
	if err != nil {
		return nil, 0, errors.Wrapf(err, "failed to get attributes for %s", objectKey)
	}

	return reader, attr.Size, err
}

// List implements chunk.ObjectClient.
func (s *BlobStorageThanosObjectClient) List(ctx context.Context, prefix, delimiter string) ([]client.StorageObject, []client.StorageCommonPrefix, error) {
	var storageObjects []client.StorageObject
	var commonPrefixes []client.StorageCommonPrefix
	var iterParams []objstore.IterOption

	// If delimiter is empty we want to list all files
	if delimiter == "" {
		iterParams = append(iterParams, objstore.WithRecursiveIter)
	}

	err := s.client.Iter(ctx, prefix, func(objectKey string) error {
		// CommonPrefixes are keys that have the prefix and have the delimiter
		// as a suffix
		if delimiter != "" && strings.HasSuffix(objectKey, delimiter) {
			commonPrefixes = append(commonPrefixes, client.StorageCommonPrefix(objectKey))
			return nil
		}
		attr, err := s.client.Attributes(ctx, objectKey)
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

// IsObjectNotFoundErr returns true if error means that object is not found. Relevant to GetObject and DeleteObject operations.
func (s *BlobStorageThanosObjectClient) IsObjectNotFoundErr(err error) bool {
	return s.client.IsObjNotFoundErr(err)
}

// TODO(dannyk): implement for client
func (s *BlobStorageThanosObjectClient) IsRetryableErr(error) bool { return false }
