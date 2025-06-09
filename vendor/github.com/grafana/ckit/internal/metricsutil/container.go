package metricsutil

import "github.com/prometheus/client_golang/prometheus"

// Container wraps around a set of prometheus.Collectors and exposes them as a
// single prometheus.Collector.
type Container struct {
	cs []prometheus.Collector
}

var _ prometheus.Collector = (*Container)(nil)

// Add registers a new Collector into the Container.
func (c *Container) Add(cs ...prometheus.Collector) {
	c.cs = append(c.cs, cs...)
}

// Describe implements prometheus.Collector.
func (c *Container) Describe(ch chan<- *prometheus.Desc) {
	for _, cl := range c.cs {
		cl.Describe(ch)
	}
}

// Collect implements prometheus.Collector.
func (c *Container) Collect(ch chan<- prometheus.Metric) {
	for _, cl := range c.cs {
		cl.Collect(ch)
	}
}
