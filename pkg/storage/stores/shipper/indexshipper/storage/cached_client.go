package storage

import (
	"context"
	"fmt"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/go-kit/log/level"
	"github.com/opentracing/opentracing-go"
	"golang.org/x/sync/singleflight"

	"github.com/grafana/loki/v3/pkg/storage/chunk/client"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
)

const (
	cacheTimeout = 1 * time.Minute
	refreshKey   = "refresh"
)

type table struct {
	name          string
	mtx           sync.RWMutex
	commonObjects []client.StorageObject
	userIDs       []client.StorageCommonPrefix
	userObjects   map[string][]client.StorageObject

	cacheBuiltAt    time.Time
	buildCacheGroup singleflight.Group
}

func newTable(tableName string) *table {
	return &table{
		name:          tableName,
		userIDs:       []client.StorageCommonPrefix{},
		userObjects:   map[string][]client.StorageObject{},
		commonObjects: []client.StorageObject{},
	}
}

type cachedObjectClient struct {
	client.ObjectClient

	tables                 map[string]*table
	tableNames             []client.StorageCommonPrefix
	tablesMtx              sync.RWMutex
	tableNamesCacheBuiltAt time.Time

	buildCacheGroup singleflight.Group
}

func newCachedObjectClient(downstreamClient client.ObjectClient) *cachedObjectClient {
	return &cachedObjectClient{
		ObjectClient: downstreamClient,
		tables:       map[string]*table{},
	}
}

func (c *cachedObjectClient) RefreshIndexTableNamesCache(ctx context.Context) {
	_, _, _ = c.buildCacheGroup.Do(refreshKey, func() (interface{}, error) {
		return nil, c.buildTableNamesCache(ctx)
	})
}

func (c *cachedObjectClient) RefreshIndexTableCache(ctx context.Context, tableName string) {
	tbl := c.getTable(ctx, tableName)
	// It would be rare that a non-existent table name is being referred.
	// Should happen only when a table got deleted by compactor due to retention policy or user issued delete requests.
	if tbl == nil {
		return
	}

	_, _, _ = tbl.buildCacheGroup.Do(refreshKey, func() (interface{}, error) {
		err := tbl.buildCache(ctx, c.ObjectClient)
		return nil, err
	})
}

func (c *cachedObjectClient) List(ctx context.Context, prefix, objectDelimiter string, bypassCache bool) ([]client.StorageObject, []client.StorageCommonPrefix, error) {
	if bypassCache {
		return c.ObjectClient.List(ctx, prefix, objectDelimiter)
	}

	c.tablesMtx.RLock()
	neverBuiltCache := c.tableNamesCacheBuiltAt.IsZero()
	c.tablesMtx.RUnlock()

	// if we have never built table names cache, let us build it first.
	if neverBuiltCache {
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
	err := c.updateTableNamesCache(ctx)
	if err != nil {
		return nil, err
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

	err := tbl.updateCache(ctx, c.ObjectClient)
	if err != nil {
		return nil, nil, err
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

	err := tbl.updateCache(ctx, c.ObjectClient)
	if err != nil {
		return nil, err
	}

	tbl.mtx.RLock()
	defer tbl.mtx.RUnlock()

	if objects, ok := tbl.userObjects[userID]; ok {
		return objects, nil
	}

	return []client.StorageObject{}, nil
}

// Check if the cache is out of date, and build it if so, ensuring only one cache-build is running at a time.
func (c *cachedObjectClient) updateTableNamesCache(ctx context.Context) error {
	c.tablesMtx.RLock()
	outOfDate := time.Since(c.tableNamesCacheBuiltAt) >= cacheTimeout
	c.tablesMtx.RUnlock()
	if !outOfDate {
		return nil
	}
	_, err, _ := c.buildCacheGroup.Do(refreshKey, func() (interface{}, error) {
		return nil, c.buildTableNamesCache(ctx)
	})
	return err
}

func (c *cachedObjectClient) buildTableNamesCache(ctx context.Context) (err error) {
	defer func() {
		if err != nil {
			level.Error(util_log.Logger).Log("msg", "failed to build table names cache", "err", err)
		}
	}()

	if sp := opentracing.SpanFromContext(ctx); sp != nil {
		sp.LogKV("msg", "building table names cache")
		now := time.Now()
		defer func() {
			sp.LogKV("msg", "table names cache built", "duration", time.Since(now))
		}()
	}

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

// Check if the cache is out of date, and build it if so, ensuring only one cache-build is running at a time.
func (t *table) updateCache(ctx context.Context, objectClient client.ObjectClient) error {
	t.mtx.RLock()
	outOfDate := time.Since(t.cacheBuiltAt) >= cacheTimeout
	t.mtx.RUnlock()
	if !outOfDate {
		return nil
	}
	_, err, _ := t.buildCacheGroup.Do(refreshKey, func() (interface{}, error) {
		err := t.buildCache(ctx, objectClient)
		return nil, err
	})
	return err
}

func (t *table) buildCache(ctx context.Context, objectClient client.ObjectClient) (err error) {
	defer func() {
		if err != nil {
			level.Error(util_log.Logger).Log("msg", "failed to build table cache", "table_name", t.name, "err", err)
		}
	}()

	if sp := opentracing.SpanFromContext(ctx); sp != nil {
		sp.LogKV("msg", "building table cache")
		now := time.Now()
		defer func() {
			sp.LogKV("msg", "table cache built", "duration", time.Since(now))
		}()
	}

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
