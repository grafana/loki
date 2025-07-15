package metricsutil

import (
	"fmt"
	"sync"

	"github.com/prometheus/client_golang/prometheus"
)

// InfoOpts is an alias for prometheus.Opts. See there for doc comments.
type InfoOpts prometheus.Opts

// InfoCollector is a Collector that represents a constant value with a set of
// changing labels.
type InfoCollector struct {
	desc *prometheus.Desc

	valuesMut     sync.RWMutex
	names, values []string
}

var (
	_ prometheus.Collector = (*InfoCollector)(nil)
)

// NewInfoCollector creates a new info metric with dynamic values. Call Set to
// change the specific value for a label. All labels are always reported, and
// are initialized to empty strings by default.
func NewInfoCollector(opts InfoOpts, labelNames ...string) *InfoCollector {
	var (
		names  = make([]string, len(labelNames))
		values = make([]string, len(labelNames))
	)

	copy(names, labelNames)

	desc := prometheus.NewDesc(
		prometheus.BuildFQName(opts.Namespace, opts.Subsystem, opts.Name),
		opts.Help,
		names,
		nil,
	)

	return &InfoCollector{
		desc: desc,

		names:  names,
		values: values,
	}
}

// Set updates the value for a given label.
func (ic *InfoCollector) Set(labelName, labelValue string) error {
	for i, name := range ic.names {
		if name != labelName {
			continue
		}

		ic.valuesMut.Lock()
		ic.values[i] = labelValue
		ic.valuesMut.Unlock()
		return nil
	}

	return fmt.Errorf("label name %q does not exist", labelName)
}

// MustSet calls Set and panics if it returns an error.
func (ic *InfoCollector) MustSet(labelName, labelValue string) {
	if err := ic.Set(labelName, labelValue); err != nil {
		panic(err)
	}
}

// Describe implements prometheus.Collector.
func (ic *InfoCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- ic.desc
}

// Collect implements prometheus.Collector.
func (ic *InfoCollector) Collect(ch chan<- prometheus.Metric) {
	ic.valuesMut.RLock()
	defer ic.valuesMut.RUnlock()
	ch <- prometheus.MustNewConstMetric(ic.desc, prometheus.GaugeValue, 1, ic.values...)
}
