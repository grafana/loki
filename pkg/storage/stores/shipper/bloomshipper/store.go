package bloomshipper

import (
	"context"
	"fmt"
	"path"
	"sort"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"golang.org/x/exp/slices"

	"github.com/grafana/loki/v3/pkg/storage"
	v1 "github.com/grafana/loki/v3/pkg/storage/bloom/v1"
	"github.com/grafana/loki/v3/pkg/storage/chunk/cache"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client/util"
	"github.com/grafana/loki/v3/pkg/storage/config"
	"github.com/grafana/loki/v3/pkg/util/constants"
	"github.com/grafana/loki/v3/pkg/util/mempool"
	"github.com/grafana/loki/v3/pkg/util/spanlogger"
)

var (
	errNoStore = errors.New("no store found for time")
)

type StoreBase interface {
	ResolveMetas(ctx context.Context, params MetaSearchParams) ([][]MetaRef, []*Fetcher, error)
	FetchMetas(ctx context.Context, params MetaSearchParams) ([]Meta, error)
	FetchBlocks(ctx context.Context, refs []BlockRef, opts ...FetchOption) ([]*CloseableBlockQuerier, error)
	TenantFilesForInterval(
		ctx context.Context, interval Interval,
		filter func(tenant string, object client.StorageObject) bool,
	) (map[string][]client.StorageObject, error)
	Fetcher(ts model.Time) (*Fetcher, error)
	Client(ts model.Time) (Client, error)
	Stop()
}

type Store interface {
	StoreBase
	BloomMetrics() *v1.Metrics
	Allocator() mempool.Allocator
}

type bloomStoreConfig struct {
	workingDirs      []string
	numWorkers       int
	maxBloomPageSize int
}

// Compiler check to ensure bloomStoreEntry implements the Store interface
var _ StoreBase = &bloomStoreEntry{}

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
		prefix := path.Join(rootFolder, table, params.TenantID, metasFolder)
		level.Debug(b.fetcher.logger).Log(
			"msg", "listing metas",
			"store", b.cfg.From,
			"table", table,
			"tenant", params.TenantID,
			"prefix", prefix,
		)
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

	// return empty metaRefs/fetchers if there are no refs
	if len(refs) == 0 {
		return [][]MetaRef{}, []*Fetcher{}, nil
	}

	return [][]MetaRef{refs}, []*Fetcher{b.fetcher}, nil
}

// FilterMetasOverlappingBounds filters metas that are within the given bounds.
// the input metas are expected to be sorted by fingerprint.
func FilterMetasOverlappingBounds(metas []Meta, bounds v1.FingerprintBounds) []Meta {
	withinBounds := make([]Meta, 0, len(metas))
	for _, meta := range metas {
		// We can stop iterating once we find an item greater
		// than the keyspace we're looking for
		if bounds.Cmp(meta.Bounds.Min) == v1.After {
			break
		}

		// Only check keyspace for now, because we don't have start/end timestamps in the refs
		if !bounds.Overlaps(meta.Bounds) {
			continue
		}

		withinBounds = append(withinBounds, meta)
	}

	return withinBounds
}

// FetchMetas implements store.
func (b *bloomStoreEntry) FetchMetas(ctx context.Context, params MetaSearchParams) ([]Meta, error) {
	logger := spanlogger.FromContext(ctx)

	resolverStart := time.Now()
	metaRefs, fetchers, err := b.ResolveMetas(ctx, params)
	resolverDuration := time.Since(resolverStart)
	if err != nil {
		return nil, err
	}
	if len(metaRefs) != len(fetchers) {
		return nil, errors.New("metaRefs and fetchers have unequal length")
	}

	var metaCt int
	for i := range metaRefs {
		metaCt += len(metaRefs[i])
	}
	logger.LogKV(
		"msg", "resolved metas",
		"metas", metaCt,
		"duration", resolverDuration,
	)

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

// FetchBlocks implements Store.
func (b *bloomStoreEntry) FetchBlocks(ctx context.Context, refs []BlockRef, opts ...FetchOption) ([]*CloseableBlockQuerier, error) {
	return b.fetcher.FetchBlocks(ctx, refs, opts...)
}

func (b *bloomStoreEntry) TenantFilesForInterval(
	ctx context.Context,
	interval Interval,
	filter func(tenant string, object client.StorageObject) bool,
) (map[string][]client.StorageObject, error) {
	tables := tablesForRange(b.cfg, interval)
	if len(tables) == 0 {
		return nil, nil
	}

	tenants := make(map[string][]client.StorageObject, 100)
	for _, table := range tables {
		prefix := path.Join(rootFolder, table)
		level.Debug(b.fetcher.logger).Log(
			"msg", "listing tenants",
			"store", b.cfg.From,
			"table", table,
			"prefix", prefix,
		)
		objects, _, err := b.objectClient.List(ctx, prefix, "")
		if err != nil {
			if b.objectClient.IsObjectNotFoundErr(err) {
				continue
			}

			return nil, fmt.Errorf("error listing tenants under prefix [%s]: %w", prefix, err)
		}
		if len(objects) == 0 {
			continue
		}

		// Sort objects by the key to ensure keys are sorted by tenant.
		cmpObj := func(a, b client.StorageObject) int {
			if a.Key < b.Key {
				return -1
			}
			if a.Key > b.Key {
				return 1
			}
			return 0
		}
		if !slices.IsSortedFunc(objects, cmpObj) {
			slices.SortFunc(objects, cmpObj)
		}

		for i := 0; i < len(objects); i++ {
			tenant, err := b.TenantPrefix(key(objects[i].Key))
			if err != nil {
				return nil, fmt.Errorf("error parsing tenant key [%s]: %w", objects[i].Key, err)
			}

			// Search next object with different tenant
			var j int
			for j = i + 1; j < len(objects); j++ {
				nextTenant, err := b.TenantPrefix(key(objects[j].Key))
				if err != nil {
					return nil, fmt.Errorf("error parsing tenant key [%s]: %w", objects[i].Key, err)
				}
				if nextTenant != tenant {
					break
				}
			}

			if _, ok := tenants[tenant]; !ok {
				tenants[tenant] = nil // Initialize tenant with empty slice
			}

			if filter != nil && !filter(tenant, objects[i]) {
				continue
			}

			// Add all objects for this tenant
			tenants[tenant] = append(tenants[tenant], objects[i:j]...)
			i = j - 1 // -1 because the loop will increment i by 1
		}
	}

	return tenants, nil
}

// Fetcher implements Store.
func (b *bloomStoreEntry) Fetcher(_ model.Time) (*Fetcher, error) {
	return b.fetcher, nil
}

// Client implements Store.
func (b *bloomStoreEntry) Client(_ model.Time) (Client, error) {
	return b.bloomClient, nil
}

// Stop implements Store.
func (b bloomStoreEntry) Stop() {
	b.bloomClient.Stop()
	b.fetcher.Close()
}

// Compiler check to ensure BloomStore implements the Store interface
var _ Store = &BloomStore{}

type BloomStore struct {
	stores             []*bloomStoreEntry
	storageConfig      storage.Config
	metrics            *storeMetrics
	bloomMetrics       *v1.Metrics
	logger             log.Logger
	allocator          mempool.Allocator
	defaultKeyResolver // TODO(owen-d): impl schema aware resolvers
}

func NewBloomStore(
	periodicConfigs []config.PeriodConfig,
	storageConfig storage.Config,
	clientMetrics storage.ClientMetrics,
	metasCache cache.Cache,
	blocksCache Cache,
	allocator mempool.Allocator,
	reg prometheus.Registerer,
	logger log.Logger,
) (*BloomStore, error) {
	store := &BloomStore{
		storageConfig: storageConfig,
		metrics:       newStoreMetrics(reg, constants.Loki, "bloom_store"),
		bloomMetrics:  v1.NewMetrics(reg),
		allocator:     allocator,
		logger:        logger,
	}

	if metasCache == nil {
		metasCache = cache.NewNoopCache()
	}

	if blocksCache == nil {
		return nil, errors.New("no blocks cache")
	}

	// sort by From time
	sort.Slice(periodicConfigs, func(i, j int) bool {
		return periodicConfigs[i].From.Time.Before(periodicConfigs[j].From.Time)
	})

	// TODO(chaudum): Remove wrapper
	cfg := bloomStoreConfig{
		workingDirs:      storageConfig.BloomShipperConfig.WorkingDirectory,
		numWorkers:       storageConfig.BloomShipperConfig.DownloadParallelism,
		maxBloomPageSize: int(storageConfig.BloomShipperConfig.MaxQueryPageSize),
	}

	for _, wd := range cfg.workingDirs {
		if err := util.EnsureDirectory(wd); err != nil {
			return nil, errors.Wrapf(err, "failed to create working directory for bloom store: '%s'", wd)
		}
		if err := util.RequirePermissions(wd, 0o700); err != nil {
			return nil, errors.Wrapf(err, "insufficient permissions on working directory for bloom store: '%s'", wd)
		}
	}

	for _, periodicConfig := range periodicConfigs {
		objectClient, err := storage.NewObjectClient(periodicConfig.ObjectType, storageConfig, clientMetrics)
		if err != nil {
			return nil, errors.Wrapf(err, "creating object client for period %s", periodicConfig.From)
		}

		if storageConfig.BloomShipperConfig.CacheListOps {
			objectClient = newCachedListOpObjectClient(objectClient, 5*time.Minute, 10*time.Second)
		}
		bloomClient, err := NewBloomClient(cfg, objectClient, logger)
		if err != nil {
			return nil, errors.Wrapf(err, "creating bloom client for period %s", periodicConfig.From)
		}

		regWithLabels := prometheus.WrapRegistererWith(prometheus.Labels{"store": periodicConfig.From.String()}, reg)
		fetcher, err := NewFetcher(cfg, bloomClient, metasCache, blocksCache, regWithLabels, logger, store.bloomMetrics)
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

func (b *BloomStore) BloomMetrics() *v1.Metrics {
	return b.bloomMetrics
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

func (b *BloomStore) TenantFilesForInterval(
	ctx context.Context,
	interval Interval,
	filter func(tenant string, object client.StorageObject) bool,
) (map[string][]client.StorageObject, error) {
	var allTenants map[string][]client.StorageObject

	err := b.forStores(ctx, interval, func(innerCtx context.Context, interval Interval, store StoreBase) error {
		tenants, err := store.TenantFilesForInterval(innerCtx, interval, filter)
		if err != nil {
			return err
		}

		if allTenants == nil {
			allTenants = tenants
			return nil
		}

		for tenant, files := range tenants {
			allTenants[tenant] = append(allTenants[tenant], files...)
		}

		return nil
	})

	return allTenants, err
}

// Fetcher implements Store.
func (b *BloomStore) Fetcher(ts model.Time) (*Fetcher, error) {
	if store := b.getStore(ts); store != nil {
		return store.Fetcher(ts)
	}
	return nil, errNoStore
}

// Client implements Store.
func (b *BloomStore) Client(ts model.Time) (Client, error) {
	if store := b.getStore(ts); store != nil {
		return store.Client(ts)
	}
	return nil, errNoStore
}

// Allocator implements Store.
func (b *BloomStore) Allocator() mempool.Allocator {
	return b.allocator
}

// ResolveMetas implements Store.
func (b *BloomStore) ResolveMetas(ctx context.Context, params MetaSearchParams) ([][]MetaRef, []*Fetcher, error) {
	refs := make([][]MetaRef, 0, len(b.stores))
	fetchers := make([]*Fetcher, 0, len(b.stores))

	err := b.forStores(ctx, params.Interval, func(innerCtx context.Context, interval Interval, store StoreBase) error {
		newParams := params
		newParams.Interval = interval
		metas, fetcher, err := store.ResolveMetas(innerCtx, newParams)
		if err != nil {
			return err
		}
		if len(metas) > 0 {
			// only append if there are any results
			refs = append(refs, metas...)
			fetchers = append(fetchers, fetcher...)
		}
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

	metas := []Meta{}
	for i := range fetchers {
		res, err := fetchers[i].FetchMetas(ctx, metaRefs[i])
		if err != nil {
			return nil, err
		}
		if len(res) > 0 {
			metas = append(metas, res...)
		}
	}
	return metas, nil
}

// partitionBlocksByFetcher returns a slice of BlockRefs for each fetcher
func (b *BloomStore) partitionBlocksByFetcher(blocks []BlockRef) (refs [][]BlockRef, fetchers []*Fetcher) {
	for i := len(b.stores) - 1; i >= 0; i-- {
		s := b.stores[i]
		from, through := s.start, model.Latest
		if i < len(b.stores)-1 {
			through = b.stores[i+1].start
		}

		var res []BlockRef
		for _, ref := range blocks {
			if ref.StartTimestamp >= from && ref.StartTimestamp < through {
				res = append(res, ref)
			}
		}

		if len(res) > 0 {
			refs = append(refs, res)
			fetchers = append(fetchers, s.fetcher)
		}
	}

	return refs, fetchers
}

// FetchBlocks implements Store.
func (b *BloomStore) FetchBlocks(ctx context.Context, blocks []BlockRef, opts ...FetchOption) ([]*CloseableBlockQuerier, error) {
	refs, fetchers := b.partitionBlocksByFetcher(blocks)

	results := make([]*CloseableBlockQuerier, 0, len(blocks))
	for i := range fetchers {
		res, err := fetchers[i].FetchBlocks(ctx, refs[i], opts...)
		if err != nil {
			return results, err
		}
		results = append(results, res...)
	}

	// sort responses (results []*CloseableBlockQuerier) based on requests (blocks []BlockRef)
	sortBlocks(results, blocks)

	return results, nil
}

func sortBlocks(bqs []*CloseableBlockQuerier, refs []BlockRef) {
	tmp := make([]*CloseableBlockQuerier, len(refs))
	for _, bq := range bqs {
		if bq == nil {
			// ignore responses with nil values
			continue
		}
		idx := slices.Index(refs, bq.BlockRef)
		if idx < 0 {
			// not found
			// should not happen in the context of sorting responses based on requests
			continue
		}
		tmp[idx] = bq
	}
	copy(bqs, tmp)
}

// Stop implements Store.
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

	// should in theory never happen
	return nil
}

func (b *BloomStore) storeDo(ts model.Time, f func(s *bloomStoreEntry) error) error {
	if store := b.getStore(ts); store != nil {
		return f(store)
	}
	return fmt.Errorf("no store found for timestamp %s", ts.Time())
}

func (b *BloomStore) forStores(ctx context.Context, interval Interval, f func(innerCtx context.Context, interval Interval, store StoreBase) error) error {
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

	// end is exclusive, so if it's on a step boundary, we don't want to include it
	if interval.End.Unix()%step == 0 {
		upper--
	}

	tables := make([]string, 0, 1+upper-lower)
	for i := lower; i <= upper; i++ {
		tables = append(tables, fmt.Sprintf("%s%d", periodConfig.IndexTables.Prefix, i))
	}
	return tables
}
