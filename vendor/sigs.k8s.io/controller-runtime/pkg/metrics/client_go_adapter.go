/*
Copyright 2018 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package metrics

import (
	"net/url"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	reflectormetrics "k8s.io/client-go/tools/cache"
	clientmetrics "k8s.io/client-go/tools/metrics"
)

// this file contains setup logic to initialize the myriad of places
// that client-go registers metrics.  We copy the names and formats
// from Kubernetes so that we match the core controllers.

// Metrics subsystem and all of the keys used by the rest client.
const (
	RestClientSubsystem = "rest_client"
	LatencyKey          = "request_latency_seconds"
	ResultKey           = "requests_total"
)

// Metrics subsystem and all keys used by the reflectors.
const (
	ReflectorSubsystem     = "reflector"
	ListsTotalKey          = "lists_total"
	ListsDurationKey       = "list_duration_seconds"
	ItemsPerListKey        = "items_per_list"
	WatchesTotalKey        = "watches_total"
	ShortWatchesTotalKey   = "short_watches_total"
	WatchDurationKey       = "watch_duration_seconds"
	ItemsPerWatchKey       = "items_per_watch"
	LastResourceVersionKey = "last_resource_version"
)

var (
	// client metrics
	requestLatency = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Subsystem: RestClientSubsystem,
		Name:      LatencyKey,
		Help:      "Request latency in seconds. Broken down by verb and URL.",
		Buckets:   prometheus.ExponentialBuckets(0.001, 2, 10),
	}, []string{"verb", "url"})

	requestResult = prometheus.NewCounterVec(prometheus.CounterOpts{
		Subsystem: RestClientSubsystem,
		Name:      ResultKey,
		Help:      "Number of HTTP requests, partitioned by status code, method, and host.",
	}, []string{"code", "method", "host"})

	// reflector metrics

	// TODO(directxman12): update these to be histograms once the metrics overhaul KEP
	// PRs start landing.

	listsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Subsystem: ReflectorSubsystem,
		Name:      ListsTotalKey,
		Help:      "Total number of API lists done by the reflectors",
	}, []string{"name"})

	listsDuration = prometheus.NewSummaryVec(prometheus.SummaryOpts{
		Subsystem: ReflectorSubsystem,
		Name:      ListsDurationKey,
		Help:      "How long an API list takes to return and decode for the reflectors",
	}, []string{"name"})

	itemsPerList = prometheus.NewSummaryVec(prometheus.SummaryOpts{
		Subsystem: ReflectorSubsystem,
		Name:      ItemsPerListKey,
		Help:      "How many items an API list returns to the reflectors",
	}, []string{"name"})

	watchesTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Subsystem: ReflectorSubsystem,
		Name:      WatchesTotalKey,
		Help:      "Total number of API watches done by the reflectors",
	}, []string{"name"})

	shortWatchesTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Subsystem: ReflectorSubsystem,
		Name:      ShortWatchesTotalKey,
		Help:      "Total number of short API watches done by the reflectors",
	}, []string{"name"})

	watchDuration = prometheus.NewSummaryVec(prometheus.SummaryOpts{
		Subsystem: ReflectorSubsystem,
		Name:      WatchDurationKey,
		Help:      "How long an API watch takes to return and decode for the reflectors",
	}, []string{"name"})

	itemsPerWatch = prometheus.NewSummaryVec(prometheus.SummaryOpts{
		Subsystem: ReflectorSubsystem,
		Name:      ItemsPerWatchKey,
		Help:      "How many items an API watch returns to the reflectors",
	}, []string{"name"})

	lastResourceVersion = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Subsystem: ReflectorSubsystem,
		Name:      LastResourceVersionKey,
		Help:      "Last resource version seen for the reflectors",
	}, []string{"name"})
)

func init() {
	registerClientMetrics()
	registerReflectorMetrics()
}

// registerClientMetrics sets up the client latency metrics from client-go
func registerClientMetrics() {
	// register the metrics with our registry
	Registry.MustRegister(requestLatency)
	Registry.MustRegister(requestResult)

	// register the metrics with client-go
	clientmetrics.Register(clientmetrics.RegisterOpts{
		RequestLatency: &latencyAdapter{metric: requestLatency},
		RequestResult:  &resultAdapter{metric: requestResult},
	})
}

// registerReflectorMetrics sets up reflector (reconcile) loop metrics
func registerReflectorMetrics() {
	Registry.MustRegister(listsTotal)
	Registry.MustRegister(listsDuration)
	Registry.MustRegister(itemsPerList)
	Registry.MustRegister(watchesTotal)
	Registry.MustRegister(shortWatchesTotal)
	Registry.MustRegister(watchDuration)
	Registry.MustRegister(itemsPerWatch)
	Registry.MustRegister(lastResourceVersion)

	reflectormetrics.SetReflectorMetricsProvider(reflectorMetricsProvider{})
}

// this section contains adapters, implementations, and other sundry organic, artisanally
// hand-crafted syntax trees required to convince client-go that it actually wants to let
// someone use its metrics.

// Client metrics adapters (method #1 for client-go metrics),
// copied (more-or-less directly) from k8s.io/kubernetes setup code
// (which isn't anywhere in an easily-importable place).

type latencyAdapter struct {
	metric *prometheus.HistogramVec
}

func (l *latencyAdapter) Observe(verb string, u url.URL, latency time.Duration) {
	l.metric.WithLabelValues(verb, u.String()).Observe(latency.Seconds())
}

type resultAdapter struct {
	metric *prometheus.CounterVec
}

func (r *resultAdapter) Increment(code, method, host string) {
	r.metric.WithLabelValues(code, method, host).Inc()
}

// Reflector metrics provider (method #2 for client-go metrics),
// copied (more-or-less directly) from k8s.io/kubernetes setup code
// (which isn't anywhere in an easily-importable place).

type reflectorMetricsProvider struct{}

func (reflectorMetricsProvider) NewListsMetric(name string) reflectormetrics.CounterMetric {
	return listsTotal.WithLabelValues(name)
}

func (reflectorMetricsProvider) NewListDurationMetric(name string) reflectormetrics.SummaryMetric {
	return listsDuration.WithLabelValues(name)
}

func (reflectorMetricsProvider) NewItemsInListMetric(name string) reflectormetrics.SummaryMetric {
	return itemsPerList.WithLabelValues(name)
}

func (reflectorMetricsProvider) NewWatchesMetric(name string) reflectormetrics.CounterMetric {
	return watchesTotal.WithLabelValues(name)
}

func (reflectorMetricsProvider) NewShortWatchesMetric(name string) reflectormetrics.CounterMetric {
	return shortWatchesTotal.WithLabelValues(name)
}

func (reflectorMetricsProvider) NewWatchDurationMetric(name string) reflectormetrics.SummaryMetric {
	return watchDuration.WithLabelValues(name)
}

func (reflectorMetricsProvider) NewItemsInWatchMetric(name string) reflectormetrics.SummaryMetric {
	return itemsPerWatch.WithLabelValues(name)
}

func (reflectorMetricsProvider) NewLastResourceVersionMetric(name string) reflectormetrics.GaugeMetric {
	return lastResourceVersion.WithLabelValues(name)
}
