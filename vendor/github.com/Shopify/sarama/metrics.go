package sarama

import (
	"fmt"
	"strings"
	"sync"

	"github.com/rcrowley/go-metrics"
)

// Use exponentially decaying reservoir for sampling histograms with the same defaults as the Java library:
// 1028 elements, which offers a 99.9% confidence level with a 5% margin of error assuming a normal distribution,
// and an alpha factor of 0.015, which heavily biases the reservoir to the past 5 minutes of measurements.
// See https://github.com/dropwizard/metrics/blob/v3.1.0/metrics-core/src/main/java/com/codahale/metrics/ExponentiallyDecayingReservoir.java#L38
const (
	metricsReservoirSize = 1028
	metricsAlphaFactor   = 0.015
)

func getOrRegisterHistogram(name string, r metrics.Registry) metrics.Histogram {
	return r.GetOrRegister(name, func() metrics.Histogram {
		return metrics.NewHistogram(metrics.NewExpDecaySample(metricsReservoirSize, metricsAlphaFactor))
	}).(metrics.Histogram)
}

func getMetricNameForBroker(name string, broker *Broker) string {
	// Use broker id like the Java client as it does not contain '.' or ':' characters that
	// can be interpreted as special character by monitoring tool (e.g. Graphite)
	return fmt.Sprintf(name+"-for-broker-%d", broker.ID())
}

func getMetricNameForTopic(name string, topic string) string {
	// Convert dot to _ since reporters like Graphite typically use dot to represent hierarchy
	// cf. KAFKA-1902 and KAFKA-2337
	return fmt.Sprintf(name+"-for-topic-%s", strings.Replace(topic, ".", "_", -1))
}

func getOrRegisterTopicMeter(name string, topic string, r metrics.Registry) metrics.Meter {
	return metrics.GetOrRegisterMeter(getMetricNameForTopic(name, topic), r)
}

func getOrRegisterTopicHistogram(name string, topic string, r metrics.Registry) metrics.Histogram {
	return getOrRegisterHistogram(getMetricNameForTopic(name, topic), r)
}

// cleanupRegistry is an implementation of metrics.Registry that allows
// to unregister from the parent registry only those metrics
// that have been registered in cleanupRegistry
type cleanupRegistry struct {
	parent  metrics.Registry
	metrics map[string]struct{}
	mutex   sync.RWMutex
}

func newCleanupRegistry(parent metrics.Registry) metrics.Registry {
	return &cleanupRegistry{
		parent:  parent,
		metrics: map[string]struct{}{},
	}
}

func (r *cleanupRegistry) Each(fn func(string, interface{})) {
	r.mutex.RLock()
	defer r.mutex.RUnlock()
	wrappedFn := func(name string, iface interface{}) {
		if _, ok := r.metrics[name]; ok {
			fn(name, iface)
		}
	}
	r.parent.Each(wrappedFn)
}

func (r *cleanupRegistry) Get(name string) interface{} {
	r.mutex.RLock()
	defer r.mutex.RUnlock()
	if _, ok := r.metrics[name]; ok {
		return r.parent.Get(name)
	}
	return nil
}

func (r *cleanupRegistry) GetOrRegister(name string, metric interface{}) interface{} {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	r.metrics[name] = struct{}{}
	return r.parent.GetOrRegister(name, metric)
}

func (r *cleanupRegistry) Register(name string, metric interface{}) error {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	r.metrics[name] = struct{}{}
	return r.parent.Register(name, metric)
}

func (r *cleanupRegistry) RunHealthchecks() {
	r.parent.RunHealthchecks()
}

func (r *cleanupRegistry) GetAll() map[string]map[string]interface{} {
	return r.parent.GetAll()
}

func (r *cleanupRegistry) Unregister(name string) {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	if _, ok := r.metrics[name]; ok {
		delete(r.metrics, name)
		r.parent.Unregister(name)
	}
}

func (r *cleanupRegistry) UnregisterAll() {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	for name := range r.metrics {
		delete(r.metrics, name)
		r.parent.Unregister(name)
	}
}
