// Copyright (c) The Thanos Authors.
// Licensed under the Apache License 2.0.

package compact

import (
	"context"
	"fmt"
	"io/ioutil"
	"math"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/oklog/ulid"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/prometheus/prometheus/tsdb"
	"github.com/thanos-io/thanos/pkg/extprom"
	"github.com/thanos-io/thanos/pkg/runutil"
	"golang.org/x/sync/errgroup"

	"github.com/thanos-io/thanos/pkg/block"
	"github.com/thanos-io/thanos/pkg/block/metadata"
	"github.com/thanos-io/thanos/pkg/compact/downsample"
	"github.com/thanos-io/thanos/pkg/errutil"
	"github.com/thanos-io/thanos/pkg/objstore"
)

type ResolutionLevel int64

const (
	ResolutionLevelRaw = ResolutionLevel(downsample.ResLevel0)
	ResolutionLevel5m  = ResolutionLevel(downsample.ResLevel1)
	ResolutionLevel1h  = ResolutionLevel(downsample.ResLevel2)
)

// Syncer synchronizes block metas from a bucket into a local directory.
// It sorts them into compaction groups based on equal label sets.
type Syncer struct {
	logger                   log.Logger
	reg                      prometheus.Registerer
	bkt                      objstore.Bucket
	fetcher                  block.MetadataFetcher
	mtx                      sync.Mutex
	blocks                   map[ulid.ULID]*metadata.Meta
	partial                  map[ulid.ULID]error
	blockSyncConcurrency     int
	metrics                  *syncerMetrics
	duplicateBlocksFilter    *block.DeduplicateFilter
	ignoreDeletionMarkFilter *block.IgnoreDeletionMarkFilter
}

type syncerMetrics struct {
	garbageCollectedBlocks    prometheus.Counter
	garbageCollections        prometheus.Counter
	garbageCollectionFailures prometheus.Counter
	garbageCollectionDuration prometheus.Histogram
	blocksMarkedForDeletion   prometheus.Counter
}

func newSyncerMetrics(reg prometheus.Registerer, blocksMarkedForDeletion prometheus.Counter, garbageCollectedBlocks prometheus.Counter) *syncerMetrics {
	var m syncerMetrics

	m.garbageCollectedBlocks = garbageCollectedBlocks
	m.garbageCollections = promauto.With(reg).NewCounter(prometheus.CounterOpts{
		Name: "thanos_compact_garbage_collection_total",
		Help: "Total number of garbage collection operations.",
	})
	m.garbageCollectionFailures = promauto.With(reg).NewCounter(prometheus.CounterOpts{
		Name: "thanos_compact_garbage_collection_failures_total",
		Help: "Total number of failed garbage collection operations.",
	})
	m.garbageCollectionDuration = promauto.With(reg).NewHistogram(prometheus.HistogramOpts{
		Name:    "thanos_compact_garbage_collection_duration_seconds",
		Help:    "Time it took to perform garbage collection iteration.",
		Buckets: []float64{0.01, 0.1, 0.3, 0.6, 1, 3, 6, 9, 20, 30, 60, 90, 120, 240, 360, 720},
	})

	m.blocksMarkedForDeletion = blocksMarkedForDeletion

	return &m
}

// NewMetaSyncer returns a new Syncer for the given Bucket and directory.
// Blocks must be at least as old as the sync delay for being considered.
func NewMetaSyncer(logger log.Logger, reg prometheus.Registerer, bkt objstore.Bucket, fetcher block.MetadataFetcher, duplicateBlocksFilter *block.DeduplicateFilter, ignoreDeletionMarkFilter *block.IgnoreDeletionMarkFilter, blocksMarkedForDeletion prometheus.Counter, garbageCollectedBlocks prometheus.Counter, blockSyncConcurrency int) (*Syncer, error) {
	if logger == nil {
		logger = log.NewNopLogger()
	}
	return &Syncer{
		logger:                   logger,
		reg:                      reg,
		bkt:                      bkt,
		fetcher:                  fetcher,
		blocks:                   map[ulid.ULID]*metadata.Meta{},
		metrics:                  newSyncerMetrics(reg, blocksMarkedForDeletion, garbageCollectedBlocks),
		duplicateBlocksFilter:    duplicateBlocksFilter,
		ignoreDeletionMarkFilter: ignoreDeletionMarkFilter,
		blockSyncConcurrency:     blockSyncConcurrency,
	}, nil
}

// UntilNextDownsampling calculates how long it will take until the next downsampling operation.
// Returns an error if there will be no downsampling.
func UntilNextDownsampling(m *metadata.Meta) (time.Duration, error) {
	timeRange := time.Duration((m.MaxTime - m.MinTime) * int64(time.Millisecond))
	switch m.Thanos.Downsample.Resolution {
	case downsample.ResLevel2:
		return time.Duration(0), errors.New("no downsampling")
	case downsample.ResLevel1:
		return time.Duration(downsample.DownsampleRange1*time.Millisecond) - timeRange, nil
	case downsample.ResLevel0:
		return time.Duration(downsample.DownsampleRange0*time.Millisecond) - timeRange, nil
	default:
		panic(errors.Errorf("invalid resolution %v", m.Thanos.Downsample.Resolution))
	}
}

// SyncMetas synchronizes local state of block metas with what we have in the bucket.
func (s *Syncer) SyncMetas(ctx context.Context) error {
	s.mtx.Lock()
	defer s.mtx.Unlock()

	metas, partial, err := s.fetcher.Fetch(ctx)
	if err != nil {
		return retry(err)
	}
	s.blocks = metas
	s.partial = partial
	return nil
}

// Partial returns partial blocks since last sync.
func (s *Syncer) Partial() map[ulid.ULID]error {
	s.mtx.Lock()
	defer s.mtx.Unlock()

	return s.partial
}

// Metas returns loaded metadata blocks since last sync.
func (s *Syncer) Metas() map[ulid.ULID]*metadata.Meta {
	s.mtx.Lock()
	defer s.mtx.Unlock()

	return s.blocks
}

// GarbageCollect marks blocks for deletion from bucket if their data is available as part of a
// block with a higher compaction level.
// Call to SyncMetas function is required to populate duplicateIDs in duplicateBlocksFilter.
func (s *Syncer) GarbageCollect(ctx context.Context) error {
	s.mtx.Lock()
	defer s.mtx.Unlock()

	begin := time.Now()

	// Ignore filter exists before deduplicate filter.
	deletionMarkMap := s.ignoreDeletionMarkFilter.DeletionMarkBlocks()
	duplicateIDs := s.duplicateBlocksFilter.DuplicateIDs()

	// GarbageIDs contains the duplicateIDs, since these blocks can be replaced with other blocks.
	// We also remove ids present in deletionMarkMap since these blocks are already marked for deletion.
	garbageIDs := []ulid.ULID{}
	for _, id := range duplicateIDs {
		if _, exists := deletionMarkMap[id]; exists {
			continue
		}
		garbageIDs = append(garbageIDs, id)
	}

	for _, id := range garbageIDs {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		// Spawn a new context so we always mark a block for deletion in full on shutdown.
		delCtx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)

		level.Info(s.logger).Log("msg", "marking outdated block for deletion", "block", id)
		err := block.MarkForDeletion(delCtx, s.logger, s.bkt, id, "outdated block", s.metrics.blocksMarkedForDeletion)
		cancel()
		if err != nil {
			s.metrics.garbageCollectionFailures.Inc()
			return retry(errors.Wrapf(err, "mark block %s for deletion", id))
		}

		// Immediately update our in-memory state so no further call to SyncMetas is needed
		// after running garbage collection.
		delete(s.blocks, id)
		s.metrics.garbageCollectedBlocks.Inc()
	}
	s.metrics.garbageCollections.Inc()
	s.metrics.garbageCollectionDuration.Observe(time.Since(begin).Seconds())
	return nil
}

// Grouper is responsible to group all known blocks into sub groups which are safe to be
// compacted concurrently.
type Grouper interface {
	// Groups returns the compaction groups for all blocks currently known to the syncer.
	// It creates all groups from the scratch on every call.
	Groups(blocks map[ulid.ULID]*metadata.Meta) (res []*Group, err error)
}

// DefaultGroupKey returns a unique identifier for the group the block belongs to, based on
// the DefaultGrouper logic. It considers the downsampling resolution and the block's labels.
func DefaultGroupKey(meta metadata.Thanos) string {
	return defaultGroupKey(meta.Downsample.Resolution, labels.FromMap(meta.Labels))
}

func defaultGroupKey(res int64, lbls labels.Labels) string {
	return fmt.Sprintf("%d@%v", res, lbls.Hash())
}

// DefaultGrouper is the Thanos built-in grouper. It groups blocks based on downsample
// resolution and block's labels.
type DefaultGrouper struct {
	bkt                      objstore.Bucket
	logger                   log.Logger
	acceptMalformedIndex     bool
	enableVerticalCompaction bool
	compactions              *prometheus.CounterVec
	compactionRunsStarted    *prometheus.CounterVec
	compactionRunsCompleted  *prometheus.CounterVec
	compactionFailures       *prometheus.CounterVec
	verticalCompactions      *prometheus.CounterVec
	garbageCollectedBlocks   prometheus.Counter
	blocksMarkedForDeletion  prometheus.Counter
	hashFunc                 metadata.HashFunc
}

// NewDefaultGrouper makes a new DefaultGrouper.
func NewDefaultGrouper(
	logger log.Logger,
	bkt objstore.Bucket,
	acceptMalformedIndex bool,
	enableVerticalCompaction bool,
	reg prometheus.Registerer,
	blocksMarkedForDeletion prometheus.Counter,
	garbageCollectedBlocks prometheus.Counter,
	hashFunc metadata.HashFunc,
) *DefaultGrouper {
	return &DefaultGrouper{
		bkt:                      bkt,
		logger:                   logger,
		acceptMalformedIndex:     acceptMalformedIndex,
		enableVerticalCompaction: enableVerticalCompaction,
		compactions: promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
			Name: "thanos_compact_group_compactions_total",
			Help: "Total number of group compaction attempts that resulted in a new block.",
		}, []string{"group"}),
		compactionRunsStarted: promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
			Name: "thanos_compact_group_compaction_runs_started_total",
			Help: "Total number of group compaction attempts.",
		}, []string{"group"}),
		compactionRunsCompleted: promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
			Name: "thanos_compact_group_compaction_runs_completed_total",
			Help: "Total number of group completed compaction runs. This also includes compactor group runs that resulted with no compaction.",
		}, []string{"group"}),
		compactionFailures: promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
			Name: "thanos_compact_group_compactions_failures_total",
			Help: "Total number of failed group compactions.",
		}, []string{"group"}),
		verticalCompactions: promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
			Name: "thanos_compact_group_vertical_compactions_total",
			Help: "Total number of group compaction attempts that resulted in a new block based on overlapping blocks.",
		}, []string{"group"}),
		garbageCollectedBlocks:  garbageCollectedBlocks,
		blocksMarkedForDeletion: blocksMarkedForDeletion,
		hashFunc:                hashFunc,
	}
}

// Groups returns the compaction groups for all blocks currently known to the syncer.
// It creates all groups from the scratch on every call.
func (g *DefaultGrouper) Groups(blocks map[ulid.ULID]*metadata.Meta) (res []*Group, err error) {
	groups := map[string]*Group{}
	for _, m := range blocks {
		groupKey := DefaultGroupKey(m.Thanos)
		group, ok := groups[groupKey]
		if !ok {
			lbls := labels.FromMap(m.Thanos.Labels)
			group, err = NewGroup(
				log.With(g.logger, "group", fmt.Sprintf("%d@%v", m.Thanos.Downsample.Resolution, lbls.String()), "groupKey", groupKey),
				g.bkt,
				groupKey,
				lbls,
				m.Thanos.Downsample.Resolution,
				g.acceptMalformedIndex,
				g.enableVerticalCompaction,
				g.compactions.WithLabelValues(groupKey),
				g.compactionRunsStarted.WithLabelValues(groupKey),
				g.compactionRunsCompleted.WithLabelValues(groupKey),
				g.compactionFailures.WithLabelValues(groupKey),
				g.verticalCompactions.WithLabelValues(groupKey),
				g.garbageCollectedBlocks,
				g.blocksMarkedForDeletion,
				g.hashFunc,
			)
			if err != nil {
				return nil, errors.Wrap(err, "create compaction group")
			}
			groups[groupKey] = group
			res = append(res, group)
		}
		if err := group.AppendMeta(m); err != nil {
			return nil, errors.Wrap(err, "add compaction group")
		}
	}
	sort.Slice(res, func(i, j int) bool {
		return res[i].Key() < res[j].Key()
	})
	return res, nil
}

// Group captures a set of blocks that have the same origin labels and downsampling resolution.
// Those blocks generally contain the same series and can thus efficiently be compacted.
type Group struct {
	logger                      log.Logger
	bkt                         objstore.Bucket
	key                         string
	labels                      labels.Labels
	resolution                  int64
	mtx                         sync.Mutex
	metasByMinTime              []*metadata.Meta
	acceptMalformedIndex        bool
	enableVerticalCompaction    bool
	compactions                 prometheus.Counter
	compactionRunsStarted       prometheus.Counter
	compactionRunsCompleted     prometheus.Counter
	compactionFailures          prometheus.Counter
	verticalCompactions         prometheus.Counter
	groupGarbageCollectedBlocks prometheus.Counter
	blocksMarkedForDeletion     prometheus.Counter
	hashFunc                    metadata.HashFunc
}

// NewGroup returns a new compaction group.
func NewGroup(
	logger log.Logger,
	bkt objstore.Bucket,
	key string,
	lset labels.Labels,
	resolution int64,
	acceptMalformedIndex bool,
	enableVerticalCompaction bool,
	compactions prometheus.Counter,
	compactionRunsStarted prometheus.Counter,
	compactionRunsCompleted prometheus.Counter,
	compactionFailures prometheus.Counter,
	verticalCompactions prometheus.Counter,
	groupGarbageCollectedBlocks prometheus.Counter,
	blocksMarkedForDeletion prometheus.Counter,
	hashFunc metadata.HashFunc,
) (*Group, error) {
	if logger == nil {
		logger = log.NewNopLogger()
	}
	g := &Group{
		logger:                      logger,
		bkt:                         bkt,
		key:                         key,
		labels:                      lset,
		resolution:                  resolution,
		acceptMalformedIndex:        acceptMalformedIndex,
		enableVerticalCompaction:    enableVerticalCompaction,
		compactions:                 compactions,
		compactionRunsStarted:       compactionRunsStarted,
		compactionRunsCompleted:     compactionRunsCompleted,
		compactionFailures:          compactionFailures,
		verticalCompactions:         verticalCompactions,
		groupGarbageCollectedBlocks: groupGarbageCollectedBlocks,
		blocksMarkedForDeletion:     blocksMarkedForDeletion,
		hashFunc:                    hashFunc,
	}
	return g, nil
}

// Key returns an identifier for the group.
func (cg *Group) Key() string {
	return cg.key
}

// AppendMeta the block with the given meta to the group.
func (cg *Group) AppendMeta(meta *metadata.Meta) error {
	cg.mtx.Lock()
	defer cg.mtx.Unlock()

	if !labels.Equal(cg.labels, labels.FromMap(meta.Thanos.Labels)) {
		return errors.New("block and group labels do not match")
	}
	if cg.resolution != meta.Thanos.Downsample.Resolution {
		return errors.New("block and group resolution do not match")
	}

	cg.metasByMinTime = append(cg.metasByMinTime, meta)
	sort.Slice(cg.metasByMinTime, func(i, j int) bool {
		return cg.metasByMinTime[i].MinTime < cg.metasByMinTime[j].MinTime
	})
	return nil
}

// IDs returns all sorted IDs of blocks in the group.
func (cg *Group) IDs() (ids []ulid.ULID) {
	cg.mtx.Lock()
	defer cg.mtx.Unlock()

	for _, m := range cg.metasByMinTime {
		ids = append(ids, m.ULID)
	}
	sort.Slice(ids, func(i, j int) bool {
		return ids[i].Compare(ids[j]) < 0
	})
	return ids
}

// MinTime returns the min time across all group's blocks.
func (cg *Group) MinTime() int64 {
	cg.mtx.Lock()
	defer cg.mtx.Unlock()

	if len(cg.metasByMinTime) > 0 {
		return cg.metasByMinTime[0].MinTime
	}
	return math.MaxInt64
}

// MaxTime returns the max time across all group's blocks.
func (cg *Group) MaxTime() int64 {
	cg.mtx.Lock()
	defer cg.mtx.Unlock()

	max := int64(math.MinInt64)
	for _, m := range cg.metasByMinTime {
		if m.MaxTime > max {
			max = m.MaxTime
		}
	}
	return max
}

// Labels returns the labels that all blocks in the group share.
func (cg *Group) Labels() labels.Labels {
	return cg.labels
}

// Resolution returns the common downsampling resolution of blocks in the group.
func (cg *Group) Resolution() int64 {
	return cg.resolution
}

// Planner returns blocks to compact.
type Planner interface {
	// Plan returns a list of blocks that should be compacted into single one.
	// The blocks can be overlapping. The provided metadata has to be ordered by minTime.
	Plan(ctx context.Context, metasByMinTime []*metadata.Meta) ([]*metadata.Meta, error)
}

// Compactor provides compaction against an underlying storage of time series data.
// This is similar to tsdb.Compactor just without Plan method.
// TODO(bwplotka): Split the Planner from Compactor on upstream as well, so we can import it.
type Compactor interface {
	// Write persists a Block into a directory.
	// No Block is written when resulting Block has 0 samples, and returns empty ulid.ULID{}.
	Write(dest string, b tsdb.BlockReader, mint, maxt int64, parent *tsdb.BlockMeta) (ulid.ULID, error)

	// Compact runs compaction against the provided directories. Must
	// only be called concurrently with results of Plan().
	// Can optionally pass a list of already open blocks,
	// to avoid having to reopen them.
	// When resulting Block has 0 samples
	//  * No block is written.
	//  * The source dirs are marked Deletable.
	//  * Returns empty ulid.ULID{}.
	Compact(dest string, dirs []string, open []*tsdb.Block) (ulid.ULID, error)
}

// Compact plans and runs a single compaction against the group. The compacted result
// is uploaded into the bucket the blocks were retrieved from.
func (cg *Group) Compact(ctx context.Context, dir string, planner Planner, comp Compactor) (shouldRerun bool, compID ulid.ULID, rerr error) {
	cg.compactionRunsStarted.Inc()

	subDir := filepath.Join(dir, cg.Key())

	defer func() {
		// Leave the compact directory for inspection if it is a halt error
		// or if it is not then so that possibly we would not have to download everything again.
		if rerr != nil {
			return
		}
		if err := os.RemoveAll(subDir); err != nil {
			level.Error(cg.logger).Log("msg", "failed to remove compaction group work directory", "path", subDir, "err", err)
		}
	}()

	if err := os.MkdirAll(subDir, 0777); err != nil {
		return false, ulid.ULID{}, errors.Wrap(err, "create compaction group dir")
	}

	shouldRerun, compID, err := cg.compact(ctx, subDir, planner, comp)
	if err != nil {
		cg.compactionFailures.Inc()
		return false, ulid.ULID{}, err
	}
	cg.compactionRunsCompleted.Inc()
	return shouldRerun, compID, nil
}

// Issue347Error is a type wrapper for errors that should invoke repair process for broken block.
type Issue347Error struct {
	err error

	id ulid.ULID
}

func issue347Error(err error, brokenBlock ulid.ULID) Issue347Error {
	return Issue347Error{err: err, id: brokenBlock}
}

func (e Issue347Error) Error() string {
	return e.err.Error()
}

// IsIssue347Error returns true if the base error is a Issue347Error.
func IsIssue347Error(err error) bool {
	_, ok := errors.Cause(err).(Issue347Error)
	return ok
}

// HaltError is a type wrapper for errors that should halt any further progress on compactions.
type HaltError struct {
	err error
}

func halt(err error) HaltError {
	return HaltError{err: err}
}

func (e HaltError) Error() string {
	return e.err.Error()
}

// IsHaltError returns true if the base error is a HaltError.
// If a multierror is passed, any halt error will return true.
func IsHaltError(err error) bool {
	if multiErr, ok := errors.Cause(err).(errutil.NonNilMultiError); ok {
		for _, err := range multiErr {
			if _, ok := errors.Cause(err).(HaltError); ok {
				return true
			}
		}
		return false
	}

	_, ok := errors.Cause(err).(HaltError)
	return ok
}

// RetryError is a type wrapper for errors that should trigger warning log and retry whole compaction loop, but aborting
// current compaction further progress.
type RetryError struct {
	err error
}

func retry(err error) error {
	if IsHaltError(err) {
		return err
	}
	return RetryError{err: err}
}

func (e RetryError) Error() string {
	return e.err.Error()
}

// IsRetryError returns true if the base error is a RetryError.
// If a multierror is passed, all errors must be retriable.
func IsRetryError(err error) bool {
	if multiErr, ok := errors.Cause(err).(errutil.NonNilMultiError); ok {
		for _, err := range multiErr {
			if _, ok := errors.Cause(err).(RetryError); !ok {
				return false
			}
		}
		return true
	}

	_, ok := errors.Cause(err).(RetryError)
	return ok
}

func (cg *Group) areBlocksOverlapping(include *metadata.Meta, exclude ...*metadata.Meta) error {
	var (
		metas      []tsdb.BlockMeta
		excludeMap = map[ulid.ULID]struct{}{}
	)

	for _, meta := range exclude {
		excludeMap[meta.ULID] = struct{}{}
	}

	for _, m := range cg.metasByMinTime {
		if _, ok := excludeMap[m.ULID]; ok {
			continue
		}
		metas = append(metas, m.BlockMeta)
	}

	if include != nil {
		metas = append(metas, include.BlockMeta)
	}

	sort.Slice(metas, func(i, j int) bool {
		return metas[i].MinTime < metas[j].MinTime
	})
	if overlaps := tsdb.OverlappingBlocks(metas); len(overlaps) > 0 {
		return errors.Errorf("overlaps found while gathering blocks. %s", overlaps)
	}
	return nil
}

// RepairIssue347 repairs the https://github.com/prometheus/tsdb/issues/347 issue when having issue347Error.
func RepairIssue347(ctx context.Context, logger log.Logger, bkt objstore.Bucket, blocksMarkedForDeletion prometheus.Counter, issue347Err error) error {
	ie, ok := errors.Cause(issue347Err).(Issue347Error)
	if !ok {
		return errors.Errorf("Given error is not an issue347 error: %v", issue347Err)
	}

	level.Info(logger).Log("msg", "Repairing block broken by https://github.com/prometheus/tsdb/issues/347", "id", ie.id, "err", issue347Err)

	tmpdir, err := ioutil.TempDir("", fmt.Sprintf("repair-issue-347-id-%s-", ie.id))
	if err != nil {
		return err
	}

	defer func() {
		if err := os.RemoveAll(tmpdir); err != nil {
			level.Warn(logger).Log("msg", "failed to remote tmpdir", "err", err, "tmpdir", tmpdir)
		}
	}()

	bdir := filepath.Join(tmpdir, ie.id.String())
	if err := block.Download(ctx, logger, bkt, ie.id, bdir); err != nil {
		return retry(errors.Wrapf(err, "download block %s", ie.id))
	}

	meta, err := metadata.ReadFromDir(bdir)
	if err != nil {
		return errors.Wrapf(err, "read meta from %s", bdir)
	}

	resid, err := block.Repair(logger, tmpdir, ie.id, metadata.CompactorRepairSource, block.IgnoreIssue347OutsideChunk)
	if err != nil {
		return errors.Wrapf(err, "repair failed for block %s", ie.id)
	}

	// Verify repaired id before uploading it.
	if err := block.VerifyIndex(logger, filepath.Join(tmpdir, resid.String(), block.IndexFilename), meta.MinTime, meta.MaxTime); err != nil {
		return errors.Wrapf(err, "repaired block is invalid %s", resid)
	}

	level.Info(logger).Log("msg", "uploading repaired block", "newID", resid)
	if err = block.Upload(ctx, logger, bkt, filepath.Join(tmpdir, resid.String()), metadata.NoneFunc); err != nil {
		return retry(errors.Wrapf(err, "upload of %s failed", resid))
	}

	level.Info(logger).Log("msg", "deleting broken block", "id", ie.id)

	// Spawn a new context so we always mark a block for deletion in full on shutdown.
	delCtx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// TODO(bplotka): Issue with this will introduce overlap that will halt compactor. Automate that (fix duplicate overlaps caused by this).
	if err := block.MarkForDeletion(delCtx, logger, bkt, ie.id, "source of repaired block", blocksMarkedForDeletion); err != nil {
		return errors.Wrapf(err, "marking old block %s for deletion has failed", ie.id)
	}
	return nil
}

func (cg *Group) compact(ctx context.Context, dir string, planner Planner, comp Compactor) (shouldRerun bool, compID ulid.ULID, err error) {
	cg.mtx.Lock()
	defer cg.mtx.Unlock()

	// Check for overlapped blocks.
	overlappingBlocks := false
	if err := cg.areBlocksOverlapping(nil); err != nil {
		// TODO(bwplotka): It would really nice if we could still check for other overlaps than replica. In fact this should be checked
		// in syncer itself. Otherwise with vertical compaction enabled we will sacrifice this important check.
		if !cg.enableVerticalCompaction {
			return false, ulid.ULID{}, halt(errors.Wrap(err, "pre compaction overlap check"))
		}

		overlappingBlocks = true
	}

	toCompact, err := planner.Plan(ctx, cg.metasByMinTime)
	if err != nil {
		return false, ulid.ULID{}, errors.Wrap(err, "plan compaction")
	}
	if len(toCompact) == 0 {
		// Nothing to do.
		return false, ulid.ULID{}, nil
	}

	level.Info(cg.logger).Log("msg", "compaction available and planned; downloading blocks", "plan", fmt.Sprintf("%v", toCompact))

	// Due to #183 we verify that none of the blocks in the plan have overlapping sources.
	// This is one potential source of how we could end up with duplicated chunks.
	uniqueSources := map[ulid.ULID]struct{}{}

	// Once we have a plan we need to download the actual data.
	begin := time.Now()

	toCompactDirs := make([]string, 0, len(toCompact))
	for _, meta := range toCompact {
		bdir := filepath.Join(dir, meta.ULID.String())
		for _, s := range meta.Compaction.Sources {
			if _, ok := uniqueSources[s]; ok {
				return false, ulid.ULID{}, halt(errors.Errorf("overlapping sources detected for plan %v", toCompact))
			}
			uniqueSources[s] = struct{}{}
		}

		if err := block.Download(ctx, cg.logger, cg.bkt, meta.ULID, bdir); err != nil {
			return false, ulid.ULID{}, retry(errors.Wrapf(err, "download block %s", meta.ULID))
		}

		// Ensure all input blocks are valid.
		stats, err := block.GatherIndexHealthStats(cg.logger, filepath.Join(bdir, block.IndexFilename), meta.MinTime, meta.MaxTime)
		if err != nil {
			return false, ulid.ULID{}, errors.Wrapf(err, "gather index issues for block %s", bdir)
		}

		if err := stats.CriticalErr(); err != nil {
			return false, ulid.ULID{}, halt(errors.Wrapf(err, "block with not healthy index found %s; Compaction level %v; Labels: %v", bdir, meta.Compaction.Level, meta.Thanos.Labels))
		}

		if err := stats.Issue347OutsideChunksErr(); err != nil {
			return false, ulid.ULID{}, issue347Error(errors.Wrapf(err, "invalid, but reparable block %s", bdir), meta.ULID)
		}

		if err := stats.PrometheusIssue5372Err(); !cg.acceptMalformedIndex && err != nil {
			return false, ulid.ULID{}, errors.Wrapf(err,
				"block id %s, try running with --debug.accept-malformed-index", meta.ULID)
		}
		toCompactDirs = append(toCompactDirs, bdir)
	}
	level.Info(cg.logger).Log("msg", "downloaded and verified blocks; compacting blocks", "plan", fmt.Sprintf("%v", toCompactDirs), "duration", time.Since(begin))

	begin = time.Now()
	compID, err = comp.Compact(dir, toCompactDirs, nil)
	if err != nil {
		return false, ulid.ULID{}, halt(errors.Wrapf(err, "compact blocks %v", toCompactDirs))
	}
	if compID == (ulid.ULID{}) {
		// Prometheus compactor found that the compacted block would have no samples.
		level.Info(cg.logger).Log("msg", "compacted block would have no samples, deleting source blocks", "blocks", fmt.Sprintf("%v", toCompactDirs))
		for _, meta := range toCompact {
			if meta.Stats.NumSamples == 0 {
				if err := cg.deleteBlock(meta.ULID, filepath.Join(dir, meta.ULID.String())); err != nil {
					level.Warn(cg.logger).Log("msg", "failed to mark for deletion an empty block found during compaction", "block", meta.ULID)
				}
			}
		}
		// Even though this block was empty, there may be more work to do.
		return true, ulid.ULID{}, nil
	}
	cg.compactions.Inc()
	if overlappingBlocks {
		cg.verticalCompactions.Inc()
	}
	level.Info(cg.logger).Log("msg", "compacted blocks", "new", compID,
		"blocks", fmt.Sprintf("%v", toCompactDirs), "duration", time.Since(begin), "overlapping_blocks", overlappingBlocks)

	bdir := filepath.Join(dir, compID.String())
	index := filepath.Join(bdir, block.IndexFilename)

	newMeta, err := metadata.InjectThanos(cg.logger, bdir, metadata.Thanos{
		Labels:       cg.labels.Map(),
		Downsample:   metadata.ThanosDownsample{Resolution: cg.resolution},
		Source:       metadata.CompactorSource,
		SegmentFiles: block.GetSegmentFiles(bdir),
	}, nil)
	if err != nil {
		return false, ulid.ULID{}, errors.Wrapf(err, "failed to finalize the block %s", bdir)
	}

	if err = os.Remove(filepath.Join(bdir, "tombstones")); err != nil {
		return false, ulid.ULID{}, errors.Wrap(err, "remove tombstones")
	}

	// Ensure the output block is valid.
	if err := block.VerifyIndex(cg.logger, index, newMeta.MinTime, newMeta.MaxTime); !cg.acceptMalformedIndex && err != nil {
		return false, ulid.ULID{}, halt(errors.Wrapf(err, "invalid result block %s", bdir))
	}

	// Ensure the output block is not overlapping with anything else,
	// unless vertical compaction is enabled.
	if !cg.enableVerticalCompaction {
		if err := cg.areBlocksOverlapping(newMeta, toCompact...); err != nil {
			return false, ulid.ULID{}, halt(errors.Wrapf(err, "resulted compacted block %s overlaps with something", bdir))
		}
	}

	begin = time.Now()

	if err := block.Upload(ctx, cg.logger, cg.bkt, bdir, cg.hashFunc); err != nil {
		return false, ulid.ULID{}, retry(errors.Wrapf(err, "upload of %s failed", compID))
	}
	level.Info(cg.logger).Log("msg", "uploaded block", "result_block", compID, "duration", time.Since(begin))

	// Mark for deletion the blocks we just compacted from the group and bucket so they do not get included
	// into the next planning cycle.
	// Eventually the block we just uploaded should get synced into the group again (including sync-delay).
	for _, meta := range toCompact {
		if err := cg.deleteBlock(meta.ULID, filepath.Join(dir, meta.ULID.String())); err != nil {
			return false, ulid.ULID{}, retry(errors.Wrapf(err, "mark old block for deletion from bucket"))
		}
		cg.groupGarbageCollectedBlocks.Inc()
	}
	return true, compID, nil
}

func (cg *Group) deleteBlock(id ulid.ULID, bdir string) error {
	if err := os.RemoveAll(bdir); err != nil {
		return errors.Wrapf(err, "remove old block dir %s", id)
	}

	// Spawn a new context so we always mark a block for deletion in full on shutdown.
	delCtx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	level.Info(cg.logger).Log("msg", "marking compacted block for deletion", "old_block", id)
	if err := block.MarkForDeletion(delCtx, cg.logger, cg.bkt, id, "source of compacted block", cg.blocksMarkedForDeletion); err != nil {
		return errors.Wrapf(err, "mark block %s for deletion from bucket", id)
	}
	return nil
}

// BucketCompactor compacts blocks in a bucket.
type BucketCompactor struct {
	logger      log.Logger
	sy          *Syncer
	grouper     Grouper
	comp        Compactor
	planner     Planner
	compactDir  string
	bkt         objstore.Bucket
	concurrency int
}

// NewBucketCompactor creates a new bucket compactor.
func NewBucketCompactor(
	logger log.Logger,
	sy *Syncer,
	grouper Grouper,
	planner Planner,
	comp Compactor,
	compactDir string,
	bkt objstore.Bucket,
	concurrency int,
) (*BucketCompactor, error) {
	if concurrency <= 0 {
		return nil, errors.Errorf("invalid concurrency level (%d), concurrency level must be > 0", concurrency)
	}
	return &BucketCompactor{
		logger:      logger,
		sy:          sy,
		grouper:     grouper,
		planner:     planner,
		comp:        comp,
		compactDir:  compactDir,
		bkt:         bkt,
		concurrency: concurrency,
	}, nil
}

// Compact runs compaction over bucket.
func (c *BucketCompactor) Compact(ctx context.Context) (rerr error) {
	defer func() {
		// Do not remove the compactDir if an error has occurred
		// because potentially on the next run we would not have to download
		// everything again.
		if rerr != nil {
			return
		}
		if err := os.RemoveAll(c.compactDir); err != nil {
			level.Error(c.logger).Log("msg", "failed to remove compaction work directory", "path", c.compactDir, "err", err)
		}
	}()

	// Loop over bucket and compact until there's no work left.
	for {
		var (
			wg                     sync.WaitGroup
			workCtx, workCtxCancel = context.WithCancel(ctx)
			groupChan              = make(chan *Group)
			errChan                = make(chan error, c.concurrency)
			finishedAllGroups      = true
			mtx                    sync.Mutex
		)
		defer workCtxCancel()

		// Set up workers who will compact the groups when the groups are ready.
		// They will compact available groups until they encounter an error, after which they will stop.
		for i := 0; i < c.concurrency; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for g := range groupChan {
					shouldRerunGroup, _, err := g.Compact(workCtx, c.compactDir, c.planner, c.comp)
					if err == nil {
						if shouldRerunGroup {
							mtx.Lock()
							finishedAllGroups = false
							mtx.Unlock()
						}
						continue
					}

					if IsIssue347Error(err) {
						if err := RepairIssue347(workCtx, c.logger, c.bkt, c.sy.metrics.blocksMarkedForDeletion, err); err == nil {
							mtx.Lock()
							finishedAllGroups = false
							mtx.Unlock()
							continue
						}
					}
					errChan <- errors.Wrapf(err, "group %s", g.Key())
					return
				}
			}()
		}

		level.Info(c.logger).Log("msg", "start sync of metas")
		if err := c.sy.SyncMetas(ctx); err != nil {
			return errors.Wrap(err, "sync")
		}

		level.Info(c.logger).Log("msg", "start of GC")
		// Blocks that were compacted are garbage collected after each Compaction.
		// However if compactor crashes we need to resolve those on startup.
		if err := c.sy.GarbageCollect(ctx); err != nil {
			return errors.Wrap(err, "garbage")
		}

		groups, err := c.grouper.Groups(c.sy.Metas())
		if err != nil {
			return errors.Wrap(err, "build compaction groups")
		}

		ignoreDirs := []string{}
		for _, gr := range groups {
			for _, grID := range gr.IDs() {
				ignoreDirs = append(ignoreDirs, filepath.Join(gr.Key(), grID.String()))
			}
		}

		if err := runutil.DeleteAll(c.compactDir, ignoreDirs...); err != nil {
			level.Warn(c.logger).Log("msg", "failed deleting non-compaction group directories/files, some disk space usage might have leaked. Continuing", "err", err, "dir", c.compactDir)
		}

		level.Info(c.logger).Log("msg", "start of compactions")

		// Send all groups found during this pass to the compaction workers.
		var groupErrs errutil.MultiError
	groupLoop:
		for _, g := range groups {
			select {
			case groupErr := <-errChan:
				groupErrs.Add(groupErr)
				break groupLoop
			case groupChan <- g:
			}
		}
		close(groupChan)
		wg.Wait()

		// Collect any other error reported by the workers, or any error reported
		// while we were waiting for the last batch of groups to run the compaction.
		close(errChan)
		for groupErr := range errChan {
			groupErrs.Add(groupErr)
		}

		workCtxCancel()
		if len(groupErrs) > 0 {
			return groupErrs.Err()
		}

		if finishedAllGroups {
			break
		}
	}
	level.Info(c.logger).Log("msg", "compaction iterations done")
	return nil
}

var _ block.MetadataFilter = &GatherNoCompactionMarkFilter{}

// GatherNoCompactionMarkFilter is a block.Fetcher filter that passes all metas. While doing it, it gathers all no-compact-mark.json markers.
// Not go routine safe.
// TODO(bwplotka): Add unit test.
type GatherNoCompactionMarkFilter struct {
	logger             log.Logger
	bkt                objstore.InstrumentedBucketReader
	noCompactMarkedMap map[ulid.ULID]*metadata.NoCompactMark
	concurrency        int
}

// NewGatherNoCompactionMarkFilter creates GatherNoCompactionMarkFilter.
func NewGatherNoCompactionMarkFilter(logger log.Logger, bkt objstore.InstrumentedBucketReader, concurrency int) *GatherNoCompactionMarkFilter {
	return &GatherNoCompactionMarkFilter{
		logger:      logger,
		bkt:         bkt,
		concurrency: concurrency,
	}
}

// NoCompactMarkedBlocks returns block ids that were marked for no compaction.
func (f *GatherNoCompactionMarkFilter) NoCompactMarkedBlocks() map[ulid.ULID]*metadata.NoCompactMark {
	return f.noCompactMarkedMap
}

// Filter passes all metas, while gathering no compact markers.
func (f *GatherNoCompactionMarkFilter) Filter(ctx context.Context, metas map[ulid.ULID]*metadata.Meta, synced *extprom.TxGaugeVec) error {
	f.noCompactMarkedMap = make(map[ulid.ULID]*metadata.NoCompactMark)

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
				m := &metadata.NoCompactMark{}
				// TODO(bwplotka): Hook up bucket cache here + reset API so we don't introduce API calls .
				if err := metadata.ReadMarker(ctx, f.logger, f.bkt, id.String(), m); err != nil {
					if errors.Cause(err) == metadata.ErrorMarkerNotFound {
						continue
					}
					if errors.Cause(err) == metadata.ErrorUnmarshalMarker {
						level.Warn(f.logger).Log("msg", "found partial no-compact-mark.json; if we will see it happening often for the same block, consider manually deleting no-compact-mark.json from the object storage", "block", id, "err", err)
						continue
					}
					// Remember the last error and continue draining the channel.
					lastErr = err
					continue
				}

				mtx.Lock()
				f.noCompactMarkedMap[id] = m
				mtx.Unlock()
				synced.WithLabelValues(block.MarkedForNoCompactionMeta).Inc()
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
		return errors.Wrap(err, "filter blocks marked for no compaction")
	}

	return nil
}
