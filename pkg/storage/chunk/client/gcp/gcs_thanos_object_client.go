package gcp

import (
	"context"
	"io"
	"net"
	"net/http"
	"strings"

	"github.com/go-kit/log"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/thanos-io/objstore"
	"google.golang.org/api/googleapi"
	amnet "k8s.io/apimachinery/pkg/util/net"

	"github.com/grafana/loki/pkg/storage/bucket"
	"github.com/grafana/loki/pkg/storage/chunk/client"
	"github.com/grafana/loki/pkg/storage/chunk/client/hedging"
)

type GCSThanosObjectClient struct {
	client objstore.Bucket
}

func NewGCSThanosObjectClient(ctx context.Context, cfg bucket.Config, logger log.Logger, hedgingCfg hedging.Config) (*GCSThanosObjectClient, error) {
	return newGCSThanosObjectClient(ctx, cfg, logger)
}

func newGCSThanosObjectClient(ctx context.Context, cfg bucket.Config, logger log.Logger) (*GCSThanosObjectClient, error) {
	// TODO (JoaoBraveCoding) accessing cfg.GCS directly
	bucket, err := bucket.NewClient(ctx, cfg, cfg.GCS.BucketName, logger, prometheus.DefaultRegisterer)
	if err != nil {
		return nil, err
	}
	return &GCSThanosObjectClient{
		client: bucket,
	}, nil
}

func (s *GCSThanosObjectClient) Stop() {
}

func (s *GCSThanosObjectClient) ObjectExists(ctx context.Context, objectKey string) (bool, error) {
	return s.client.Exists(ctx, objectKey)
}

// PutObject puts the specified bytes into the configured GCS bucket at the provided key
func (s *GCSThanosObjectClient) PutObject(ctx context.Context, objectKey string, object io.ReadSeeker) error {
	return s.client.Upload(ctx, objectKey, object)
}

// GetObject returns a reader and the size for the specified object key from the configured GCS bucket.
func (s *GCSThanosObjectClient) GetObject(ctx context.Context, objectKey string) (io.ReadCloser, int64, error) {
	reader, err := s.client.Get(ctx, objectKey)
	if err != nil {
		return nil, 0, err
	}
	// TODO (JoaoBraveCoding) currently returning 0 for the int64 as no
	return reader, 0, err
}

// List implements chunk.ObjectClient.
func (s *GCSThanosObjectClient) List(ctx context.Context, prefix, delimiter string) ([]client.StorageObject, []client.StorageCommonPrefix, error) {
	var storageObjects []client.StorageObject
	var commonPrefixes []client.StorageCommonPrefix
	iterParams := objstore.IterOption(func(params *objstore.IterParams) {})

	if delimiter == "" {
		iterParams = objstore.WithRecursiveIter
	}

	s.client.Iter(ctx, prefix, func(objectKey string) error {
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

	}, iterParams)

	return storageObjects, commonPrefixes, nil
}

// DeleteObject deletes the specified object key from the configured GCS bucket.
func (s *GCSThanosObjectClient) DeleteObject(ctx context.Context, objectKey string) error {
	return s.client.Delete(ctx, objectKey)
}

// IsObjectNotFoundErr returns true if error means that object is not found. Relevant to GetObject and DeleteObject operations.
func (s *GCSThanosObjectClient) IsObjectNotFoundErr(err error) bool {
	return s.client.IsObjNotFoundErr(err)
}

// IsStorageTimeoutErr returns true if error means that object cannot be retrieved right now due to server-side timeouts.
func (s *GCSThanosObjectClient) IsStorageTimeoutErr(err error) bool {
	// TODO(dannyk): move these out to be generic
	// context errors are all client-side
	if isContextErr(err) {
		return false
	}

	// connection misconfiguration, or writing on a closed connection
	// do NOT retry; this is not a server-side issue
	if errors.Is(err, net.ErrClosed) || amnet.IsConnectionRefused(err) {
		return false
	}

	// this is a server-side timeout
	if isTimeoutError(err) {
		return true
	}

	// connection closed (closed before established) or reset (closed after established)
	// this is a server-side issue
	if errors.Is(err, io.EOF) || amnet.IsConnectionReset(err) {
		return true
	}

	if gerr, ok := err.(*googleapi.Error); ok {
		// https://cloud.google.com/storage/docs/retry-strategy
		return gerr.Code == http.StatusRequestTimeout ||
			gerr.Code == http.StatusGatewayTimeout
	}

	return false
}

// IsStorageThrottledErr returns true if error means that object cannot be retrieved right now due to throttling.
func (s *GCSThanosObjectClient) IsStorageThrottledErr(err error) bool {
	if gerr, ok := err.(*googleapi.Error); ok {
		// https://cloud.google.com/storage/docs/retry-strategy
		return gerr.Code == http.StatusTooManyRequests ||
			(gerr.Code/100 == 5) // all 5xx errors are retryable
	}

	return false
}

// IsRetryableErr returns true if the request failed due to some retryable server-side scenario
func (s *GCSThanosObjectClient) IsRetryableErr(err error) bool {
	return s.IsStorageTimeoutErr(err) || s.IsStorageThrottledErr(err)
}
