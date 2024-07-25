package metric

import (
	"fmt"
	"slices"
	"sort"
	"sync"
	"time"

	"github.com/dustin/go-humanize"
	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/detection"
	"github.com/grafana/loki/v3/pkg/distributor"
	"github.com/grafana/loki/v3/pkg/iter"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/pattern/chunk"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
)

type Type int

const (
	Bytes Type = iota
	Count
	Unsupported
)

type metrics struct {
	chunks  prometheus.Gauge
	samples prometheus.Counter
}

type Chunks struct {
	chunks     []*Chunk
	labels     labels.Labels
	lock       sync.RWMutex
	logger     log.Logger
	metrics    metrics
	rawSamples SamplesWithoutTS
	service    string
	level      string
}

func NewChunks(labels labels.Labels, chunkMetrics *ChunkMetrics, logger log.Logger) *Chunks {
	service := labels.Get(detection.LabelServiceName)
	if service == "" {
		service = distributor.ServiceUnknown
	}

	lvl := labels.Get("level")
	if lvl == "" {
		lvl = detection.LogLevelUnknown
	}

	level.Debug(logger).Log(
		"msg", "creating new chunks",
		"labels", labels.String(),
		"service", service,
		"level", lvl,
	)

	return &Chunks{
		chunks:     []*Chunk{},
		labels:     labels,
		logger:     logger,
		rawSamples: SamplesWithoutTS{},
		service:    service,
		level:      lvl,

		metrics: metrics{
			chunks:  chunkMetrics.chunks.WithLabelValues(service),
			samples: chunkMetrics.samples.WithLabelValues(service),
		},
	}
}

func (c *Chunks) Observe(bytes, count uint64) {
	c.lock.Lock()
	defer c.lock.Unlock()

	c.rawSamples = append(c.rawSamples, newSampleWithoutTS(bytes, count))
	c.metrics.samples.Inc()
}

func (c *Chunks) Prune(olderThan time.Duration) bool {
	c.lock.Lock()
	defer c.lock.Unlock()

	if len(c.chunks) == 0 {
		return true
	}

	oldest := time.Now().Add(-olderThan).UnixNano()
	// keep the last chunk
	for i := 0; i < len(c.chunks)-1; {
		if c.chunks[i].maxt < oldest {
			c.chunks = append(c.chunks[:i], c.chunks[i+1:]...)
			c.metrics.chunks.Set(float64(len(c.chunks)))
			continue
		}
		i++
	}

	return len(c.chunks) == 0
}

func (c *Chunks) Iterator(
	typ Type,
	grouping *syntax.Grouping,
	from, through, step model.Time,
) (iter.SampleIterator, error) {
	if typ == Unsupported {
		return nil, fmt.Errorf("unsupported metric type")
	}

	c.lock.RLock()
	defer c.lock.RUnlock()

	lbls := c.labels
	if grouping != nil {
		sort.Strings(grouping.Groups)
		lbls = make(labels.Labels, 0, len(grouping.Groups))
		for _, group := range grouping.Groups {
			value := c.labels.Get(group)
			lbls = append(lbls, labels.Label{Name: group, Value: value})
		}
	}

	maximumSteps := int64(((through-from)/step)+1) * int64(len(c.chunks))
	// prevent a panic if maximumSteps is negative
	if maximumSteps < 0 {
		level.Warn(c.logger).Log(
			"msg", "returning an empty series because of a negative maximumSteps",
			"labels", lbls.String(),
			"from", from,
			"through", through,
			"step", step,
			"maximumSteps", maximumSteps,
			"num_chunks", len(c.chunks),
		)
		series := logproto.Series{
			Labels:     lbls.String(),
			Samples:    []logproto.Sample{},
			StreamHash: lbls.Hash(),
		}
		return iter.NewSeriesIterator(series), nil
	}

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

	numSamples := 0
	for _, chunk := range c.chunks {
		numSamples += len(chunk.Samples)
	}

	level.Debug(c.logger).Log(
		"msg", "found matching samples",
		"samples", fmt.Sprintf("%v", samples),
		"found_samples", len(samples),
		"labels", lbls.String(),
		"from", from,
		"through", through,
		"step", step,
		"num_chunks", len(c.chunks),
		"num_samples", numSamples,
	)

	series := logproto.Series{Labels: lbls.String(), Samples: samples, StreamHash: lbls.Hash()}
	return iter.NewSeriesIterator(series), nil
}

type Sample struct {
	Timestamp model.Time
	Bytes     uint64
	Count     uint64
}

type SampleWithoutTS struct {
	Bytes uint64
	Count uint64
}

func newSample(bytes, count uint64, ts model.Time) Sample {
	return Sample{
		Timestamp: ts,
		Bytes:     bytes,
		Count:     count,
	}
}

func newSampleWithoutTS(bytes, count uint64) SampleWithoutTS {
	return SampleWithoutTS{
		Bytes: bytes,
		Count: count,
	}
}

type (
	Samples          []Sample
	SamplesWithoutTS []SampleWithoutTS
)

type Chunk struct {
	Samples    Samples
	mint, maxt int64
}

func (c *Chunk) Bounds() (fromT, toT time.Time) {
	return time.Unix(0, c.mint), time.Unix(0, c.maxt)
}

func (c *Chunk) AddSample(s Sample) {
	c.Samples = append(c.Samples, s)
	ts := int64(s.Timestamp)

	if ts < c.mint {
		c.mint = ts
	}

	if ts > c.maxt {
		c.maxt = ts
	}
}

func newChunk(bytes, count uint64, ts model.Time) *Chunk {
	// TODO(twhitney): maybe bring this back when we introduce downsampling
	// maxSize := int(chunk.MaxChunkTime.Nanoseconds()/chunk.TimeResolution.UnixNano()) + 1
	v := &Chunk{Samples: []Sample{}}
	v.Samples = append(v.Samples, newSample(bytes, count, ts))
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
	typ Type,
	start, end model.Time,
) ([]logproto.Sample, error) {
	if typ == Unsupported {
		return nil, fmt.Errorf("unsupported metric type")
	}

	aggregatedSamples := make([]logproto.Sample, 0, len(c.Samples))
	if len(c.Samples) == 0 {
		return aggregatedSamples, nil
	}

	level.Debug(util_log.Logger).Log("msg", "finding chunk samples for type and range",
		"start", start,
		"end", end,
		"samples", fmt.Sprintf("%v", c.Samples))

	for _, sample := range c.Samples {
		if sample.Timestamp >= start && sample.Timestamp < end {
			var v uint64
			if typ == Bytes {
				v = sample.Bytes
			} else {
				v = sample.Count
			}
			aggregatedSamples = append(aggregatedSamples, logproto.Sample{
				Timestamp: sample.Timestamp.UnixNano(),
				Value:     float64(v),
			})
		}
	}

	return aggregatedSamples, nil
}

func (c *Chunks) Downsample(now model.Time, w EntryWriter) {
	c.lock.Lock()
	defer func() {
		c.lock.Unlock()
		c.rawSamples = c.rawSamples[:0]
	}()

	var totalBytes, totalCount uint64
	for _, sample := range c.rawSamples {
		totalBytes += sample.Bytes
		totalCount += sample.Count
	}

	c.metrics.samples.Inc()
	c.writeAggregatedMetrics(w, now, totalBytes, totalCount)

	if len(c.chunks) == 0 {
		c.chunks = append(c.chunks, newChunk(totalBytes, totalCount, now))
		c.metrics.chunks.Set(float64(len(c.chunks)))
		return
	}

	last := c.chunks[len(c.chunks)-1]
	if !last.spaceFor(now) {
		c.chunks = append(c.chunks, newChunk(totalBytes, totalCount, now))
		c.metrics.chunks.Set(float64(len(c.chunks)))
		return
	}
	last.AddSample(newSample(totalBytes, totalCount, now))
}

func (c *Chunks) writeAggregatedMetrics(
	w EntryWriter,
	now model.Time,
	totalBytes, totalCount uint64,
) {
	lbls := labels.Labels{
		labels.Label{Name: detection.AggregatedMetricLabel, Value: c.service},
		labels.Label{Name: "level", Value: c.level},
	}

	if w != nil {
		w.WriteEntry(
			now.Time(),
			AggregatedMetricEntry(now, totalBytes, totalCount, c.service, c.labels),
			lbls,
		)
	}
}

func AggregatedMetricEntry(ts model.Time, totalBytes, totalCount uint64, service string, lbls labels.Labels) string {
	byteString := humanize.Bytes(totalBytes)
	base := fmt.Sprintf(
		"ts=%d bytes=%s count=%d %s=%s",
		ts.UnixNano(),
		byteString,
		totalCount,
		detection.LabelServiceName, service,
	)

	for _, l := range lbls {
		base += fmt.Sprintf(" %s=%s", l.Name, l.Value)
	}

	return base
}
