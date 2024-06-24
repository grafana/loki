package v1

import (
	"context"

	"github.com/efficientgo/core/errors"
	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/prometheus/common/model"
	"go.uber.org/atomic"

	"github.com/grafana/loki/v3/pkg/util/spanlogger"
)

type Request struct {
	Fp       model.Fingerprint
	Chks     ChunkRefs
	Search   BloomTest
	Response chan<- Output
	Recorder *BloomRecorder
}

// BloomRecorder records the results of a bloom search
func NewBloomRecorder(ctx context.Context, id string) *BloomRecorder {
	return &BloomRecorder{
		ctx:            ctx,
		id:             id,
		seriesFound:    atomic.NewInt64(0),
		chunksFound:    atomic.NewInt64(0),
		seriesSkipped:  atomic.NewInt64(0),
		chunksSkipped:  atomic.NewInt64(0),
		seriesMissed:   atomic.NewInt64(0),
		chunksMissed:   atomic.NewInt64(0),
		chunksFiltered: atomic.NewInt64(0),
	}
}

type BloomRecorder struct {
	ctx context.Context
	id  string
	// exists in the bloom+queried
	seriesFound, chunksFound *atomic.Int64
	// exists in bloom+skipped
	seriesSkipped, chunksSkipped *atomic.Int64
	// not found in bloom
	seriesMissed, chunksMissed *atomic.Int64
	// filtered out
	chunksFiltered *atomic.Int64
}

func (r *BloomRecorder) Merge(other *BloomRecorder) {
	r.seriesFound.Add(other.seriesFound.Load())
	r.chunksFound.Add(other.chunksFound.Load())
	r.seriesSkipped.Add(other.seriesSkipped.Load())
	r.chunksSkipped.Add(other.chunksSkipped.Load())
	r.seriesMissed.Add(other.seriesMissed.Load())
	r.chunksMissed.Add(other.chunksMissed.Load())
	r.chunksFiltered.Add(other.chunksFiltered.Load())
}

func (r *BloomRecorder) Report(logger log.Logger, metrics *Metrics) {
	logger = spanlogger.FromContextWithFallback(r.ctx, logger)

	var (
		seriesFound     = r.seriesFound.Load()
		seriesSkipped   = r.seriesSkipped.Load()
		seriesMissed    = r.seriesMissed.Load()
		seriesRequested = seriesFound + seriesSkipped + seriesMissed

		chunksFound     = r.chunksFound.Load()
		chunksSkipped   = r.chunksSkipped.Load()
		chunksMissed    = r.chunksMissed.Load()
		chunksFiltered  = r.chunksFiltered.Load()
		chunksRequested = chunksFound + chunksSkipped + chunksMissed
	)
	level.Debug(logger).Log(
		"recorder_msg", "bloom search results",
		"recorder_id", r.id,

		"recorder_series_requested", seriesRequested,
		"recorder_series_found", seriesFound,
		"recorder_series_skipped", seriesSkipped,
		"recorder_series_missed", seriesMissed,

		"recorder_chunks_requested", chunksRequested,
		"recorder_chunks_found", chunksFound,
		"recorder_chunks_skipped", chunksSkipped,
		"recorder_chunks_missed", chunksMissed,
		"recorder_chunks_filtered", chunksFiltered,
	)

	if metrics != nil {
		metrics.recorderSeries.WithLabelValues(recorderRequested).Add(float64(seriesRequested))
		metrics.recorderSeries.WithLabelValues(recorderFound).Add(float64(seriesFound))
		metrics.recorderSeries.WithLabelValues(recorderSkipped).Add(float64(seriesSkipped))
		metrics.recorderSeries.WithLabelValues(recorderMissed).Add(float64(seriesMissed))

		metrics.recorderChunks.WithLabelValues(recorderRequested).Add(float64(chunksRequested))
		metrics.recorderChunks.WithLabelValues(recorderFound).Add(float64(chunksFound))
		metrics.recorderChunks.WithLabelValues(recorderSkipped).Add(float64(chunksSkipped))
		metrics.recorderChunks.WithLabelValues(recorderMissed).Add(float64(chunksMissed))
		metrics.recorderChunks.WithLabelValues(recorderFiltered).Add(float64(chunksFiltered))
	}
}

func (r *BloomRecorder) record(
	seriesFound, chunksFound, seriesSkipped, chunksSkipped, seriesMissed, chunksMissed, chunksFiltered int,
) {
	r.seriesFound.Add(int64(seriesFound))
	r.chunksFound.Add(int64(chunksFound))
	r.seriesSkipped.Add(int64(seriesSkipped))
	r.chunksSkipped.Add(int64(chunksSkipped))
	r.seriesMissed.Add(int64(seriesMissed))
	r.chunksMissed.Add(int64(chunksMissed))
	r.chunksFiltered.Add(int64(chunksFiltered))
}

// Output represents a chunk that failed to pass all searches
// and must be downloaded
type Output struct {
	Fp       model.Fingerprint
	Removals ChunkRefs
}

// Fuse combines multiple requests into a single loop iteration
// over the data set and returns the corresponding outputs
// TODO(owen-d): better async control
func (bq *BlockQuerier) Fuse(inputs []PeekingIterator[Request], logger log.Logger) *FusedQuerier {
	return NewFusedQuerier(bq, inputs, logger)
}

type FusedQuerier struct {
	bq     *BlockQuerier
	inputs Iterator[[]Request]
	logger log.Logger
}

func NewFusedQuerier(bq *BlockQuerier, inputs []PeekingIterator[Request], logger log.Logger) *FusedQuerier {
	heap := NewHeapIterator[Request](
		func(a, b Request) bool {
			return a.Fp < b.Fp
		},
		inputs...,
	)

	merging := NewDedupingIter[Request, []Request](
		func(a Request, b []Request) bool {
			return a.Fp == b[0].Fp
		},
		func(a Request) []Request { return []Request{a} },
		func(a Request, b []Request) []Request {
			return append(b, a)
		},
		NewPeekingIter[Request](heap),
	)
	return &FusedQuerier{
		bq:     bq,
		inputs: merging,
		logger: logger,
	}
}

func (fq *FusedQuerier) recordMissingFp(
	batch []Request,
	fp model.Fingerprint,
) {
	fq.noRemovals(batch, fp, func(input Request) {
		input.Recorder.record(
			0, 0, // found
			0, 0, // skipped
			1, len(input.Chks), // missed
			0, // chunks filtered
		)
	})
}

func (fq *FusedQuerier) recordSkippedFp(
	batch []Request,
	fp model.Fingerprint,
) {
	fq.noRemovals(batch, fp, func(input Request) {
		input.Recorder.record(
			0, 0, // found
			1, len(input.Chks), // skipped
			0, 0, // missed
			0, // chunks filtered
		)
	})
}

func (fq *FusedQuerier) noRemovals(
	batch []Request,
	fp model.Fingerprint,
	fn func(Request),
) {
	for _, input := range batch {
		if fp != input.Fp {
			// should not happen, but log just in case
			level.Error(fq.logger).Log(
				"msg", "fingerprint mismatch",
				"expected", fp,
				"actual", input.Fp,
				"block", "TODO",
			)
		}
		fn(input)
		input.Response <- Output{
			Fp:       fp,
			Removals: nil,
		}
	}
}

func (fq *FusedQuerier) Run() error {
	schema, err := fq.bq.Schema()
	if err != nil {
		return errors.Wrap(err, "getting schema")
	}

	for fq.inputs.Next() {
		// find all queries for the next relevant fingerprint
		nextBatch := fq.inputs.At()

		fp := nextBatch[0].Fp

		// advance the series iterator to the next fingerprint
		if err := fq.bq.Seek(fp); err != nil {
			return errors.Wrap(err, "seeking to fingerprint")
		}

		if !fq.bq.Next() {
			// no more series, we're done since we're iterating desired fingerprints in order
			return nil
		}

		series := fq.bq.At()
		if series.Fingerprint != fp {
			// fingerprint not found, can't remove chunks
			level.Debug(fq.logger).Log("msg", "fingerprint not found", "fp", series.Fingerprint, "err", fq.bq.Err())
			fq.recordMissingFp(nextBatch, fp)
			continue
		}

		fq.runSeries(schema, series, nextBatch)
	}

	return nil
}

func (fq *FusedQuerier) runSeries(schema Schema, series *SeriesWithOffsets, reqs []Request) {
	// For a given chunk|series to be removed, it must fail to match all blooms.
	// Because iterating/loading blooms can be expensive, we iterate blooms one at a time, collecting
	// the removals (failures) for each (bloom, chunk) pair.
	// At the end, we intersect the removals for each series to determine if it should be removed

	type inputChunks struct {
		Missing  ChunkRefs // chunks that do not exist in the blooms and cannot be queried
		InBlooms ChunkRefs // chunks which do exist in the blooms and can be queried

		found map[int]bool // map of the index in `InBlooms` to whether the chunk
		// was found in _any_ of the blooms for the series. In order to
		// be eligible for removal, a chunk must be found in _no_ blooms.
	}

	inputs := make([]inputChunks, 0, len(reqs))
	for _, req := range reqs {
		missing, inBlooms := req.Chks.Compare(series.Chunks, true)
		inputs = append(inputs, inputChunks{
			Missing:  missing,
			InBlooms: inBlooms,

			found: make(map[int]bool, len(inBlooms)),
		})
	}

	for i, offset := range series.Offsets {
		skip := fq.bq.blooms.LoadOffset(offset)
		if skip {
			// could not seek to the desired bloom,
			// likely because the page was too large to load
			// NB(owen-d): since all blooms must be tested to guarantee result correctness,
			// we do not filter any chunks|series
			fq.recordSkippedFp(reqs, series.Fingerprint)
			return
		}

		if !fq.bq.blooms.Next() {
			// bloom not found, can't remove chunks
			level.Debug(fq.logger).Log(
				"msg", "bloom not found",
				"fp", series.Fingerprint,
				"err", fq.bq.blooms.Err(),
				"i", i,
			)
			fq.recordMissingFp(reqs, series.Fingerprint)
			return
		}

		// Test each bloom individually
		bloom := fq.bq.blooms.At()
		for j, req := range reqs {

			// shortcut: series level removal
			// we can skip testing chunk keys individually if the bloom doesn't match
			// the query.
			if !req.Search.Matches(bloom) {
				// Nothing else needs to be done for this (bloom, request);
				// check the next input request
				continue
			}

			// TODO(owen-d): copying this over, but they're going to be the same
			// across any block schema because prefix len is determined by n-gram and
			// all chunks have the same encoding length. tl;dr: it's weird/unnecessary to have
			// these defined this way and recreated across each bloom
			var (
				tokenBuf  []byte
				prefixLen int
			)
			for k, chk := range inputs[j].InBlooms {
				// if we've already found this chunk in a previous bloom, skip testing it
				if inputs[j].found[k] {
					continue
				}

				// Get buf to concatenate the chunk and search token
				tokenBuf, prefixLen = prefixedToken(schema.NGramLen(), chk, tokenBuf)
				if matched := req.Search.MatchesWithPrefixBuf(bloom, tokenBuf, prefixLen); matched {
					inputs[j].found[k] = true
				}
			}

		}

	}

	for i, req := range reqs {

		removals := removalsFor(inputs[i].InBlooms, inputs[i].found)

		req.Recorder.record(
			1, len(inputs[i].InBlooms), // found
			0, 0, // skipped
			0, len(inputs[i].Missing), // missed
			len(removals), // filtered
		)
		req.Response <- Output{
			Fp:       series.Fingerprint,
			Removals: removals,
		}
	}
}

func removalsFor(chunks ChunkRefs, found map[int]bool) ChunkRefs {
	// shortcut: all chunks removed
	if len(found) == 0 {
		return chunks
	}

	// shortcut: no chunks removed
	if len(found) == len(chunks) {
		return nil
	}

	removals := make(ChunkRefs, 0, len(chunks))
	for i, chk := range chunks {
		if !found[i] {
			removals = append(removals, chk)
		}
	}
	return removals
}
