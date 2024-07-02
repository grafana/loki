package aws

import (
	"context"
	"hash/fnv"
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

type S3ThanosObjectClient struct {
	clients       []objstore.Bucket
	hedgedClients []objstore.Bucket
}

// NewS3ObjectClient makes a new S3-backed ObjectClient.
func NewS3ThanosObjectClient(ctx context.Context, cfg bucket.Config, component string, logger log.Logger, hedgingCfg hedging.Config, reg prometheus.Registerer) (*S3ThanosObjectClient, error) {
	bucketNames, err := thanosBuckets(cfg)
	if err != nil {
		return nil, err
	}

	clients := make([]objstore.Bucket, len(bucketNames))
	hedgedClients := make([]objstore.Bucket, len(bucketNames))
	for i, bucketName := range bucketNames {
		cfg.S3.BucketName = bucketName
		clients[i], err = newS3ThanosObjClient(ctx, cfg, component, logger, false, hedgingCfg, prometheus.WrapRegistererWith(prometheus.Labels{"hedging": "false"}, reg))
		if err != nil {
			return nil, err
		}
		hedgedClients[i], err = newS3ThanosObjClient(ctx, cfg, component, logger, true, hedgingCfg, prometheus.WrapRegistererWith(prometheus.Labels{"hedging": "true"}, reg))
		if err != nil {
			return nil, err
		}
	}

	return &S3ThanosObjectClient{
		clients:       clients,
		hedgedClients: hedgedClients,
	}, nil
}

func thanosBuckets(cfg bucket.Config) ([]string, error) {
	// bucketnames
	var bucketNames []string
	if cfg.S3.BucketName != "" {
		bucketNames = strings.Split(cfg.S3.BucketName, ",") // comma separated list of bucket names
	}

	if len(bucketNames) == 0 {
		return nil, errors.New("at least one bucket name must be specified")
	}
	return bucketNames, nil
}

func newS3ThanosObjClient(ctx context.Context, cfg bucket.Config, component string, logger log.Logger, hedging bool, hedgingCfg hedging.Config, reg prometheus.Registerer) (objstore.Bucket, error) {
	if hedging {
		hedgedTrasport, err := hedgingCfg.RoundTripperWithRegisterer(nil, reg)
		if err != nil {
			return nil, err
		}

		cfg.S3.HTTP.Transport = hedgedTrasport
	}

	return bucket.NewClient(ctx, cfg, component, logger, reg)
}

// bucketFromKey maps a key to a bucket name
func (s *S3ThanosObjectClient) bucketIndexFromKey(key string) uint32 {
	if len(s.clients) == 0 {
		return 0
	}

	hasher := fnv.New32a()
	hasher.Write([]byte(key)) //nolint: errcheck
	hash := hasher.Sum32()

	return hash % uint32(len(s.clients))
}

// Stop fulfills the chunk.ObjectClient interface
func (s *S3ThanosObjectClient) Stop() {}

// ObjectExists checks if a given objectKey exists in the AWS bucket
func (s *S3ThanosObjectClient) ObjectExists(ctx context.Context, objectKey string) (bool, error) {
	bIndex := s.bucketIndexFromKey(objectKey)
	return s.hedgedClients[bIndex].Exists(ctx, objectKey)
}

// PutObject into the store
func (s *S3ThanosObjectClient) PutObject(ctx context.Context, objectKey string, object io.ReadSeeker) error {
	bIndex := s.bucketIndexFromKey(objectKey)
	return s.clients[bIndex].Upload(ctx, objectKey, object)
}

// DeleteObject deletes the specified objectKey from the appropriate S3 bucket
func (s *S3ThanosObjectClient) DeleteObject(ctx context.Context, objectKey string) error {
	bIndex := s.bucketIndexFromKey(objectKey)
	return s.clients[bIndex].Delete(ctx, objectKey)
}

// GetObject returns a reader and the size for the specified object key from the configured S3 bucket.
func (s *S3ThanosObjectClient) GetObject(ctx context.Context, objectKey string) (io.ReadCloser, int64, error) {
	bIndex := s.bucketIndexFromKey(objectKey)
	reader, err := s.hedgedClients[bIndex].Get(ctx, objectKey)
	if err != nil {
		return nil, 0, err
	}

	attr, err := s.hedgedClients[bIndex].Attributes(ctx, objectKey)
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

	for bIndex := range s.clients {
		err := s.clients[bIndex].Iter(ctx, prefix, func(objectKey string) error {
			// CommonPrefixes are keys that have the prefix and have the delimiter
			// as a suffix
			if delimiter != "" && strings.HasSuffix(objectKey, delimiter) {
				commonPrefixes = append(commonPrefixes, client.StorageCommonPrefix(objectKey))
				return nil
			}
			attr, err := s.clients[bIndex].Attributes(ctx, objectKey)
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
	}

	return storageObjects, commonPrefixes, nil
}

// IsObjectNotFoundErr returns true if error means that object is not found. Relevant to GetObject and DeleteObject operations.
func (s *S3ThanosObjectClient) IsObjectNotFoundErr(err error) bool {
	// We can just use the first element as this is only used to check the error code
	return s.clients[0].IsObjNotFoundErr(err)
}

// TODO(dannyk): implement for client
func (s *S3ThanosObjectClient) IsRetryableErr(error) bool { return false }
