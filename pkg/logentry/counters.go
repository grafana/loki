package logentry

import (
	"sync"
	"time"

	"github.com/grafana/loki/pkg/util"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/model"
)

type counters struct {
	mtx      sync.Mutex
	counters map[model.Fingerprint]prometheus.Counter
}

// newCounters Counts log entries by streams.
func newCounters() *counters {
	return &counters{
		counters: map[model.Fingerprint]prometheus.Counter{},
	}
}

func (c *counters) Gather() ([]*dto.MetricFamily, error) {
	c.mtx.Lock()
	defer c.mtx.Unlock()

	var result []*dto.MetricFamily
	help := "the total count of log entries"
	name := "log_entries_total"
	mtype := dto.MetricType_COUNTER
	counters := &dto.MetricFamily{
		Help: &help,
		Name: &name,
		Type: &mtype,
	}
	result = append(result, counters)
	for _, m := range c.counters {
		metric := &dto.Metric{}
		if err := m.Write(metric); err == nil {
			counters.Metric = append(counters.Metric, metric)
		}
	}

	return result, nil
}

func (c *counters) Handle(labels model.LabelSet, time time.Time, entry string) error {
	c.mtx.Lock()
	defer c.mtx.Unlock()

	fp := labels.Fingerprint()
	var ok bool
	var counter prometheus.Counter
	if counter, ok = c.counters[fp]; !ok {
		counter = prometheus.NewCounter(prometheus.CounterOpts{
			Help:        "the total count of log entries",
			Name:        "log_entries_total",
			ConstLabels: util.ModelLabelSetToMap(labels),
		})
		c.counters[fp] = counter
	}
	counter.Inc()
	return nil
}
