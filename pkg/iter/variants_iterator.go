package iter

import (
	"context"
	"io"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logqlmodel/metadata"
	"github.com/grafana/loki/v3/pkg/logqlmodel/stats"
)

type variantsQueryClientIterator struct {
	client QueryVariantsClient
	err    error
	curr   SampleIterator
}

type QueryVariantsClient interface {
	Recv() (*logproto.VariantsQueryResponse, error)
	Context() context.Context
	CloseSend() error
}

// NewSampleQueryClientIterator returns an iterator over a QueryClient.
func NewVariantsQueryClientIterator(client QueryVariantsClient) SampleIterator {
	return &variantsQueryClientIterator{
		client: client,
	}
}

func (i *variantsQueryClientIterator) Next() bool {
	ctx := i.client.Context()
	for i.curr == nil || !i.curr.Next() {
		batch, err := i.client.Recv()
		if err == io.EOF {
			return false
		} else if err != nil {
			i.err = err
			return false
		}
		stats.JoinIngesters(ctx, batch.Stats)
		_ = metadata.AddWarnings(ctx, batch.Warnings...)

		i.curr = NewVariantsQueryResponseIterator(batch)
	}
	return true
}

func (i *variantsQueryClientIterator) At() logproto.Sample {
	return i.curr.At()
}

func (i *variantsQueryClientIterator) Labels() string {
	return i.curr.Labels()
}

func (i *variantsQueryClientIterator) StreamHash() uint64 {
	return i.curr.StreamHash()
}

func (i *variantsQueryClientIterator) Err() error {
	return i.err
}

func (i *variantsQueryClientIterator) Close() error {
	return i.client.CloseSend()
}

func NewVariantsQueryResponseIterator(resp *logproto.VariantsQueryResponse) SampleIterator {
	return NewMultiSeriesIterator(resp.Series)
}

func ReadVariantsBatch(i SampleIterator, size uint32) (*logproto.VariantsQueryResponse, uint32, error) {
	var (
		series      = map[uint64]map[string]*logproto.Series{}
		respSize    uint32
		seriesCount int
	)
	for ; respSize < size && i.Next(); respSize++ {
		labels, hash, sample := i.Labels(), i.StreamHash(), i.At()
		streams, ok := series[hash]
		if !ok {
			streams = map[string]*logproto.Series{}
			series[hash] = streams
		}
		s, ok := streams[labels]
		if !ok {
			seriesCount++
			s = &logproto.Series{
				Labels:     labels,
				StreamHash: hash,
			}
			streams[labels] = s
		}
		s.Samples = append(s.Samples, sample)
	}

	result := logproto.VariantsQueryResponse{
		Series: make([]logproto.Series, 0, seriesCount),
	}
	for _, streams := range series {
		for _, s := range streams {
			result.Series = append(result.Series, *s)
		}
	}
	return &result, respSize, i.Err()
}
