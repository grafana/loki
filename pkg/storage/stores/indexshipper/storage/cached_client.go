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
	cacheTimeout = 1 * time.Minute
)

type table struct {
	name          string
	mtx           sync.RWMutex
	commonObjects []client.StorageObject
	userIDs       []client.StorageCommonPrefix
	userObjects   map[string][]client.StorageObject

	cacheBuiltAt   time.Time
	buildCacheChan chan struct{}
	buildCacheWg   sync.WaitGroup
	err            error
}

func newTable(tableName string) *table {
	return &table{
		name:           tableName,
		buildCacheChan: make(chan struct{}, 1),
		userIDs:        []client.StorageCommonPrefix{},
		userObjects:    map[string][]client.StorageObject{},
		commonObjects:  []client.StorageObject{},
	}
}

type cachedObjectClient struct {
	client.ObjectClient

	tables                 map[string]*table
	tableNames             []client.StorageCommonPrefix
	tablesMtx              sync.RWMutex
	tableNamesCacheBuiltAt time.Time

	buildTableNamesCacheChan chan struct{}
	buildTableNamesCacheWg   sync.WaitGroup
	err                      error
}

func newCachedObjectClient(downstreamClient client.ObjectClient) *cachedObjectClient {
	return &cachedObjectClient{
		ObjectClient:             downstreamClient,
		tables:                   map[string]*table{},
		buildTableNamesCacheChan: make(chan struct{}, 1),
	}
}

// buildCacheOnce makes sure we build the cache just once when it is called concurrently.
// We have a buffered channel here with a capacity of 1 to make sure only one concurrent call makes it through.
// We also have a sync.WaitGroup to make sure all the concurrent calls to buildCacheOnce wait until the cache gets rebuilt since
// we are doing read-through cache, and we do not want to serve stale results.
func buildCacheOnce(buildCacheWg *sync.WaitGroup, buildCacheChan chan struct{}, buildCacheFunc func()) {
	buildCacheWg.Add(1)
	defer buildCacheWg.Done()

	// when the cache is expired, only one concurrent call must be able to rebuild it
	// all other calls will wait until the cache is built successfully or failed with an error
	select {
	case buildCacheChan <- struct{}{}:
		buildCacheFunc()
		<-buildCacheChan
	default:
	}
}

func (c *cachedObjectClient) RefreshIndexTableNamesCache(ctx context.Context) {
	buildCacheOnce(&c.buildTableNamesCacheWg, c.buildTableNamesCacheChan, func() {
		c.err = nil
		c.err = c.buildTableNamesCache(ctx, true)
	})
	c.buildTableNamesCacheWg.Wait()
}

func (c *cachedObjectClient) RefreshIndexTableCache(ctx context.Context, tableName string) {
	tbl := c.getTable(ctx, tableName)
	// It would be rare that a non-existent table name is being referred.
	// Should happen only when a table got deleted by compactor due to retention policy or user issued delete requests.
	if tbl == nil {
		return
	}

	buildCacheOnce(&tbl.buildCacheWg, tbl.buildCacheChan, func() {
		tbl.err = nil
		tbl.err = tbl.buildCache(ctx, c.ObjectClient, true)
	})
	tbl.buildCacheWg.Wait()
}

func (c *cachedObjectClient) List(ctx context.Context, prefix, objectDelimiter string, bypassCache bool) ([]client.StorageObject, []client.StorageCommonPrefix, error) {
	if bypassCache {
		return c.ObjectClient.List(ctx, prefix, objectDelimiter)
	}

	// if we have never built table names cache, let us build it first.
	if c.tableNamesCacheBuiltAt.IsZero() {
		c.RefreshIndexTableNamesCache(ctx)
	}

	prefix = strings.TrimSuffix(prefix, delimiter)
	ss := strings.Split(prefix, delimiter)
	if len(ss) > 2 {
		return nil, nil, fmt.Errorf("invalid prefix %s", prefix)
	}

	// list of tables were requested
	if prefix == "" {
		tableNames, err := c.listTableNames(ctx)
		return []client.StorageObject{}, tableNames, err
	}

	// common objects and list of users having objects in a table were requested
	if len(ss) == 1 {
		tableName := ss[0]
		return c.listTable(ctx, tableName)
	}

	// user objects in a table were requested
	tableName := ss[0]
	userID := ss[1]

	userObjects, err := c.listUserIndexInTable(ctx, tableName, userID)
	return userObjects, []client.StorageCommonPrefix{}, err
}

func (c *cachedObjectClient) listTableNames(ctx context.Context) ([]client.StorageCommonPrefix, error) {
	if time.Since(c.tableNamesCacheBuiltAt) >= cacheTimeout {
		buildCacheOnce(&c.buildTableNamesCacheWg, c.buildTableNamesCacheChan, func() {
			c.err = nil
			c.err = c.buildTableNamesCache(ctx, false)
		})
	}

	// wait for cache build operation to finish, if running
	c.buildTableNamesCacheWg.Wait()

	if c.err != nil {
		return nil, c.err
	}

	c.tablesMtx.RLock()
	defer c.tablesMtx.RUnlock()

	return c.tableNames, nil
}

func (c *cachedObjectClient) listTable(ctx context.Context, tableName string) ([]client.StorageObject, []client.StorageCommonPrefix, error) {
	tbl := c.getTable(ctx, tableName)
	if tbl == nil {
		return []client.StorageObject{}, []client.StorageCommonPrefix{}, nil
	}

	if time.Since(tbl.cacheBuiltAt) >= cacheTimeout {
		buildCacheOnce(&tbl.buildCacheWg, tbl.buildCacheChan, func() {
			tbl.err = nil
			tbl.err = tbl.buildCache(ctx, c.ObjectClient, false)
		})
	}

	// wait for cache build operation to finish, if running
	tbl.buildCacheWg.Wait()

	if tbl.err != nil {
		return nil, nil, tbl.err
	}

	tbl.mtx.RLock()
	defer tbl.mtx.RUnlock()

	return tbl.commonObjects, tbl.userIDs, nil
}

func (c *cachedObjectClient) listUserIndexInTable(ctx context.Context, tableName, userID string) ([]client.StorageObject, error) {
	tbl := c.getTable(ctx, tableName)
	if tbl == nil {
		return []client.StorageObject{}, nil
	}

	if time.Since(tbl.cacheBuiltAt) >= cacheTimeout {
		buildCacheOnce(&tbl.buildCacheWg, tbl.buildCacheChan, func() {
			tbl.err = nil
			tbl.err = tbl.buildCache(ctx, c.ObjectClient, false)
		})
	}

	// wait for cache build operation to finish, if running
	tbl.buildCacheWg.Wait()

	if tbl.err != nil {
		return nil, tbl.err
	}

	tbl.mtx.RLock()
	defer tbl.mtx.RUnlock()

	if objects, ok := tbl.userObjects[userID]; ok {
		return objects, nil
	}

	return []client.StorageObject{}, nil
}

func (c *cachedObjectClient) buildTableNamesCache(ctx context.Context, forceRefresh bool) (err error) {
	if !forceRefresh && time.Since(c.tableNamesCacheBuiltAt) < cacheTimeout {
		return nil
	}

	defer func() {
		if err != nil {
			level.Error(util_log.Logger).Log("msg", "failed to build table names cache", "err", err)
		}
	}()

	logger := spanlogger.FromContextWithFallback(ctx, util_log.Logger)
	level.Info(logger).Log("msg", "building table names cache")
	now := time.Now()
	defer func() {
		level.Info(logger).Log("msg", "table names cache built", "duration", time.Since(now))
	}()

	_, tableNames, err := c.ObjectClient.List(ctx, "", delimiter)
	if err != nil {
		return err
	}

	c.tablesMtx.Lock()
	defer c.tablesMtx.Unlock()

	tableNamesMap := make(map[string]struct{}, len(tableNames))
	tableNamesNormalized := make([]client.StorageCommonPrefix, len(tableNames))
	for i := range tableNames {
		tableName := strings.TrimSuffix(string(tableNames[i]), delimiter)
		tableNamesMap[tableName] = struct{}{}
		tableNamesNormalized[i] = client.StorageCommonPrefix(tableName)
		if _, ok := c.tables[tableName]; ok {
			continue
		}

		c.tables[tableName] = newTable(tableName)
	}

	for tableName := range c.tables {
		if _, ok := tableNamesMap[tableName]; ok {
			continue
		}

		delete(c.tables, tableName)
	}

	c.tableNames = tableNamesNormalized
	c.tableNamesCacheBuiltAt = time.Now()
	return nil
}

// getCachedTable returns a table instance that contains common objects and user objects.
// It only returns the table if it is in cache, otherwise it returns nil.
func (c *cachedObjectClient) getCachedTable(tableName string) *table {
	c.tablesMtx.RLock()
	defer c.tablesMtx.RUnlock()

	return c.tables[tableName]
}

// getTable returns a table instance that contains common objects and user objects.
// It tries to get read the table from cache first and refreshes the cache in
// case was not found. It then returns the table from the refreshed cache.
// It returns nil if the table was also not found after refreshing the cache.
func (c *cachedObjectClient) getTable(ctx context.Context, tableName string) *table {
	// First, get the table from tables cache.
	if t := c.getCachedTable(tableName); t != nil {
		return t
	}
	// If we did not find the table in the cache, let us force refresh the table names cache to see if we can find it.
	c.RefreshIndexTableNamesCache(ctx)
	return c.getCachedTable(tableName)
}

func (t *table) buildCache(ctx context.Context, objectClient client.ObjectClient, forceRefresh bool) (err error) {
	if !forceRefresh && time.Since(t.cacheBuiltAt) < cacheTimeout {
		return nil
	}

	defer func() {
		if err != nil {
			level.Error(util_log.Logger).Log("msg", "failed to build table cache", "table_name", t.name, "err", err)
		}
	}()

	logger := spanlogger.FromContextWithFallback(ctx, util_log.Logger)
	level.Info(logger).Log("msg", "building table cache")
	now := time.Now()
	defer func() {
		level.Info(logger).Log("msg", "table cache built", "duration", time.Since(now))
	}()

	objects, _, err := objectClient.List(ctx, t.name+delimiter, "")
	if err != nil {
		return err
	}

	t.mtx.Lock()
	defer t.mtx.Unlock()

	t.commonObjects = t.commonObjects[:0]
	t.userObjects = map[string][]client.StorageObject{}
	t.userIDs = t.userIDs[:0]

	for _, object := range objects {
		// The s3 client can also return the directory itself in the ListObjects.
		if object.Key == "" {
			continue
		}

		ss := strings.Split(object.Key, delimiter)
		if len(ss) < 2 || len(ss) > 3 {
			return fmt.Errorf("invalid key: %s", object.Key)
		}

		if len(ss) == 2 {
			t.commonObjects = append(t.commonObjects, object)
		} else {
			userID := ss[1]
			if len(t.userObjects[userID]) == 0 {
				t.userIDs = append(t.userIDs, client.StorageCommonPrefix(path.Join(t.name, userID)))
			}
			t.userObjects[userID] = append(t.userObjects[userID], object)
		}
	}

	t.cacheBuiltAt = time.Now()
	return nil
}
