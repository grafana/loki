package aws

import (
	"context"
	"io"
	"strings"

	"github.com/go-kit/log"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/thanos-io/objstore"

	"github.com/grafana/loki/pkg/storage/bucket"
	"github.com/grafana/loki/pkg/storage/chunk/client"
	"github.com/grafana/loki/pkg/storage/chunk/client/hedging"
)

type S3ThanosObjectClient struct {
	client       objstore.Bucket
	hedgedClient objstore.Bucket
}

// NewS3ObjectClient makes a new S3-backed ObjectClient.
func NewS3ThanosObjectClient(ctx context.Context, cfg bucket.Config, component string, logger log.Logger, hedgingCfg hedging.Config, reg prometheus.Registerer) (*S3ThanosObjectClient, error) {
	client, err := newS3ThanosObjClient(ctx, cfg, component, logger, false, hedgingCfg, reg)
	if err != nil {
		return nil, err
	}
	hedgedClient, err := newS3ThanosObjClient(ctx, cfg, component, logger, true, hedgingCfg, prometheus.WrapRegistererWithPrefix("hedging_", reg))
	if err != nil {
		return nil, err
	}
	return &S3ThanosObjectClient{
		client:       client,
		hedgedClient: hedgedClient,
	}, nil
}

func newS3ThanosObjClient(ctx context.Context, cfg bucket.Config, component string, logger log.Logger, hedging bool, hedgingCfg hedging.Config, reg prometheus.Registerer) (objstore.Bucket, error) {
	if hedging {
		hedgedTrasport, err := hedgingCfg.RoundTripperWithRegisterer(nil, reg)
		if err != nil {
			return nil, err
		}

		cfg.S3.HTTP.Transport = hedgedTrasport
	}

	//TODO(JoaoBraveCoding) Fix registry
	return bucket.NewClient(ctx, cfg, component, logger, reg)
}

// Stop fulfills the chunk.ObjectClient interface
func (s *S3ThanosObjectClient) Stop() {}

// ObjectExists checks if a given objectKey exists in the AWS bucket
func (s *S3ThanosObjectClient) ObjectExists(ctx context.Context, objectKey string) (bool, error) {
	return s.hedgedClient.Exists(ctx, objectKey)
}

// PutObject into the store
func (s *S3ThanosObjectClient) PutObject(ctx context.Context, objectKey string, object io.ReadSeeker) error {
	return s.client.Upload(ctx, objectKey, object)
}

// DeleteObject deletes the specified objectKey from the appropriate S3 bucket
func (s *S3ThanosObjectClient) DeleteObject(ctx context.Context, objectKey string) error {
	return s.client.Delete(ctx, objectKey)
}

// GetObject returns a reader and the size for the specified object key from the configured S3 bucket.
func (s *S3ThanosObjectClient) GetObject(ctx context.Context, objectKey string) (io.ReadCloser, int64, error) {
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
func (s *S3ThanosObjectClient) List(ctx context.Context, prefix, delimiter string) ([]client.StorageObject, []client.StorageCommonPrefix, error) {
	var storageObjects []client.StorageObject
	var commonPrefixes []client.StorageCommonPrefix
	var iterParams []objstore.IterOption

	// If delimiter is empty we want to list all files
	if delimiter == "" {
		iterParams = append(iterParams, objstore.WithRecursiveIter)
	}

	s.client.Iter(ctx, prefix, func(objectKey string) error {
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

	return storageObjects, commonPrefixes, nil
}

// IsObjectNotFoundErr returns true if error means that object is not found. Relevant to GetObject and DeleteObject operations.
func (s *S3ThanosObjectClient) IsObjectNotFoundErr(err error) bool {
	return s.client.IsObjNotFoundErr(err)
}

// TODO(dannyk): implement for client
func (s *S3ThanosObjectClient) IsRetryableErr(error) bool { return false }
