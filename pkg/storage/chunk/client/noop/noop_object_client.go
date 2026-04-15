package noop

import (
	"context"
	"io"
	"time"

	"github.com/pkg/errors"

	"github.com/grafana/loki/v3/pkg/storage/chunk/client"
)

// ObjectClient holds config for filesystem as object store
type ObjectClient struct {
}

// NewNoopObjectClient makes a chunk.Client which stores chunks as files in the local filesystem.
func NewNoopObjectClient() (*ObjectClient, error) {
	return &ObjectClient{}, nil
}

// Stop implements ObjectClient
func (ObjectClient) Stop() {}

// GetObject from the store
func (f *ObjectClient) GetObject(_ context.Context, _ string) (io.ReadCloser, int64, error) {
	return nil, 0, errors.New("noop storage cannot return any objects")
}

func (f *ObjectClient) GetObjectRange(_ context.Context, _ string, _, _ int64) (io.ReadCloser, error) {
	return nil, errors.New("noop storage cannot return any objects")
}

// PutObject into the store
func (f *ObjectClient) PutObject(_ context.Context, _ string, _ io.Reader) error {
	return nil
}

// List implements chunk.ObjectClient.
// ObjectClient assumes that prefix is a directory, and only supports "" and "/" delimiters.
func (f *ObjectClient) List(_ context.Context, _, _ string) ([]client.StorageObject, []client.StorageCommonPrefix, error) {
	return []client.StorageObject{}, []client.StorageCommonPrefix{}, nil
}

func (f *ObjectClient) DeleteObject(_ context.Context, _ string) error {
	return nil
}

// DeleteChunksBefore implements BucketClient
func (f *ObjectClient) DeleteChunksBefore(_ context.Context, _ time.Time) error {
	return nil
}

// IsObjectNotFoundErr returns true if error means that object is not found. Relevant to GetObject and DeleteObject operations.
func (f *ObjectClient) IsObjectNotFoundErr(_ error) bool {
	return true
}

func (f *ObjectClient) ObjectExists(_ context.Context, _ string) (bool, error) {
	return false, nil
}

func (f *ObjectClient) IsRetryableErr(_ error) bool {
	return false
}

// GetAttributes implements ObjectClient
func (f *ObjectClient) GetAttributes(_ context.Context, _ string) (client.ObjectAttributes, error) {
	return client.ObjectAttributes{}, nil
}
