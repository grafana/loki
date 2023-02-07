package client

import (
	"fmt"
	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/tsdb/record"

	"github.com/grafana/loki/clients/pkg/promtail/api"
	"github.com/grafana/loki/pkg/ingester/wal"
	"github.com/grafana/loki/pkg/util"
)

type seriesCache = map[uint64]model.LabelSet

// clientWriteTo implements a wal.WriteTo that re-builds entries with the stored series, and the received entries. After,
// sends each to the provided Client channel.
type clientWriteTo struct {
	series   seriesCache
	logger   log.Logger
	toClient chan<- api.Entry
}

// newClientWriteTo creates a new clientWriteTo
func newClientWriteTo(toClient chan<- api.Entry, logger log.Logger) *clientWriteTo {
	return &clientWriteTo{
		series:   make(seriesCache),
		toClient: toClient,
		logger:   logger,
	}
}

func (c *clientWriteTo) StoreSeries(series []record.RefSeries, _ int) {
	for _, seriesRec := range series {
		level.Debug(c.logger).Log("msg", "storing series", "ref", fmt.Sprint(seriesRec.Ref))
		c.series[uint64(seriesRec.Ref)] = util.MapToModelLabelSet(seriesRec.Labels.Map())
	}
}

func (c *clientWriteTo) AppendEntries(entries wal.RefEntries) error {
	var entry api.Entry
	level.Debug(c.logger).Log("msg", "sending entries", "count", len(entries.Entries))
	if l, ok := c.series[uint64(entries.Ref)]; ok {
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
