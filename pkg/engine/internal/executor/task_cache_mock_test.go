package executor

import (
	"bytes"
	"context"
	"sync"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
	"github.com/grafana/loki/v3/pkg/logqlmodel/stats"
	"github.com/grafana/loki/v3/pkg/storage/chunk/cache"
)

// stubMetastore is used so executePointersScan does not return early; tests do not Open/Read the pipeline.
type stubMetastore struct{}

func (stubMetastore) Sections(context.Context, metastore.SectionsRequest) (metastore.SectionsResponse, error) {
	panic("not used in task_cache_mock_test")
}
func (stubMetastore) GetIndexes(context.Context, metastore.GetIndexesRequest) (metastore.GetIndexesResponse, error) {
	panic("not used in task_cache_mock_test")
}
func (stubMetastore) IndexSectionsReader(context.Context, metastore.IndexSectionsReaderRequest) (metastore.IndexSectionsReaderResponse, error) {
	panic("not used in task_cache_mock_test")
}
func (stubMetastore) CollectSections(context.Context, metastore.CollectSectionsRequest) (metastore.CollectSectionsResponse, error) {
	panic("not used in task_cache_mock_test")
}
func (stubMetastore) Labels(context.Context, time.Time, time.Time, ...*labels.Matcher) ([]string, error) {
	panic("not used in task_cache_mock_test")
}
func (stubMetastore) Values(context.Context, time.Time, time.Time, ...*labels.Matcher) ([]string, error) {
	panic("not used in task_cache_mock_test")
}

// fakeTaskCache is an in-memory cache for testing task_cache_id mock hit/miss.
type fakeTaskCache struct {
	mu    sync.Mutex
	store map[string][]byte
}

var _ cache.Cache = (*fakeTaskCache)(nil)

func newFakeTaskCache() *fakeTaskCache {
	return &fakeTaskCache{store: make(map[string][]byte)}
}

func (f *fakeTaskCache) Store(ctx context.Context, key []string, buf [][]byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i, k := range key {
		if i < len(buf) {
			f.store[k] = buf[i]
		} else {
			f.store[k] = nil
		}
	}
	return nil
}

func (f *fakeTaskCache) Fetch(ctx context.Context, keys []string) (found []string, bufs [][]byte, missing []string, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, k := range keys {
		if v, ok := f.store[k]; ok {
			found = append(found, k)
			bufs = append(bufs, v)
		} else {
			missing = append(missing, k)
		}
	}
	return found, bufs, missing, nil
}

func (f *fakeTaskCache) Stop() {}

func (f *fakeTaskCache) GetCacheType() stats.CacheType {
	return stats.ResultCache
}

func TestTaskCacheIDMock_MissThenHit(t *testing.T) {
	ctx := t.Context()
	fake := newFakeTaskCache()
	var logBuf bytes.Buffer
	logger := log.NewLogfmtLogger(&logBuf)
	c := &Context{
		logger:                   logger,
		metastore:                stubMetastore{},
		taskCacheIDCachePointers: fake,
	}
	node := &physical.PointersScan{
		Location: "index/0",
		Start:    time.Unix(10, 0),
		End:      time.Unix(3700, 0),
	}
	// First call: cache miss, then store.
	c.executePointersScan(ctx, node)
	require.Contains(t, logBuf.String(), "result=miss", "first call should log miss")
	logBuf.Reset()
	// Second call: same task_cache_id -> cache hit.
	c.executePointersScan(ctx, node)
	require.Contains(t, logBuf.String(), "result=hit", "second call should log hit")
}

func TestDataObjectKeyMock_MissThenHit(t *testing.T) {
	ctx := t.Context()
	fake := newFakeTaskCache()
	var logBuf bytes.Buffer
	logger := log.NewLogfmtLogger(&logBuf)
	c := &Context{
		logger:                  logger,
		metastore:               stubMetastore{},
		dataObjectPointersCache: fake,
	}
	node := &physical.PointersScan{
		Location: "index/0",
		Start:    time.Unix(10, 0),
		End:      time.Unix(3700, 0),
	}
	// First call: cache miss, then store.
	c.executePointersScan(ctx, node)
	require.Contains(t, logBuf.String(), "key=\"DataObject location=index/0 section=-1\"")
	require.Contains(t, logBuf.String(), "result=miss", "first call should log miss")
	logBuf.Reset()
	// Second call: same data object cache key -> cache hit.
	c.executePointersScan(ctx, node)
	require.Contains(t, logBuf.String(), "key=\"DataObject location=index/0 section=-1\"")
	require.Contains(t, logBuf.String(), "result=hit", "second call should log hit")
}

func TestTaskCacheIDMock_NilCache_DefaultsToMiss(t *testing.T) {
	ctx := t.Context()
	var logBuf bytes.Buffer
	logger := log.NewLogfmtLogger(&logBuf)
	c := &Context{
		logger:    logger,
		metastore: stubMetastore{},
		// taskCacheIDCacheDataObj and taskCacheIDCachePointers nil -> defaults to miss
	}
	node := &physical.PointersScan{
		Location: "index/0",
		Start:    time.Unix(10, 0),
		End:      time.Unix(3700, 0),
	}
	c.executePointersScan(ctx, node)
	out := logBuf.String()
	require.Contains(t, out, "pointersscan pipeline ready")
	require.Contains(t, out, "result=miss", "when cache is nil default is miss")
}

func TestTaskCacheIDMock_NilSelector_NoPanic(t *testing.T) {
	ctx := t.Context()
	fake := newFakeTaskCache()
	logger := log.NewNopLogger()
	c := &Context{
		logger:                   logger,
		metastore:                stubMetastore{},
		taskCacheIDCachePointers: fake,
	}
	node := &physical.PointersScan{
		Location: "index/0",
		Selector: nil, // nil selector must not panic when building logger
		Start:    time.Unix(10, 0),
		End:      time.Unix(3700, 0),
	}
	// Should not panic (nil selector in log.With was previously node.Selector.String()).
	pipeline := c.executePointersScan(ctx, node)
	require.NotNil(t, pipeline)
}
