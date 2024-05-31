package metric

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"time"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/pattern/chunk"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/iter"
)

type MetricType int

const (
	Bytes MetricType = iota
	Count
	Unsupported
)

type Chunks struct {
	chunks []Chunk
	labels labels.Labels
}

func NewChunks(labels labels.Labels) *Chunks {
	return &Chunks{
		chunks: make([]Chunk, 0),
		labels: labels,
	}
}

func (c *Chunks) Observe(bytes, count float64, ts model.Time) {
	if len(c.chunks) == 0 {
		c.chunks = append(c.chunks, newChunk(bytes, count, ts))
		return
	}

	last := &(c.chunks)[len(c.chunks)-1]
	if !last.spaceFor(ts) {
		c.chunks = append(c.chunks, newChunk(bytes, count, ts))
		return
	}

	last.AddSample(newSample(bytes, count, ts))
}

func (c *Chunks) Iterator(
	ctx context.Context,
	typ MetricType,
	grouping *syntax.Grouping,
	from, through, step model.Time,
) (iter.SampleIterator, error) {
	if typ == Unsupported {
		return nil, fmt.Errorf("unsupported metric type")
	}

	lbls := c.labels
	if grouping != nil {
		sort.Strings(grouping.Groups)
		lbls = make(labels.Labels, 0, len(grouping.Groups))
		for _, group := range grouping.Groups {
			value := c.labels.Get(group)
			lbls = append(lbls, labels.Label{Name: group, Value: value})
		}
	}

	// could have up to through-from/step steps for each chunk
	maximumSteps := int64(((through-from)/step)+1) * int64(len(c.chunks))
	samples := make([]logproto.Sample, 0, maximumSteps)
	for _, chunk := range c.chunks {
		ss, err := chunk.ForTypeAndRange(typ, from, through)
		if err != nil {
			return nil, err
		}

		if len(ss) == 0 {
			continue
		}

		samples = append(samples, ss...)
	}

	slices.SortFunc(samples, func(i, j logproto.Sample) int {
		if i.Timestamp < j.Timestamp {
			return -1
		}

		if i.Timestamp > j.Timestamp {
			return 1
		}
		return 0
	})

	series := logproto.Series{Labels: lbls.String(), Samples: samples, StreamHash: lbls.Hash()}
	return iter.NewSeriesIterator(series), nil
}

type MetricSample struct {
	Timestamp model.Time
	Bytes     float64
	Count     float64
}

func newSample(bytes, count float64, ts model.Time) MetricSample {
	return MetricSample{
		Timestamp: ts,
		Bytes:     bytes,
		Count:     count,
	}
}

type MetricSamples []MetricSample

type Chunk struct {
	Samples    MetricSamples
	mint, maxt int64
}

func (c *Chunk) Bounds() (fromT, toT time.Time) {
	return time.Unix(0, c.mint), time.Unix(0, c.maxt)
}

func (c *Chunk) AddSample(s MetricSample) {
	c.Samples = append(c.Samples, s)
	ts := int64(s.Timestamp)

	if ts < c.mint {
		c.mint = ts
	}

	if ts > c.maxt {
		c.maxt = ts
	}
}

func newChunk(bytes, count float64, ts model.Time) Chunk {
	maxSize := int(chunk.MaxChunkTime.Nanoseconds()/chunk.TimeResolution.UnixNano()) + 1
	v := Chunk{Samples: make(MetricSamples, 1, maxSize)}
	v.Samples[0] = newSample(bytes, count, ts)
	return v
}

func (c *Chunk) spaceFor(ts model.Time) bool {
	if len(c.Samples) == 0 {
		return true
	}

	return ts.Sub(c.Samples[0].Timestamp) < chunk.MaxChunkTime
}

// ForTypeAndRange returns samples with only the values
// in the given range [start:end), with no aggregation as that will be done in
// the step evaluator. start and end are in milliseconds since epoch.
// step is a duration in milliseconds.
func (c *Chunk) ForTypeAndRange(
	typ MetricType,
	start, end model.Time,
) ([]logproto.Sample, error) {
	if typ == Unsupported {
		return nil, fmt.Errorf("unsupported metric type")
	}

	if len(c.Samples) == 0 {
		return nil, nil
	}

	first := c.Samples[0].Timestamp
	last := c.Samples[len(c.Samples)-1].Timestamp
	startBeforeEnd := start >= end
	samplesAreAfterRange := first >= end
	samplesAreBeforeRange := last < start
	if startBeforeEnd || samplesAreAfterRange || samplesAreBeforeRange {
		return nil, nil
	}

	lo := 0
	hi := len(c.Samples)

	for i, sample := range c.Samples {
		if first >= start {
			break
		}

		if first < start && sample.Timestamp >= start {
			lo = i
			first = sample.Timestamp
		}
	}

	for i := hi - 1; i >= 0; i-- {
		if last < end {
			break
		}

		sample := c.Samples[i]
		if last >= end && sample.Timestamp < end {
			hi = i + 1
			last = sample.Timestamp
		}
	}

	aggregatedSamples := make([]logproto.Sample, len(c.Samples[lo:hi]))
	for i, sample := range c.Samples[lo:hi] {
		if sample.Timestamp >= start && sample.Timestamp < end {
			var v float64
			if typ == Bytes {
				v = sample.Bytes
			} else {
				v = sample.Count
			}
			aggregatedSamples[i] = logproto.Sample{
				Timestamp: sample.Timestamp.UnixNano(),
				Value:     v,
			}
		}
	}

	return aggregatedSamples, nil
}
