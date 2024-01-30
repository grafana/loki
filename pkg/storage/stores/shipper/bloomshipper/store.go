package bloomshipper

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"sort"

	"github.com/go-kit/log"
	"github.com/pkg/errors"
	"github.com/prometheus/common/model"

	"github.com/grafana/loki/pkg/storage"
	"github.com/grafana/loki/pkg/storage/chunk/cache"
	"github.com/grafana/loki/pkg/storage/chunk/client"
	"github.com/grafana/loki/pkg/storage/config"
)

type Store interface {
	SearchMetas(ctx context.Context, params MetaSearchParams) ([]Meta, error)
	Fetcher(ts model.Time) *Fetcher
	Stop()
}

var _ Client = &bloomStoreEntry{}
var _ Store = &bloomStoreEntry{}

type bloomStoreEntry struct {
	start        model.Time
	cfg          config.PeriodConfig
	objectClient client.ObjectClient
	bloomClient  Client
	fetcher      *Fetcher
}

// SearchMetas implements store.
func (b *bloomStoreEntry) SearchMetas(ctx context.Context, params MetaSearchParams) ([]Meta, error) {
	var refs []MetaRef
	tables := tablesForRange(b.cfg, params.Interval.Start, params.Interval.End)
	for _, table := range tables {
		prefix := filepath.Join(rootFolder, table, params.TenantID, metasFolder)
		list, _, err := b.objectClient.List(ctx, prefix, "")
		if err != nil {
			return nil, fmt.Errorf("error listing metas under prefix [%s]: %w", prefix, err)
		}
		for _, object := range list {
			metaRef, err := createMetaRef(object.Key, params.TenantID, table)

			if err != nil {
				return nil, err
			}
			if metaRef.MaxFingerprint < uint64(params.Keyspace.Min) || uint64(params.Keyspace.Max) < metaRef.MinFingerprint ||
				metaRef.EndTimestamp.Before(params.Interval.Start) || metaRef.StartTimestamp.After(params.Interval.End) {
				continue
			}
			refs = append(refs, metaRef)
		}
	}
	return b.fetcher.FetchMetas(ctx, refs)
}

// SearchMetas implements store.
func (b *bloomStoreEntry) Fetcher(_ model.Time) *Fetcher {
	return b.fetcher
}

// DeleteBlocks implements Client.
func (b *bloomStoreEntry) DeleteBlocks(ctx context.Context, refs []BlockRef) error {
	return b.bloomClient.DeleteBlocks(ctx, refs)
}

// DeleteMeta implements Client.
func (b *bloomStoreEntry) DeleteMeta(ctx context.Context, meta Meta) error {
	return b.bloomClient.DeleteMeta(ctx, meta)
}

// GetBlock implements Client.
func (b *bloomStoreEntry) GetBlock(ctx context.Context, ref BlockRef) (LazyBlock, error) {
	return b.bloomClient.GetBlock(ctx, ref)
}

// GetMetas implements Client.
func (b *bloomStoreEntry) GetMetas(ctx context.Context, refs []MetaRef) ([]Meta, error) {
	return b.bloomClient.GetMetas(ctx, refs)
}

// PutBlocks implements Client.
func (b *bloomStoreEntry) PutBlocks(ctx context.Context, blocks []Block) ([]Block, error) {
	return b.bloomClient.PutBlocks(ctx, blocks)
}

// PutMeta implements Client.
func (b *bloomStoreEntry) PutMeta(ctx context.Context, meta Meta) error {
	return b.bloomClient.PutMeta(ctx, meta)
}

// Stop implements Client.
func (b bloomStoreEntry) Stop() {
	b.bloomClient.Stop()
}

var _ Client = &BloomStore{}
var _ Store = &BloomStore{}

type BloomStore struct {
	stores        []*bloomStoreEntry
	storageConfig storage.Config
}

func NewBloomStore(
	periodicConfigs []config.PeriodConfig,
	storageConfig storage.Config,
	clientMetrics storage.ClientMetrics,
	metasCache cache.Cache,
	blocksCache *cache.EmbeddedCache[string, io.ReadCloser],
	logger log.Logger,
) (*BloomStore, error) {
	store := &BloomStore{
		storageConfig: storageConfig,
	}

	if metasCache == nil {
		metasCache = cache.NewNoopCache()
	}

	// sort by From time
	sort.Slice(periodicConfigs, func(i, j int) bool {
		return periodicConfigs[i].From.Time.Before(periodicConfigs[i].From.Time)
	})

	for _, periodicConfig := range periodicConfigs {
		objectClient, err := storage.NewObjectClient(periodicConfig.ObjectType, storageConfig, clientMetrics)
		if err != nil {
			return nil, errors.Wrapf(err, "creating object client for period %s", periodicConfig.From)
		}
		bloomClient, err := NewBloomClient(objectClient, logger)
		if err != nil {
			return nil, errors.Wrapf(err, "creating bloom client for period %s", periodicConfig.From)
		}
		fetcher, err := NewFetcher(bloomClient, metasCache, blocksCache, logger)
		if err != nil {
			return nil, errors.Wrapf(err, "creating fetcher for period %s", periodicConfig.From)
		}

		store.stores = append(store.stores, &bloomStoreEntry{
			start:        periodicConfig.From.Time,
			cfg:          periodicConfig,
			objectClient: objectClient,
			bloomClient:  bloomClient,
			fetcher:      fetcher,
		})
	}

	return store, nil
}

// SearchMetas implements store.
func (b *BloomStore) Fetcher(ts model.Time) *Fetcher {
	if store := b.getStore(ts); store != nil {
		return store.Fetcher(ts)
	}
	return nil
}

// SearchMetas implements store.
func (b *BloomStore) SearchMetas(ctx context.Context, params MetaSearchParams) ([]Meta, error) {
	var result []Meta
	err := b.forStores(ctx, params.Interval, func(innerCtx context.Context, interval Interval, store Store) error {
		newParams := params
		newParams.Interval = interval
		metas, err := store.SearchMetas(innerCtx, newParams)
		if err != nil {
			return err
		}
		result = append(result, metas...)
		return nil
	})
	return result, err
}

// DeleteBlocks implements Client.
func (b *BloomStore) DeleteBlocks(ctx context.Context, blocks []BlockRef) error {
	for _, ref := range blocks {
		err := b.storeDo(
			ref.StartTimestamp,
			func(s *bloomStoreEntry) error {
				return s.DeleteBlocks(ctx, []BlockRef{ref})
			},
		)
		if err != nil {
			return err
		}
	}
	return nil
}

// DeleteMeta implements Client.
func (b *BloomStore) DeleteMeta(ctx context.Context, meta Meta) error {
	return b.storeDo(meta.StartTimestamp, func(s *bloomStoreEntry) error {
		return s.DeleteMeta(ctx, meta)
	})
}

// GetBlock implements Client.
func (b *BloomStore) GetBlock(ctx context.Context, ref BlockRef) (LazyBlock, error) {
	var block LazyBlock
	var err error
	err = b.storeDo(ref.StartTimestamp, func(s *bloomStoreEntry) error {
		block, err = s.GetBlock(ctx, ref)
		return err
	})
	return block, err
}

// GetMetas implements Client.
func (b *BloomStore) GetMetas(ctx context.Context, metas []MetaRef) ([]Meta, error) {
	var refs [][]MetaRef
	var fetchers []*Fetcher

	for i := len(b.stores) - 1; i >= 0; i-- {
		s := b.stores[i]
		from, through := s.start, model.Latest
		if i < len(b.stores)-1 {
			through = b.stores[i+1].start
		}

		var res []MetaRef
		for _, meta := range metas {
			if meta.StartTimestamp >= from && meta.StartTimestamp < through {
				res = append(res, meta)
			}
		}

		if len(res) > 0 {
			refs = append(refs, res)
			fetchers = append(fetchers, s.Fetcher(s.start))
		}
	}

	results := make([]Meta, 0, len(metas))
	for i := range fetchers {
		res, err := fetchers[i].FetchMetas(ctx, refs[i])
		results = append(results, res...)
		if err != nil {
			return results, err
		}
	}

	return results, nil
}

// PutBlocks implements Client.
func (b *BloomStore) PutBlocks(ctx context.Context, blocks []Block) ([]Block, error) {
	results := make([]Block, 0, len(blocks))
	for _, ref := range blocks {
		err := b.storeDo(
			ref.StartTimestamp,
			func(s *bloomStoreEntry) error {
				res, err := s.PutBlocks(ctx, []Block{ref})
				results = append(results, res...)
				return err
			},
		)
		if err != nil {
			return nil, err
		}
	}
	return results, nil
}

// PutMeta implements Client.
func (b *BloomStore) PutMeta(ctx context.Context, meta Meta) error {
	return b.storeDo(meta.StartTimestamp, func(s *bloomStoreEntry) error {
		return s.PutMeta(ctx, meta)
	})
}

// Stop implements Client.
func (b *BloomStore) Stop() {
	for _, s := range b.stores {
		s.Stop()
	}
}

func (b *BloomStore) getStore(ts model.Time) *bloomStoreEntry {
	// find the schema with the lowest start _after_ tm
	j := sort.Search(len(b.stores), func(j int) bool {
		return b.stores[j].start > ts
	})

	// reduce it by 1 because we want a schema with start <= tm
	j--

	if 0 <= j && j < len(b.stores) {
		return b.stores[j]
	}

	return nil
}

func (b *BloomStore) storeDo(ts model.Time, f func(s *bloomStoreEntry) error) error {
	if store := b.getStore(ts); store != nil {
		return f(store)
	}
	return nil
}

func (b *BloomStore) forStores(ctx context.Context, interval Interval, f func(innerCtx context.Context, interval Interval, store Store) error) error {
	if len(b.stores) == 0 {
		return nil
	}

	from, through := interval.Start, interval.End

	// first, find the schema with the highest start _before or at_ from
	i := sort.Search(len(b.stores), func(i int) bool {
		return b.stores[i].start > from
	})
	if i > 0 {
		i--
	} else {
		// This could happen if we get passed a sample from before 1970.
		i = 0
		from = b.stores[0].start
	}

	// next, find the schema with the lowest start _after_ through
	j := sort.Search(len(b.stores), func(j int) bool {
		return b.stores[j].start > through
	})

	min := func(a, b model.Time) model.Time {
		if a < b {
			return a
		}
		return b
	}

	start := from
	for ; i < j; i++ {
		nextSchemaStarts := model.Latest
		if i+1 < len(b.stores) {
			nextSchemaStarts = b.stores[i+1].start
		}

		end := min(through, nextSchemaStarts-1)
		err := f(ctx, Interval{start, end}, b.stores[i])
		if err != nil {
			return err
		}

		start = nextSchemaStarts
	}
	return nil
}
