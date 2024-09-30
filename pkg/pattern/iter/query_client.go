package iter

import (
	"io"

	"github.com/grafana/loki/v3/pkg/logproto"
)

type queryClientIterator struct {
	client logproto.Pattern_QueryClient
	err    error
	curr   Iterator
}

// NewQueryClientIterator returns an iterator over a QueryClient.
func NewQueryClientIterator(client logproto.Pattern_QueryClient) Iterator {
	return &queryClientIterator{
		client: client,
	}
}

func (i *queryClientIterator) Next() bool {
	for i.curr == nil || !i.curr.Next() {
		batch, err := i.client.Recv()
		if err == io.EOF {
			return false
		} else if err != nil {
			i.err = err
			return false
		}
		i.curr = NewQueryResponseIterator(batch)
	}

	return true
}

func (i *queryClientIterator) Pattern() string {
	return i.curr.Pattern()
}

func (i *queryClientIterator) At() logproto.PatternSample {
	return i.curr.At()
}

func (i *queryClientIterator) Err() error {
	return i.err
}

func (i *queryClientIterator) Close() error {
	return i.client.CloseSend()
}

func NewQueryResponseIterator(resp *logproto.QueryPatternsResponse) Iterator {
	iters := make([]Iterator, len(resp.Series))
	for i, s := range resp.Series {
		// todo we should avoid this conversion
		samples := make([]logproto.PatternSample, len(s.Samples))
		for j, sample := range s.Samples {
			samples[j] = *sample
		}
		iters[i] = NewSlice(s.Pattern, samples)
	}
	return NewMerge(iters...)
}
