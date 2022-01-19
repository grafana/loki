package storage

import (
	"context"
	"fmt"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/go-kit/log/level"

	util_log "github.com/cortexproject/cortex/pkg/util/log"

	"github.com/grafana/loki/pkg/storage/chunk"
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

	rebuildCacheChan chan struct{}
	err              error
}

func newCachedObjectClient(downstreamClient chunk.ObjectClient) *cachedObjectClient {
	return &cachedObjectClient{
		ObjectClient:     downstreamClient,
		tables:           map[string]*table{},
		rebuildCacheChan: make(chan struct{}, 1),
	}
}

func (c *cachedObjectClient) List(ctx context.Context, prefix, _ string) ([]chunk.StorageObject, []chunk.StorageCommonPrefix, error) {
	prefix = strings.TrimSuffix(prefix, delimiter)
	ss := strings.Split(prefix, delimiter)
	if len(ss) > 2 {
		return nil, nil, fmt.Errorf("invalid prefix %s", prefix)
	}

	if !c.cacheBuiltAt.Add(cacheTimeout).After(time.Now()) {
		select {
		case c.rebuildCacheChan <- struct{}{}:
			c.err = nil
			c.err = c.buildCache(ctx)
			<-c.rebuildCacheChan
			if c.err != nil {
				level.Error(util_log.Logger).Log("msg", "failed to build cache", "err", c.err)
			}
		default:
			for !c.cacheBuiltAt.Add(cacheTimeout).After(time.Now()) && c.err == nil {
				time.Sleep(time.Millisecond)
			}
		}
	}

	if c.err != nil {
		return nil, nil, c.err
	}

	c.tablesMtx.RLock()
	defer c.tablesMtx.RUnlock()

	if prefix == "" {
		return []chunk.StorageObject{}, c.tableNames, nil
	} else if len(ss) == 1 {
		tableName := ss[0]
		table, ok := c.tables[tableName]
		if !ok {
			return []chunk.StorageObject{}, []chunk.StorageCommonPrefix{}, nil
		}

		return table.commonObjects, table.userIDs, nil
	} else {
		tableName := ss[0]
		table, ok := c.tables[tableName]
		if !ok {
			return []chunk.StorageObject{}, []chunk.StorageCommonPrefix{}, nil
		}

		userID := ss[1]
		objects, ok := table.userObjects[userID]
		if !ok {
			return []chunk.StorageObject{}, []chunk.StorageCommonPrefix{}, nil
		}

		return objects, []chunk.StorageCommonPrefix{}, nil
	}
}

func (c *cachedObjectClient) buildCache(ctx context.Context) error {
	if c.cacheBuiltAt.Add(cacheTimeout).After(time.Now()) {
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
