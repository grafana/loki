package noop

import (
	"context"
	"io"
	"time"

	"github.com/pkg/errors"

	"github.com/grafana/loki/v3/pkg/storage/chunk/client"
)

// NoopObjectClient holds config for filesystem as object store
type NoopObjectClient struct {
}

// NewNoopObjectClient makes a chunk.Client which stores chunks as files in the local filesystem.
func NewNoopObjectClient() (*NoopObjectClient, error) {
	return &NoopObjectClient{}, nil
}

// Stop implements ObjectClient
func (NoopObjectClient) Stop() {}

// GetObject from the store
func (f *NoopObjectClient) GetObject(_ context.Context, objectKey string) (io.ReadCloser, int64, error) {
	return nil, 0, errors.New("noop storage cannot return any objects")
}

func (f *NoopObjectClient) GetObjectRange(_ context.Context, _ string, _, _ int64) (io.ReadCloser, error) {
	return nil, errors.New("noop storage cannot return any objects")
}

// PutObject into the store
func (f *NoopObjectClient) PutObject(_ context.Context, objectKey string, object io.Reader) error {
	return nil
}

// List implements chunk.ObjectClient.
// NoopObjectClient assumes that prefix is a directory, and only supports "" and "/" delimiters.
func (f *NoopObjectClient) List(ctx context.Context, prefix, delimiter string) ([]client.StorageObject, []client.StorageCommonPrefix, error) {
	return []client.StorageObject{}, []client.StorageCommonPrefix{}, nil
}

func (f *NoopObjectClient) DeleteObject(ctx context.Context, objectKey string) error {
	return nil
}

// DeleteChunksBefore implements BucketClient
func (f *NoopObjectClient) DeleteChunksBefore(ctx context.Context, ts time.Time) error {
	return nil
}

// IsObjectNotFoundErr returns true if error means that object is not found. Relevant to GetObject and DeleteObject operations.
func (f *NoopObjectClient) IsObjectNotFoundErr(err error) bool {
	return true
}

func (f *NoopObjectClient) ObjectExists(ctx context.Context, objectKey string) (bool, error) {
	return false, nil
}

func (f *NoopObjectClient) IsRetryableErr(err error) bool {
	return false
}

// GetAttributes implements ObjectClient
func (f *NoopObjectClient) GetAttributes(_ context.Context, _ string) (client.ObjectAttributes, error) {
	return client.ObjectAttributes{}, nil
}
