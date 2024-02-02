package bloomshipper

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"

	"github.com/go-kit/log"
	"github.com/pkg/errors"
	"github.com/prometheus/common/model"

	"github.com/grafana/loki/pkg/storage"
	v1 "github.com/grafana/loki/pkg/storage/bloom/v1"
	"github.com/grafana/loki/pkg/storage/chunk/cache"
	"github.com/grafana/loki/pkg/storage/chunk/client"
	"github.com/grafana/loki/pkg/storage/config"
)

type Store interface {
	ResolveMetas(ctx context.Context, params MetaSearchParams) ([][]MetaRef, []*Fetcher, error)
	FetchMetas(ctx context.Context, params MetaSearchParams) ([]Meta, error)
	Fetcher(ts model.Time) *Fetcher
	Stop()
}

type bloomStoreConfig struct {
	workingDir string
	numWorkers int
}

// Compiler check to ensure bloomStoreEntry implements the Client interface
var _ Client = &bloomStoreEntry{}

// Compiler check to ensure bloomStoreEntry implements the Store interface
var _ Store = &bloomStoreEntry{}

type bloomStoreEntry struct {
	start              model.Time
	cfg                config.PeriodConfig
	objectClient       client.ObjectClient
	bloomClient        Client
	fetcher            *Fetcher
	defaultKeyResolver // TODO(owen-d): impl schema aware resolvers
}

// ResolveMetas implements store.
func (b *bloomStoreEntry) ResolveMetas(ctx context.Context, params MetaSearchParams) ([][]MetaRef, []*Fetcher, error) {
	var refs []MetaRef
	tables := tablesForRange(b.cfg, params.Interval)
	for _, table := range tables {
		prefix := filepath.Join(rootFolder, table, params.TenantID, metasFolder)
		list, _, err := b.objectClient.List(ctx, prefix, "")
		if err != nil {
			return nil, nil, fmt.Errorf("error listing metas under prefix [%s]: %w", prefix, err)
		}
		for _, object := range list {
			metaRef, err := b.ParseMetaKey(key(object.Key))

			if err != nil {
				return nil, nil, err
			}

			// LIST calls return keys in lexicographic order.
			// Since fingerprints are the first part of the path,
			// we can stop iterating once we find an item greater
			// than the keyspace we're looking for
			if params.Keyspace.Cmp(metaRef.Bounds.Min) == v1.After {
				break
			}

			// Only check keyspace for now, because we don't have start/end timestamps in the refs
			if !params.Keyspace.Overlaps(metaRef.Bounds) {
				continue
			}

			refs = append(refs, metaRef)
		}
	}
	return [][]MetaRef{refs}, []*Fetcher{b.fetcher}, nil
}

// FetchMetas implements store.
func (b *bloomStoreEntry) FetchMetas(ctx context.Context, params MetaSearchParams) ([]Meta, error) {
	metaRefs, fetchers, err := b.ResolveMetas(ctx, params)
	if err != nil {
		return nil, err
	}
	if len(metaRefs) != len(fetchers) {
		return nil, errors.New("metaRefs and fetchers have unequal length")
	}

	var metas []Meta
	for i := range fetchers {
		res, err := fetchers[i].FetchMetas(ctx, metaRefs[i])
		if err != nil {
			return nil, err
		}
		metas = append(metas, res...)
	}
	return metas, nil
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
func (b *bloomStoreEntry) DeleteMetas(ctx context.Context, refs []MetaRef) error {
	return b.bloomClient.DeleteMetas(ctx, refs)
}

// GetBlock implements Client.
func (b *bloomStoreEntry) GetBlock(ctx context.Context, ref BlockRef) (BlockDirectory, error) {
	return b.bloomClient.GetBlock(ctx, ref)
}

// GetBlocks implements Client.
func (b *bloomStoreEntry) GetBlocks(ctx context.Context, refs []BlockRef) ([]BlockDirectory, error) {
	return b.fetcher.FetchBlocks(ctx, refs)
}

// GetMeta implements Client.
func (b *bloomStoreEntry) GetMeta(ctx context.Context, ref MetaRef) (Meta, error) {
	return b.bloomClient.GetMeta(ctx, ref)
}

// GetMetas implements Client.
func (b *bloomStoreEntry) GetMetas(ctx context.Context, refs []MetaRef) ([]Meta, error) {
	return b.fetcher.FetchMetas(ctx, refs)
}

// PutBlocks implements Client.
func (b *bloomStoreEntry) PutBlock(ctx context.Context, block Block) error {
	return b.bloomClient.PutBlock(ctx, block)
}

// PutMeta implements Client.
func (b *bloomStoreEntry) PutMeta(ctx context.Context, meta Meta) error {
	return b.bloomClient.PutMeta(ctx, meta)
}

// Stop implements Client.
func (b bloomStoreEntry) Stop() {
	b.bloomClient.Stop()
}

// Compiler check to ensure BloomStore implements the Client interface
var _ Client = &BloomStore{}

// Compiler check to ensure BloomStore implements the Store interface
var _ Store = &BloomStore{}

type BloomStore struct {
	stores             []*bloomStoreEntry
	storageConfig      storage.Config
	defaultKeyResolver // TODO(owen-d): impl schema aware resolvers
}

func NewBloomStore(
	periodicConfigs []config.PeriodConfig,
	storageConfig storage.Config,
	clientMetrics storage.ClientMetrics,
	metasCache cache.Cache,
	blocksCache *cache.EmbeddedCache[string, BlockDirectory],
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

	// TODO(chaudum): Remove wrapper
	cfg := bloomStoreConfig{
		workingDir: storageConfig.BloomShipperConfig.WorkingDirectory,
		numWorkers: storageConfig.BloomShipperConfig.BlocksDownloadingQueue.WorkersCount,
	}

	for _, periodicConfig := range periodicConfigs {
		objectClient, err := storage.NewObjectClient(periodicConfig.ObjectType, storageConfig, clientMetrics)
		if err != nil {
			return nil, errors.Wrapf(err, "creating object client for period %s", periodicConfig.From)
		}

		bloomClient, err := NewBloomClient(cfg, objectClient, logger)
		if err != nil {
			return nil, errors.Wrapf(err, "creating bloom client for period %s", periodicConfig.From)
		}

		fetcher, err := NewFetcher(cfg, bloomClient, metasCache, blocksCache, logger)
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

// Impements KeyResolver
func (b *BloomStore) Meta(ref MetaRef) (loc Location) {
	_ = b.storeDo(ref.StartTimestamp, func(s *bloomStoreEntry) error {
		loc = s.Meta(ref)
		return nil
	})

	// NB(owen-d): should not happen unless a ref is requested outside the store's accepted range.
	// This should be prevented during query validation
	if loc == nil {
		loc = defaultKeyResolver{}.Meta(ref)
	}

	return
}

// Impements KeyResolver
func (b *BloomStore) Block(ref BlockRef) (loc Location) {
	_ = b.storeDo(ref.StartTimestamp, func(s *bloomStoreEntry) error {
		loc = s.Block(ref)
		return nil
	})

	// NB(owen-d): should not happen unless a ref is requested outside the store's accepted range.
	// This should be prevented during query validation
	if loc == nil {
		loc = defaultKeyResolver{}.Block(ref)
	}

	return
}

// Fetcher implements Store.
func (b *BloomStore) Fetcher(ts model.Time) *Fetcher {
	if store := b.getStore(ts); store != nil {
		return store.Fetcher(ts)
	}
	return nil
}

// ResolveMetas implements Store.
func (b *BloomStore) ResolveMetas(ctx context.Context, params MetaSearchParams) ([][]MetaRef, []*Fetcher, error) {
	var refs [][]MetaRef
	var fetchers []*Fetcher
	err := b.forStores(ctx, params.Interval, func(innerCtx context.Context, interval Interval, store Store) error {
		newParams := params
		newParams.Interval = interval
		metas, fetcher, err := store.ResolveMetas(innerCtx, newParams)
		if err != nil {
			return err
		}
		refs = append(refs, metas...)
		fetchers = append(fetchers, fetcher...)
		return nil
	})
	return refs, fetchers, err
}

// FetchMetas implements Store.
func (b *BloomStore) FetchMetas(ctx context.Context, params MetaSearchParams) ([]Meta, error) {
	metaRefs, fetchers, err := b.ResolveMetas(ctx, params)
	if err != nil {
		return nil, err
	}
	if len(metaRefs) != len(fetchers) {
		return nil, errors.New("metaRefs and fetchers have unequal length")
	}

	var metas []Meta
	for i := range fetchers {
		res, err := fetchers[i].FetchMetas(ctx, metaRefs[i])
		if err != nil {
			return nil, err
		}
		metas = append(metas, res...)
	}
	return metas, nil
}

// DeleteBlocks implements Client.
func (b *BloomStore) DeleteBlocks(ctx context.Context, refs []BlockRef) error {
	for _, ref := range refs {
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

// DeleteMetas implements Client.
func (b *BloomStore) DeleteMetas(ctx context.Context, refs []MetaRef) error {
	for _, ref := range refs {
		err := b.storeDo(
			ref.StartTimestamp,
			func(s *bloomStoreEntry) error {
				return s.DeleteMetas(ctx, []MetaRef{ref})
			},
		)
		if err != nil {
			return err
		}
	}
	return nil
}

// GetBlock implements Client.
func (b *BloomStore) GetBlock(ctx context.Context, ref BlockRef) (BlockDirectory, error) {
	res := make([]BlockDirectory, 1)
	err := b.storeDo(ref.StartTimestamp, func(s *bloomStoreEntry) error {
		block, err := s.GetBlock(ctx, ref)
		if err != nil {
			return err
		}
		res[0] = block
		return nil
	})
	return res[0], err
}

// GetBlocks implements Client.
func (b *BloomStore) GetBlocks(ctx context.Context, blocks []BlockRef) ([]BlockDirectory, error) {
	var refs [][]BlockRef
	var fetchers []*Fetcher

	for i := len(b.stores) - 1; i >= 0; i-- {
		s := b.stores[i]
		from, through := s.start, model.Latest
		if i < len(b.stores)-1 {
			through = b.stores[i+1].start
		}

		var res []BlockRef
		for _, meta := range blocks {
			if meta.StartTimestamp >= from && meta.StartTimestamp < through {
				res = append(res, meta)
			}
		}

		if len(res) > 0 {
			refs = append(refs, res)
			fetchers = append(fetchers, s.Fetcher(s.start))
		}
	}

	results := make([]BlockDirectory, 0, len(blocks))
	for i := range fetchers {
		res, err := fetchers[i].FetchBlocks(ctx, refs[i])
		results = append(results, res...)
		if err != nil {
			return results, err
		}
	}

	return results, nil
}

// GetMeta implements Client.
func (b *BloomStore) GetMeta(ctx context.Context, ref MetaRef) (Meta, error) {
	res := make([]Meta, 1)
	err := b.storeDo(ref.StartTimestamp, func(s *bloomStoreEntry) error {
		meta, err := s.GetMeta(ctx, ref)
		if err != nil {
			return err
		}
		res[0] = meta
		return nil
	})
	return res[0], err
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

// PutBlock implements Client.
func (b *BloomStore) PutBlock(ctx context.Context, block Block) error {
	return b.storeDo(block.StartTimestamp, func(s *bloomStoreEntry) error {
		return s.PutBlock(ctx, block)
	})
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
	return fmt.Errorf("no store found for timestamp %s", ts.Time())
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
		err := f(ctx, NewInterval(start, end), b.stores[i])
		if err != nil {
			return err
		}

		start = nextSchemaStarts
	}
	return nil
}

func tablesForRange(periodConfig config.PeriodConfig, interval Interval) []string {
	step := int64(periodConfig.IndexTables.Period.Seconds())
	lower := interval.Start.Unix() / step
	upper := interval.End.Unix() / step
	tables := make([]string, 0, 1+upper-lower)
	for i := lower; i <= upper; i++ {
		tables = append(tables, fmt.Sprintf("%s%d", periodConfig.IndexTables.Prefix, i))
	}
	return tables
}
