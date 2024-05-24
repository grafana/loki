package metric

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/pattern/chunk"
	"github.com/grafana/loki/v3/pkg/pattern/iter"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
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

func (c *Chunks) Observe(bytes, count uint64, ts model.Time) {
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
) (iter.Iterator, error) {
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

	iters := make([]iter.Iterator, 0, len(c.chunks))
	for _, chunk := range c.chunks {
		samples, err := chunk.ForRangeAndType(typ, from, through, step)
		if err != nil {
			return nil, err
		}

		if len(samples) == 0 {
			continue
		}

		iters = append(iters, iter.NewLabelsSlice(lbls, samples))
	}

	return iter.NewNonOverlappingLabelsIterator(lbls, iters), nil
}

// TODO(twhitney): These values should be float64s (to match prometheus samples) or int64s (to match pattern samples)
type MetricSample struct {
	Timestamp model.Time
	Bytes     uint64
	Count     uint64
}

func newSample(bytes, count uint64, ts model.Time) MetricSample {
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

func newChunk(bytes, count uint64, ts model.Time) Chunk {
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

// TODO(twhitney): any way to remove the duplication between this and the drain chunk ForRange method?
// ForRangeAndType returns samples with only the values
// in the given range [start:end] and aggregates them by step duration.
// start and end are in milliseconds since epoch. step is a duration in milliseconds.
func (c *Chunk) ForRangeAndType(
	typ MetricType,
	start, end, step model.Time,
) ([]logproto.PatternSample, error) {
	if typ == Unsupported {
		return nil, fmt.Errorf("unsupported metric type")
	}

	if len(c.Samples) == 0 {
		return nil, nil
	}

	first := c.Samples[0].Timestamp // why is this in the future?
	last := c.Samples[len(c.Samples)-1].Timestamp
	startBeforeEnd := start >= end
	samplesAreAfterRange := first > end
	samplesAreBeforeRange := last < start
	if startBeforeEnd || samplesAreAfterRange || samplesAreBeforeRange {
		return nil, nil
	}

	var lo int
	if start > first {
		lo = sort.Search(len(c.Samples), func(i int) bool {
			return c.Samples[i].Timestamp >= start
		})
	}
	hi := len(c.Samples)
	if end < last {
		hi = sort.Search(len(c.Samples), func(i int) bool {
			return c.Samples[i].Timestamp > end
		})
	}

	// Re-scale samples into step-sized buckets
	currentStep := chunk.TruncateTimestamp(c.Samples[lo].Timestamp, step)
	numOfSteps := ((c.Samples[hi-1].Timestamp - currentStep) / step) + 1
	aggregatedSamples := make([]logproto.PatternSample, 0, numOfSteps)
	aggregatedSamples = append(aggregatedSamples,
		logproto.PatternSample{
			Timestamp: currentStep,
			Value:     0,
		})

	for _, sample := range c.Samples[lo:hi] {
		if sample.Timestamp >= currentStep+step {
			stepForSample := chunk.TruncateTimestamp(sample.Timestamp, step)
			for i := currentStep + step; i <= stepForSample; i += step {
				aggregatedSamples = append(aggregatedSamples, logproto.PatternSample{
					Timestamp: i,
					Value:     0,
				})
			}
			currentStep = stepForSample
		}

		var v int64
		if typ == Bytes {
			v = int64(sample.Bytes)
		} else {
			v = int64(sample.Count)
		}

		aggregatedSamples[len(aggregatedSamples)-1].Value += v
	}

	return aggregatedSamples, nil
}
