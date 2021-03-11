// Copyright (c) The Thanos Authors.
// Licensed under the Apache License 2.0.

package block

import (
	"context"
	"fmt"
	"hash/crc32"
	"math"
	"math/rand"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/thanos-io/thanos/pkg/block/metadata"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/oklog/ulid"
	"github.com/pkg/errors"

	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/prometheus/prometheus/tsdb"
	"github.com/prometheus/prometheus/tsdb/chunks"
	"github.com/prometheus/prometheus/tsdb/index"
	"github.com/thanos-io/thanos/pkg/runutil"
)

// VerifyIndex does a full run over a block index and verifies that it fulfills the order invariants.
func VerifyIndex(logger log.Logger, fn string, minTime int64, maxTime int64) error {
	stats, err := GatherIndexHealthStats(logger, fn, minTime, maxTime)
	if err != nil {
		return err
	}

	return stats.AnyErr()
}

type HealthStats struct {
	// TotalSeries represents total number of series in block.
	TotalSeries int64
	// OutOfOrderSeries represents number of series that have out of order chunks.
	OutOfOrderSeries int

	// OutOfOrderChunks represents number of chunks that are out of order (older time range is after younger one).
	OutOfOrderChunks int
	// DuplicatedChunks represents number of chunks with same time ranges within same series, potential duplicates.
	DuplicatedChunks int
	// OutsideChunks represents number of all chunks that are before or after time range specified in block meta.
	OutsideChunks int
	// CompleteOutsideChunks is subset of OutsideChunks that will be never accessed. They are completely out of time range specified in block meta.
	CompleteOutsideChunks int
	// Issue347OutsideChunks represents subset of OutsideChunks that are outsiders caused by https://github.com/prometheus/tsdb/issues/347
	// and is something that Thanos handle.
	//
	// Specifically we mean here chunks with minTime == block.maxTime and maxTime > block.MaxTime. These are
	// are segregated into separate counters. These chunks are safe to be deleted, since they are duplicated across 2 blocks.
	Issue347OutsideChunks int
	// OutOfOrderLabels represents the number of postings that contained out
	// of order labels, a bug present in Prometheus 2.8.0 and below.
	OutOfOrderLabels int

	// Debug Statistics.
	SeriesMinLifeDuration time.Duration
	SeriesAvgLifeDuration time.Duration
	SeriesMaxLifeDuration time.Duration

	SeriesMinLifeDurationWithoutSingleSampleSeries time.Duration
	SeriesAvgLifeDurationWithoutSingleSampleSeries time.Duration
	SeriesMaxLifeDurationWithoutSingleSampleSeries time.Duration

	SeriesMinChunks int64
	SeriesAvgChunks int64
	SeriesMaxChunks int64

	TotalChunks int64

	ChunkMinDuration time.Duration
	ChunkAvgDuration time.Duration
	ChunkMaxDuration time.Duration

	ChunkMinSize int64
	ChunkAvgSize int64
	ChunkMaxSize int64

	SingleSampleSeries int64
	SingleSampleChunks int64

	LabelNamesCount        int64
	MetricLabelValuesCount int64
}

// PrometheusIssue5372Err returns an error if the HealthStats object indicates
// postings with out of order labels.  This is corrected by Prometheus Issue
// #5372 and affects Prometheus versions 2.8.0 and below.
func (i HealthStats) PrometheusIssue5372Err() error {
	if i.OutOfOrderLabels > 0 {
		return errors.Errorf("index contains %d postings with out of order labels",
			i.OutOfOrderLabels)
	}
	return nil
}

// Issue347OutsideChunksErr returns error if stats indicates issue347 block issue, that is repaired explicitly before compaction (on plan block).
func (i HealthStats) Issue347OutsideChunksErr() error {
	if i.Issue347OutsideChunks > 0 {
		return errors.Errorf("found %d chunks outside the block time range introduced by https://github.com/prometheus/tsdb/issues/347", i.Issue347OutsideChunks)
	}
	return nil
}

// CriticalErr returns error if stats indicates critical block issue, that might solved only by manual repair procedure.
func (i HealthStats) CriticalErr() error {
	var errMsg []string

	if i.OutOfOrderSeries > 0 {
		errMsg = append(errMsg, fmt.Sprintf(
			"%d/%d series have an average of %.3f out-of-order chunks: "+
				"%.3f of these are exact duplicates (in terms of data and time range)",
			i.OutOfOrderSeries,
			i.TotalSeries,
			float64(i.OutOfOrderChunks)/float64(i.OutOfOrderSeries),
			float64(i.DuplicatedChunks)/float64(i.OutOfOrderChunks),
		))
	}

	n := i.OutsideChunks - (i.CompleteOutsideChunks + i.Issue347OutsideChunks)
	if n > 0 {
		errMsg = append(errMsg, fmt.Sprintf("found %d chunks non-completely outside the block time range", n))
	}

	if i.CompleteOutsideChunks > 0 {
		errMsg = append(errMsg, fmt.Sprintf("found %d chunks completely outside the block time range", i.CompleteOutsideChunks))
	}

	if len(errMsg) > 0 {
		return errors.New(strings.Join(errMsg, ", "))
	}

	return nil
}

// AnyErr returns error if stats indicates any block issue.
func (i HealthStats) AnyErr() error {
	var errMsg []string

	if err := i.CriticalErr(); err != nil {
		errMsg = append(errMsg, err.Error())
	}

	if err := i.Issue347OutsideChunksErr(); err != nil {
		errMsg = append(errMsg, err.Error())
	}

	if err := i.PrometheusIssue5372Err(); err != nil {
		errMsg = append(errMsg, err.Error())
	}

	if len(errMsg) > 0 {
		return errors.New(strings.Join(errMsg, ", "))
	}

	return nil
}

type minMaxSumInt64 struct {
	sum int64
	min int64
	max int64

	cnt int64
}

func newMinMaxSumInt64() minMaxSumInt64 {
	return minMaxSumInt64{
		min: math.MaxInt64,
		max: math.MinInt64,
	}
}

func (n *minMaxSumInt64) Add(v int64) {
	n.cnt++
	n.sum += v
	if n.min > v {
		n.min = v
	}
	if n.max < v {
		n.max = v
	}
}

func (n *minMaxSumInt64) Avg() int64 {
	if n.cnt == 0 {
		return 0
	}
	return n.sum / n.cnt
}

// GatherIndexHealthStats returns useful counters as well as outsider chunks (chunks outside of block time range) that
// helps to assess index health.
// It considers https://github.com/prometheus/tsdb/issues/347 as something that Thanos can handle.
// See HealthStats.Issue347OutsideChunks for details.
func GatherIndexHealthStats(logger log.Logger, fn string, minTime int64, maxTime int64) (stats HealthStats, err error) {
	r, err := index.NewFileReader(fn)
	if err != nil {
		return stats, errors.Wrap(err, "open index file")
	}
	defer runutil.CloseWithErrCapture(&err, r, "gather index issue file reader")

	p, err := r.Postings(index.AllPostingsKey())
	if err != nil {
		return stats, errors.Wrap(err, "get all postings")
	}
	var (
		lastLset labels.Labels
		lset     labels.Labels
		chks     []chunks.Meta

		seriesLifeDuration                          = newMinMaxSumInt64()
		seriesLifeDurationWithoutSingleSampleSeries = newMinMaxSumInt64()
		seriesChunks                                = newMinMaxSumInt64()
		chunkDuration                               = newMinMaxSumInt64()
		chunkSize                                   = newMinMaxSumInt64()
	)

	lnames, err := r.LabelNames()
	if err != nil {
		return stats, errors.Wrap(err, "label names")
	}
	stats.LabelNamesCount = int64(len(lnames))

	lvals, err := r.LabelValues("__name__")
	if err != nil {
		return stats, errors.Wrap(err, "metric label values")
	}
	stats.MetricLabelValuesCount = int64(len(lvals))

	// Per series.
	for p.Next() {
		lastLset = append(lastLset[:0], lset...)

		id := p.At()
		stats.TotalSeries++

		if err := r.Series(id, &lset, &chks); err != nil {
			return stats, errors.Wrap(err, "read series")
		}
		if len(lset) == 0 {
			return stats, errors.Errorf("empty label set detected for series %d", id)
		}
		if lastLset != nil && labels.Compare(lastLset, lset) >= 0 {
			return stats, errors.Errorf("series %v out of order; previous %v", lset, lastLset)
		}
		l0 := lset[0]
		for _, l := range lset[1:] {
			if l.Name < l0.Name {
				stats.OutOfOrderLabels++
				level.Warn(logger).Log("msg",
					"out-of-order label set: known bug in Prometheus 2.8.0 and below",
					"labelset", lset.String(),
					"series", fmt.Sprintf("%d", id),
				)
			}
			l0 = l
		}
		if len(chks) == 0 {
			return stats, errors.Errorf("empty chunks for series %d", id)
		}

		ooo := 0
		seriesLifeTimeMs := int64(0)
		// Per chunk in series.
		for i, c := range chks {
			stats.TotalChunks++

			chkDur := c.MaxTime - c.MinTime
			seriesLifeTimeMs += chkDur
			chunkDuration.Add(chkDur)
			if chkDur == 0 {
				stats.SingleSampleChunks++
			}

			// Approximate size.
			if i < len(chks)-2 {
				chunkSize.Add(int64(chks[i+1].Ref - c.Ref))
			}

			// Chunk vs the block ranges.
			if c.MinTime < minTime || c.MaxTime > maxTime {
				stats.OutsideChunks++
				if c.MinTime > maxTime || c.MaxTime < minTime {
					stats.CompleteOutsideChunks++
				} else if c.MinTime == maxTime {
					stats.Issue347OutsideChunks++
				}
			}

			if i == 0 {
				continue
			}

			c0 := chks[i-1]

			// Chunk order within block.
			if c.MinTime > c0.MaxTime {
				continue
			}

			if c.MinTime == c0.MinTime && c.MaxTime == c0.MaxTime {
				// TODO(bplotka): Calc and check checksum from chunks itself.
				// The chunks can overlap 1:1 in time, but does not have same data.
				// We assume same data for simplicity, but it can be a symptom of error.
				stats.DuplicatedChunks++
				continue
			}
			// Chunks partly overlaps or out of order.
			ooo++
		}
		if ooo > 0 {
			stats.OutOfOrderSeries++
			stats.OutOfOrderChunks += ooo
		}

		seriesChunks.Add(int64(len(chks)))
		seriesLifeDuration.Add(seriesLifeTimeMs)

		if seriesLifeTimeMs == 0 {
			stats.SingleSampleSeries++
		} else {
			seriesLifeDurationWithoutSingleSampleSeries.Add(seriesLifeTimeMs)
		}
	}
	if p.Err() != nil {
		return stats, errors.Wrap(err, "walk postings")
	}

	stats.SeriesMaxLifeDuration = time.Duration(seriesLifeDuration.max) * time.Millisecond
	stats.SeriesAvgLifeDuration = time.Duration(seriesLifeDuration.Avg()) * time.Millisecond
	stats.SeriesMinLifeDuration = time.Duration(seriesLifeDuration.min) * time.Millisecond

	stats.SeriesMaxLifeDurationWithoutSingleSampleSeries = time.Duration(seriesLifeDurationWithoutSingleSampleSeries.max) * time.Millisecond
	stats.SeriesAvgLifeDurationWithoutSingleSampleSeries = time.Duration(seriesLifeDurationWithoutSingleSampleSeries.Avg()) * time.Millisecond
	stats.SeriesMinLifeDurationWithoutSingleSampleSeries = time.Duration(seriesLifeDurationWithoutSingleSampleSeries.min) * time.Millisecond

	stats.SeriesMaxChunks = seriesChunks.max
	stats.SeriesAvgChunks = seriesChunks.Avg()
	stats.SeriesMinChunks = seriesChunks.min

	stats.ChunkMaxSize = chunkSize.max
	stats.ChunkAvgSize = chunkSize.Avg()
	stats.ChunkMinSize = chunkSize.min

	stats.ChunkMaxDuration = time.Duration(chunkDuration.max) * time.Millisecond
	stats.ChunkAvgDuration = time.Duration(chunkDuration.Avg()) * time.Millisecond
	stats.ChunkMinDuration = time.Duration(chunkDuration.min) * time.Millisecond
	return stats, nil
}

type ignoreFnType func(mint, maxt int64, prev *chunks.Meta, curr *chunks.Meta) (bool, error)

// Repair open the block with given id in dir and creates a new one with fixed data.
// It:
// - removes out of order duplicates
// - all "complete" outsiders (they will not accessed anyway)
// - removes all near "complete" outside chunks introduced by https://github.com/prometheus/tsdb/issues/347.
// Fixable inconsistencies are resolved in the new block.
// TODO(bplotka): https://github.com/thanos-io/thanos/issues/378.
func Repair(logger log.Logger, dir string, id ulid.ULID, source metadata.SourceType, ignoreChkFns ...ignoreFnType) (resid ulid.ULID, err error) {
	if len(ignoreChkFns) == 0 {
		return resid, errors.New("no ignore chunk function specified")
	}

	bdir := filepath.Join(dir, id.String())
	entropy := rand.New(rand.NewSource(time.Now().UnixNano()))
	resid = ulid.MustNew(ulid.Now(), entropy)

	meta, err := metadata.ReadFromDir(bdir)
	if err != nil {
		return resid, errors.Wrap(err, "read meta file")
	}
	if meta.Thanos.Downsample.Resolution > 0 {
		return resid, errors.New("cannot repair downsampled block")
	}

	b, err := tsdb.OpenBlock(logger, bdir, nil)
	if err != nil {
		return resid, errors.Wrap(err, "open block")
	}
	defer runutil.CloseWithErrCapture(&err, b, "repair block reader")

	indexr, err := b.Index()
	if err != nil {
		return resid, errors.Wrap(err, "open index")
	}
	defer runutil.CloseWithErrCapture(&err, indexr, "repair index reader")

	chunkr, err := b.Chunks()
	if err != nil {
		return resid, errors.Wrap(err, "open chunks")
	}
	defer runutil.CloseWithErrCapture(&err, chunkr, "repair chunk reader")

	resdir := filepath.Join(dir, resid.String())

	chunkw, err := chunks.NewWriter(filepath.Join(resdir, ChunksDirname))
	if err != nil {
		return resid, errors.Wrap(err, "open chunk writer")
	}
	defer runutil.CloseWithErrCapture(&err, chunkw, "repair chunk writer")

	indexw, err := index.NewWriter(context.TODO(), filepath.Join(resdir, IndexFilename))
	if err != nil {
		return resid, errors.Wrap(err, "open index writer")
	}
	defer runutil.CloseWithErrCapture(&err, indexw, "repair index writer")

	// TODO(fabxc): adapt so we properly handle the version once we update to an upstream
	// that has multiple.
	resmeta := *meta
	resmeta.ULID = resid
	resmeta.Stats = tsdb.BlockStats{} // Reset stats.
	resmeta.Thanos.Source = source    // Update source.

	if err := rewrite(logger, indexr, chunkr, indexw, chunkw, &resmeta, ignoreChkFns); err != nil {
		return resid, errors.Wrap(err, "rewrite block")
	}
	resmeta.Thanos.SegmentFiles = GetSegmentFiles(resdir)
	if err := resmeta.WriteToDir(logger, resdir); err != nil {
		return resid, err
	}
	// TSDB may rewrite metadata in bdir.
	// TODO: This is not needed in newer TSDB code. See https://github.com/prometheus/tsdb/pull/637.
	if err := meta.WriteToDir(logger, bdir); err != nil {
		return resid, err
	}
	return resid, nil
}

var castagnoli = crc32.MakeTable(crc32.Castagnoli)

func IgnoreCompleteOutsideChunk(mint int64, maxt int64, _ *chunks.Meta, curr *chunks.Meta) (bool, error) {
	if curr.MinTime > maxt || curr.MaxTime < mint {
		// "Complete" outsider. Ignore.
		return true, nil
	}
	return false, nil
}

func IgnoreIssue347OutsideChunk(_ int64, maxt int64, _ *chunks.Meta, curr *chunks.Meta) (bool, error) {
	if curr.MinTime == maxt {
		// "Near" outsider from issue https://github.com/prometheus/tsdb/issues/347. Ignore.
		return true, nil
	}
	return false, nil
}

func IgnoreDuplicateOutsideChunk(_ int64, _ int64, last *chunks.Meta, curr *chunks.Meta) (bool, error) {
	if last == nil {
		return false, nil
	}

	if curr.MinTime > last.MaxTime {
		return false, nil
	}

	// Verify that the overlapping chunks are exact copies so we can safely discard
	// the current one.
	if curr.MinTime != last.MinTime || curr.MaxTime != last.MaxTime {
		return false, errors.Errorf("non-sequential chunks not equal: [%d, %d] and [%d, %d]",
			last.MinTime, last.MaxTime, curr.MinTime, curr.MaxTime)
	}
	ca := crc32.Checksum(last.Chunk.Bytes(), castagnoli)
	cb := crc32.Checksum(curr.Chunk.Bytes(), castagnoli)

	if ca != cb {
		return false, errors.Errorf("non-sequential chunks not equal: %x and %x", ca, cb)
	}

	return true, nil
}

// sanitizeChunkSequence ensures order of the input chunks and drops any duplicates.
// It errors if the sequence contains non-dedupable overlaps.
func sanitizeChunkSequence(chks []chunks.Meta, mint int64, maxt int64, ignoreChkFns []ignoreFnType) ([]chunks.Meta, error) {
	if len(chks) == 0 {
		return nil, nil
	}
	// First, ensure that chunks are ordered by their start time.
	sort.Slice(chks, func(i, j int) bool {
		return chks[i].MinTime < chks[j].MinTime
	})

	// Remove duplicates, complete outsiders and near outsiders.
	repl := make([]chunks.Meta, 0, len(chks))
	var last *chunks.Meta

OUTER:
	// This compares the current chunk to the chunk from the last iteration
	// by pointers.  If we use "i, c := range chks" the variable c is a new
	// variable who's address doesn't change through the entire loop.
	// The current element of the chks slice is copied into it. We must take
	// the address of the indexed slice instead.
	for i := range chks {
		for _, ignoreChkFn := range ignoreChkFns {
			ignore, err := ignoreChkFn(mint, maxt, last, &chks[i])
			if err != nil {
				return nil, errors.Wrap(err, "ignore function")
			}

			if ignore {
				continue OUTER
			}
		}

		last = &chks[i]
		repl = append(repl, chks[i])
	}

	return repl, nil
}

type seriesRepair struct {
	lset labels.Labels
	chks []chunks.Meta
}

// rewrite writes all data from the readers back into the writers while cleaning
// up mis-ordered and duplicated chunks.
func rewrite(
	logger log.Logger,
	indexr tsdb.IndexReader, chunkr tsdb.ChunkReader,
	indexw tsdb.IndexWriter, chunkw tsdb.ChunkWriter,
	meta *metadata.Meta,
	ignoreChkFns []ignoreFnType,
) error {
	symbols := indexr.Symbols()
	for symbols.Next() {
		if err := indexw.AddSymbol(symbols.At()); err != nil {
			return errors.Wrap(err, "add symbol")
		}
	}
	if symbols.Err() != nil {
		return errors.Wrap(symbols.Err(), "next symbol")
	}

	all, err := indexr.Postings(index.AllPostingsKey())
	if err != nil {
		return errors.Wrap(err, "postings")
	}
	all = indexr.SortedPostings(all)

	// We fully rebuild the postings list index from merged series.
	var (
		postings = index.NewMemPostings()
		values   = map[string]stringset{}
		i        = uint64(0)
		series   = []seriesRepair{}
	)

	var lset labels.Labels
	var chks []chunks.Meta
	for all.Next() {
		id := all.At()

		if err := indexr.Series(id, &lset, &chks); err != nil {
			return errors.Wrap(err, "series")
		}
		// Make sure labels are in sorted order.
		sort.Sort(lset)

		for i, c := range chks {
			chks[i].Chunk, err = chunkr.Chunk(c.Ref)
			if err != nil {
				return errors.Wrap(err, "chunk read")
			}
		}

		chks, err := sanitizeChunkSequence(chks, meta.MinTime, meta.MaxTime, ignoreChkFns)
		if err != nil {
			return err
		}

		if len(chks) == 0 {
			continue
		}

		series = append(series, seriesRepair{
			lset: lset,
			chks: chks,
		})
	}

	if all.Err() != nil {
		return errors.Wrap(all.Err(), "iterate series")
	}

	// Sort the series, if labels are re-ordered then the ordering of series
	// will be different.
	sort.Slice(series, func(i, j int) bool {
		return labels.Compare(series[i].lset, series[j].lset) < 0
	})

	lastSet := labels.Labels{}
	// Build a new TSDB block.
	for _, s := range series {
		// The TSDB library will throw an error if we add a series with
		// identical labels as the last series. This means that we have
		// discovered a duplicate time series in the old block. We drop
		// all duplicate series preserving the first one.
		// TODO: Add metric to count dropped series if repair becomes a daemon
		// rather than a batch job.
		if labels.Compare(lastSet, s.lset) == 0 {
			level.Warn(logger).Log("msg",
				"dropping duplicate series in tsdb block found",
				"labelset", s.lset.String(),
			)
			continue
		}
		if err := chunkw.WriteChunks(s.chks...); err != nil {
			return errors.Wrap(err, "write chunks")
		}
		if err := indexw.AddSeries(i, s.lset, s.chks...); err != nil {
			return errors.Wrap(err, "add series")
		}

		meta.Stats.NumChunks += uint64(len(s.chks))
		meta.Stats.NumSeries++

		for _, chk := range s.chks {
			meta.Stats.NumSamples += uint64(chk.Chunk.NumSamples())
		}

		for _, l := range s.lset {
			valset, ok := values[l.Name]
			if !ok {
				valset = stringset{}
				values[l.Name] = valset
			}
			valset.set(l.Value)
		}
		postings.Add(i, s.lset)
		i++
		lastSet = s.lset
	}
	return nil
}

type stringset map[string]struct{}

func (ss stringset) set(s string) {
	ss[s] = struct{}{}
}

func (ss stringset) String() string {
	return strings.Join(ss.slice(), ",")
}

func (ss stringset) slice() []string {
	slice := make([]string, 0, len(ss))
	for k := range ss {
		slice = append(slice, k)
	}
	sort.Strings(slice)
	return slice
}
