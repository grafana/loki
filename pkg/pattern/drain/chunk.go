package drain

import (
	"sort"
	"time"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/pattern/iter"
	"github.com/prometheus/common/model"
)

const (
	timeResolution = model.Time(int64(time.Second*10) / 1e6)

	defaultVolumeSize = 500

	maxChunkTime = time.Duration(1 * time.Hour)
)

type Chunks []Chunk

type Chunk struct {
	Samples []logproto.PatternSample
}

func newChunk(ts model.Time) Chunk {
	maxSize := int(maxChunkTime.Nanoseconds()/timeResolution.UnixNano()) + 1
	v := Chunk{Samples: make([]logproto.PatternSample, 1, maxSize)}
	v.Samples[0] = logproto.PatternSample{
		Timestamp: ts,
		Value:     1,
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
// in the given range [start:end).
// start and end are in milliseconds since epoch.
func (c Chunk) ForRange(start, end model.Time) []logproto.PatternSample {
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
	return c.Samples[lo:hi]
}

func (c *Chunks) Add(ts model.Time) {
	t := truncateTimestamp(ts)

	if len(*c) == 0 {
		*c = append(*c, newChunk(t))
		return
	}
	last := &(*c)[len(*c)-1]
	if last.Samples[len(last.Samples)-1].Timestamp == t {
		last.Samples[len(last.Samples)-1].Value++
		return
	}
	if !last.spaceFor(t) {
		*c = append(*c, newChunk(t))
		return
	}
	last.Samples = append(last.Samples, logproto.PatternSample{
		Timestamp: t,
		Value:     1,
	})
}

func (c Chunks) Iterator(pattern string, from, through model.Time) iter.Iterator {
	iters := make([]iter.Iterator, 0, len(c))
	for _, chunk := range c {
		samples := chunk.ForRange(from, through)
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

func truncateTimestamp(ts model.Time) model.Time { return ts - ts%timeResolution }
