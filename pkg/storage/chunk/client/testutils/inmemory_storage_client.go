package testutils

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"

	"github.com/go-kit/log/level"

	"github.com/grafana/loki/pkg/storage/chunk"
	"github.com/grafana/loki/pkg/storage/chunk/client"
	"github.com/grafana/loki/pkg/storage/config"
	"github.com/grafana/loki/pkg/storage/stores/series/index"
	"github.com/grafana/loki/pkg/util/log"
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
	mtx       sync.RWMutex
	tables    map[string]*mockTable
	objects   map[string][]byte
	schemaCfg config.SchemaConfig

	numIndexWrites int
	numChunkWrites int
	mode           MockStorageMode
}

type mockTable struct {
	items       map[string][]mockItem
	write, read int64
}

type mockItem struct {
	rangeValue []byte
	value      []byte
}

var singleton *MockStorage

func ResetMockStorage() {
	singleton = nil
}

// NewMockStorage creates a new MockStorage.
func NewMockStorage() *MockStorage {
	if singleton == nil {
		singleton = &MockStorage{
			schemaCfg: config.SchemaConfig{
				Configs: []config.PeriodConfig{
					{
						From:      config.DayTime{Time: 0},
						Schema:    "v11",
						RowShards: 16,
					},
				},
			},
			tables:  map[string]*mockTable{},
			objects: map[string][]byte{},
		}
	}
	return singleton
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
}

// ListTables implements StorageClient.
func (m *MockStorage) ListTables(_ context.Context) ([]string, error) {
	m.mtx.RLock()
	defer m.mtx.RUnlock()

	var tableNames []string
	for tableName := range m.tables {
		func(tableName string) {
			tableNames = append(tableNames, tableName)
		}(tableName)
	}
	return tableNames, nil
}

// CreateTable implements StorageClient.
func (m *MockStorage) CreateTable(_ context.Context, desc config.TableDesc) error {
	m.mtx.Lock()
	defer m.mtx.Unlock()

	if _, ok := m.tables[desc.Name]; ok {
		return fmt.Errorf("table already exists")
	}

	m.tables[desc.Name] = &mockTable{
		items: map[string][]mockItem{},
		write: desc.ProvisionedWrite,
		read:  desc.ProvisionedRead,
	}

	return nil
}

// DeleteTable implements StorageClient.
func (m *MockStorage) DeleteTable(_ context.Context, name string) error {
	m.mtx.Lock()
	defer m.mtx.Unlock()

	if _, ok := m.tables[name]; !ok {
		return fmt.Errorf("table does not exist")
	}

	delete(m.tables, name)

	return nil
}

// DescribeTable implements StorageClient.
func (m *MockStorage) DescribeTable(_ context.Context, name string) (desc config.TableDesc, isActive bool, err error) {
	m.mtx.RLock()
	defer m.mtx.RUnlock()

	table, ok := m.tables[name]
	if !ok {
		return config.TableDesc{}, false, fmt.Errorf("not found")
	}

	return config.TableDesc{
		Name:             name,
		ProvisionedRead:  table.read,
		ProvisionedWrite: table.write,
	}, true, nil
}

// UpdateTable implements StorageClient.
func (m *MockStorage) UpdateTable(_ context.Context, _, desc config.TableDesc) error {
	m.mtx.Lock()
	defer m.mtx.Unlock()

	table, ok := m.tables[desc.Name]
	if !ok {
		return fmt.Errorf("not found")
	}

	table.read = desc.ProvisionedRead
	table.write = desc.ProvisionedWrite

	return nil
}

// NewWriteBatch implements StorageClient.
func (m *MockStorage) NewWriteBatch() index.WriteBatch {
	return &mockWriteBatch{}
}

// BatchWrite implements StorageClient.
func (m *MockStorage) BatchWrite(ctx context.Context, batch index.WriteBatch) error {
	m.mtx.Lock()
	defer m.mtx.Unlock()

	if m.mode == MockStorageModeReadOnly {
		return errPermissionDenied
	}

	mockBatch := *batch.(*mockWriteBatch)
	seenWrites := map[string]bool{}

	m.numIndexWrites += len(mockBatch.inserts)

	for _, req := range mockBatch.inserts {
		table, ok := m.tables[req.tableName]
		if !ok {
			return fmt.Errorf("table not found: %s", req.tableName)
		}

		// Check for duplicate writes by RangeKey in same batch
		key := fmt.Sprintf("%s:%s:%x", req.tableName, req.hashValue, req.rangeValue)
		if _, ok := seenWrites[key]; ok {
			return fmt.Errorf("Dupe write in batch")
		}
		seenWrites[key] = true

		level.Debug(log.WithContext(ctx, log.Logger)).Log("msg", "write", "hash", req.hashValue, "range", req.rangeValue)

		items := table.items[req.hashValue]

		// insert in order
		i := sort.Search(len(items), func(i int) bool {
			return bytes.Compare(items[i].rangeValue, req.rangeValue) >= 0
		})
		if i >= len(items) || !bytes.Equal(items[i].rangeValue, req.rangeValue) {
			items = append(items, mockItem{})
			copy(items[i+1:], items[i:])
		} else {
			// if duplicate write then just update the value
			items[i].value = req.value
			continue
		}
		items[i] = mockItem{
			rangeValue: req.rangeValue,
			value:      req.value,
		}

		table.items[req.hashValue] = items
	}

	for _, req := range mockBatch.deletes {
		table, ok := m.tables[req.tableName]
		if !ok {
			return fmt.Errorf("table not found: %s", req.tableName)
		}

		items := table.items[req.hashValue]

		i := sort.Search(len(items), func(i int) bool {
			return bytes.Compare(items[i].rangeValue, req.rangeValue) >= 0
		})

		if i >= len(items) || !bytes.Equal(items[i].rangeValue, req.rangeValue) {
			continue
		}

		if len(items) == 1 {
			items = nil
		} else {
			items = items[:i+copy(items[i:], items[i+1:])]
		}

		table.items[req.hashValue] = items
	}
	return nil
}

// QueryPages implements StorageClient.
func (m *MockStorage) QueryPages(ctx context.Context, queries []index.Query, callback index.QueryPagesCallback) error {
	m.mtx.RLock()
	defer m.mtx.RUnlock()

	if m.mode == MockStorageModeWriteOnly {
		return errPermissionDenied
	}

	for _, query := range queries {
		err := m.query(ctx, query, func(b index.ReadBatchResult) bool {
			return callback(query, b)
		})
		if err != nil {
			return err
		}
	}

	return nil
}

func (m *MockStorage) query(ctx context.Context, query index.Query, callback func(index.ReadBatchResult) (shouldContinue bool)) error {
	logger := log.WithContext(ctx, log.Logger)
	level.Debug(logger).Log("msg", "QueryPages", "query", query.HashValue)

	table, ok := m.tables[query.TableName]
	if !ok {
		return fmt.Errorf("table not found: %s", query.TableName)
	}

	items, ok := table.items[query.HashValue]
	if !ok {
		level.Debug(logger).Log("msg", "not found")
		return nil
	}

	if query.RangeValuePrefix != nil {
		level.Debug(logger).Log("msg", "lookup prefix", "hash", query.HashValue, "range_prefix", query.RangeValuePrefix, "num_items", len(items))

		// the smallest index i in [0, n) at which f(i) is true
		i := sort.Search(len(items), func(i int) bool {
			if bytes.Compare(items[i].rangeValue, query.RangeValuePrefix) > 0 {
				return true
			}
			return bytes.HasPrefix(items[i].rangeValue, query.RangeValuePrefix)
		})
		j := sort.Search(len(items)-i, func(j int) bool {
			if bytes.Compare(items[i+j].rangeValue, query.RangeValuePrefix) < 0 {
				return false
			}
			return !bytes.HasPrefix(items[i+j].rangeValue, query.RangeValuePrefix)
		})

		level.Debug(logger).Log("msg", "found range", "from_inclusive", i, "to_exclusive", i+j)
		if i > len(items) || j == 0 {
			return nil
		}
		items = items[i : i+j]

	} else if query.RangeValueStart != nil {
		level.Debug(logger).Log("msg", "lookup range", "hash", query.HashValue, "range_start", query.RangeValueStart, "num_items", len(items))

		// the smallest index i in [0, n) at which f(i) is true
		i := sort.Search(len(items), func(i int) bool {
			return bytes.Compare(items[i].rangeValue, query.RangeValueStart) >= 0
		})

		level.Debug(logger).Log("msg", "found range [%d)", "index", i)
		if i > len(items) {
			return nil
		}
		items = items[i:]

	} else {
		level.Debug(logger).Log("msg", "lookup", "hash", query.HashValue, "num_items", len(items))
	}

	// Filters
	if query.ValueEqual != nil {
		level.Debug(logger).Log("msg", "filter by equality", "value_equal", query.ValueEqual)

		filtered := make([]mockItem, 0)
		for _, v := range items {
			if bytes.Equal(v.value, query.ValueEqual) {
				filtered = append(filtered, v)
			}
		}
		items = filtered
	}

	result := mockReadBatch{}
	result.items = append(result.items, items...)

	callback(&result)
	return nil
}

// PutChunks implements StorageClient.
func (m *MockStorage) PutChunks(_ context.Context, chunks []chunk.Chunk) error {
	m.mtx.Lock()
	defer m.mtx.Unlock()

	if m.mode == MockStorageModeReadOnly {
		return errPermissionDenied
	}

	m.numChunkWrites += len(chunks)

	for i := range chunks {
		buf, err := chunks[i].Encoded()
		if err != nil {
			return err
		}
		m.objects[m.schemaCfg.ExternalKey(chunks[i].ChunkRef)] = buf
	}
	return nil
}

// GetChunks implements StorageClient.
func (m *MockStorage) GetChunks(_ context.Context, chunkSet []chunk.Chunk) ([]chunk.Chunk, error) {
	m.mtx.RLock()
	defer m.mtx.RUnlock()

	if m.mode == MockStorageModeWriteOnly {
		return nil, errPermissionDenied
	}

	decodeContext := chunk.NewDecodeContext()
	result := []chunk.Chunk{}
	for _, chunk := range chunkSet {
		key := m.schemaCfg.ExternalKey(chunk.ChunkRef)
		buf, ok := m.objects[key]
		if !ok {
			return nil, errStorageObjectNotFound
		}
		if err := chunk.Decode(decodeContext, buf); err != nil {
			return nil, err
		}
		result = append(result, chunk)
	}
	return result, nil
}

// DeleteChunk implements StorageClient.
func (m *MockStorage) DeleteChunk(ctx context.Context, _, chunkID string) error {
	if m.mode == MockStorageModeReadOnly {
		return errPermissionDenied
	}

	return m.DeleteObject(ctx, chunkID)
}

func (m *MockStorage) GetObject(_ context.Context, objectKey string) (io.ReadCloser, int64, error) {
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

func (m *MockStorage) PutObject(_ context.Context, objectKey string, object io.ReadSeeker) error {
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

func (m *MockStorage) IsObjectNotFoundErr(err error) bool {
	return errors.Is(err, errStorageObjectNotFound)
}

func (m *MockStorage) IsChunkNotFoundErr(err error) bool {
	return m.IsObjectNotFoundErr(err)
}

func (m *MockStorage) DeleteObject(_ context.Context, objectKey string) error {
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
func (m *MockStorage) List(_ context.Context, prefix, delimiter string) ([]client.StorageObject, []client.StorageCommonPrefix, error) {
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

type mockWriteBatch struct {
	inserts []struct {
		tableName, hashValue string
		rangeValue           []byte
		value                []byte
	}
	deletes []struct {
		tableName, hashValue string
		rangeValue           []byte
	}
}

func (b *mockWriteBatch) Delete(tableName, hashValue string, rangeValue []byte) {
	b.deletes = append(b.deletes, struct {
		tableName, hashValue string
		rangeValue           []byte
	}{tableName: tableName, hashValue: hashValue, rangeValue: rangeValue})
}

func (b *mockWriteBatch) Add(tableName, hashValue string, rangeValue []byte, value []byte) {
	b.inserts = append(b.inserts, struct {
		tableName, hashValue string
		rangeValue           []byte
		value                []byte
	}{tableName, hashValue, rangeValue, value})
}

type mockReadBatch struct {
	items []mockItem
}

func (b *mockReadBatch) Iterator() index.ReadBatchIterator {
	return &mockReadBatchIter{
		index:         -1,
		mockReadBatch: b,
	}
}

type mockReadBatchIter struct {
	index int
	*mockReadBatch
}

func (b *mockReadBatchIter) Next() bool {
	b.index++
	return b.index < len(b.items)
}

func (b *mockReadBatchIter) RangeValue() []byte {
	return b.items[b.index].rangeValue
}

func (b *mockReadBatchIter) Value() []byte {
	return b.items[b.index].value
}
