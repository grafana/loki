package iter

import (
	"fmt"
	"math"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"

	"github.com/grafana/loki/v3/pkg/iter"
	"github.com/grafana/loki/v3/pkg/logproto"
)

func ReadBatch(it Iterator, batchSize int) (*logproto.QueryPatternsResponse, error) {
	var (
		series   = map[string][]*logproto.PatternSample{}
		respSize int
	)

	for ; respSize < batchSize && it.Next(); respSize++ {
		pattern := it.Pattern()
		sample := it.At()
		series[pattern] = append(series[pattern], &sample)
	}
	result := logproto.QueryPatternsResponse{
		Series: make([]*logproto.PatternSeries, 0, len(series)),
	}
	for pattern, samples := range series {
		result.Series = append(result.Series, &logproto.PatternSeries{
			Pattern: pattern,
			Samples: samples,
		})
	}
	return &result, it.Err()
}

func ReadAll(it Iterator) (*logproto.QueryPatternsResponse, error) {
	return ReadBatch(it, math.MaxInt32)
}

func ReadMetrics(it iter.SampleIterator, logger log.Logger) (*logproto.QuerySamplesResponse, error) {
	series := map[uint64]logproto.Series{}

	var mint, maxt int64
	for it.Next() && it.Err() == nil {
		hash := it.StreamHash()
		s, ok := series[hash]
		if !ok {
			s = logproto.Series{
				Labels:     it.Labels(),
				Samples:    []logproto.Sample{},
				StreamHash: hash,
			}
			series[hash] = s
		}

		curSample := it.At()
		if mint == 0 || curSample.Timestamp < mint {
			mint = curSample.Timestamp
		}

		if maxt == 0 || curSample.Timestamp > maxt {
			maxt = curSample.Timestamp
		}

		s.Samples = append(s.Samples, curSample)
		series[hash] = s
	}

	result := logproto.QuerySamplesResponse{
		Series: make([]logproto.Series, 0, len(series)),
	}
	for _, s := range series {
		s := s
		level.Debug(logger).Log("msg", "appending series", "s", fmt.Sprintf("%v", s))
		result.Series = append(result.Series, s)
	}

	level.Debug(logger).Log("msg", "finished reading metrics",
		"num_series", len(result.Series),
		"mint", mint,
		"maxt", mint,
	)

	return &result, it.Err()
}

// ReadAllSamples reads all samples from the given iterator. It is only used in tests.
func ReadAllSamples(it iter.SampleIterator) (*logproto.QuerySamplesResponse, error) {
	return ReadMetrics(it, log.NewNopLogger())
}
