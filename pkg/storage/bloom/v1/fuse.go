package v1

import (
	"context"

	"github.com/efficientgo/core/errors"
	"github.com/prometheus/common/model"
)

type request struct {
	fp       model.Fingerprint
	chks     ChunkRefs
	searches [][]byte
	response chan<- output
}

type CancellableInputsIter struct {
	ctx context.Context
	Iterator[request]
}

func (cii *CancellableInputsIter) Next() bool {
	select {
	case <-cii.ctx.Done():
		return false
	default:
		return cii.Iterator.Next()
	}
}

// output represents a chunk that failed to pass all searches
// and must be downloaded
type output struct {
	fp   model.Fingerprint
	chks ChunkRefs
}

// Fuse combines multiple requests into a single loop iteration
// over the data set and returns the corresponding outputs
func (bq *BlockQuerier) Fuse(inputs []Iterator[request]) *FusedQuerier {
	return NewFusedQuerier(bq, inputs)
}

type FusedQuerier struct {
	bq     *BlockQuerier
	inputs Iterator[[]request]
}

func NewFusedQuerier(bq *BlockQuerier, inputs []Iterator[request]) *FusedQuerier {
	// heap := NewHeapIterator[request]()
	return &FusedQuerier{
		bq:     bq,
		inputs: nil,
	}
}

// returns a batch of inputs for the next fingerprint
func (fq *FusedQuerier) nextFP() ([]request, error) {
	return nil, nil
}

func (fq *FusedQuerier) Run() error {
	for {
		// find all queries for the next relevant fingerprint
		nextBatch, err := fq.nextFP()
		if err != nil {
			return errors.Wrap(err, "getting next fingerprint")
		}

		if len(nextBatch) == 0 {
			return nil
		}

		fp := nextBatch[0].fp

		// advance the series iterator to the next fingerprint
		if err := fq.bq.Seek(fp); err != nil {
			return errors.Wrap(err, "seeking to fingerprint")
		}

		if !fq.bq.series.Next() {
			// TODO(owen-d): fingerprint not found, can't remove chunks
		}

		series := fq.bq.series.At()
		if series.Fingerprint != fp {
			// TODO(owen-d): fingerprint not found, can't remove chunks
		}

		// Now that we've found the series, we need to find the unpack the bloom
		fq.bq.blooms.Seek(series.Offset)
		if !fq.bq.blooms.Next() {
			// TODO(owen-d): fingerprint not found, can't remove chunks
		}

		bloom := fq.bq.blooms.At()
		// test every input against this chunk
		for _, input := range nextBatch {
			mustCheck, inBlooms := input.chks.Compare(series.Chunks, true)

		outer:
			for _, chk := range inBlooms {
				for _, search := range input.searches {
					// TODO(owen-d): meld chunk + search into a single byte slice from the block schema
					var combined = search

					if !bloom.ScalableBloomFilter.Test(combined) {
						continue outer
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

}
