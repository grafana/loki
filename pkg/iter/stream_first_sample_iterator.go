package iter

import "context"

// NewStreamFirstMergeSampleIterator returns a sample iterator that merges and deduplicates samples
// from multiple iterators in stream-first order.
func NewStreamFirstMergeSampleIterator(ctx context.Context, is []SampleIterator) SampleIterator {
	return newMergeSampleIterator(ctx, is, true)
}

// sampleIteratorWithStreamHash overrides the wrapped iterator's StreamHash with a fixed value. It
// lets a per-stream iterator expose the raw stream identity (fingerprint) the stream-first merge
// orders and deduplicates by, independent of the reduced hash the underlying extractor reports.
type sampleIteratorWithStreamHash struct {
	SampleIterator
	hash uint64
}

// NewSampleIteratorWithStreamHash wraps it so StreamHash() returns hash.
func NewSampleIteratorWithStreamHash(it SampleIterator, hash uint64) SampleIterator {
	return &sampleIteratorWithStreamHash{SampleIterator: it, hash: hash}
}

func (i *sampleIteratorWithStreamHash) StreamHash() uint64 {
	return i.hash
}
