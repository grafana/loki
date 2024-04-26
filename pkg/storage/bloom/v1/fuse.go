package v1

import (
	"github.com/efficientgo/core/errors"
	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/prometheus/common/model"
)

type Request struct {
	Fp       model.Fingerprint
	Chks     ChunkRefs
	Search   BloomTest
	Response chan<- Output
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

func (fq *FusedQuerier) noRemovals(batch []Request, fp model.Fingerprint) {
	for _, input := range batch {
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
			fq.noRemovals(nextBatch, fp)
			continue
		}

		// Now that we've found the series, we need to find the unpack the bloom
		ok := fq.bq.blooms.LoadOffset(series.Offset)
		if !ok {
			// could not seek to the desired bloom,
			// likely because the page was too large to load
			fq.noRemovals(nextBatch, fp)
		}

		if !fq.bq.blooms.Next() {
			// fingerprint not found, can't remove chunks
			level.Debug(fq.logger).Log("msg", "fingerprint not found", "fp", series.Fingerprint, "err", fq.bq.blooms.Err())
			fq.noRemovals(nextBatch, fp)
			continue
		}

		bloom := fq.bq.blooms.At()
		// test every input against this chunk
		for _, input := range nextBatch {
			_, inBlooms := input.Chks.Compare(series.Chunks, true)

			// First, see if the search passes the series level bloom before checking for chunks individually
			if !input.Search.Matches(bloom) {
				// We return all the chunks that were the intersection of the query
				// because they for sure do not match the search and don't
				// need to be downloaded
				input.Response <- Output{
					Fp:       fp,
					Removals: inBlooms,
				}
				continue
			}

			// TODO(owen-d): pool
			var removals ChunkRefs

			// TODO(salvacorts): pool tokenBuf
			var tokenBuf []byte
			var prefixLen int

			for _, chk := range inBlooms {
				// Get buf to concatenate the chunk and search token
				tokenBuf, prefixLen = prefixedToken(schema.NGramLen(), chk, tokenBuf)
				if !input.Search.MatchesWithPrefixBuf(bloom, tokenBuf, prefixLen) {
					removals = append(removals, chk)
					continue
				}
				// Otherwise, the chunk passed all the searches
			}

			input.Response <- Output{
				Fp:       fp,
				Removals: removals,
			}
		}

	}

	return nil
}
