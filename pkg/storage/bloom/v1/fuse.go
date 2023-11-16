package v1

import (
	"github.com/efficientgo/core/errors"
	"github.com/prometheus/common/model"
)

type request struct {
	fp       model.Fingerprint
	chks     ChunkRefs
	searches [][]byte
	response chan output
}

// output represents a chunk that was present in the bloom
// but failed to pass the search filters and can be removed from
// the list of chunks to download
type output struct {
	fp       model.Fingerprint
	removals ChunkRefs
}

// Fuse combines multiple requests into a single loop iteration
// over the data set and returns the corresponding outputs
// TODO(owen-d): better async control
func (bq *BlockQuerier) Fuse(inputs []PeekingIterator[request]) *FusedQuerier {
	return NewFusedQuerier(bq, inputs)
}

type FusedQuerier struct {
	bq     *BlockQuerier
	inputs Iterator[[]request]
}

func NewFusedQuerier(bq *BlockQuerier, inputs []PeekingIterator[request]) *FusedQuerier {
	heap := NewHeapIterator[request](
		func(a, b request) bool {
			return a.fp < b.fp
		},
		inputs...,
	)

	merging := NewDedupingIter[request, []request](
		func(a request, b []request) bool {
			return a.fp == b[0].fp
		},
		func(a request) []request { return []request{a} },
		func(a request, b []request) []request {
			return append(b, a)
		},
		NewPeekingIter[request](heap),
	)
	return &FusedQuerier{
		bq:     bq,
		inputs: merging,
	}
}

func (fq *FusedQuerier) Run() error {
	for fq.inputs.Next() {
		// find all queries for the next relevant fingerprint
		nextBatch := fq.inputs.At()

		fp := nextBatch[0].fp

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
			for _, input := range nextBatch {
				input.response <- output{
					fp:       fp,
					removals: nil,
				}
			}
		}

		// Now that we've found the series, we need to find the unpack the bloom
		fq.bq.blooms.Seek(series.Offset)
		if !fq.bq.blooms.Next() {
			// fingerprint not found, can't remove chunks
			for _, input := range nextBatch {
				input.response <- output{
					fp:       fp,
					removals: nil,
				}
			}
			continue
		}

		bloom := fq.bq.blooms.At()
		// test every input against this chunk
	inputLoop:
		for _, input := range nextBatch {
			_, inBlooms := input.chks.Compare(series.Chunks, true)

			// First, see if the search passes the series level bloom before checking for chunks individually
			for _, search := range input.searches {
				if !bloom.Test(search) {
					// the entire series bloom didn't pass one of the searches,
					// so we can skip checking chunks individually.
					// We return all the chunks that were the intersection of the query
					// because they for sure do not match the search and don't
					// need to be downloaded
					input.response <- output{
						fp:       fp,
						removals: inBlooms,
					}
					continue inputLoop
				}
			}

			// TODO(owen-d): pool
			var removals ChunkRefs

		chunkLoop:
			for _, chk := range inBlooms {
				for _, search := range input.searches {
					// TODO(owen-d): meld chunk + search into a single byte slice from the block schema
					var combined = search

					if !bloom.ScalableBloomFilter.Test(combined) {
						removals = append(removals, chk)
						continue chunkLoop
					}
				}
				// Otherwise, the chunk passed all the searches
			}

			input.response <- output{
				fp:       fp,
				removals: removals,
			}
		}

	}

	return nil
}
