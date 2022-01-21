package storage

import (
	"context"
	"fmt"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/go-kit/log/level"

	"github.com/grafana/loki/pkg/storage/chunk"
	util_log "github.com/grafana/loki/pkg/util/log"
)

const (
	cacheTimeout = 1 * time.Minute
)

type table struct {
	commonObjects []chunk.StorageObject
	userIDs       []chunk.StorageCommonPrefix
	userObjects   map[string][]chunk.StorageObject
}

type cachedObjectClient struct {
	chunk.ObjectClient

	tables       map[string]*table
	tableNames   []chunk.StorageCommonPrefix
	tablesMtx    sync.RWMutex
	cacheBuiltAt time.Time

	buildCacheChan chan struct{}
	buildCacheWg   sync.WaitGroup
	err            error
}

func newCachedObjectClient(downstreamClient chunk.ObjectClient) *cachedObjectClient {
	return &cachedObjectClient{
		ObjectClient:   downstreamClient,
		tables:         map[string]*table{},
		buildCacheChan: make(chan struct{}, 1),
	}
}

// buildCacheOnce makes sure we build the cache just once when it is called concurrently.
// We have a buffered channel here with a capacity of 1 to make sure only one concurrent call makes it through.
// We also have a sync.WaitGroup to make sure all the concurrent calls to buildCacheOnce wait until the cache gets rebuilt since
// we are doing read-through cache, and we do not want to serve stale results.
func (c *cachedObjectClient) buildCacheOnce(ctx context.Context) {
	c.buildCacheWg.Add(1)
	defer c.buildCacheWg.Done()

	// when the cache is expired, only one concurrent call must be able to rebuild it
	// all other calls will wait until the cache is built successfully or failed with an error
	select {
	case c.buildCacheChan <- struct{}{}:
		c.err = nil
		c.err = c.buildCache(ctx)
		<-c.buildCacheChan
		if c.err != nil {
			level.Error(util_log.Logger).Log("msg", "failed to build cache", "err", c.err)
		}
	default:
	}
}

func (c *cachedObjectClient) List(ctx context.Context, prefix, _ string) ([]chunk.StorageObject, []chunk.StorageCommonPrefix, error) {
	prefix = strings.TrimSuffix(prefix, delimiter)
	ss := strings.Split(prefix, delimiter)
	if len(ss) > 2 {
		return nil, nil, fmt.Errorf("invalid prefix %s", prefix)
	}

	if time.Since(c.cacheBuiltAt) >= cacheTimeout {
		c.buildCacheOnce(ctx)
	}

	// wait for cache build operation to finish, if running
	c.buildCacheWg.Wait()

	if c.err != nil {
		return nil, nil, c.err
	}

	c.tablesMtx.RLock()
	defer c.tablesMtx.RUnlock()

	// list of tables were requested
	if prefix == "" {
		return []chunk.StorageObject{}, c.tableNames, nil
	}

	// common objects and list of users having objects in a table were requested
	if len(ss) == 1 {
		tableName := ss[0]
		if table, ok := c.tables[tableName]; ok {
			return table.commonObjects, table.userIDs, nil
		}

		return []chunk.StorageObject{}, []chunk.StorageCommonPrefix{}, nil
	}

	// user objects in a table were requested
	tableName := ss[0]
	table, ok := c.tables[tableName]
	if !ok {
		return []chunk.StorageObject{}, []chunk.StorageCommonPrefix{}, nil
	}

	userID := ss[1]
	if objects, ok := table.userObjects[userID]; ok {
		return objects, []chunk.StorageCommonPrefix{}, nil
	}

	return []chunk.StorageObject{}, []chunk.StorageCommonPrefix{}, nil
}

// buildCache builds the cache if expired
func (c *cachedObjectClient) buildCache(ctx context.Context) error {
	if time.Since(c.cacheBuiltAt) < cacheTimeout {
		return nil
	}

	objects, _, err := c.ObjectClient.List(ctx, "", "")
	if err != nil {
		return err
	}

	c.tablesMtx.Lock()
	defer c.tablesMtx.Unlock()

	c.tables = map[string]*table{}
	c.tableNames = []chunk.StorageCommonPrefix{}

	for _, object := range objects {
		ss := strings.Split(object.Key, delimiter)
		if len(ss) < 2 || len(ss) > 3 {
			return fmt.Errorf("invalid key: %s", object.Key)
		}

		tableName := ss[0]
		tbl, ok := c.tables[tableName]
		if !ok {
			tbl = &table{
				commonObjects: []chunk.StorageObject{},
				userObjects:   map[string][]chunk.StorageObject{},
				userIDs:       []chunk.StorageCommonPrefix{},
			}
			c.tables[tableName] = tbl
			c.tableNames = append(c.tableNames, chunk.StorageCommonPrefix(tableName))
		}

		if len(ss) == 2 {
			tbl.commonObjects = append(tbl.commonObjects, object)
		} else {
			userID := ss[1]
			if len(tbl.userObjects[userID]) == 0 {
				tbl.userIDs = append(tbl.userIDs, chunk.StorageCommonPrefix(path.Join(tableName, userID)))
			}
			tbl.userObjects[userID] = append(tbl.userObjects[userID], object)
		}
	}

	c.cacheBuiltAt = time.Now()
	return nil
}
