package logentry

import (
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"

	"github.com/grafana/loki/pkg/promtail/api"
	"github.com/grafana/loki/pkg/util"
)

type counters struct {
	name, help string
	mtx        sync.Mutex
	counters   map[model.Fingerprint]prometheus.Counter
}

func newCounters(name, help string) *counters {
	return &counters{
		counters: map[model.Fingerprint]prometheus.Counter{},
		help:     help,
		name:     name,
	}
}

func (c *counters) Describe(ch chan<- *prometheus.Desc) {}

func (c *counters) Collect(ch chan<- prometheus.Metric) {
	c.mtx.Lock()
	defer c.mtx.Unlock()
	for _, m := range c.counters {
		ch <- m
	}
}

func (c *counters) With(labels model.LabelSet) prometheus.Counter {
	c.mtx.Lock()
	defer c.mtx.Unlock()
	fp := labels.Fingerprint()
	var ok bool
	var counter prometheus.Counter
	if counter, ok = c.counters[fp]; !ok {
		counter = prometheus.NewCounter(prometheus.CounterOpts{
			Help:        c.help,
			Name:        c.name,
			ConstLabels: util.ModelLabelSetToMap(labels),
		})
		c.counters[fp] = counter
	}
	return counter
}

func logCount(reg prometheus.Registerer) api.EntryHandler {
	c := newCounters("log_entries_total", "the total count of log entries")
	reg.MustRegister(c)
	return api.EntryHandlerFunc(func(labels model.LabelSet, time time.Time, entry string) error {
		c.With(labels).Inc()
		return nil
	})
}

func logSize(reg prometheus.Registerer) api.EntryHandler {
	c := newCounters("log_entries_bytes", "the total count of bytes")
	reg.MustRegister(c)
	return api.EntryHandlerFunc(func(labels model.LabelSet, time time.Time, entry string) error {
		c.With(labels).Add(float64(len(entry)))
		return nil
	})
}
