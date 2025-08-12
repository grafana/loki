package consumer

import (
	"bytes"
	"context"
	"errors"
	"io"
	"sync"

	"github.com/thanos-io/objstore"
)

// mockBucket implements objstore.Bucket interface for testing
type mockBucket struct {
	uploads map[string][]byte
	mu      sync.Mutex
}

func newMockBucket() *mockBucket {
	return &mockBucket{
		uploads: make(map[string][]byte),
	}
}

func (m *mockBucket) Close() error                             { return nil }
func (m *mockBucket) Delete(_ context.Context, _ string) error { return nil }
func (m *mockBucket) Exists(_ context.Context, name string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, exists := m.uploads[name]
	return exists, nil
}
func (m *mockBucket) Get(_ context.Context, name string) (io.ReadCloser, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	data, exists := m.uploads[name]
	if !exists {
		return nil, errors.New("object not found")
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}
func (m *mockBucket) GetRange(_ context.Context, _ string, _, _ int64) (io.ReadCloser, error) {
	return nil, nil
}
func (m *mockBucket) Upload(_ context.Context, name string, r io.Reader) error {
	data, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.uploads[name] = data
	return nil
}
func (m *mockBucket) Iter(_ context.Context, _ string, _ func(string) error, _ ...objstore.IterOption) error {
	return nil
}
func (m *mockBucket) Name() string { return "mock" }
func (m *mockBucket) Attributes(_ context.Context, _ string) (objstore.ObjectAttributes, error) {
	return objstore.ObjectAttributes{}, nil
}
func (m *mockBucket) GetAndReplace(_ context.Context, name string, _ func(io.ReadCloser) (io.ReadCloser, error)) error {
	return m.Upload(context.Background(), name, io.NopCloser(bytes.NewReader([]byte{})))
}
func (m *mockBucket) IsAccessDeniedErr(_ error) bool {
	return false
}
func (m *mockBucket) IsObjNotFoundErr(err error) bool {
	return err != nil && err.Error() == "object not found"
}
func (m *mockBucket) IterWithAttributes(_ context.Context, _ string, _ func(objstore.IterObjectAttributes) error, _ ...objstore.IterOption) error {
	return nil
}
func (m *mockBucket) Provider() objstore.ObjProvider {
	return objstore.ObjProvider("MOCK")
}
func (m *mockBucket) SupportedIterOptions() []objstore.IterOptionType {
	return nil
}
