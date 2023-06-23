package metric

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/grafana/loki/pkg/util"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
)

type UnregisterCollector interface {
	Unregister()
}

type CollectorMetric interface {
	prometheus.Collector
	prometheus.Metric
}

// Expirable allows checking if something has exceeded the provided maxAge based on the provided currentTime
type Expirable interface {
	HasExpired(currentTimeSec int64, maxAgeSec int64) bool
}

var gcInterval = time.Minute

type metricVec struct {
	factory    func(labels map[string]string) CollectorMetric
	mtx        sync.Mutex
	metrics    map[model.Fingerprint]CollectorMetric
	maxAgeSec  int64
	registry   prometheus.Registerer
	lastGcTime time.Time
}

func newMetricVec(factory func(labels map[string]string) CollectorMetric, maxAgeSec int64, registry prometheus.Registerer) *metricVec {
	return &metricVec{
		metrics:    map[model.Fingerprint]CollectorMetric{},
		factory:    factory,
		maxAgeSec:  maxAgeSec,
		registry:   registry,
		lastGcTime: time.Now(),
	}
}

func (c *metricVec) Unregister() {
	if c.registry == nil {
		return
	}

// Describe implements prometheus.Collector and doesn't declare any metrics on purpose to bypass prometheus validation.
// see https://godoc.org/github.com/prometheus/client_golang/prometheus#hdr-Custom_Collectors_and_constant_Metrics search for "unchecked"
func (c *metricVec) Describe(_ chan<- *prometheus.Desc) {}

// Collect implements prometheus.Collector
func (c *metricVec) Collect(ch chan<- prometheus.Metric) {
	c.mtx.Lock()
	defer c.mtx.Unlock()
	for _, m := range c.metrics {
		c.registry.Unregister(m)
	}
}

// With returns the metric associated with the labelset.
func (c *metricVec) With(labels model.LabelSet) (prometheus.Metric, error) {
	c.mtx.Lock()
	defer c.mtx.Unlock()
	fp := labels.Fingerprint()
	var ok bool
	var metric CollectorMetric
	if metric, ok = c.metrics[fp]; !ok {
		metric = c.factory(util.ModelLabelSetToMap(cleanLabels(labels)))
		if c.registry != nil {
			err := c.registry.Register(metric)
			if err != nil {
				return nil, err
			}
		}
		c.metrics[fp] = metric
	}
	now := time.Now()
	if now.Sub(c.lastGcTime) > gcInterval {
		c.prune()
		c.lastGcTime = now
	}
	return metric, nil
}

// cleanLabels removes labels whose label name is not a valid prometheus one, or has the reserved `__` prefix.
func cleanLabels(set model.LabelSet) model.LabelSet {
	out := make(model.LabelSet, len(set))
	for k, v := range set {
		// Performing the same label validity check the prometheus go client library does.
		// https://github.com/prometheus/client_golang/blob/618194de6ad3db637313666104533639011b470d/prometheus/labels.go#L85
		if !k.IsValid() || strings.HasPrefix(string(k), "__") {
			continue
		}
		out[k] = v
	}
	return out
}

func (c *metricVec) Delete(labels model.LabelSet) bool {
	c.mtx.Lock()
	defer c.mtx.Unlock()
	fp := labels.Fingerprint()
	_, ok := c.metrics[fp]
	if ok {
		delete(c.metrics, fp)
	}
	return ok
}

// prune will remove all metrics which implement the Expirable interface and have expired
// it does not take out a lock on the metrics map so whoever calls this function should do so.
func (c *metricVec) prune() {
	currentTimeSec := time.Now().Unix()
	for fp, m := range c.metrics {
		if em, ok := m.(Expirable); ok {
			if em.HasExpired(currentTimeSec, c.maxAgeSec) {
				fmt.Println(em, "delete")
				if c.registry != nil {
					c.registry.Unregister(m)
				}
				delete(c.metrics, fp)
			}
		}
	}
}
