package storage

import (
	"context"
	"fmt"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/go-kit/log/level"

	"github.com/grafana/loki/pkg/storage/chunk/client"
	util_log "github.com/grafana/loki/pkg/util/log"
	"github.com/grafana/loki/pkg/util/spanlogger"
)

const (
	cacheAutoRefreshInterval = 1 * time.Minute
)

type table struct {
	commonObjects []client.StorageObject
	userIDs       []client.StorageCommonPrefix
	userObjects   map[string][]client.StorageObject
}

type CacheRefresher interface {
	EnableCacheAutoRefresh()
	DisableCacheAutoRefresh()
	RefreshCache()
}

type CachedObjectClient interface {
	client.ObjectClient
	CacheRefresher
}

type cachedObjectClient struct {
	client.ObjectClient

	tables     map[string]*table
	tableNames []client.StorageCommonPrefix
	tablesMtx  sync.RWMutex

	// cacheAutoRefreshEnabledCount keeps a count of number of requests we get for enabling the auto refresh since
	// there could be multiple services requesting for enabling auto refresh at once.
	cacheAutoRefreshEnabledCount int
	cacheAutoRefreshMtx          sync.Mutex
	cacheAutoRefresher           *cacheAutoRefresher

	buildCacheChan chan struct{}
	buildCacheWg   sync.WaitGroup
	err            error
}

func newCachedObjectClient(downstreamClient client.ObjectClient) *cachedObjectClient {
	client := &cachedObjectClient{
		ObjectClient:   downstreamClient,
		tables:         map[string]*table{},
		buildCacheChan: make(chan struct{}, 1),
	}

	// do the initial build of the cache
	client.RefreshCache()

	return client
}

// EnableCacheAutoRefresh enables the cache auto refresh and does the initial refresh once.
func (c *cachedObjectClient) EnableCacheAutoRefresh() {
	c.cacheAutoRefreshMtx.Lock()
	defer c.cacheAutoRefreshMtx.Unlock()

	c.cacheAutoRefreshEnabledCount++
	// if this is the first request to (re)enable auto refresh, we need to do the initial refresh and reset the timer.
	if c.cacheAutoRefreshEnabledCount == 1 {
		c.RefreshCache()
		c.cacheAutoRefresher = newCacheAutoRefresher(c.RefreshCache, cacheAutoRefreshInterval)
	}
}

// DisableCacheAutoRefresh disables the auto refreshing of the cache.
func (c *cachedObjectClient) DisableCacheAutoRefresh() {
	c.cacheAutoRefreshMtx.Lock()
	defer c.cacheAutoRefreshMtx.Unlock()

	c.cacheAutoRefreshEnabledCount--
	if c.cacheAutoRefreshEnabledCount == 0 {
		c.cacheAutoRefresher.stop()
		c.cacheAutoRefresher = nil
	}
}

// Stop stops the cache auto refresh loop before stopping the underlying ObjectClient.
func (c *cachedObjectClient) Stop() {
	if c.cacheAutoRefresher != nil {
		c.cacheAutoRefresher.stop()
	}
	c.ObjectClient.Stop()
}

// RefreshCache refreshes the cache. It is safe to call this concurrently.
// This call is mostly used by tests and would be blocked until the cache is refreshed.
func (c *cachedObjectClient) RefreshCache() {
	c.buildCacheOnce()
	c.buildCacheWg.Wait()
}

// buildCacheOnce makes sure we build the cache just once when it is called concurrently.
// We have a buffered channel here with a capacity of 1 to make sure only one concurrent call makes it through.
// We also have a sync.WaitGroup to make sure all the concurrent calls to buildCacheOnce wait until the cache gets rebuilt since
// we want to guarantee the callers that we will have freshly built cache when we return.
func (c *cachedObjectClient) buildCacheOnce() {
	c.buildCacheWg.Add(1)
	defer c.buildCacheWg.Done()

	// when the cache is expired, only one concurrent call must be able to rebuild it
	// all other calls will wait until the cache is built successfully or failed with an error
	select {
	case c.buildCacheChan <- struct{}{}:
		c.err = nil
		c.err = c.buildCache(context.Background())
		<-c.buildCacheChan
		if c.err != nil {
			level.Error(util_log.Logger).Log("msg", "failed to build cache", "err", c.err)
		}
	default:
	}
}

func (c *cachedObjectClient) List(ctx context.Context, prefix, _ string) ([]client.StorageObject, []client.StorageCommonPrefix, error) {
	prefix = strings.TrimSuffix(prefix, delimiter)
	ss := strings.Split(prefix, delimiter)
	if len(ss) > 2 {
		return nil, nil, fmt.Errorf("invalid prefix %s", prefix)
	}

	if c.err != nil {
		return nil, nil, c.err
	}

	c.tablesMtx.RLock()
	defer c.tablesMtx.RUnlock()

	// list of tables were requested
	if prefix == "" {
		return []client.StorageObject{}, c.tableNames, nil
	}

	// common objects and list of users having objects in a table were requested
	if len(ss) == 1 {
		tableName := ss[0]
		if table, ok := c.tables[tableName]; ok {
			return table.commonObjects, table.userIDs, nil
		}

		return []client.StorageObject{}, []client.StorageCommonPrefix{}, nil
	}

	// user objects in a table were requested
	tableName := ss[0]
	table, ok := c.tables[tableName]
	if !ok {
		return []client.StorageObject{}, []client.StorageCommonPrefix{}, nil
	}

	userID := ss[1]
	if objects, ok := table.userObjects[userID]; ok {
		return objects, []client.StorageCommonPrefix{}, nil
	}

	return []client.StorageObject{}, []client.StorageCommonPrefix{}, nil
}

// buildCache builds the cache if expired
func (c *cachedObjectClient) buildCache(ctx context.Context) error {
	logger := spanlogger.FromContextWithFallback(ctx, util_log.Logger)
	level.Info(logger).Log("msg", "building index list cache")
	now := time.Now()
	defer func() {
		level.Info(logger).Log("msg", "index list cache built", "duration", time.Since(now))
	}()

	objects, _, err := c.ObjectClient.List(ctx, "", "")
	if err != nil {
		return err
	}

	tables := map[string]*table{}
	tableNames := []client.StorageCommonPrefix{}

	for _, object := range objects {
		ss := strings.Split(object.Key, delimiter)
		if len(ss) < 2 || len(ss) > 3 {
			return fmt.Errorf("invalid key: %s", object.Key)
		}

		tableName := ss[0]
		tbl, ok := tables[tableName]
		if !ok {
			tbl = &table{
				commonObjects: []client.StorageObject{},
				userObjects:   map[string][]client.StorageObject{},
				userIDs:       []client.StorageCommonPrefix{},
			}
			tables[tableName] = tbl
			tableNames = append(tableNames, client.StorageCommonPrefix(tableName))
		}

		if len(ss) == 2 {
			tbl.commonObjects = append(tbl.commonObjects, object)
		} else {
			userID := ss[1]
			if len(tbl.userObjects[userID]) == 0 {
				tbl.userIDs = append(tbl.userIDs, client.StorageCommonPrefix(path.Join(tableName, userID)))
			}
			tbl.userObjects[userID] = append(tbl.userObjects[userID], object)
		}
	}

	c.tablesMtx.Lock()
	defer c.tablesMtx.Unlock()

	c.tables = tables
	c.tableNames = tableNames

	return nil
}

type cacheAutoRefresher struct {
	refreshCacheFunc func()
	interval         time.Duration
	stopChan         chan struct{}
	wg               sync.WaitGroup
}

func newCacheAutoRefresher(refreshCacheFunc func(), interval time.Duration) *cacheAutoRefresher {
	car := &cacheAutoRefresher{
		refreshCacheFunc: refreshCacheFunc,
		interval:         interval,
		stopChan:         make(chan struct{}),
	}
	go car.loop()

	return car
}

func (c *cacheAutoRefresher) loop() {
	c.wg.Add(1)
	defer c.wg.Done()

	ticker := time.NewTicker(c.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			c.refreshCacheFunc()
		case <-c.stopChan:
			return
		}
	}
}

func (c *cacheAutoRefresher) stop() {
	close(c.stopChan)
	c.wg.Wait()
}
