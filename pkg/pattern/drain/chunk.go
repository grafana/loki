package drain

import (
	"sort"
	"time"

	"github.com/grafana/loki/pkg/push"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/log"
	"github.com/grafana/loki/v3/pkg/pattern/iter"
)

const (
	TimeResolution = model.Time(int64(time.Second*10) / 1e6)

	defaultVolumeSize = 500

	maxChunkTime = 1 * time.Hour
)

type Chunks []Chunk

type Chunk struct {
	Samples            []logproto.PatternSample
	StructuredMetadata map[string]string
}

func newChunk(ts model.Time, structuredMetadata push.LabelsAdapter) Chunk {
	maxSize := int(maxChunkTime.Nanoseconds()/TimeResolution.UnixNano()) + 1
	v := Chunk{Samples: make([]logproto.PatternSample, 1, maxSize), StructuredMetadata: make(map[string]string, 32)}
	v.Samples[0] = logproto.PatternSample{
		Timestamp: ts,
		Value:     1,
	}
	for _, lbl := range structuredMetadata {
		v.StructuredMetadata[lbl.Name] = lbl.Value
	}
	return v
}

func (c Chunk) spaceFor(ts model.Time) bool {
	if len(c.Samples) == 0 {
		return true
	}

	return ts.Sub(c.Samples[0].Timestamp) < maxChunkTime
}

// ForRange returns samples with only the values
// in the given range [start:end) and aggregates them by step duration.
// start and end are in milliseconds since epoch. step is a duration in milliseconds.
func (c Chunk) ForRange(start, end, step model.Time) []logproto.PatternSample {
	if len(c.Samples) == 0 {
		return nil
	}
	first := c.Samples[0].Timestamp
	last := c.Samples[len(c.Samples)-1].Timestamp
	if start >= end || first >= end || last < start {
		return nil
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
			return c.Samples[i].Timestamp >= end
		})
	}
	if step == TimeResolution {
		return c.Samples[lo:hi]
	}

	// Re-scale samples into step-sized buckets
	currentStep := truncateTimestamp(c.Samples[lo].Timestamp, step)
	aggregatedSamples := make([]logproto.PatternSample, 0, ((c.Samples[hi-1].Timestamp-currentStep)/step)+1)
	aggregatedSamples = append(aggregatedSamples, logproto.PatternSample{
		Timestamp: currentStep,
		Value:     0,
	})
	for _, sample := range c.Samples[lo:hi] {
		if sample.Timestamp >= currentStep+step {
			stepForSample := truncateTimestamp(sample.Timestamp, step)
			for i := currentStep + step; i <= stepForSample; i += step {
				aggregatedSamples = append(aggregatedSamples, logproto.PatternSample{
					Timestamp: i,
					Value:     0,
				})
			}
			currentStep = stepForSample
		}
		aggregatedSamples[len(aggregatedSamples)-1].Value += sample.Value
	}

	return aggregatedSamples
}

func (c *Chunks) Add(ts model.Time, metadata push.LabelsAdapter) {
	t := truncateTimestamp(ts, TimeResolution)

	if len(*c) == 0 {
		*c = append(*c, newChunk(t, metadata))
		return
	}
	last := &(*c)[len(*c)-1]
	if last.Samples[len(last.Samples)-1].Timestamp == t {
		last.Samples[len(last.Samples)-1].Value++
		for _, md := range metadata {
			last.StructuredMetadata[md.Name] = md.Value
		}
		return
	}
	if !last.spaceFor(t) {
		*c = append(*c, newChunk(t, metadata))
		return
	}
	last.Samples = append(last.Samples, logproto.PatternSample{
		Timestamp: t,
		Value:     1,
	})
	for _, md := range metadata {
		last.StructuredMetadata[md.Name] = md.Value
	}
}

func (c Chunks) Iterator(pattern string, from, through, step model.Time, labelFilters log.StreamPipeline) iter.Iterator {
	iters := make([]iter.Iterator, 0, len(c))
	var emptyLine []byte
	for _, chunk := range c {
		chunkMetadata := make([]labels.Label, 0, len(chunk.StructuredMetadata))
		for k, v := range chunk.StructuredMetadata {
			chunkMetadata = append(chunkMetadata, labels.Label{
				Name:  k,
				Value: v,
			})
		}
		_, _, matches := labelFilters.Process(0, emptyLine, chunkMetadata...)
		if !matches {
			continue
		}

		samples := chunk.ForRange(from, through, step)
		if len(samples) == 0 {
			continue
		}
		iters = append(iters, iter.NewSlice(pattern, samples))
	}
	return iter.NewNonOverlappingIterator(pattern, iters)
}

func (c Chunks) samples() []*logproto.PatternSample {
	// TODO: []*logproto.PatternSample -> []logproto.PatternSample
	//   Or consider AoS to SoA conversion.
	totalSample := 0
	for i := range c {
		totalSample += len(c[i].Samples)
	}
	s := make([]*logproto.PatternSample, 0, totalSample)
	for _, chunk := range c {
		for i := range chunk.Samples {
			s = append(s, &chunk.Samples[i])
		}
	}
	return s
}

func (c *Chunks) merge(samples []*logproto.PatternSample) []logproto.PatternSample {
	toMerge := c.samples()
	// TODO: Avoid allocating a new slice, if possible.
	result := make([]logproto.PatternSample, 0, len(toMerge)+len(samples))
	var i, j int
	for i < len(toMerge) && j < len(samples) {
		if toMerge[i].Timestamp < samples[j].Timestamp {
			result = append(result, *toMerge[i])
			i++
		} else if toMerge[i].Timestamp > samples[j].Timestamp {
			result = append(result, *samples[j])
			j++
		} else {
			result = append(result, logproto.PatternSample{
				Value:     toMerge[i].Value + samples[j].Value,
				Timestamp: toMerge[i].Timestamp,
			})
			i++
			j++
		}
	}
	for ; i < len(toMerge); i++ {
		result = append(result, *toMerge[i])
	}
	for ; j < len(samples); j++ {
		result = append(result, *samples[j])
	}
	*c = Chunks{Chunk{Samples: result}}
	return result
}

func (c *Chunks) prune(olderThan time.Duration) {
	if len(*c) == 0 {
		return
	}
	// go for every chunks, check the last timestamp is after duration from now and remove the chunk
	for i := 0; i < len(*c); i++ {
		if time.Since((*c)[i].Samples[len((*c)[i].Samples)-1].Timestamp.Time()) > olderThan {
			*c = append((*c)[:i], (*c)[i+1:]...)
			i--
		}
	}
}

func (c *Chunks) size() int {
	size := 0
	for _, chunk := range *c {
		for _, sample := range chunk.Samples {
			size += int(sample.Value)
		}
	}
	return size
}

func truncateTimestamp(ts, step model.Time) model.Time { return ts - ts%step }
