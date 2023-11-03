package gcp

import (
	"context"
	"io"
	"net"
	"net/http"

	"cloud.google.com/go/storage"
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
	cfg    GCSConfig
	client objstore.Bucket
}

func NewGCSThanosObjectClient(ctx context.Context, cfg GCSConfig, hedgingCfg hedging.Config) (*GCSThanosObjectClient, error) {
	return newGCSThanosObjectClient(ctx, cfg, hedgingCfg, storage.NewClient)
}

func newGCSThanosObjectClient(ctx context.Context, cfg GCSConfig, hedgingCfg hedging.Config, clientFactory ClientFactory) (*GCSThanosObjectClient, error) {
	bucket, err := bucket.NewClient(ctx, cfg.bCfg, cfg.BucketName, log.NewNopLogger(), prometheus.DefaultRegisterer)
	if err != nil {
		return nil, err
	}
	return &GCSThanosObjectClient{
		cfg:    cfg,
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
	q := &storage.Query{Prefix: prefix, Delimiter: delimiter}

	// Using delimiter and selected attributes doesn't work well together -- it returns nothing.
	// Reason is that Go's API only sets "fields=items(name,updated)" parameter in the request,
	// but what we really need is "fields=prefixes,items(name,updated)". Unfortunately we cannot set that,
	// so instead we don't use attributes selection when using delimiter.
	if delimiter == "" {
		err := q.SetAttrSelection([]string{"Name", "Updated"})
		if err != nil {
			return nil, nil, err
		}
	}

	s.client.Iter(ctx, prefix, func(objectKey string) error {

		// // When doing query with Delimiter, Prefix is the only field set for entries which represent synthetic "directory entries".
		// if attr.Prefix != "" {
		// 	commonPrefixes = append(commonPrefixes, client.StorageCommonPrefix(attr.Prefix))
		// 	continue
		// }

		// storageObjects = append(storageObjects, client.StorageObject{
		// 	Key:        attr.Name,
		// 	ModifiedAt: attr.Updated,
		// })


		return nil

	})

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
