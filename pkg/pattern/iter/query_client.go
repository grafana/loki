package iter

import (
	"fmt"
	"io"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"

	"github.com/grafana/loki/v3/pkg/iter"
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

func (i *queryClientIterator) Error() error {
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

type querySamplesClientIterator struct {
	client logproto.Pattern_QuerySampleClient
	logger log.Logger
	err    error
	curr   iter.SampleIterator
}

// NewQueryClientIterator returns an iterator over a QueryClient.
func NewQuerySamplesClientIterator(client logproto.Pattern_QuerySampleClient, logger log.Logger) iter.SampleIterator {
	return &querySamplesClientIterator{
		client: client,
		logger: logger,
	}
}

func (i *querySamplesClientIterator) Next() bool {
	for i.curr == nil || !i.curr.Next() {
		batch, err := i.client.Recv()
		level.Debug(i.logger).Log("msg", "received batch", "batch", fmt.Sprintf("%v", batch))
		if err == io.EOF {
			return false
		} else if err != nil {
			i.err = err
			return false
		}
		i.curr = NewQuerySamplesResponseIterator(batch)
	}

	return true
}

func (i *querySamplesClientIterator) Sample() logproto.Sample {
	return i.curr.Sample()
}

func (i *querySamplesClientIterator) StreamHash() uint64 {
	return i.curr.StreamHash()
}

func (i *querySamplesClientIterator) Labels() string {
	return i.curr.Labels()
}

func (i *querySamplesClientIterator) Error() error {
	return i.err
}

func (i *querySamplesClientIterator) Close() error {
	return i.client.CloseSend()
}

func NewQuerySamplesResponseIterator(resp *logproto.QuerySamplesResponse) iter.SampleIterator {
	return iter.NewMultiSeriesIterator(resp.Series)
}
