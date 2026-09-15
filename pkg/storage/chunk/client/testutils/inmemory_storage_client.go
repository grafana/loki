package testutils

import (
	"bytes"
	"context"
	"errors"
	"io"
	"sort"
	"strings"
	"sync"

	"github.com/grafana/loki/v3/pkg/storage/chunk/client"
	"github.com/grafana/loki/v3/pkg/storage/config"
)

type MockStorageMode int

var (
	errPermissionDenied      = errors.New("permission denied")
	errStorageObjectNotFound = errors.New("object not found in storage")
)

const (
	MockStorageModeReadWrite = 0
	MockStorageModeReadOnly  = 1
	MockStorageModeWriteOnly = 2
)

// MockStorage is a fake in-memory StorageClient.
type MockStorage struct {
	*InMemoryObjectClient

	mtx       sync.RWMutex
	schemaCfg config.SchemaConfig

	mode MockStorageMode
}

// compiler check
var _ client.ObjectClient = &InMemoryObjectClient{}

type InMemoryObjectClient struct {
	objects map[string][]byte
	mtx     sync.RWMutex
	mode    MockStorageMode
}

func NewInMemoryObjectClient() *InMemoryObjectClient {
	return &InMemoryObjectClient{
		objects: make(map[string][]byte),
	}
}

func (m *InMemoryObjectClient) Internals() map[string][]byte {
	return m.objects
}

var singleton *MockStorage

func ResetMockStorage() {
	singleton = nil
}

// NewMockStorage creates a mock storage singleton.
// MockStorage implements the interfaces client.ObjectClient and storage.SchemaConfigProvider.
func NewMockStorage() *MockStorage {
	if singleton == nil {
		singleton = &MockStorage{
			InMemoryObjectClient: NewInMemoryObjectClient(),
			schemaCfg: config.SchemaConfig{
				Configs: []config.PeriodConfig{
					{
						From:   config.DayTime{Time: 0},
						Schema: "v11",
					},
				},
			},
		}
	}
	return singleton
}

func (m *MockStorage) GetSchemaConfigs() []config.PeriodConfig {
	return m.schemaCfg.Configs
}

func (m *MockStorage) GetSortedObjectKeys() []string {
	m.mtx.RLock()
	defer m.mtx.RUnlock()

	keys := make([]string, 0, len(m.objects))
	for k := range m.objects {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func (m *MockStorage) GetObjectCount() int {
	m.mtx.RLock()
	defer m.mtx.RUnlock()

	return len(m.objects)
}

// Stop doesn't do anything.
func (*MockStorage) Stop() {
}

func (m *MockStorage) SetMode(mode MockStorageMode) {
	m.mode = mode
	m.InMemoryObjectClient.mode = mode
}

// ObjectExists implments client.ObjectClient
func (m *InMemoryObjectClient) ObjectExists(ctx context.Context, objectKey string) (bool, error) {
	if _, err := m.GetAttributes(ctx, objectKey); err != nil {
		if m.IsObjectNotFoundErr(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func (m *InMemoryObjectClient) GetAttributes(_ context.Context, objectKey string) (client.ObjectAttributes, error) {
	m.mtx.RLock()
	defer m.mtx.RUnlock()

	if m.mode == MockStorageModeWriteOnly {
		return client.ObjectAttributes{}, errPermissionDenied
	}

	_, ok := m.objects[objectKey]
	if !ok {
		return client.ObjectAttributes{}, errStorageObjectNotFound
	}
	objectSize := len(m.objects[objectKey])
	return client.ObjectAttributes{Size: int64(objectSize)}, nil
}

// GetObject implements client.ObjectClient.
func (m *InMemoryObjectClient) GetObject(_ context.Context, objectKey string) (io.ReadCloser, int64, error) {
	m.mtx.RLock()
	defer m.mtx.RUnlock()

	if m.mode == MockStorageModeWriteOnly {
		return nil, 0, errPermissionDenied
	}

	buf, ok := m.objects[objectKey]
	if !ok {
		return nil, 0, errStorageObjectNotFound
	}

	return io.NopCloser(bytes.NewReader(buf)), int64(len(buf)), nil
}

// GetObject implements client.ObjectClient.
func (m *InMemoryObjectClient) GetObjectRange(_ context.Context, objectKey string, offset, length int64) (io.ReadCloser, error) {
	m.mtx.RLock()
	defer m.mtx.RUnlock()

	if m.mode == MockStorageModeWriteOnly {
		return nil, errPermissionDenied
	}

	buf, ok := m.objects[objectKey]
	if !ok {
		return nil, errStorageObjectNotFound
	}
	if len(buf) < int(offset+length) {
		return nil, io.ErrUnexpectedEOF
	}

	return io.NopCloser(bytes.NewReader(buf[offset : offset+length])), nil
}

// PutObject implements client.ObjectClient.
func (m *InMemoryObjectClient) PutObject(_ context.Context, objectKey string, object io.Reader) error {
	buf, err := io.ReadAll(object)
	if err != nil {
		return err
	}

	if m.mode == MockStorageModeReadOnly {
		return errPermissionDenied
	}

	m.mtx.Lock()
	defer m.mtx.Unlock()

	m.objects[objectKey] = buf
	return nil
}

// IsObjectNotFoundErr implements client.ObjectClient.
func (m *InMemoryObjectClient) IsObjectNotFoundErr(err error) bool {
	return errors.Is(err, errStorageObjectNotFound)
}

func (m *MockStorage) IsChunkNotFoundErr(err error) bool {
	return m.IsObjectNotFoundErr(err)
}

// IsRetryableErr implements client.ObjectClient.
func (m *InMemoryObjectClient) IsRetryableErr(error) bool { return false }

// DeleteObject implements client.ObjectClient.
func (m *InMemoryObjectClient) DeleteObject(_ context.Context, objectKey string) error {
	m.mtx.Lock()
	defer m.mtx.Unlock()

	if m.mode == MockStorageModeReadOnly {
		return errPermissionDenied
	}

	if _, ok := m.objects[objectKey]; !ok {
		return errStorageObjectNotFound
	}

	delete(m.objects, objectKey)
	return nil
}

// List implements chunk.ObjectClient.
func (m *InMemoryObjectClient) List(_ context.Context, prefix, delimiter string) ([]client.StorageObject, []client.StorageCommonPrefix, error) {
	m.mtx.RLock()
	defer m.mtx.RUnlock()

	if m.mode == MockStorageModeWriteOnly {
		return nil, nil, errPermissionDenied
	}

	prefixes := map[string]struct{}{}

	storageObjects := make([]client.StorageObject, 0, len(m.objects))
	for key := range m.objects {
		if !strings.HasPrefix(key, prefix) {
			continue
		}

		// ToDo: Store mtime when we have mtime based use-cases for storage objects
		if delimiter == "" {
			storageObjects = append(storageObjects, client.StorageObject{Key: key})
			continue
		}

		ix := strings.Index(key[len(prefix):], delimiter)
		if ix < 0 {
			storageObjects = append(storageObjects, client.StorageObject{Key: key})
			continue
		}

		commonPrefix := key[:len(prefix)+ix+len(delimiter)] // Include delimeter in the common prefix.
		prefixes[commonPrefix] = struct{}{}
	}

	commonPrefixes := []client.StorageCommonPrefix(nil)
	for p := range prefixes {
		commonPrefixes = append(commonPrefixes, client.StorageCommonPrefix(p))
	}

	// Object stores return results in sorted order.
	sort.Slice(storageObjects, func(i, j int) bool {
		return storageObjects[i].Key < storageObjects[j].Key
	})
	sort.Slice(commonPrefixes, func(i, j int) bool {
		return commonPrefixes[i] < commonPrefixes[j]
	})

	return storageObjects, commonPrefixes, nil
}

// Stop implements client.ObjectClient
func (*InMemoryObjectClient) Stop() {
}
