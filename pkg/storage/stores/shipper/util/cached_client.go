package util

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/cortexproject/cortex/pkg/chunk"
)

const (
	delimiter    = "/"
	cacheTimeout = time.Minute
)

// CachedObjectClient is meant for reducing number of LIST calls on hosted object stores(S3, GCS, Azure Blob Storage and Swift).
// We as of now do a LIST call per table when we need to find its objects.
// CachedObjectClient does flat listing of objects which is only supported by hosted object stores mentioned above.
// In case of boltdb files stored by shipper, the listed objects would have keys like <table-name>/<filename>.
// For each List call without a prefix(which is actually done to get list of tables),
// CachedObjectClient would build a map of TableName -> chunk.StorageObject which would be used as a cache for subsequent List calls for getting list of objects for tables.
// Cache items are evicted after first read or a timeout. The cache is rebuilt during List call with empty prefix or we encounter a cache miss.
type CachedObjectClient struct {
	chunk.ObjectClient
	tables       map[string][]chunk.StorageObject
	tablesMtx    sync.Mutex
	cacheBuiltAt time.Time
}

func NewCachedObjectClient(downstreamClient chunk.ObjectClient) *CachedObjectClient {
	return &CachedObjectClient{
		ObjectClient: downstreamClient,
		tables:       map[string][]chunk.StorageObject{},
	}
}

func (c *CachedObjectClient) List(ctx context.Context, prefix, _ string) ([]chunk.StorageObject, []chunk.StorageCommonPrefix, error) {
	c.tablesMtx.Lock()
	defer c.tablesMtx.Unlock()

	if prefix == "" {
		tables, err := c.listTables(ctx)
		if err != nil {
			return nil, nil, err
		}

		return []chunk.StorageObject{}, tables, nil
	}

	// While listing objects in a table, prefix is set to <table-name>+delimiter so trim the delimiter first.
	tableName := strings.TrimSuffix(prefix, delimiter)
	if strings.Contains(tableName, delimiter) {
		return nil, nil, fmt.Errorf("invalid prefix %s for listing table objects", prefix)
	}
	tableObjects, err := c.listTableObjects(ctx, tableName)
	if err != nil {
		return nil, nil, err
	}

	return tableObjects, []chunk.StorageCommonPrefix{}, nil
}

// listTables assumes that tablesMtx is already locked by the caller
func (c *CachedObjectClient) listTables(ctx context.Context) ([]chunk.StorageCommonPrefix, error) {
	// do a flat listing by setting delimiter to empty string
	objects, _, err := c.ObjectClient.List(ctx, "", "")
	if err != nil {
		return nil, err
	}

	// build the cache and response containing just table names as chunk.StorageCommonPrefix
	var tableNames []chunk.StorageCommonPrefix
	for _, object := range objects {
		ss := strings.Split(object.Key, delimiter)
		if len(ss) != 2 {
			return nil, fmt.Errorf("invalid object key found %s", object.Key)
		}

		if _, ok := c.tables[ss[0]]; !ok {
			tableNames = append(tableNames, chunk.StorageCommonPrefix(ss[0]))
		}
		c.tables[ss[0]] = append(c.tables[ss[0]], object)
	}

	c.cacheBuiltAt = time.Now()

	return tableNames, nil
}

// listTableObjects assumes that tablesMtx is already locked by the caller
func (c *CachedObjectClient) listTableObjects(ctx context.Context, tableName string) ([]chunk.StorageObject, error) {
	objects, ok := c.tables[tableName]
	if ok && c.cacheBuiltAt.Add(cacheTimeout).After(time.Now()) {
		// evict the element read from cache
		delete(c.tables, tableName)
		return objects, nil
	}

	// requested element not found in the cache, rebuild the cache.
	_, err := c.listTables(ctx)
	if err != nil {
		return nil, err
	}

	objects = c.tables[tableName]
	// evict the element read from cache
	delete(c.tables, tableName)
	return objects, nil
}
