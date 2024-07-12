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

	"github.com/grafana/loki/v3/pkg/storage/bucket"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client/hedging"
)

type GCSThanosObjectClient struct {
	client       objstore.Bucket
	hedgedClient objstore.Bucket
}

func NewGCSThanosObjectClient(ctx context.Context, cfg bucket.Config, component string, logger log.Logger, hedgingCfg hedging.Config, reg prometheus.Registerer) (*GCSThanosObjectClient, error) {
	client, err := newGCSThanosObjectClient(ctx, cfg, component, logger, false, hedgingCfg, prometheus.WrapRegistererWith(prometheus.Labels{"hedging": "false"}, reg))
	if err != nil {
		return nil, err
	}
	hedgedClient, err := newGCSThanosObjectClient(ctx, cfg, component, logger, true, hedgingCfg, prometheus.WrapRegistererWith(prometheus.Labels{"hedging": "true"}, reg))
	if err != nil {
		return nil, err
	}
	return &GCSThanosObjectClient{
		client:       client,
		hedgedClient: hedgedClient,
	}, nil
}

func newGCSThanosObjectClient(ctx context.Context, cfg bucket.Config, component string, logger log.Logger, hedging bool, hedgingCfg hedging.Config, reg prometheus.Registerer) (objstore.Bucket, error) {
	if hedging {
		hedgedTrasport, err := hedgingCfg.RoundTripperWithRegisterer(nil, reg)
		if err != nil {
			return nil, err
		}

		cfg.GCS.HTTP.Transport = hedgedTrasport
	}

	return bucket.NewClient(ctx, cfg, component, logger, reg)
}

func (s *GCSThanosObjectClient) Stop() {}

// ObjectExists checks if a given objectKey exists in the GCS bucket
func (s *GCSThanosObjectClient) ObjectExists(ctx context.Context, objectKey string) (bool, error) {
	return s.client.Exists(ctx, objectKey)
}

// PutObject puts the specified bytes into the configured GCS bucket at the provided key
func (s *GCSThanosObjectClient) PutObject(ctx context.Context, objectKey string, object io.Reader) error {
	return s.client.Upload(ctx, objectKey, object)
}

// GetObject returns a reader and the size for the specified object key from the configured GCS bucket.
func (s *GCSThanosObjectClient) GetObject(ctx context.Context, objectKey string) (io.ReadCloser, int64, error) {
	reader, err := s.client.Get(ctx, objectKey)
	if err != nil {
		return nil, 0, err
	}

	attr, err := s.client.Attributes(ctx, objectKey)
	if err != nil {
		return nil, 0, errors.Wrapf(err, "failed to get attributes for %s", objectKey)
	}

	return reader, attr.Size, err
}

// List objects with given prefix.
func (s *GCSThanosObjectClient) List(ctx context.Context, prefix, delimiter string) ([]client.StorageObject, []client.StorageCommonPrefix, error) {
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
