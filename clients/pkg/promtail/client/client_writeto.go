package client

import (
	"fmt"
	"sync"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/tsdb/record"

	"github.com/grafana/loki/clients/pkg/promtail/api"
	"github.com/grafana/loki/pkg/ingester/wal"
	"github.com/grafana/loki/pkg/util"
)

type seriesCacheEntry struct {
	labels            model.LabelSet
	lastSeenInSegment int
}

type seriesCache = map[uint64]*seriesCacheEntry

// clientWriteTo implements a wal.WriteTo that re-builds entries with the stored series, and the received entries. After,
// sends each to the provided Client channel.
type clientWriteTo struct {
	series    seriesCache
	cacheLock sync.RWMutex
	logger    log.Logger
	toClient  chan<- api.Entry
}

// newClientWriteTo creates a new clientWriteTo
func newClientWriteTo(toClient chan<- api.Entry, logger log.Logger) *clientWriteTo {
	return &clientWriteTo{
		series:   make(seriesCache),
		toClient: toClient,
		logger:   logger,
	}
}

func (c *clientWriteTo) StoreSeries(series []record.RefSeries, segment int) {
	c.cacheLock.Lock()
	defer c.cacheLock.Unlock()
	for _, seriesRec := range series {
		entry, ok := c.series[uint64(seriesRec.Ref)]
		if ok && entry.lastSeenInSegment < segment {
			// entry is present, touch if required
			entry.lastSeenInSegment = segment
			continue
		}
		// entry is not present
		c.series[uint64(seriesRec.Ref)] = &seriesCacheEntry{
			labels:            util.MapToModelLabelSet(seriesRec.Labels.Map()),
			lastSeenInSegment: segment,
		}
	}
}

// SeriesReset will delete all cache entries that were last seen in segments numbered equal or lower than segmentNum
func (c *clientWriteTo) SeriesReset(segmentNum int) {
	c.cacheLock.Lock()
	defer c.cacheLock.Unlock()
	for ref, entry := range c.series {
		// Since segmentNum means all segment with number i <= segmentNum are safe to be reclaimed, evict all cache
		// entries that were last seen in those
		if entry.lastSeenInSegment <= segmentNum {
			level.Debug(c.logger).Log("msg", fmt.Sprintf("reclaiming series under segment %d", segmentNum))
			delete(c.series, ref)
		}
	}
}

func (c *clientWriteTo) AppendEntries(entries wal.RefEntries) error {
	var entry api.Entry
	c.cacheLock.RLock()
	l, ok := c.series[uint64(entries.Ref)]
	c.cacheLock.RUnlock()
	if ok {
		entry.Labels = l.labels
		for _, e := range entries.Entries {
			entry.Entry = e
			c.toClient <- entry
		}
	} else {
		level.Debug(c.logger).Log("msg", "series for entry not found")
	}
	return nil
}
