package main

import (
	"maps"
	"math"
	"slices"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

var (
	postingsSpreadBuckets    = []float64{1, 2, 4, 8, 16, 32, 64, 128, 256, 512, 1024}
	logsSectionSpreadBuckets = []float64{1, 2, 4, 8, 16, 32, 64, 128, 256, 512, 1024}
	logsSpreadBuckets        = []float64{1, 1.25, 1.5, 2, 3, 5, 10, 20, 50, 100, 200, 500, 1000, 2000, 5000}
)

type classTotals struct {
	objects int64
	bytes   int64
}

type postingsSnapshot struct {
	window   string
	tenant   string
	class    string
	sections int64
	spread   histogramSnapshot
}

type logsSnapshot struct {
	window        string
	tenant        string
	spread        histogramSnapshot
	sectionSpread histogramSnapshot
	logsSections  int64
	idealSections int64
}

// histogramSnapshot retains the aggregate state Prometheus needs to expose a
// histogram without retaining the individual observations.
type histogramSnapshot struct {
	count   uint64
	sum     float64
	buckets map[float64]uint64
}

// localitySnapshot retains completed windows independently so a scan can
// publish each window as it completes.
type localitySnapshot struct {
	windows map[string]windowScanSnapshot

	lastRun        float64
	runDuration    float64
	windowsScanned int
}

type localityCollector struct {
	mu       sync.RWMutex
	snapshot localitySnapshot

	postingsSpreadDesc *prometheus.Desc
	logsSpreadDesc     *prometheus.Desc
	logsSectionDesc    *prometheus.Desc
	objectsDesc        *prometheus.Desc
	bytesDesc          *prometheus.Desc
	sectionsDesc       *prometheus.Desc
	readAmpDesc        *prometheus.Desc
	lastRunDesc        *prometheus.Desc
	runDurationDesc    *prometheus.Desc
	windowsDesc        *prometheus.Desc
}

func newLocalityCollector() *localityCollector {
	return &localityCollector{
		postingsSpreadDesc: prometheus.NewDesc("dataobj_locality_postings_section_spread", "Distribution of postings sections containing each label name-value.", []string{"window", "tenant", "class"}, nil),
		logsSpreadDesc:     prometheus.NewDesc("dataobj_locality_logs_spread_factor", "Distribution of logs-section spread factors by sort-key value.", []string{"window", "tenant"}, nil),
		logsSectionDesc:    prometheus.NewDesc("dataobj_locality_logs_section_spread", "Distribution of actual logs sections containing each sort-key value.", []string{"window", "tenant"}, nil),
		objectsDesc:        prometheus.NewDesc("dataobj_locality_index_objects", "Number of index objects.", []string{"window", "class"}, nil),
		bytesDesc:          prometheus.NewDesc("dataobj_locality_index_object_bytes", "Compressed bytes of index objects.", []string{"window", "class"}, nil),
		sectionsDesc:       prometheus.NewDesc("dataobj_locality_postings_sections", "Number of postings sections.", []string{"window", "tenant", "class"}, nil),
		readAmpDesc:        prometheus.NewDesc("dataobj_locality_logs_read_amplification", "Ratio of actual to ideal logs sections.", []string{"window", "tenant"}, nil),
		lastRunDesc:        prometheus.NewDesc("dataobj_locality_last_run_timestamp_seconds", "Unix timestamp of the last completed scan.", nil, nil),
		runDurationDesc:    prometheus.NewDesc("dataobj_locality_run_duration_seconds", "Duration of the last completed scan.", nil, nil),
		windowsDesc:        prometheus.NewDesc("dataobj_locality_windows_scanned", "Number of windows scanned in the last run.", nil, nil),
	}
}

func (c *localityCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.postingsSpreadDesc
	ch <- c.logsSpreadDesc
	ch <- c.logsSectionDesc
	ch <- c.objectsDesc
	ch <- c.bytesDesc
	ch <- c.sectionsDesc
	ch <- c.readAmpDesc
	ch <- c.lastRunDesc
	ch <- c.runDurationDesc
	ch <- c.windowsDesc
}

func (c *localityCollector) setSnapshot(snapshot localitySnapshot) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.snapshot = snapshot
}

// setWindow replaces every metric series for a completed window. Replacing,
// rather than merging, removes series for tenants or classes that disappeared
// since the previous scan.
func (c *localityCollector) setWindow(window string, snapshot windowScanSnapshot) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.snapshot.windows == nil {
		c.snapshot.windows = make(map[string]windowScanSnapshot)
	}
	c.snapshot.windows[window] = snapshot
}

// completeScan removes expired windows only after every expected window has
// completed, so a failed scan cannot make previously reported metrics vanish.
func (c *localityCollector) completeScan(validWindows map[string]struct{}, duration time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for window := range c.snapshot.windows {
		if _, ok := validWindows[window]; !ok {
			delete(c.snapshot.windows, window)
		}
	}
	c.snapshot.lastRun = float64(time.Now().Unix())
	c.snapshot.runDuration = duration.Seconds()
	c.snapshot.windowsScanned = len(validWindows)
}

func (c *localityCollector) Collect(ch chan<- prometheus.Metric) {
	c.mu.RLock()
	snapshot := c.snapshot
	snapshot.windows = maps.Clone(snapshot.windows)
	c.mu.RUnlock()

	for _, window := range snapshot.windows {
		for _, p := range window.postings {
			ch <- prometheus.MustNewConstHistogram(c.postingsSpreadDesc, p.spread.count, p.spread.sum, p.spread.buckets, p.window, p.tenant, p.class)
			ch <- prometheus.MustNewConstMetric(c.sectionsDesc, prometheus.GaugeValue, float64(p.sections), p.window, p.tenant, p.class)
		}
		for _, logs := range window.logs {
			ch <- prometheus.MustNewConstHistogram(c.logsSpreadDesc, logs.spread.count, logs.spread.sum, logs.spread.buckets, logs.window, logs.tenant)
			ch <- prometheus.MustNewConstHistogram(c.logsSectionDesc, logs.sectionSpread.count, logs.sectionSpread.sum, logs.sectionSpread.buckets, logs.window, logs.tenant)
			var readAmp float64
			if logs.idealSections > 0 {
				readAmp = float64(logs.logsSections) / float64(logs.idealSections)
			}
			ch <- prometheus.MustNewConstMetric(c.readAmpDesc, prometheus.GaugeValue, readAmp, logs.window, logs.tenant)
		}
		for class, totals := range window.objects {
			ch <- prometheus.MustNewConstMetric(c.objectsDesc, prometheus.GaugeValue, float64(totals.objects), window.window, class)
			ch <- prometheus.MustNewConstMetric(c.bytesDesc, prometheus.GaugeValue, float64(totals.bytes), window.window, class)
		}
	}
	ch <- prometheus.MustNewConstMetric(c.lastRunDesc, prometheus.GaugeValue, snapshot.lastRun)
	ch <- prometheus.MustNewConstMetric(c.runDurationDesc, prometheus.GaugeValue, snapshot.runDuration)
	ch <- prometheus.MustNewConstMetric(c.windowsDesc, prometheus.GaugeValue, float64(snapshot.windowsScanned))
}

func newHistogramSnapshot(values, bounds []float64) histogramSnapshot {
	result := histogramSnapshot{buckets: make(map[float64]uint64, len(bounds))}
	for _, value := range values {
		result.count++
		result.sum += value
		for _, bound := range bounds {
			if value <= bound {
				result.buckets[bound]++
			}
		}
	}
	return result
}

func mergeHistogramSnapshots(left, right histogramSnapshot) histogramSnapshot {
	if left.buckets == nil {
		left.buckets = make(map[float64]uint64, len(right.buckets))
	}
	left.count += right.count
	left.sum += right.sum
	for bound, count := range right.buckets {
		left.buckets[bound] += count
	}
	return left
}

// histogramP95UpperBound returns the upper bound of the first bucket that
// contains at least 95% of the observations.
func histogramP95UpperBound(snapshot histogramSnapshot) float64 {
	if snapshot.count == 0 {
		return 0
	}
	target := uint64(math.Ceil(float64(snapshot.count) * 0.95))
	bounds := make([]float64, 0, len(snapshot.buckets))
	for bound := range snapshot.buckets {
		bounds = append(bounds, bound)
	}
	slices.Sort(bounds)
	for _, bound := range bounds {
		if snapshot.buckets[bound] >= target {
			return bound
		}
	}
	return math.Inf(1)
}

func logsSnapshotFromCollector(window, tenant string, c *collector) logsSnapshot {
	result := logsSnapshot{window: window, tenant: tenant}
	spreadFactors := make([]float64, 0, len(c.logsBySortValue))
	sectionSpreads := make([]float64, 0, len(c.logsBySortValue))
	for _, agg := range c.logsBySortValue {
		var bytes int64
		for _, size := range agg.sections {
			bytes += size
		}
		ideal := int64(math.Ceil(float64(bytes) / float64(c.opts.logsSectionTargetBytes)))
		if ideal < 1 {
			ideal = 1
		}
		result.logsSections += int64(len(agg.sections))
		result.idealSections += ideal
		spreadFactors = append(spreadFactors, float64(len(agg.sections))/float64(ideal))
		sectionSpreads = append(sectionSpreads, float64(len(agg.sections)))
	}
	result.spread = newHistogramSnapshot(spreadFactors, logsSpreadBuckets)
	result.sectionSpread = newHistogramSnapshot(sectionSpreads, logsSectionSpreadBuckets)
	return result
}
