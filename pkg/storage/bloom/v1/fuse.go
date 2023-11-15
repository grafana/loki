package v1

import (
	"sort"

	"github.com/efficientgo/core/errors"
	"github.com/prometheus/common/model"
)

type request struct {
	fp       model.Fingerprint
	chks     ChunkRefs
	searches [][]byte
	response chan output
}

// output represents a chunk that failed to pass all searches
// and must be downloaded
type output struct {
	fp   model.Fingerprint
	chks ChunkRefs
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
					fp:   fp,
					chks: input.chks,
				}
			}
		}

		// Now that we've found the series, we need to find the unpack the bloom
		fq.bq.blooms.Seek(series.Offset)
		if !fq.bq.blooms.Next() {
			// fingerprint not found, can't remove chunks
			for _, input := range nextBatch {
				input.response <- output{
					fp:   fp,
					chks: input.chks,
				}
			}
			continue
		}

		bloom := fq.bq.blooms.At()
		// test every input against this chunk
	inputLoop:
		for _, input := range nextBatch {
			mustCheck, inBlooms := input.chks.Compare(series.Chunks, true)

			// First, see if the search passes the series level bloom before checking for chunks individually
			for _, search := range input.searches {
				if !bloom.Test(search) {
					// the entire series bloom didn't pass one of the searches,
					// so we can skip checking chunks individually.
					// We still return all chunks that are not included in the bloom
					// as they may still have the data
					input.response <- output{
						fp:   fp,
						chks: mustCheck,
					}
					continue inputLoop
				}
			}

		chunkLoop:
			for _, chk := range inBlooms {
				for _, search := range input.searches {
					// TODO(owen-d): meld chunk + search into a single byte slice from the block schema
					var combined = search

					if !bloom.ScalableBloomFilter.Test(combined) {
						continue chunkLoop
					}
				}
				// chunk passed all searches, add to the list of chunks to download
				mustCheck = append(mustCheck, chk)

			}

			input.response <- output{
				fp:   fp,
				chks: mustCheck,
			}
		}

	}

	return nil
}

// boundedRequests is a set of requests that are clamped to a specific range
type boundedRequests struct {
	bounds FingerprintBounds
	reqs   [][]model.Fingerprint
}

// reqs models a set of requests covering many fingerprints.
// consumers models a set of blocks covering different fingerprint ranges
func partitionFingerprintRange(reqs [][]model.Fingerprint, blocks []FingerprintBounds) (res []boundedRequests) {
	for _, block := range blocks {
		bounded := boundedRequests{
			bounds: block,
		}

		for _, req := range reqs {
			min := sort.Search(len(req), func(i int) bool {
				return block.Cmp(req[i]) > Before
			})

			max := sort.Search(len(req), func(i int) bool {
				return block.Cmp(req[i]) == After
			})

			// All fingerprints fall outside of the consumer's range
			if min == len(req) || max == 0 {
				continue
			}

			bounded.reqs = append(bounded.reqs, req[min:max])
		}

		if len(bounded.reqs) > 0 {
			res = append(res, bounded)
		}

	}

	return res
}
