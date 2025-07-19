// Copyright 2017 The Prometheus Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package tsdb

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/promslog"
	"go.uber.org/atomic"
	"golang.org/x/sync/semaphore"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/storage"
	"github.com/prometheus/prometheus/tsdb/chunkenc"
	"github.com/prometheus/prometheus/tsdb/chunks"
	tsdb_errors "github.com/prometheus/prometheus/tsdb/errors"
	"github.com/prometheus/prometheus/tsdb/fileutil"
	"github.com/prometheus/prometheus/tsdb/index"
	"github.com/prometheus/prometheus/tsdb/tombstones"
)

// ExponentialBlockRanges returns the time ranges based on the stepSize.
func ExponentialBlockRanges(minSize int64, steps, stepSize int) []int64 {
	ranges := make([]int64, 0, steps)
	curRange := minSize
	for i := 0; i < steps; i++ {
		ranges = append(ranges, curRange)
		curRange *= int64(stepSize)
	}

	return ranges
}

// Compactor provides compaction against an underlying storage
// of time series data.
type Compactor interface {
	// Plan returns a set of directories that can be compacted concurrently.
	// The directories can be overlapping.
	// Results returned when compactions are in progress are undefined.
	Plan(dir string) ([]string, error)

	// Write persists one or more Blocks into a directory.
	// No Block is written when resulting Block has 0 samples and returns an empty slice.
	// Prometheus always return one or no block. The interface allows returning more than one
	// block for downstream users to experiment with compactor.
	Write(dest string, b BlockReader, mint, maxt int64, base *BlockMeta) ([]ulid.ULID, error)

	// Compact runs compaction against the provided directories. Must
	// only be called concurrently with results of Plan().
	// Can optionally pass a list of already open blocks,
	// to avoid having to reopen them.
	// Prometheus always return one or no block. The interface allows returning more than one
	// block for downstream users to experiment with compactor.
	// When one resulting Block has 0 samples
	//  * No block is written.
	//  * The source dirs are marked Deletable.
	//  * Block is not included in the result.
	Compact(dest string, dirs []string, open []*Block) ([]ulid.ULID, error)

	// CompactOOO creates a new block per possible block range in the compactor's directory from the OOO Head given.
	// Each ULID in the result corresponds to a block in a unique time range.
	CompactOOO(dest string, oooHead *OOOCompactionHead) (result []ulid.ULID, err error)
}

// LeveledCompactor implements the Compactor interface.
type LeveledCompactor struct {
	metrics                     *CompactorMetrics
	logger                      *slog.Logger
	ranges                      []int64
	chunkPool                   chunkenc.Pool
	ctx                         context.Context
	maxBlockChunkSegmentSize    int64
	useUncachedIO               bool
	mergeFunc                   storage.VerticalChunkSeriesMergeFunc
	postingsEncoder             index.PostingsEncoder
	postingsDecoderFactory      PostingsDecoderFactory
	enableOverlappingCompaction bool
	concurrencyOpts             LeveledCompactorConcurrencyOptions
}

type CompactorMetrics struct {
	Ran               prometheus.Counter
	PopulatingBlocks  prometheus.Gauge
	OverlappingBlocks prometheus.Counter
	Duration          prometheus.Histogram
	ChunkSize         prometheus.Histogram
	ChunkSamples      prometheus.Histogram
	ChunkRange        prometheus.Histogram
}

// NewCompactorMetrics initializes metrics for Compactor.
func NewCompactorMetrics(r prometheus.Registerer) *CompactorMetrics {
	m := &CompactorMetrics{}

	m.Ran = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "prometheus_tsdb_compactions_total",
		Help: "Total number of compactions that were executed for the partition.",
	})
	m.PopulatingBlocks = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "prometheus_tsdb_compaction_populating_block",
		Help: "Set to 1 when a block is currently being written to the disk.",
	})
	m.OverlappingBlocks = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "prometheus_tsdb_vertical_compactions_total",
		Help: "Total number of compactions done on overlapping blocks.",
	})
	m.Duration = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:                            "prometheus_tsdb_compaction_duration_seconds",
		Help:                            "Duration of compaction runs",
		Buckets:                         prometheus.ExponentialBuckets(1, 2, 14),
		NativeHistogramBucketFactor:     1.1,
		NativeHistogramMaxBucketNumber:  100,
		NativeHistogramMinResetDuration: 1 * time.Hour,
	})
	m.ChunkSize = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "prometheus_tsdb_compaction_chunk_size_bytes",
		Help:    "Final size of chunks on their first compaction",
		Buckets: prometheus.ExponentialBuckets(32, 1.5, 12),
	})
	m.ChunkSamples = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "prometheus_tsdb_compaction_chunk_samples",
		Help:    "Final number of samples on their first compaction",
		Buckets: prometheus.ExponentialBuckets(4, 1.5, 12),
	})
	m.ChunkRange = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "prometheus_tsdb_compaction_chunk_range_seconds",
		Help:    "Final time range of chunks on their first compaction",
		Buckets: prometheus.ExponentialBuckets(100, 4, 10),
	})

	if r != nil {
		r.MustRegister(
			m.Ran,
			m.PopulatingBlocks,
			m.OverlappingBlocks,
			m.Duration,
			m.ChunkRange,
			m.ChunkSamples,
			m.ChunkSize,
		)
	}
	return m
}

type LeveledCompactorOptions struct {
	// PE specifies the postings encoder. It is called when compactor is writing out the postings for a label name/value pair during compaction.
	// If it is nil then the default encoder is used. At the moment that is the "raw" encoder. See index.EncodePostingsRaw for more.
	PE index.PostingsEncoder
	// PD specifies the postings decoder factory to return different postings decoder based on BlockMeta. It is called when opening a block or opening the index file.
	// If it is nil then a default decoder is used, compatible with Prometheus v2.
	PD PostingsDecoderFactory
	// MaxBlockChunkSegmentSize is the max block chunk segment size. If it is 0 then the default chunks.DefaultChunkSegmentSize is used.
	MaxBlockChunkSegmentSize int64
	// MergeFunc is used for merging series together in vertical compaction. By default storage.NewCompactingChunkSeriesMerger(storage.ChainedSeriesMerge) is used.
	MergeFunc storage.VerticalChunkSeriesMergeFunc
	// EnableOverlappingCompaction enables compaction of overlapping blocks. In Prometheus it is always enabled.
	// It is useful for downstream projects like Mimir, Cortex, Thanos where they have a separate component that does compaction.
	EnableOverlappingCompaction bool
	// Metrics is set of metrics for Compactor. By default, NewCompactorMetrics would be called to initialize metrics unless it is provided.
	Metrics *CompactorMetrics
	// UseUncachedIO allows bypassing the page cache when appropriate.
	UseUncachedIO bool
}

type PostingsDecoderFactory func(meta *BlockMeta) index.PostingsDecoder

func DefaultPostingsDecoderFactory(_ *BlockMeta) index.PostingsDecoder {
	return index.DecodePostingsRaw
}

func NewLeveledCompactorWithChunkSize(ctx context.Context, r prometheus.Registerer, l *slog.Logger, ranges []int64, pool chunkenc.Pool, maxBlockChunkSegmentSize int64, mergeFunc storage.VerticalChunkSeriesMergeFunc) (*LeveledCompactor, error) {
	return NewLeveledCompactorWithOptions(ctx, r, l, ranges, pool, LeveledCompactorOptions{
		MaxBlockChunkSegmentSize:    maxBlockChunkSegmentSize,
		MergeFunc:                   mergeFunc,
		EnableOverlappingCompaction: true,
	})
}

func NewLeveledCompactor(ctx context.Context, r prometheus.Registerer, l *slog.Logger, ranges []int64, pool chunkenc.Pool, mergeFunc storage.VerticalChunkSeriesMergeFunc) (*LeveledCompactor, error) {
	return NewLeveledCompactorWithOptions(ctx, r, l, ranges, pool, LeveledCompactorOptions{
		MergeFunc:                   mergeFunc,
		EnableOverlappingCompaction: true,
	})
}

func NewLeveledCompactorWithOptions(ctx context.Context, r prometheus.Registerer, l *slog.Logger, ranges []int64, pool chunkenc.Pool, opts LeveledCompactorOptions) (*LeveledCompactor, error) {
	if len(ranges) == 0 {
		return nil, errors.New("at least one range must be provided")
	}
	if pool == nil {
		pool = chunkenc.NewPool()
	}
	if l == nil {
		l = promslog.NewNopLogger()
	}
	mergeFunc := opts.MergeFunc
	if mergeFunc == nil {
		mergeFunc = storage.NewCompactingChunkSeriesMerger(storage.ChainedSeriesMerge)
	}
	maxBlockChunkSegmentSize := opts.MaxBlockChunkSegmentSize
	if maxBlockChunkSegmentSize == 0 {
		maxBlockChunkSegmentSize = chunks.DefaultChunkSegmentSize
	}
	pe := opts.PE
	if pe == nil {
		pe = index.EncodePostingsRaw
	}
	if opts.Metrics == nil {
		opts.Metrics = NewCompactorMetrics(r)
	}
	return &LeveledCompactor{
		ranges:                      ranges,
		chunkPool:                   pool,
		logger:                      l,
		metrics:                     opts.Metrics,
		ctx:                         ctx,
		maxBlockChunkSegmentSize:    maxBlockChunkSegmentSize,
		useUncachedIO:               opts.UseUncachedIO,
		mergeFunc:                   mergeFunc,
		postingsEncoder:             pe,
		postingsDecoderFactory:      opts.PD,
		enableOverlappingCompaction: opts.EnableOverlappingCompaction,
		concurrencyOpts:             DefaultLeveledCompactorConcurrencyOptions(),
	}, nil
}

// LeveledCompactorConcurrencyOptions is a collection of concurrency options used by LeveledCompactor.
type LeveledCompactorConcurrencyOptions struct {
	MaxOpeningBlocks     int // Number of goroutines opening blocks before compaction.
	MaxClosingBlocks     int // Max number of blocks that can be closed concurrently during split compaction. Note that closing of newly compacted block uses a lot of memory for writing index.
	SymbolsFlushersCount int // Number of symbols flushers used when doing split compaction.
}

func DefaultLeveledCompactorConcurrencyOptions() LeveledCompactorConcurrencyOptions {
	return LeveledCompactorConcurrencyOptions{
		MaxClosingBlocks:     1,
		SymbolsFlushersCount: 1,
		MaxOpeningBlocks:     1,
	}
}

func (c *LeveledCompactor) SetConcurrencyOptions(opts LeveledCompactorConcurrencyOptions) {
	c.concurrencyOpts = opts
}

type dirMeta struct {
	dir  string
	meta *BlockMeta
}

// Plan returns a list of compactable blocks in the provided directory.
func (c *LeveledCompactor) Plan(dir string) ([]string, error) {
	dirs, err := blockDirs(dir)
	if err != nil {
		return nil, err
	}
	if len(dirs) < 1 {
		return nil, nil
	}

	var dms []dirMeta
	for _, dir := range dirs {
		meta, _, err := readMetaFile(dir)
		if err != nil {
			return nil, err
		}
		dms = append(dms, dirMeta{dir, meta})
	}
	return c.plan(dms)
}

func (c *LeveledCompactor) plan(dms []dirMeta) ([]string, error) {
	slices.SortFunc(dms, func(a, b dirMeta) int {
		switch {
		case a.meta.MinTime < b.meta.MinTime:
			return -1
		case a.meta.MinTime > b.meta.MinTime:
			return 1
		default:
			return 0
		}
	})

	res := c.selectOverlappingDirs(dms)
	if len(res) > 0 {
		return res, nil
	}
	// No overlapping blocks or overlapping block compaction not allowed, do compaction the usual way.
	// We do not include a recently created block with max(minTime), so the block which was just created from WAL.
	// This gives users a window of a full block size to piece-wise backup new data without having to care about data overlap.
	dms = dms[:len(dms)-1]

	for _, dm := range c.selectDirs(dms) {
		res = append(res, dm.dir)
	}
	if len(res) > 0 {
		return res, nil
	}

	// Compact any blocks with big enough time range that have >5% tombstones.
	for i := len(dms) - 1; i >= 0; i-- {
		meta := dms[i].meta
		if meta.MaxTime-meta.MinTime < c.ranges[len(c.ranges)/2] {
			// If the block is entirely deleted, then we don't care about the block being big enough.
			// TODO: This is assuming a single tombstone is for a distinct series, which might not be true.
			if meta.Stats.NumTombstones > 0 && meta.Stats.NumTombstones >= meta.Stats.NumSeries {
				return []string{dms[i].dir}, nil
			}
			break
		}
		if float64(meta.Stats.NumTombstones)/float64(meta.Stats.NumSeries+1) > 0.05 {
			return []string{dms[i].dir}, nil
		}
	}

	return nil, nil
}

// selectDirs returns the dir metas that should be compacted into a single new block.
// If only a single block range is configured, the result is always nil.
func (c *LeveledCompactor) selectDirs(ds []dirMeta) []dirMeta {
	if len(c.ranges) < 2 || len(ds) < 1 {
		return nil
	}

	highTime := ds[len(ds)-1].meta.MinTime

	for _, iv := range c.ranges[1:] {
		parts := splitByRange(ds, iv)
		if len(parts) == 0 {
			continue
		}

	Outer:
		for _, p := range parts {
			// Do not select the range if it has a block whose compaction failed.
			for _, dm := range p {
				if dm.meta.Compaction.Failed {
					continue Outer
				}
			}

			mint := p[0].meta.MinTime
			maxt := p[len(p)-1].meta.MaxTime
			// Pick the range of blocks if it spans the full range (potentially with gaps)
			// or is before the most recent block.
			// This ensures we don't compact blocks prematurely when another one of the same
			// size still fits in the range.
			if (maxt-mint == iv || maxt <= highTime) && len(p) > 1 {
				return p
			}
		}
	}

	return nil
}

// selectOverlappingDirs returns all dirs with overlapping time ranges.
// It expects sorted input by mint and returns the overlapping dirs in the same order as received.
func (c *LeveledCompactor) selectOverlappingDirs(ds []dirMeta) []string {
	if !c.enableOverlappingCompaction {
		return nil
	}
	if len(ds) < 2 {
		return nil
	}
	var overlappingDirs []string
	globalMaxt := ds[0].meta.MaxTime
	for i, d := range ds[1:] {
		if d.meta.MinTime < globalMaxt {
			if len(overlappingDirs) == 0 { // When it is the first overlap, need to add the last one as well.
				overlappingDirs = append(overlappingDirs, ds[i].dir)
			}
			overlappingDirs = append(overlappingDirs, d.dir)
		} else if len(overlappingDirs) > 0 {
			break
		}
		if d.meta.MaxTime > globalMaxt {
			globalMaxt = d.meta.MaxTime
		}
	}
	return overlappingDirs
}

// splitByRange splits the directories by the time range. The range sequence starts at 0.
//
// For example, if we have blocks [0-10, 10-20, 50-60, 90-100] and the split range tr is 30
// it returns [0-10, 10-20], [50-60], [90-100].
func splitByRange(ds []dirMeta, tr int64) [][]dirMeta {
	var splitDirs [][]dirMeta

	for i := 0; i < len(ds); {
		var (
			group []dirMeta
			t0    int64
			m     = ds[i].meta
		)
		// Compute start of aligned time range of size tr closest to the current block's start.
		if m.MinTime >= 0 {
			t0 = tr * (m.MinTime / tr)
		} else {
			t0 = tr * ((m.MinTime - tr + 1) / tr)
		}
		// Skip blocks that don't fall into the range. This can happen via mis-alignment or
		// by being a multiple of the intended range.
		if m.MaxTime > t0+tr {
			i++
			continue
		}

		// Add all dirs to the current group that are within [t0, t0+tr].
		for ; i < len(ds); i++ {
			// Either the block falls into the next range or doesn't fit at all (checked above).
			if ds[i].meta.MaxTime > t0+tr {
				break
			}
			group = append(group, ds[i])
		}

		if len(group) > 0 {
			splitDirs = append(splitDirs, group)
		}
	}

	return splitDirs
}

// CompactBlockMetas merges many block metas into one, combining its source blocks together
// and adjusting compaction level. Min/Max time of result block meta covers all input blocks.
func CompactBlockMetas(uid ulid.ULID, blocks ...*BlockMeta) *BlockMeta {
	res := &BlockMeta{
		ULID: uid,
	}

	sources := map[ulid.ULID]struct{}{}
	mint := blocks[0].MinTime
	maxt := blocks[0].MaxTime

	for _, b := range blocks {
		if b.MinTime < mint {
			mint = b.MinTime
		}
		if b.MaxTime > maxt {
			maxt = b.MaxTime
		}
		if b.Compaction.Level > res.Compaction.Level {
			res.Compaction.Level = b.Compaction.Level
		}
		for _, s := range b.Compaction.Sources {
			sources[s] = struct{}{}
		}
		res.Compaction.Parents = append(res.Compaction.Parents, BlockDesc{
			ULID:    b.ULID,
			MinTime: b.MinTime,
			MaxTime: b.MaxTime,
		})
	}
	res.Compaction.Level++

	for s := range sources {
		res.Compaction.Sources = append(res.Compaction.Sources, s)
	}
	slices.SortFunc(res.Compaction.Sources, func(a, b ulid.ULID) int {
		return a.Compare(b)
	})

	res.MinTime = mint
	res.MaxTime = maxt
	return res
}

// CompactWithSplitting merges and splits the input blocks into shardCount number of output blocks,
// and returns slice of block IDs. Position of returned block ID in the result slice corresponds to the shard index.
// If given output block has no series, corresponding block ID will be zero ULID value.
//
// Note that this is different from Compact, which *removes* empty blocks from the result instead.
func (c *LeveledCompactor) CompactWithSplitting(dest string, dirs []string, open []*Block, shardCount uint64) (result []ulid.ULID, _ error) {
	return c.CompactWithBlockPopulator(dest, dirs, open, DefaultBlockPopulator{}, shardCount)
}

// Compact creates a new block in the compactor's directory from the blocks in the
// provided directories.
func (c *LeveledCompactor) Compact(dest string, dirs []string, open []*Block) ([]ulid.ULID, error) {
	ulids, err := c.CompactWithBlockPopulator(dest, dirs, open, DefaultBlockPopulator{}, 1)
	if err != nil {
		return nil, err
	}
	// Updated contract for Compact says that empty blocks are not returned.
	if ulids[0] == (ulid.ULID{}) {
		return nil, nil
	}
	return ulids, nil
}

// shardedBlock describes single *output* block during compaction. This struct is passed between
// compaction methods to wrap output block details, index and chunk writer together.
// Shard index is determined by the position of this structure in the slice of output blocks.
type shardedBlock struct {
	meta *BlockMeta

	blockDir string
	tmpDir   string // Temp directory used when block is being built (= blockDir + temp suffix)
	chunkw   ChunkWriter
	indexw   IndexWriter
}

func (c *LeveledCompactor) CompactWithBlockPopulator(dest string, dirs []string, open []*Block, blockPopulator BlockPopulator, shardCount uint64) (_ []ulid.ULID, err error) {
	if shardCount == 0 {
		shardCount = 1
	}

	start := time.Now()

	bs, blocksToClose, err := openBlocksForCompaction(c.ctx, dirs, open, c.logger, c.chunkPool, c.postingsDecoderFactory, c.concurrencyOpts.MaxOpeningBlocks)
	for _, b := range blocksToClose {
		defer b.Close()
	}

	if err != nil {
		return nil, err
	}

	var (
		blocks []BlockReader
		metas  []*BlockMeta
		uids   []string
	)
	for _, b := range bs {
		blocks = append(blocks, b)
		m := b.Meta()
		metas = append(metas, &m)
		uids = append(uids, b.meta.ULID.String())
	}

	outBlocks := make([]shardedBlock, shardCount)
	outBlocksTime := ulid.Now() // Make all out blocks share the same timestamp in the ULID.
	for ix := range outBlocks {
		outBlocks[ix] = shardedBlock{meta: CompactBlockMetas(ulid.MustNew(outBlocksTime, rand.Reader), metas...)}
	}

	err = c.write(dest, outBlocks, blockPopulator, blocks...)
	if err == nil {
		ulids := make([]ulid.ULID, len(outBlocks))
		allOutputBlocksAreEmpty := true

		for ix := range outBlocks {
			meta := outBlocks[ix].meta

			if meta.Stats.NumSamples == 0 {
				c.logger.Info(
					"compact blocks resulted in empty block",
					"count", len(blocks),
					"sources", fmt.Sprintf("%v", uids),
					"duration", time.Since(start),
					"shard", fmt.Sprintf("%d_of_%d", ix+1, shardCount),
				)
			} else {
				allOutputBlocksAreEmpty = false
				ulids[ix] = outBlocks[ix].meta.ULID

				c.logger.Info(
					"compact blocks",
					"count", len(blocks),
					"mint", meta.MinTime,
					"maxt", meta.MaxTime,
					"ulid", meta.ULID,
					"sources", fmt.Sprintf("%v", uids),
					"duration", time.Since(start),
					"shard", fmt.Sprintf("%d_of_%d", ix+1, shardCount),
				)
			}
		}

		if allOutputBlocksAreEmpty {
			// Mark source blocks as deletable.
			for _, b := range bs {
				b.meta.Compaction.Deletable = true
				n, err := writeMetaFile(c.logger, b.dir, &b.meta)
				if err != nil {
					c.logger.Error(
						"Failed to write 'Deletable' to meta file after compaction",
						"ulid", b.meta.ULID,
					)
				}
				b.numBytesMeta = n
			}
		}

		return ulids, nil
	}

	errs := tsdb_errors.NewMulti(err)
	if !errors.Is(err, context.Canceled) {
		for _, b := range bs {
			if err := b.setCompactionFailed(); err != nil {
				errs.Add(fmt.Errorf("setting compaction failed for block: %s: %w", b.Dir(), err))
			}
		}
	}

	return nil, errs.Err()
}

// CompactOOOWithSplitting splits the input OOO Head into shardCount number of output blocks
// per possible block range, and returns slice of block IDs. In result[i][j],
// 'i' corresponds to a single time range of blocks while 'j' corresponds to the shard index.
// If given output block has no series, corresponding block ID will be zero ULID value.
// TODO: write tests for this.
func (c *LeveledCompactor) CompactOOOWithSplitting(dest string, oooHead *OOOCompactionHead, shardCount uint64) (result [][]ulid.ULID, _ error) {
	return c.compactOOO(dest, oooHead, shardCount)
}

// CompactOOO creates a new block per possible block range in the compactor's directory from the OOO Head given.
// Each ULID in the result corresponds to a block in a unique time range.
func (c *LeveledCompactor) CompactOOO(dest string, oooHead *OOOCompactionHead) (result []ulid.ULID, err error) {
	ulids, err := c.compactOOO(dest, oooHead, 1)
	if err != nil {
		return nil, err
	}
	for _, s := range ulids {
		if s[0].Compare(ulid.ULID{}) != 0 {
			result = append(result, s[0])
		}
	}
	return result, err
}

func (c *LeveledCompactor) compactOOO(dest string, oooHead *OOOCompactionHead, shardCount uint64) (_ [][]ulid.ULID, err error) {
	if shardCount == 0 {
		shardCount = 1
	}

	start := time.Now()

	// The first dimension of outBlocks determines the time based splitting (i.e. outBlocks[i] has blocks all for the same time range).
	// The second dimension of outBlocks determines the label based shard (i.e. outBlocks[i][j] is the (j+1)th shard.
	// During ingestion of samples we can identify which ooo blocks will exists so that
	// we dont have to prefill symbols and etc for the blocks that will be empty.
	// With this, len(outBlocks[x]) will still be the same for all x so that we can pick blocks easily.
	// Just that, only some of the outBlocks[x][y] will be valid and populated based on preexisting knowledge of
	// which blocks to expect.
	// In case we see a sample that is not present in the estimated block ranges, we will create them on flight.
	outBlocks := make([][]shardedBlock, 0)
	outBlocksTime := ulid.Now() // Make all out blocks share the same timestamp in the ULID.
	blockSize := oooHead.ChunkRange()
	oooHeadMint, oooHeadMaxt := oooHead.MinTime(), oooHead.MaxTime()
	ulids := make([][]ulid.ULID, 0)
	for t := blockSize * (oooHeadMint / blockSize); t <= oooHeadMaxt; t += blockSize {
		mint, maxt := t, t+blockSize

		outBlocks = append(outBlocks, make([]shardedBlock, shardCount))
		ulids = append(ulids, make([]ulid.ULID, shardCount))
		ix := len(outBlocks) - 1

		for jx := range outBlocks[ix] {
			uid := ulid.MustNew(outBlocksTime, rand.Reader)
			meta := &BlockMeta{
				ULID:       uid,
				MinTime:    mint,
				MaxTime:    maxt,
				OutOfOrder: true,
			}
			meta.Compaction.Level = 1
			meta.Compaction.Sources = []ulid.ULID{uid}

			outBlocks[ix][jx] = shardedBlock{
				meta: meta,
			}
			ulids[ix][jx] = meta.ULID
		}

		// Block intervals are half-open: [b.MinTime, b.MaxTime). Block intervals are always +1 than the total samples it includes.
		err := c.write(dest, outBlocks[ix], DefaultBlockPopulator{}, oooHead.CloneForTimeRange(mint, maxt-1))
		if err != nil {
			// We need to delete all blocks in case there was an error.
			for _, obs := range outBlocks {
				for _, ob := range obs {
					if ob.tmpDir != "" {
						if removeErr := os.RemoveAll(ob.tmpDir); removeErr != nil {
							c.logger.Error("Failed to remove temp folder after failed compaction", "dir", ob.tmpDir, "err", removeErr.Error())
						}
					}
					if ob.blockDir != "" {
						if removeErr := os.RemoveAll(ob.blockDir); removeErr != nil {
							c.logger.Error("Failed to remove block folder after failed compaction", "dir", ob.blockDir, "err", removeErr.Error())
						}
					}
				}
			}
			return nil, err
		}
	}

	noOOOBlock := true
	for ix, obs := range outBlocks {
		for jx := range obs {
			meta := outBlocks[ix][jx].meta
			if meta.Stats.NumSamples != 0 {
				noOOOBlock = false
				c.logger.Info(
					"compact ooo head",
					"mint", meta.MinTime,
					"maxt", meta.MaxTime,
					"ulid", meta.ULID,
					"duration", time.Since(start),
					"shard", fmt.Sprintf("%d_of_%d", jx+1, shardCount),
				)
			} else {
				// This block did not get any data. So clear out the ulid to signal this.
				ulids[ix][jx] = ulid.ULID{}
			}
		}
	}

	if noOOOBlock {
		c.logger.Info(
			"compact ooo head resulted in no blocks",
			"duration", time.Since(start),
		)
		return nil, nil
	}

	return ulids, nil
}

func (c *LeveledCompactor) Write(dest string, b BlockReader, mint, maxt int64, base *BlockMeta) ([]ulid.ULID, error) {
	start := time.Now()

	uid := ulid.MustNew(ulid.Now(), rand.Reader)

	meta := &BlockMeta{
		ULID:    uid,
		MinTime: mint,
		MaxTime: maxt,
	}
	meta.Compaction.Level = 1
	meta.Compaction.Sources = []ulid.ULID{uid}

	if base != nil {
		meta.Compaction.Parents = []BlockDesc{
			{ULID: base.ULID, MinTime: base.MinTime, MaxTime: base.MaxTime},
		}
		if base.Compaction.FromOutOfOrder() {
			meta.Compaction.SetOutOfOrder()
		}
	}

	err := c.write(dest, []shardedBlock{{meta: meta}}, DefaultBlockPopulator{}, b)
	if err != nil {
		return nil, err
	}

	if meta.Stats.NumSamples == 0 {
		c.logger.Info(
			"write block resulted in empty block",
			"mint", meta.MinTime,
			"maxt", meta.MaxTime,
			"duration", time.Since(start),
		)
		return nil, nil
	}

	c.logger.Info(
		"write block",
		"mint", meta.MinTime,
		"maxt", meta.MaxTime,
		"ulid", meta.ULID,
		"duration", time.Since(start),
		"ooo", meta.Compaction.FromOutOfOrder(),
	)
	return []ulid.ULID{uid}, nil
}

// instrumentedChunkWriter is used for level 1 compactions to record statistics
// about compacted chunks.
type instrumentedChunkWriter struct {
	ChunkWriter

	size    prometheus.Histogram
	samples prometheus.Histogram
	trange  prometheus.Histogram
}

func (w *instrumentedChunkWriter) WriteChunks(chunks ...chunks.Meta) error {
	for _, c := range chunks {
		w.size.Observe(float64(len(c.Chunk.Bytes())))
		w.samples.Observe(float64(c.Chunk.NumSamples()))
		w.trange.Observe(float64(c.MaxTime - c.MinTime))
	}
	return w.ChunkWriter.WriteChunks(chunks...)
}

// write creates new output blocks that are the union of the provided blocks into dir.
func (c *LeveledCompactor) write(dest string, outBlocks []shardedBlock, blockPopulator BlockPopulator, blocks ...BlockReader) (err error) {
	var closers []io.Closer

	defer func(t time.Time) {
		err = tsdb_errors.NewMulti(err, tsdb_errors.CloseAll(closers)).Err()

		for _, ob := range outBlocks {
			if ob.tmpDir != "" {
				// RemoveAll returns no error when tmp doesn't exist so it is safe to always run it.
				if removeErr := os.RemoveAll(ob.tmpDir); removeErr != nil {
					c.logger.Error("Failed to remove temp folder after failed compaction", "dir", ob.tmpDir, "err", removeErr.Error())
				}
			}

			// If there was any error, and we have multiple output blocks, some blocks may have been generated, or at
			// least have existing blockDir. In such case, we want to remove them.
			// BlockDir may also not be set yet, if preparation for some previous blocks have failed.
			if err != nil && ob.blockDir != "" {
				// RemoveAll returns no error when tmp doesn't exist so it is safe to always run it.
				if removeErr := os.RemoveAll(ob.blockDir); removeErr != nil {
					c.logger.Error("Failed to remove block folder after failed compaction", "dir", ob.blockDir, "err", removeErr.Error())
				}
			}
		}
		c.metrics.Ran.Inc()
		c.metrics.Duration.Observe(time.Since(t).Seconds())
	}(time.Now())

	for ix := range outBlocks {
		dir := filepath.Join(dest, outBlocks[ix].meta.ULID.String())
		tmp := dir + tmpForCreationBlockDirSuffix

		outBlocks[ix].blockDir = dir
		outBlocks[ix].tmpDir = tmp

		if err = os.RemoveAll(tmp); err != nil {
			return err
		}

		if err = os.MkdirAll(tmp, 0o777); err != nil {
			return err
		}

		// Populate chunk and index files into temporary directory with
		// data of all blocks.
		var chunkw ChunkWriter
		chunkw, err = chunks.NewWriter(chunkDir(tmp), chunks.WithSegmentSize(c.maxBlockChunkSegmentSize), chunks.WithUncachedIO(c.useUncachedIO))
		if err != nil {
			return fmt.Errorf("open chunk writer: %w", err)
		}
		chunkw = newPreventDoubleCloseChunkWriter(chunkw) // We now close chunkWriter in populateBlock, but keep it in the closers here as well.

		closers = append(closers, chunkw)

		// Record written chunk sizes on level 1 compactions.
		if outBlocks[ix].meta.Compaction.Level == 1 {
			chunkw = &instrumentedChunkWriter{
				ChunkWriter: chunkw,
				size:        c.metrics.ChunkSize,
				samples:     c.metrics.ChunkSamples,
				trange:      c.metrics.ChunkRange,
			}
		}

		outBlocks[ix].chunkw = chunkw

		var indexw IndexWriter
		indexw, err = index.NewWriterWithEncoder(c.ctx, filepath.Join(tmp, indexFilename), c.postingsEncoder)
		if err != nil {
			return fmt.Errorf("open index writer: %w", err)
		}
		indexw = newPreventDoubleCloseIndexWriter(indexw) // We now close indexWriter in populateBlock, but keep it in the closers here as well.
		closers = append(closers, indexw)

		outBlocks[ix].indexw = indexw
	}

	// We use MinTime and MaxTime from first output block, because ALL output blocks have the same min/max times set.
	if err := blockPopulator.PopulateBlock(c.ctx, c.metrics, c.logger, c.chunkPool, c.mergeFunc, c.concurrencyOpts, blocks, outBlocks[0].meta.MinTime, outBlocks[0].meta.MaxTime, outBlocks, AllSortedPostings); err != nil {
		return fmt.Errorf("populate block: %w", err)
	}

	select {
	case <-c.ctx.Done():
		return c.ctx.Err()
	default:
	}

	// We are explicitly closing them here to check for error even
	// though these are covered under defer. This is because in Windows,
	// you cannot delete these unless they are closed and the defer is to
	// make sure they are closed if the function exits due to an error above.
	errs := tsdb_errors.NewMulti()
	for _, w := range closers {
		errs.Add(w.Close())
	}
	closers = closers[:0] // Avoid closing the writers twice in the defer.
	if errs.Err() != nil {
		return errs.Err()
	}

	for _, ob := range outBlocks {
		// Populated block is empty, don't write meta file for it.
		if ob.meta.Stats.NumSamples == 0 {
			continue
		}

		if _, err = writeMetaFile(c.logger, ob.tmpDir, ob.meta); err != nil {
			return fmt.Errorf("write merged meta: %w", err)
		}

		// Create an empty tombstones file.
		if _, err := tombstones.WriteFile(c.logger, ob.tmpDir, tombstones.NewMemTombstones()); err != nil {
			return fmt.Errorf("write new tombstones file: %w", err)
		}

		df, err := fileutil.OpenDir(ob.tmpDir)
		if err != nil {
			return fmt.Errorf("open temporary block dir: %w", err)
		}
		defer func() {
			if df != nil {
				df.Close()
			}
		}()

		if err := df.Sync(); err != nil {
			return fmt.Errorf("sync temporary dir file: %w", err)
		}

		// Close temp dir before rename block dir (for windows platform).
		if err = df.Close(); err != nil {
			return fmt.Errorf("close temporary dir: %w", err)
		}
		df = nil

		// Block successfully written, make it visible in destination dir by moving it from tmp one.
		if err := fileutil.Replace(ob.tmpDir, ob.blockDir); err != nil {
			return fmt.Errorf("rename block dir: %w", err)
		}
	}

	return nil
}

func debugOutOfOrderChunks(lbls labels.Labels, chks []chunks.Meta, logger *slog.Logger) {
	if len(chks) <= 1 {
		return
	}

	prevChk := chks[0]
	for i := 1; i < len(chks); i++ {
		currChk := chks[i]

		if currChk.MinTime > prevChk.MaxTime {
			// Not out of order.
			continue
		}

		// Looks like the chunk is out of order.
		logValues := []any{
			"num_chunks_for_series", len(chks),
			"index", i,
			"labels", lbls.String(),
			"prev_ref", prevChk.Ref,
			"curr_ref", currChk.Ref,
			"prev_min_time", timeFromMillis(prevChk.MinTime).UTC().String(),
			"prev_max_time", timeFromMillis(prevChk.MaxTime).UTC().String(),
			"curr_min_time", timeFromMillis(currChk.MinTime).UTC().String(),
			"curr_max_time", timeFromMillis(currChk.MaxTime).UTC().String(),
			"prev_samples", prevChk.Chunk.NumSamples(),
			"curr_samples", currChk.Chunk.NumSamples(),
		}

		// Get info out of safeHeadChunk (if possible).
		if prevSafeChk, prevIsSafeChk := prevChk.Chunk.(*safeHeadChunk); prevIsSafeChk {
			logValues = append(logValues,
				"prev_head_chunk_id", prevSafeChk.cid,
				"prev_labelset", prevSafeChk.s.lset.String(),
			)
		}
		if currSafeChk, currIsSafeChk := currChk.Chunk.(*safeHeadChunk); currIsSafeChk {
			logValues = append(logValues,
				"curr_head_chunk_id", currSafeChk.cid,
				"curr_labelset", currSafeChk.s.lset.String(),
			)
		}

		logger.Warn("found out-of-order chunk when compacting", logValues...)
	}
}

func timeFromMillis(ms int64) time.Time {
	return time.Unix(0, ms*int64(time.Millisecond))
}

type BlockPopulator interface {
	PopulateBlock(ctx context.Context, metrics *CompactorMetrics, logger *slog.Logger, chunkPool chunkenc.Pool, mergeFunc storage.VerticalChunkSeriesMergeFunc, concurrencyOpts LeveledCompactorConcurrencyOptions, blocks []BlockReader, minT, maxT int64, outBlocks []shardedBlock, postingsFunc IndexReaderPostingsFunc) error
}

// IndexReaderPostingsFunc is a function to get a sorted posting iterator from a given index reader.
type IndexReaderPostingsFunc func(ctx context.Context, reader IndexReader) index.Postings

// AllSortedPostings returns a sorted all posting iterator from the input index reader.
func AllSortedPostings(ctx context.Context, reader IndexReader) index.Postings {
	k, v := index.AllPostingsKey()
	all, err := reader.Postings(ctx, k, v)
	if err != nil {
		return index.ErrPostings(err)
	}
	return reader.SortedPostings(all)
}

type DefaultBlockPopulator struct{}

// PopulateBlock fills the index and chunk writers with new data gathered as the union
// of the provided blocks. It returns meta information for the new block.
// It expects sorted blocks input by mint.
// If there is more than 1 output block, each output block will only contain series that hash into its shard
// (based on total number of output blocks).
func (c DefaultBlockPopulator) PopulateBlock(ctx context.Context, metrics *CompactorMetrics, logger *slog.Logger, chunkPool chunkenc.Pool, mergeFunc storage.VerticalChunkSeriesMergeFunc, concurrencyOpts LeveledCompactorConcurrencyOptions, blocks []BlockReader, minT, maxT int64, outBlocks []shardedBlock, postingsFunc IndexReaderPostingsFunc) (err error) {
	if len(blocks) == 0 {
		return errors.New("cannot populate block(s) from no readers")
	}

	var (
		sets        []storage.ChunkSeriesSet
		symbolsSets []storage.ChunkSeriesSet // series sets used for finding symbols. Only used when doing sharding.
		symbols     index.StringIter
		closers     []io.Closer
		overlapping bool
	)
	defer func() {
		errs := tsdb_errors.NewMulti(err)
		if cerr := tsdb_errors.CloseAll(closers); cerr != nil {
			errs.Add(fmt.Errorf("close: %w", cerr))
		}
		err = errs.Err()
		metrics.PopulatingBlocks.Set(0)
	}()
	metrics.PopulatingBlocks.Set(1)

	globalMaxt := blocks[0].Meta().MaxTime
	for i, b := range blocks {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if !overlapping {
			if i > 0 && b.Meta().MinTime < globalMaxt {
				metrics.OverlappingBlocks.Inc()
				overlapping = true
				logger.Info("Found overlapping blocks during compaction")
			}
			if b.Meta().MaxTime > globalMaxt {
				globalMaxt = b.Meta().MaxTime
			}
		}

		indexr, err := b.Index()
		if err != nil {
			return fmt.Errorf("open index reader for block %+v: %w", b.Meta(), err)
		}
		closers = append(closers, indexr)

		chunkr, err := b.Chunks()
		if err != nil {
			return fmt.Errorf("open chunk reader for block %+v: %w", b.Meta(), err)
		}
		closers = append(closers, chunkr)

		tombsr, err := b.Tombstones()
		if err != nil {
			return fmt.Errorf("open tombstone reader for block %+v: %w", b.Meta(), err)
		}
		closers = append(closers, tombsr)

		postings := postingsFunc(ctx, indexr)
		// Blocks meta is half open: [min, max), so subtract 1 to ensure we don't hold samples with exact meta.MaxTime timestamp.
		sets = append(sets, NewBlockChunkSeriesSet(b.Meta().ULID, indexr, chunkr, tombsr, postings, minT, maxT-1, false))

		if len(outBlocks) > 1 {
			// To iterate series when populating symbols, we cannot reuse postings we just got, but need to get a new copy.
			// Postings can only be iterated once.
			k, v := index.AllPostingsKey()
			all, err := indexr.Postings(ctx, k, v)
			if err != nil {
				return err
			}
			all = indexr.SortedPostings(all)
			// Blocks meta is half open: [min, max), so subtract 1 to ensure we don't hold samples with exact meta.MaxTime timestamp.
			symbolsSets = append(symbolsSets, NewBlockChunkSeriesSet(b.Meta().ULID, indexr, chunkr, tombsr, all, minT, maxT-1, false))
		} else {
			syms := indexr.Symbols()
			if i == 0 {
				symbols = syms
				continue
			}
			symbols = NewMergedStringIter(symbols, syms)
		}
	}

	if len(outBlocks) == 1 {
		for symbols.Next() {
			if err := outBlocks[0].indexw.AddSymbol(symbols.At()); err != nil {
				return fmt.Errorf("add symbol: %w", err)
			}
		}
		if err := symbols.Err(); err != nil {
			return fmt.Errorf("next symbol: %w", err)
		}
	} else {
		if err := populateSymbols(ctx, mergeFunc, concurrencyOpts, symbolsSets, outBlocks); err != nil {
			return err
		}
	}

	// Semaphore for number of blocks that can be closed at once.
	sema := semaphore.NewWeighted(int64(concurrencyOpts.MaxClosingBlocks))

	blockWriters := make([]*asyncBlockWriter, len(outBlocks))
	for ix := range outBlocks {
		blockWriters[ix] = newAsyncBlockWriter(chunkPool, outBlocks[ix].chunkw, outBlocks[ix].indexw, sema)
	}
	defer func() {
		// Stop all async writers.
		for ix := range outBlocks {
			blockWriters[ix].closeAsync()
		}

		// And wait until they have finished, to make sure that they no longer update chunk or index writers.
		for ix := range outBlocks {
			_, _ = blockWriters[ix].waitFinished()
		}
	}()

	var chksIter chunks.Iterator

	set := sets[0]
	if len(sets) > 1 {
		// Merge series using specified chunk series merger.
		// The default one is the compacting series merger.
		set = storage.NewMergeChunkSeriesSet(sets, 0, mergeFunc)
	}

	// Iterate over all sorted chunk series.
	for set.Next() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		s := set.At()

		chksIter := s.Iterator(chksIter)
		var chks []chunks.Meta
		for chksIter.Next() {
			// We are not iterating in a streaming way over chunks as
			// it's more efficient to do bulk write for index and
			// chunk file purposes.
			chks = append(chks, chksIter.At())
		}
		if err := chksIter.Err(); err != nil {
			return fmt.Errorf("chunk iter: %w", err)
		}

		// Skip series with all deleted chunks.
		if len(chks) == 0 {
			continue
		}

		debugOutOfOrderChunks(s.Labels(), chks, logger)

		obIx := uint64(0)
		if len(outBlocks) > 1 {
			obIx = labels.StableHash(s.Labels()) % uint64(len(outBlocks))
		}

		err := blockWriters[obIx].addSeries(s.Labels(), chks)
		if err != nil {
			return fmt.Errorf("adding series: %w", err)
		}
	}
	if err := set.Err(); err != nil {
		return fmt.Errorf("iterate compaction set: %w", err)
	}

	for ix := range blockWriters {
		blockWriters[ix].closeAsync()
	}

	for ix := range blockWriters {
		stats, err := blockWriters[ix].waitFinished()
		if err != nil {
			return fmt.Errorf("writing block: %w", err)
		}

		outBlocks[ix].meta.Stats = stats
	}

	return nil
}

// How many symbols we buffer in memory per output block.
const inMemorySymbolsLimit = 1_000_000

// populateSymbols writes symbols to output blocks. We need to iterate through all series to find
// which series belongs to what block. We collect symbols per sharded block, and then add sorted symbols to
// block's index.
func populateSymbols(ctx context.Context, mergeFunc storage.VerticalChunkSeriesMergeFunc, concurrencyOpts LeveledCompactorConcurrencyOptions, sets []storage.ChunkSeriesSet, outBlocks []shardedBlock) error {
	if len(outBlocks) == 0 {
		return errors.New("no output block")
	}

	flushers := newSymbolFlushers(concurrencyOpts.SymbolsFlushersCount)
	defer flushers.close() // Make sure to stop flushers before exiting to avoid leaking goroutines.

	batchers := make([]*symbolsBatcher, len(outBlocks))
	for ix := range outBlocks {
		batchers[ix] = newSymbolsBatcher(inMemorySymbolsLimit, outBlocks[ix].tmpDir, flushers)

		// Always include empty symbol. Blocks created from Head always have it in the symbols table,
		// and if we only include symbols from series, we would skip it.
		// It may not be required, but it's small and better be safe than sorry.
		if err := batchers[ix].addSymbol(""); err != nil {
			return fmt.Errorf("addSymbol to batcher: %w", err)
		}
	}

	seriesSet := sets[0]
	if len(sets) > 1 {
		seriesSet = storage.NewMergeChunkSeriesSet(sets, 0, mergeFunc)
	}

	for seriesSet.Next() {
		if err := ctx.Err(); err != nil {
			return err
		}

		s := seriesSet.At()

		var obIx uint64
		if len(outBlocks) > 1 {
			obIx = labels.StableHash(s.Labels()) % uint64(len(outBlocks))
		}

		err := s.Labels().Validate(func(l labels.Label) error {
			if err := batchers[obIx].addSymbol(l.Name); err != nil {
				return fmt.Errorf("addSymbol to batcher: %w", err)
			}
			if err := batchers[obIx].addSymbol(l.Value); err != nil {
				return fmt.Errorf("addSymbol to batcher: %w", err)
			}
			return nil
		})
		if err != nil {
			return err
		}
	}

	for ix := range outBlocks {
		// Flush the batcher to write remaining symbols.
		if err := batchers[ix].flushSymbols(true); err != nil {
			return fmt.Errorf("flushing batcher: %w", err)
		}
	}

	err := flushers.close()
	if err != nil {
		return fmt.Errorf("closing flushers: %w", err)
	}

	for ix := range outBlocks {
		if err := ctx.Err(); err != nil {
			return err
		}

		symbolFiles := batchers[ix].getSymbolFiles()

		it, err := newSymbolsIterator(symbolFiles)
		if err != nil {
			return fmt.Errorf("opening symbols iterator: %w", err)
		}

		// Each symbols iterator must be closed to close underlying files.
		closeIt := it
		defer func() {
			if closeIt != nil {
				_ = closeIt.Close()
			}
		}()

		var sym string
		for sym, err = it.NextSymbol(); err == nil; sym, err = it.NextSymbol() {
			err = outBlocks[ix].indexw.AddSymbol(sym)
			if err != nil {
				return fmt.Errorf("AddSymbol: %w", err)
			}
		}

		if !errors.Is(err, io.EOF) {
			return fmt.Errorf("iterating symbols: %w", err)
		}

		// if err == io.EOF, we have iterated through all symbols. We can close underlying
		// files now.
		closeIt = nil
		_ = it.Close()

		// Delete symbol files from symbolsBatcher. We don't need to perform the cleanup if populateSymbols
		// or compaction fails, because in that case compactor already removes entire (temp) output block directory.
		for _, fn := range symbolFiles {
			if err := os.Remove(fn); err != nil {
				return fmt.Errorf("deleting symbols file: %w", err)
			}
		}
	}

	return nil
}

// Returns opened blocks, and blocks that should be closed (also returned in case of error).
func openBlocksForCompaction(ctx context.Context, dirs []string, open []*Block, logger *slog.Logger, pool chunkenc.Pool, postingsDecoderFactory PostingsDecoderFactory, concurrency int) (blocks, blocksToClose []*Block, _ error) {
	blocks = make([]*Block, 0, len(dirs))
	blocksToClose = make([]*Block, 0, len(dirs))

	toOpenCh := make(chan string, len(dirs))
	for _, d := range dirs {
		select {
		case <-ctx.Done():
			return nil, blocksToClose, ctx.Err()
		default:
		}

		meta, _, err := readMetaFile(d)
		if err != nil {
			return nil, blocksToClose, err
		}

		var b *Block

		// Use already open blocks if we can, to avoid
		// having the index data in memory twice.
		for _, o := range open {
			if meta.ULID == o.Meta().ULID {
				b = o
				break
			}
		}

		if b != nil {
			blocks = append(blocks, b)
		} else {
			toOpenCh <- d
		}
	}
	close(toOpenCh)

	type openResult struct {
		b   *Block
		err error
	}

	openResultCh := make(chan openResult, len(toOpenCh))
	// Signals to all opening goroutines that there was an error opening some block, and they can stop early.
	// If openingError is true, at least one error is sent to openResultCh.
	openingError := atomic.NewBool(false)

	wg := sync.WaitGroup{}
	if len(dirs) < concurrency {
		concurrency = len(dirs)
	}
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			for d := range toOpenCh {
				if openingError.Load() {
					return
				}

				b, err := OpenBlock(logger, d, pool, postingsDecoderFactory)
				openResultCh <- openResult{b: b, err: err}

				if err != nil {
					openingError.Store(true)
					return
				}
			}
		}()
	}
	wg.Wait()

	// All writers to openResultCh have stopped, we can close the output channel, so we can range over it.
	close(openResultCh)

	var firstErr error
	for or := range openResultCh {
		if or.err != nil {
			// Don't stop on error, but iterate over all opened blocks to collect blocksToClose.
			if firstErr == nil {
				firstErr = or.err
			}
		} else {
			blocks = append(blocks, or.b)
			blocksToClose = append(blocksToClose, or.b)
		}
	}

	return blocks, blocksToClose, firstErr
}
