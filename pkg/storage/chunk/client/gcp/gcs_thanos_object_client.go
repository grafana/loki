package gcp

import (
	"context"
	"io"
	"net"
	"net/http"
	"strings"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
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
	logger       log.Logger
}

func NewGCSThanosObjectClient(ctx context.Context, cfg bucket.Config, component string, logger log.Logger, hedgingCfg hedging.Config) (*GCSThanosObjectClient, error) {
	client, err := newGCSThanosObjectClient(ctx, cfg, component, logger, false, hedgingCfg)
	if err != nil {
		return nil, err
	}

	if hedgingCfg.At == 0 {
		return &GCSThanosObjectClient{
			client:       client,
			hedgedClient: client,
			logger:       logger,
		}, nil
	}

	hedgedClient, err := newGCSThanosObjectClient(ctx, cfg, component, logger, true, hedgingCfg)
	if err != nil {
		return nil, err
	}

	return &GCSThanosObjectClient{
		client:       client,
		hedgedClient: hedgedClient,
		logger:       logger,
	}, nil
}

func newGCSThanosObjectClient(ctx context.Context, cfg bucket.Config, component string, logger log.Logger, hedging bool, hedgingCfg hedging.Config) (objstore.Bucket, error) {
	if hedging {
		hedgedTrasport, err := hedgingCfg.RoundTripperWithRegisterer(nil, prometheus.WrapRegistererWithPrefix("loki_", prometheus.DefaultRegisterer))
		if err != nil {
			return nil, err
		}

		cfg.GCS.Transport = hedgedTrasport
	}

	return bucket.NewClient(ctx, bucket.GCS, cfg, component, logger)
}

func (s *GCSThanosObjectClient) Stop() {
}

// ObjectExists checks if a given objectKey exists in the GCS bucket
func (s *GCSThanosObjectClient) ObjectExists(ctx context.Context, objectKey string) (bool, error) {
	return s.client.Exists(ctx, objectKey)
}

// GetAttributes returns the attributes of the specified object key from the configured GCS bucket.
func (s *GCSThanosObjectClient) GetAttributes(ctx context.Context, objectKey string) (client.ObjectAttributes, error) {
	attr := client.ObjectAttributes{}
	thanosAttr, err := s.hedgedClient.Attributes(ctx, objectKey)
	if err != nil {
		return attr, err
	}

	attr.Size = thanosAttr.Size
	return attr, nil
}

// PutObject puts the specified bytes into the configured GCS bucket at the provided key
func (s *GCSThanosObjectClient) PutObject(ctx context.Context, objectKey string, object io.Reader) error {
	return s.client.Upload(ctx, objectKey, object)
}

// GetObject returns a reader and the size for the specified object key from the configured GCS bucket.
// size is set to -1 if it cannot be succefully determined, it is up to the caller to check this value before using it.
func (s *GCSThanosObjectClient) GetObject(ctx context.Context, objectKey string) (io.ReadCloser, int64, error) {
	reader, err := s.hedgedClient.Get(ctx, objectKey)
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

func (s *GCSThanosObjectClient) GetObjectRange(ctx context.Context, objectKey string, offset, length int64) (io.ReadCloser, error) {
	return s.hedgedClient.GetRange(ctx, objectKey, offset, length)
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

		// TODO: remove this once thanos support IterWithAttributes
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
		// Go 1.23 changed the type of the error returned by the http client when a timeout occurs
		// while waiting for headers.  This is a server side timeout.
		return strings.Contains(err.Error(), "Client.Timeout")
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
