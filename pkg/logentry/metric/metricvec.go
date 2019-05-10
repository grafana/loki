package metric

import (
	"sync"

	"github.com/grafana/loki/pkg/util"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
)

type metricVec struct {
	factory func(labels map[string]string) prometheus.Metric
	mtx     sync.Mutex
	metrics map[model.Fingerprint]prometheus.Metric
}

func newMetricVec(factory func(labels map[string]string) prometheus.Metric) *metricVec {
	return &metricVec{
		metrics: map[model.Fingerprint]prometheus.Metric{},
		factory: factory,
	}
}

func (c *metricVec) Describe(ch chan<- *prometheus.Desc) {}

func (c *metricVec) Collect(ch chan<- prometheus.Metric) {
	c.mtx.Lock()
	defer c.mtx.Unlock()
	for _, m := range c.metrics {
		ch <- m
	}
}

func (c *metricVec) With(labels model.LabelSet) prometheus.Metric {
	c.mtx.Lock()
	defer c.mtx.Unlock()
	fp := labels.Fingerprint()
	var ok bool
	var metric prometheus.Metric
	if metric, ok = c.metrics[fp]; !ok {
		metric = c.factory(util.ModelLabelSetToMap(labels))
		c.metrics[fp] = metric
	}
	return metric
}
