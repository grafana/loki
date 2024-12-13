package client

import (
	"fmt"
	"sync"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/tsdb/chunks"
	"github.com/prometheus/prometheus/tsdb/record"

	"github.com/grafana/loki/v3/clients/pkg/promtail/api"

	"github.com/grafana/loki/v3/pkg/ingester/wal"
	"github.com/grafana/loki/v3/pkg/util"
)

// clientWriteTo implements a wal.WriteTo that re-builds entries with the stored series, and the received entries. After,
// sends each to the provided Client channel.
type clientWriteTo struct {
	series     map[chunks.HeadSeriesRef]model.LabelSet
	seriesLock sync.RWMutex

	// seriesSegment keeps track of the last segment in which the series pointed by each key in this map was seen. Keeping
	// this in a separate map avoids unnecessary contention.
	//
	// Even though it doesn't present a difference right now according to benchmarks, it will help when we introduce other
	// calls from the wal.Watcher to the wal.WriteTo like `UpdateSeriesSegment`.
	seriesSegment     map[chunks.HeadSeriesRef]int
	seriesSegmentLock sync.RWMutex

	logger   log.Logger
	toClient chan<- api.Entry
}

// newClientWriteTo creates a new clientWriteTo
func newClientWriteTo(toClient chan<- api.Entry, logger log.Logger) *clientWriteTo {
	return &clientWriteTo{
		series:        make(map[chunks.HeadSeriesRef]model.LabelSet),
		seriesSegment: make(map[chunks.HeadSeriesRef]int),
		toClient:      toClient,
		logger:        logger,
	}
}

func (c *clientWriteTo) StoreSeries(series []record.RefSeries, segment int) {
	c.seriesLock.Lock()
	defer c.seriesLock.Unlock()
	c.seriesSegmentLock.Lock()
	defer c.seriesSegmentLock.Unlock()
	for _, seriesRec := range series {
		c.seriesSegment[seriesRec.Ref] = segment
		labels := util.MapToModelLabelSet(seriesRec.Labels.Map())
		c.series[seriesRec.Ref] = labels
	}
}

// SeriesReset will delete all cache entries that were last seen in segments numbered equal or lower than segmentNum
func (c *clientWriteTo) SeriesReset(segmentNum int) {
	c.seriesLock.Lock()
	defer c.seriesLock.Unlock()
	c.seriesSegmentLock.Lock()
	defer c.seriesSegmentLock.Unlock()
	for k, v := range c.seriesSegment {
		if v <= segmentNum {
			level.Debug(c.logger).Log("msg", fmt.Sprintf("reclaiming series under segment %d", segmentNum))
			delete(c.seriesSegment, k)
			delete(c.series, k)
		}
	}
}

func (c *clientWriteTo) AppendEntries(entries wal.RefEntries) error {
	var entry api.Entry
	c.seriesLock.RLock()
	l, ok := c.series[entries.Ref]
	c.seriesLock.RUnlock()
	if ok {
		entry.Labels = l
		for _, e := range entries.Entries {
			entry.Entry = e
			c.toClient <- entry
		}
	} else {
		level.Debug(c.logger).Log("msg", "series for entry not found")
	}
	return nil
}
