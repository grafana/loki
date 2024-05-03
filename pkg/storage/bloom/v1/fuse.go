package v1

import (
	"context"

	"github.com/efficientgo/core/errors"
	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/loki/v3/pkg/util/spanlogger"
	"github.com/prometheus/common/model"
	"go.uber.org/atomic"
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

func (r *BloomRecorder) Log(logger log.Logger) {
	logger = spanlogger.FromContextWithFallback(r.ctx, logger)
	level.Debug(logger).Log(
		"recorder_msg", "bloom search results",
		"recorder_id", r.id,

		"recorder_series_requested", r.seriesFound.Load()+r.seriesSkipped.Load()+r.seriesMissed.Load(),
		"recorder_series_found", r.seriesFound.Load(),
		"recorder_series_skipped", r.seriesSkipped.Load(),
		"recorder_series_missed", r.seriesMissed.Load(),

		"recorder_chunks_requested", r.chunksFound.Load()+r.chunksSkipped.Load()+r.chunksMissed.Load(),
		"recorder_chunks_found", r.chunksFound.Load(),
		"recorder_chunks_skipped", r.chunksSkipped.Load(),
		"recorder_chunks_missed", r.chunksMissed.Load(),
		"recorder_chunks_filtered", r.chunksFiltered.Load(),
	)
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

		if !fq.bq.series.Next() {
			// no more series, we're done since we're iterating desired fingerprints in order
			return nil
		}

		series := fq.bq.series.At()
		if series.Fingerprint != fp {
			// fingerprint not found, can't remove chunks
			level.Debug(fq.logger).Log("msg", "fingerprint not found", "fp", series.Fingerprint, "err", fq.bq.series.Err())
			fq.recordMissingFp(nextBatch, fp)
			continue
		}

		// Now that we've found the series, we need to find the unpack the bloom
		skip := fq.bq.blooms.LoadOffset(series.Offset)
		if skip {
			// could not seek to the desired bloom,
			// likely because the page was too large to load
			fq.recordSkippedFp(nextBatch, fp)
			continue
		}

		if !fq.bq.blooms.Next() {
			// fingerprint not found, can't remove chunks
			level.Debug(fq.logger).Log("msg", "fingerprint not found", "fp", series.Fingerprint, "err", fq.bq.blooms.Err())
			fq.recordMissingFp(nextBatch, fp)
			continue
		}

		bloom := fq.bq.blooms.At()
		// test every input against this chunk
		for _, input := range nextBatch {
			missing, inBlooms := input.Chks.Compare(series.Chunks, true)

			var (
				// TODO(owen-d): pool
				removals ChunkRefs
				// TODO(salvacorts): pool tokenBuf
				tokenBuf  []byte
				prefixLen int
			)

			// First, see if the search passes the series level bloom before checking for chunks individually
			if matchedSeries := input.Search.Matches(bloom); !matchedSeries {
				removals = inBlooms
			} else {
				for _, chk := range inBlooms {
					// Get buf to concatenate the chunk and search token
					tokenBuf, prefixLen = prefixedToken(schema.NGramLen(), chk, tokenBuf)
					if !input.Search.MatchesWithPrefixBuf(bloom, tokenBuf, prefixLen) {
						removals = append(removals, chk)
					}
				}
			}

			input.Recorder.record(
				1, len(inBlooms), // found
				0, 0, // skipped
				0, len(missing), // missed
				len(removals), // filtered
			)
			input.Response <- Output{
				Fp:       fp,
				Removals: removals,
			}
		}

	}

	return nil
}
