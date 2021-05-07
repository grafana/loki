// Copyright (c) The Thanos Authors.
// Licensed under the Apache License 2.0.

package block

import (
	"context"
	"encoding/json"
	"io/ioutil"
	"os"
	"path"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/golang/groupcache/singleflight"
	"github.com/oklog/ulid"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/prometheus/prometheus/pkg/relabel"
	"github.com/prometheus/prometheus/tsdb"
	"golang.org/x/sync/errgroup"
	"gopkg.in/yaml.v2"

	"github.com/thanos-io/thanos/pkg/block/metadata"
	"github.com/thanos-io/thanos/pkg/errutil"
	"github.com/thanos-io/thanos/pkg/extprom"
	"github.com/thanos-io/thanos/pkg/model"
	"github.com/thanos-io/thanos/pkg/objstore"
	"github.com/thanos-io/thanos/pkg/runutil"
)

const FetcherConcurrency = 32

// FetcherMetrics holds metrics tracked by the metadata fetcher. This struct and its fields are exported
// to allow depending projects (eg. Cortex) to implement their own custom metadata fetcher while tracking
// compatible metrics.
type FetcherMetrics struct {
	Syncs        prometheus.Counter
	SyncFailures prometheus.Counter
	SyncDuration prometheus.Histogram

	Synced   *extprom.TxGaugeVec
	Modified *extprom.TxGaugeVec
}

// Submit applies new values for metrics tracked by transaction GaugeVec.
func (s *FetcherMetrics) Submit() {
	s.Synced.Submit()
	s.Modified.Submit()
}

// ResetTx starts new transaction for metrics tracked by transaction GaugeVec.
func (s *FetcherMetrics) ResetTx() {
	s.Synced.ResetTx()
	s.Modified.ResetTx()
}

const (
	fetcherSubSys = "blocks_meta"

	CorruptedMeta = "corrupted-meta-json"
	NoMeta        = "no-meta-json"
	LoadedMeta    = "loaded"
	FailedMeta    = "failed"

	// Synced label values.
	labelExcludedMeta = "label-excluded"
	timeExcludedMeta  = "time-excluded"
	tooFreshMeta      = "too-fresh"
	duplicateMeta     = "duplicate"
	// Blocks that are marked for deletion can be loaded as well. This is done to make sure that we load blocks that are meant to be deleted,
	// but don't have a replacement block yet.
	MarkedForDeletionMeta = "marked-for-deletion"

	// MarkedForNoCompactionMeta is label for blocks which are loaded but also marked for no compaction. This label is also counted in `loaded` label metric.
	MarkedForNoCompactionMeta = "marked-for-no-compact"

	// Modified label values.
	replicaRemovedMeta = "replica-label-removed"
)

func NewFetcherMetrics(reg prometheus.Registerer, syncedExtraLabels, modifiedExtraLabels [][]string) *FetcherMetrics {
	var m FetcherMetrics

	m.Syncs = promauto.With(reg).NewCounter(prometheus.CounterOpts{
		Subsystem: fetcherSubSys,
		Name:      "syncs_total",
		Help:      "Total blocks metadata synchronization attempts",
	})
	m.SyncFailures = promauto.With(reg).NewCounter(prometheus.CounterOpts{
		Subsystem: fetcherSubSys,
		Name:      "sync_failures_total",
		Help:      "Total blocks metadata synchronization failures",
	})
	m.SyncDuration = promauto.With(reg).NewHistogram(prometheus.HistogramOpts{
		Subsystem: fetcherSubSys,
		Name:      "sync_duration_seconds",
		Help:      "Duration of the blocks metadata synchronization in seconds",
		Buckets:   []float64{0.01, 1, 10, 100, 300, 600, 1000},
	})
	m.Synced = extprom.NewTxGaugeVec(
		reg,
		prometheus.GaugeOpts{
			Subsystem: fetcherSubSys,
			Name:      "synced",
			Help:      "Number of block metadata synced",
		},
		[]string{"state"},
		append([][]string{
			{CorruptedMeta},
			{NoMeta},
			{LoadedMeta},
			{tooFreshMeta},
			{FailedMeta},
			{labelExcludedMeta},
			{timeExcludedMeta},
			{duplicateMeta},
			{MarkedForDeletionMeta},
			{MarkedForNoCompactionMeta},
		}, syncedExtraLabels...)...,
	)
	m.Modified = extprom.NewTxGaugeVec(
		reg,
		prometheus.GaugeOpts{
			Subsystem: fetcherSubSys,
			Name:      "modified",
			Help:      "Number of blocks whose metadata changed",
		},
		[]string{"modified"},
		append([][]string{
			{replicaRemovedMeta},
		}, modifiedExtraLabels...)...,
	)
	return &m
}

type MetadataFetcher interface {
	Fetch(ctx context.Context) (metas map[ulid.ULID]*metadata.Meta, partial map[ulid.ULID]error, err error)
	UpdateOnChange(func([]metadata.Meta, error))
}

type MetadataFilter interface {
	Filter(ctx context.Context, metas map[ulid.ULID]*metadata.Meta, synced *extprom.TxGaugeVec) error
}

type MetadataModifier interface {
	Modify(ctx context.Context, metas map[ulid.ULID]*metadata.Meta, modified *extprom.TxGaugeVec) error
}

// BaseFetcher is a struct that synchronizes filtered metadata of all block in the object storage with the local state.
// Go-routine safe.
type BaseFetcher struct {
	logger      log.Logger
	concurrency int
	bkt         objstore.InstrumentedBucketReader

	// Optional local directory to cache meta.json files.
	cacheDir string
	cached   map[ulid.ULID]*metadata.Meta
	syncs    prometheus.Counter
	g        singleflight.Group
}

// NewBaseFetcher constructs BaseFetcher.
func NewBaseFetcher(logger log.Logger, concurrency int, bkt objstore.InstrumentedBucketReader, dir string, reg prometheus.Registerer) (*BaseFetcher, error) {
	if logger == nil {
		logger = log.NewNopLogger()
	}

	cacheDir := ""
	if dir != "" {
		cacheDir = filepath.Join(dir, "meta-syncer")
		if err := os.MkdirAll(cacheDir, os.ModePerm); err != nil {
			return nil, err
		}
	}

	return &BaseFetcher{
		logger:      log.With(logger, "component", "block.BaseFetcher"),
		concurrency: concurrency,
		bkt:         bkt,
		cacheDir:    cacheDir,
		cached:      map[ulid.ULID]*metadata.Meta{},
		syncs: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Subsystem: fetcherSubSys,
			Name:      "base_syncs_total",
			Help:      "Total blocks metadata synchronization attempts by base Fetcher",
		}),
	}, nil
}

// NewRawMetaFetcher returns basic meta fetcher without proper handling for eventual consistent backends or partial uploads.
// NOTE: Not suitable to use in production.
func NewRawMetaFetcher(logger log.Logger, bkt objstore.InstrumentedBucketReader) (*MetaFetcher, error) {
	return NewMetaFetcher(logger, 1, bkt, "", nil, nil, nil)
}

// NewMetaFetcher returns meta fetcher.
func NewMetaFetcher(logger log.Logger, concurrency int, bkt objstore.InstrumentedBucketReader, dir string, reg prometheus.Registerer, filters []MetadataFilter, modifiers []MetadataModifier) (*MetaFetcher, error) {
	b, err := NewBaseFetcher(logger, concurrency, bkt, dir, reg)
	if err != nil {
		return nil, err
	}
	return b.NewMetaFetcher(reg, filters, modifiers), nil
}

// NewMetaFetcher transforms BaseFetcher into actually usable *MetaFetcher.
func (f *BaseFetcher) NewMetaFetcher(reg prometheus.Registerer, filters []MetadataFilter, modifiers []MetadataModifier, logTags ...interface{}) *MetaFetcher {
	return &MetaFetcher{metrics: NewFetcherMetrics(reg, nil, nil), wrapped: f, filters: filters, modifiers: modifiers, logger: log.With(f.logger, logTags...)}
}

var (
	ErrorSyncMetaNotFound  = errors.New("meta.json not found")
	ErrorSyncMetaCorrupted = errors.New("meta.json corrupted")
)

// loadMeta returns metadata from object storage or error.
// It returns `ErrorSyncMetaNotFound` and `ErrorSyncMetaCorrupted` sentinel errors in those cases.
func (f *BaseFetcher) loadMeta(ctx context.Context, id ulid.ULID) (*metadata.Meta, error) {
	var (
		metaFile       = path.Join(id.String(), MetaFilename)
		cachedBlockDir = filepath.Join(f.cacheDir, id.String())
	)

	// TODO(bwplotka): If that causes problems (obj store rate limits), add longer ttl to cached items.
	// For 1y and 100 block sources this generates ~1.5-3k HEAD RPM. AWS handles 330k RPM per prefix.
	// TODO(bwplotka): Consider filtering by consistency delay here (can't do until compactor healthyOverride work).
	ok, err := f.bkt.Exists(ctx, metaFile)
	if err != nil {
		return nil, errors.Wrapf(err, "meta.json file exists: %v", metaFile)
	}
	if !ok {
		return nil, ErrorSyncMetaNotFound
	}

	if m, seen := f.cached[id]; seen {
		return m, nil
	}

	// Best effort load from local dir.
	if f.cacheDir != "" {
		m, err := metadata.ReadFromDir(cachedBlockDir)
		if err == nil {
			return m, nil
		}

		if !errors.Is(err, os.ErrNotExist) {
			level.Warn(f.logger).Log("msg", "best effort read of the local meta.json failed; removing cached block dir", "dir", cachedBlockDir, "err", err)
			if err := os.RemoveAll(cachedBlockDir); err != nil {
				level.Warn(f.logger).Log("msg", "best effort remove of cached dir failed; ignoring", "dir", cachedBlockDir, "err", err)
			}
		}
	}

	r, err := f.bkt.ReaderWithExpectedErrs(f.bkt.IsObjNotFoundErr).Get(ctx, metaFile)
	if f.bkt.IsObjNotFoundErr(err) {
		// Meta.json was deleted between bkt.Exists and here.
		return nil, errors.Wrapf(ErrorSyncMetaNotFound, "%v", err)
	}
	if err != nil {
		return nil, errors.Wrapf(err, "get meta file: %v", metaFile)
	}

	defer runutil.CloseWithLogOnErr(f.logger, r, "close bkt meta get")

	metaContent, err := ioutil.ReadAll(r)
	if err != nil {
		return nil, errors.Wrapf(err, "read meta file: %v", metaFile)
	}

	m := &metadata.Meta{}
	if err := json.Unmarshal(metaContent, m); err != nil {
		return nil, errors.Wrapf(ErrorSyncMetaCorrupted, "meta.json %v unmarshal: %v", metaFile, err)
	}

	if m.Version != metadata.TSDBVersion1 {
		return nil, errors.Errorf("unexpected meta file: %s version: %d", metaFile, m.Version)
	}

	// Best effort cache in local dir.
	if f.cacheDir != "" {
		if err := os.MkdirAll(cachedBlockDir, os.ModePerm); err != nil {
			level.Warn(f.logger).Log("msg", "best effort mkdir of the meta.json block dir failed; ignoring", "dir", cachedBlockDir, "err", err)
		}

		if err := m.WriteToDir(f.logger, cachedBlockDir); err != nil {
			level.Warn(f.logger).Log("msg", "best effort save of the meta.json to local dir failed; ignoring", "dir", cachedBlockDir, "err", err)
		}
	}
	return m, nil
}

type response struct {
	metas   map[ulid.ULID]*metadata.Meta
	partial map[ulid.ULID]error
	// If metaErr > 0 it means incomplete view, so some metas, failed to be loaded.
	metaErrs errutil.MultiError

	noMetas        float64
	corruptedMetas float64
}

func (f *BaseFetcher) fetchMetadata(ctx context.Context) (interface{}, error) {
	f.syncs.Inc()

	var (
		resp = response{
			metas:   make(map[ulid.ULID]*metadata.Meta),
			partial: make(map[ulid.ULID]error),
		}
		eg  errgroup.Group
		ch  = make(chan ulid.ULID, f.concurrency)
		mtx sync.Mutex
	)
	level.Debug(f.logger).Log("msg", "fetching meta data", "concurrency", f.concurrency)
	for i := 0; i < f.concurrency; i++ {
		eg.Go(func() error {
			for id := range ch {
				meta, err := f.loadMeta(ctx, id)
				if err == nil {
					mtx.Lock()
					resp.metas[id] = meta
					mtx.Unlock()
					continue
				}

				switch errors.Cause(err) {
				default:
					mtx.Lock()
					resp.metaErrs.Add(err)
					mtx.Unlock()
					continue
				case ErrorSyncMetaNotFound:
					mtx.Lock()
					resp.noMetas++
					mtx.Unlock()
				case ErrorSyncMetaCorrupted:
					mtx.Lock()
					resp.corruptedMetas++
					mtx.Unlock()
				}

				mtx.Lock()
				resp.partial[id] = err
				mtx.Unlock()
			}
			return nil
		})
	}

	// Workers scheduled, distribute blocks.
	eg.Go(func() error {
		defer close(ch)
		return f.bkt.Iter(ctx, "", func(name string) error {
			id, ok := IsBlockDir(name)
			if !ok {
				return nil
			}

			select {
			case <-ctx.Done():
				return ctx.Err()
			case ch <- id:
			}

			return nil
		})
	})

	if err := eg.Wait(); err != nil {
		return nil, errors.Wrap(err, "BaseFetcher: iter bucket")
	}

	if len(resp.metaErrs) > 0 {
		return resp, nil
	}

	// Only for complete view of blocks update the cache.
	cached := make(map[ulid.ULID]*metadata.Meta, len(resp.metas))
	for id, m := range resp.metas {
		cached[id] = m
	}
	f.cached = cached

	// Best effort cleanup of disk-cached metas.
	if f.cacheDir != "" {
		fis, err := ioutil.ReadDir(f.cacheDir)
		names := make([]string, 0, len(fis))
		for _, fi := range fis {
			names = append(names, fi.Name())
		}
		if err != nil {
			level.Warn(f.logger).Log("msg", "best effort remove of not needed cached dirs failed; ignoring", "err", err)
		} else {
			for _, n := range names {
				id, ok := IsBlockDir(n)
				if !ok {
					continue
				}

				if _, ok := resp.metas[id]; ok {
					continue
				}

				cachedBlockDir := filepath.Join(f.cacheDir, id.String())

				// No such block loaded, remove the local dir.
				if err := os.RemoveAll(cachedBlockDir); err != nil {
					level.Warn(f.logger).Log("msg", "best effort remove of not needed cached dir failed; ignoring", "dir", cachedBlockDir, "err", err)
				}
			}
		}
	}
	return resp, nil
}

func (f *BaseFetcher) fetch(ctx context.Context, metrics *FetcherMetrics, filters []MetadataFilter, modifiers []MetadataModifier) (_ map[ulid.ULID]*metadata.Meta, _ map[ulid.ULID]error, err error) {
	start := time.Now()
	defer func() {
		metrics.SyncDuration.Observe(time.Since(start).Seconds())
		if err != nil {
			metrics.SyncFailures.Inc()
		}
	}()
	metrics.Syncs.Inc()
	metrics.ResetTx()

	// Run this in thread safe run group.
	// TODO(bwplotka): Consider custom singleflight with ttl.
	v, err := f.g.Do("", func() (i interface{}, err error) {
		// NOTE: First go routine context will go through.
		return f.fetchMetadata(ctx)
	})
	if err != nil {
		return nil, nil, err
	}
	resp := v.(response)

	// Copy as same response might be reused by different goroutines.
	metas := make(map[ulid.ULID]*metadata.Meta, len(resp.metas))
	for id, m := range resp.metas {
		metas[id] = m
	}

	metrics.Synced.WithLabelValues(FailedMeta).Set(float64(len(resp.metaErrs)))
	metrics.Synced.WithLabelValues(NoMeta).Set(resp.noMetas)
	metrics.Synced.WithLabelValues(CorruptedMeta).Set(resp.corruptedMetas)

	for _, filter := range filters {
		// NOTE: filter can update synced metric accordingly to the reason of the exclude.
		if err := filter.Filter(ctx, metas, metrics.Synced); err != nil {
			return nil, nil, errors.Wrap(err, "filter metas")
		}
	}

	for _, m := range modifiers {
		// NOTE: modifier can update modified metric accordingly to the reason of the modification.
		if err := m.Modify(ctx, metas, metrics.Modified); err != nil {
			return nil, nil, errors.Wrap(err, "modify metas")
		}
	}

	metrics.Synced.WithLabelValues(LoadedMeta).Set(float64(len(metas)))
	metrics.Submit()

	if len(resp.metaErrs) > 0 {
		return metas, resp.partial, errors.Wrap(resp.metaErrs.Err(), "incomplete view")
	}

	level.Info(f.logger).Log("msg", "successfully synchronized block metadata", "duration", time.Since(start).String(), "cached", len(f.cached), "returned", len(metas), "partial", len(resp.partial))
	return metas, resp.partial, nil
}

type MetaFetcher struct {
	wrapped *BaseFetcher
	metrics *FetcherMetrics

	filters   []MetadataFilter
	modifiers []MetadataModifier

	listener func([]metadata.Meta, error)

	logger log.Logger
}

// Fetch returns all block metas as well as partial blocks (blocks without or with corrupted meta file) from the bucket.
// It's caller responsibility to not change the returned metadata files. Maps can be modified.
//
// Returned error indicates a failure in fetching metadata. Returned meta can be assumed as correct, with some blocks missing.
func (f *MetaFetcher) Fetch(ctx context.Context) (metas map[ulid.ULID]*metadata.Meta, partial map[ulid.ULID]error, err error) {
	metas, partial, err = f.wrapped.fetch(ctx, f.metrics, f.filters, f.modifiers)
	if f.listener != nil {
		blocks := make([]metadata.Meta, 0, len(metas))
		for _, meta := range metas {
			blocks = append(blocks, *meta)
		}
		f.listener(blocks, err)
	}
	return metas, partial, err
}

// UpdateOnChange allows to add listener that will be update on every change.
func (f *MetaFetcher) UpdateOnChange(listener func([]metadata.Meta, error)) {
	f.listener = listener
}

var _ MetadataFilter = &TimePartitionMetaFilter{}

// TimePartitionMetaFilter is a BaseFetcher filter that filters out blocks that are outside of specified time range.
// Not go-routine safe.
type TimePartitionMetaFilter struct {
	minTime, maxTime model.TimeOrDurationValue
}

// NewTimePartitionMetaFilter creates TimePartitionMetaFilter.
func NewTimePartitionMetaFilter(MinTime, MaxTime model.TimeOrDurationValue) *TimePartitionMetaFilter {
	return &TimePartitionMetaFilter{minTime: MinTime, maxTime: MaxTime}
}

// Filter filters out blocks that are outside of specified time range.
func (f *TimePartitionMetaFilter) Filter(_ context.Context, metas map[ulid.ULID]*metadata.Meta, synced *extprom.TxGaugeVec) error {
	for id, m := range metas {
		if m.MaxTime >= f.minTime.PrometheusTimestamp() && m.MinTime <= f.maxTime.PrometheusTimestamp() {
			continue
		}
		synced.WithLabelValues(timeExcludedMeta).Inc()
		delete(metas, id)
	}
	return nil
}

var _ MetadataFilter = &LabelShardedMetaFilter{}

// LabelShardedMetaFilter represents struct that allows sharding.
// Not go-routine safe.
type LabelShardedMetaFilter struct {
	relabelConfig []*relabel.Config
}

// NewLabelShardedMetaFilter creates LabelShardedMetaFilter.
func NewLabelShardedMetaFilter(relabelConfig []*relabel.Config) *LabelShardedMetaFilter {
	return &LabelShardedMetaFilter{relabelConfig: relabelConfig}
}

// Special label that will have an ULID of the meta.json being referenced to.
const BlockIDLabel = "__block_id"

// Filter filters out blocks that have no labels after relabelling of each block external (Thanos) labels.
func (f *LabelShardedMetaFilter) Filter(_ context.Context, metas map[ulid.ULID]*metadata.Meta, synced *extprom.TxGaugeVec) error {
	var lbls labels.Labels
	for id, m := range metas {
		lbls = lbls[:0]
		lbls = append(lbls, labels.Label{Name: BlockIDLabel, Value: id.String()})
		for k, v := range m.Thanos.Labels {
			lbls = append(lbls, labels.Label{Name: k, Value: v})
		}

		if processedLabels := relabel.Process(lbls, f.relabelConfig...); len(processedLabels) == 0 {
			synced.WithLabelValues(labelExcludedMeta).Inc()
			delete(metas, id)
		}
	}
	return nil
}

var _ MetadataFilter = &DeduplicateFilter{}

// DeduplicateFilter is a BaseFetcher filter that filters out older blocks that have exactly the same data.
// Not go-routine safe.
type DeduplicateFilter struct {
	duplicateIDs []ulid.ULID
	mu           sync.Mutex
}

// NewDeduplicateFilter creates DeduplicateFilter.
func NewDeduplicateFilter() *DeduplicateFilter {
	return &DeduplicateFilter{}
}

// Filter filters out duplicate blocks that can be formed
// from two or more overlapping blocks that fully submatches the source blocks of the older blocks.
func (f *DeduplicateFilter) Filter(_ context.Context, metas map[ulid.ULID]*metadata.Meta, synced *extprom.TxGaugeVec) error {
	f.duplicateIDs = f.duplicateIDs[:0]

	var wg sync.WaitGroup

	metasByResolution := make(map[int64][]*metadata.Meta)
	for _, meta := range metas {
		res := meta.Thanos.Downsample.Resolution
		metasByResolution[res] = append(metasByResolution[res], meta)
	}

	for res := range metasByResolution {
		wg.Add(1)
		go func(res int64) {
			defer wg.Done()
			f.filterForResolution(NewNode(&metadata.Meta{
				BlockMeta: tsdb.BlockMeta{
					ULID: ulid.MustNew(uint64(0), nil),
				},
			}), metasByResolution[res], metas, synced)
		}(res)
	}

	wg.Wait()

	return nil
}

func (f *DeduplicateFilter) filterForResolution(root *Node, metaSlice []*metadata.Meta, metas map[ulid.ULID]*metadata.Meta, synced *extprom.TxGaugeVec) {
	sort.Slice(metaSlice, func(i, j int) bool {
		ilen := len(metaSlice[i].Compaction.Sources)
		jlen := len(metaSlice[j].Compaction.Sources)

		if ilen == jlen {
			return metaSlice[i].ULID.Compare(metaSlice[j].ULID) < 0
		}

		return ilen-jlen > 0
	})

	for _, meta := range metaSlice {
		addNodeBySources(root, NewNode(meta))
	}

	duplicateULIDs := getNonRootIDs(root)
	for _, id := range duplicateULIDs {
		f.mu.Lock()
		if metas[id] != nil {
			f.duplicateIDs = append(f.duplicateIDs, id)
		}
		synced.WithLabelValues(duplicateMeta).Inc()
		delete(metas, id)
		f.mu.Unlock()
	}
}

// DuplicateIDs returns slice of block ids that are filtered out by DeduplicateFilter.
func (f *DeduplicateFilter) DuplicateIDs() []ulid.ULID {
	return f.duplicateIDs
}

func addNodeBySources(root *Node, add *Node) bool {
	var rootNode *Node
	for _, node := range root.Children {
		parentSources := node.Compaction.Sources
		childSources := add.Compaction.Sources

		// Block exists with same sources, add as child.
		if contains(parentSources, childSources) && contains(childSources, parentSources) {
			node.Children = append(node.Children, add)
			return true
		}

		// Block's sources are present in other block's sources, add as child.
		if contains(parentSources, childSources) {
			rootNode = node
			break
		}
	}

	// Block cannot be attached to any child nodes, add it as child of root.
	if rootNode == nil {
		root.Children = append(root.Children, add)
		return true
	}

	return addNodeBySources(rootNode, add)
}

func contains(s1 []ulid.ULID, s2 []ulid.ULID) bool {
	for _, a := range s2 {
		found := false
		for _, e := range s1 {
			if a.Compare(e) == 0 {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

var _ MetadataModifier = &ReplicaLabelRemover{}

// ReplicaLabelRemover is a BaseFetcher modifier modifies external labels of existing blocks, it removes given replica labels from the metadata of blocks that have it.
type ReplicaLabelRemover struct {
	logger log.Logger

	replicaLabels []string
}

// NewReplicaLabelRemover creates a ReplicaLabelRemover.
func NewReplicaLabelRemover(logger log.Logger, replicaLabels []string) *ReplicaLabelRemover {
	return &ReplicaLabelRemover{logger: logger, replicaLabels: replicaLabels}
}

// Modify modifies external labels of existing blocks, it removes given replica labels from the metadata of blocks that have it.
func (r *ReplicaLabelRemover) Modify(_ context.Context, metas map[ulid.ULID]*metadata.Meta, modified *extprom.TxGaugeVec) error {
	if len(r.replicaLabels) == 0 {
		return nil
	}

	for u, meta := range metas {
		l := meta.Thanos.Labels
		for _, replicaLabel := range r.replicaLabels {
			if _, exists := l[replicaLabel]; exists {
				level.Debug(r.logger).Log("msg", "replica label removed", "label", replicaLabel)
				delete(l, replicaLabel)
				modified.WithLabelValues(replicaRemovedMeta).Inc()
			}
		}
		if len(l) == 0 {
			level.Warn(r.logger).Log("msg", "block has no labels left, creating one", r.replicaLabels[0], "deduped")
			l[r.replicaLabels[0]] = "deduped"
		}
		metas[u].Thanos.Labels = l
	}
	return nil
}

// ConsistencyDelayMetaFilter is a BaseFetcher filter that filters out blocks that are created before a specified consistency delay.
// Not go-routine safe.
type ConsistencyDelayMetaFilter struct {
	logger           log.Logger
	consistencyDelay time.Duration
}

// NewConsistencyDelayMetaFilter creates ConsistencyDelayMetaFilter.
func NewConsistencyDelayMetaFilter(logger log.Logger, consistencyDelay time.Duration, reg prometheus.Registerer) *ConsistencyDelayMetaFilter {
	if logger == nil {
		logger = log.NewNopLogger()
	}
	_ = promauto.With(reg).NewGaugeFunc(prometheus.GaugeOpts{
		Name: "consistency_delay_seconds",
		Help: "Configured consistency delay in seconds.",
	}, func() float64 {
		return consistencyDelay.Seconds()
	})

	return &ConsistencyDelayMetaFilter{
		logger:           logger,
		consistencyDelay: consistencyDelay,
	}
}

// Filter filters out blocks that filters blocks that have are created before a specified consistency delay.
func (f *ConsistencyDelayMetaFilter) Filter(_ context.Context, metas map[ulid.ULID]*metadata.Meta, synced *extprom.TxGaugeVec) error {
	for id, meta := range metas {
		// TODO(khyatisoneji): Remove the checks about Thanos Source
		//  by implementing delete delay to fetch metas.
		// TODO(bwplotka): Check consistency delay based on file upload / modification time instead of ULID.
		if ulid.Now()-id.Time() < uint64(f.consistencyDelay/time.Millisecond) &&
			meta.Thanos.Source != metadata.BucketRepairSource &&
			meta.Thanos.Source != metadata.CompactorSource &&
			meta.Thanos.Source != metadata.CompactorRepairSource {

			level.Debug(f.logger).Log("msg", "block is too fresh for now", "block", id)
			synced.WithLabelValues(tooFreshMeta).Inc()
			delete(metas, id)
		}
	}

	return nil
}

// IgnoreDeletionMarkFilter is a filter that filters out the blocks that are marked for deletion after a given delay.
// The delay duration is to make sure that the replacement block can be fetched before we filter out the old block.
// Delay is not considered when computing DeletionMarkBlocks map.
// Not go-routine safe.
type IgnoreDeletionMarkFilter struct {
	logger          log.Logger
	delay           time.Duration
	concurrency     int
	bkt             objstore.InstrumentedBucketReader
	deletionMarkMap map[ulid.ULID]*metadata.DeletionMark
}

// NewIgnoreDeletionMarkFilter creates IgnoreDeletionMarkFilter.
func NewIgnoreDeletionMarkFilter(logger log.Logger, bkt objstore.InstrumentedBucketReader, delay time.Duration, concurrency int) *IgnoreDeletionMarkFilter {
	return &IgnoreDeletionMarkFilter{
		logger:      logger,
		bkt:         bkt,
		delay:       delay,
		concurrency: concurrency,
	}
}

// DeletionMarkBlocks returns block ids that were marked for deletion.
func (f *IgnoreDeletionMarkFilter) DeletionMarkBlocks() map[ulid.ULID]*metadata.DeletionMark {
	return f.deletionMarkMap
}

// Filter filters out blocks that are marked for deletion after a given delay.
// It also returns the blocks that can be deleted since they were uploaded delay duration before current time.
func (f *IgnoreDeletionMarkFilter) Filter(ctx context.Context, metas map[ulid.ULID]*metadata.Meta, synced *extprom.TxGaugeVec) error {
	f.deletionMarkMap = make(map[ulid.ULID]*metadata.DeletionMark)

	// Make a copy of block IDs to check, in order to avoid concurrency issues
	// between the scheduler and workers.
	blockIDs := make([]ulid.ULID, 0, len(metas))
	for id := range metas {
		blockIDs = append(blockIDs, id)
	}

	var (
		eg  errgroup.Group
		ch  = make(chan ulid.ULID, f.concurrency)
		mtx sync.Mutex
	)

	for i := 0; i < f.concurrency; i++ {
		eg.Go(func() error {
			var lastErr error
			for id := range ch {
				m := &metadata.DeletionMark{}
				if err := metadata.ReadMarker(ctx, f.logger, f.bkt, id.String(), m); err != nil {
					if errors.Cause(err) == metadata.ErrorMarkerNotFound {
						continue
					}
					if errors.Cause(err) == metadata.ErrorUnmarshalMarker {
						level.Warn(f.logger).Log("msg", "found partial deletion-mark.json; if we will see it happening often for the same block, consider manually deleting deletion-mark.json from the object storage", "block", id, "err", err)
						continue
					}
					// Remember the last error and continue to drain the channel.
					lastErr = err
					continue
				}

				// Keep track of the blocks marked for deletion and filter them out if their
				// deletion time is greater than the configured delay.
				mtx.Lock()
				f.deletionMarkMap[id] = m
				if time.Since(time.Unix(m.DeletionTime, 0)).Seconds() > f.delay.Seconds() {
					synced.WithLabelValues(MarkedForDeletionMeta).Inc()
					delete(metas, id)
				}
				mtx.Unlock()
			}

			return lastErr
		})
	}

	// Workers scheduled, distribute blocks.
	eg.Go(func() error {
		defer close(ch)

		for _, id := range blockIDs {
			select {
			case ch <- id:
				// Nothing to do.
			case <-ctx.Done():
				return ctx.Err()
			}
		}

		return nil
	})

	if err := eg.Wait(); err != nil {
		return errors.Wrap(err, "filter blocks marked for deletion")
	}

	return nil
}

var (
	SelectorSupportedRelabelActions = map[relabel.Action]struct{}{relabel.Keep: {}, relabel.Drop: {}, relabel.HashMod: {}}
)

// ParseRelabelConfig parses relabel configuration.
func ParseRelabelConfig(contentYaml []byte, supportedActions map[relabel.Action]struct{}) ([]*relabel.Config, error) {
	var relabelConfig []*relabel.Config
	if err := yaml.Unmarshal(contentYaml, &relabelConfig); err != nil {
		return nil, errors.Wrap(err, "parsing relabel configuration")
	}

	for _, cfg := range relabelConfig {
		if _, ok := supportedActions[cfg.Action]; !ok {
			return nil, errors.Errorf("unsupported relabel action: %v", cfg.Action)
		}
	}

	return relabelConfig, nil
}
