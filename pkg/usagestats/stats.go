package usagestats

import (
	"bytes"
	"context"
	"encoding/json"
	"expvar"
	"fmt"
	"io"
	"math"
	"net/http"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/grafana/loki/pkg/util/build"

	"github.com/cespare/xxhash/v2"
	jsoniter "github.com/json-iterator/go"
	prom "github.com/prometheus/prometheus/web/api/v1"
	"go.uber.org/atomic"
)

var (
	httpClient    = http.Client{Timeout: 5 * time.Second}
	usageStatsURL = "https://stats.grafana.org/loki-usage-report"
	statsPrefix   = "github.com/grafana/loki/"
	targetKey     = "target"
	editionKey    = "edition"
)

// Report is the JSON object sent to the stats server
type Report struct {
	ClusterID              string    `json:"clusterID"`
	CreatedAt              time.Time `json:"createdAt"`
	Interval               time.Time `json:"interval"`
	IntervalPeriod         float64   `json:"intervalPeriod"`
	Target                 string    `json:"target"`
	prom.PrometheusVersion `json:"version"`
	Os                     string                 `json:"os"`
	Arch                   string                 `json:"arch"`
	Edition                string                 `json:"edition"`
	Metrics                map[string]interface{} `json:"metrics"`
}

// sendReport sends the report to the stats server
func sendReport(ctx context.Context, seed *ClusterSeed, interval time.Time, URL string) error {
	report := buildReport(seed, interval)
	out, err := jsoniter.MarshalIndent(report, "", " ")
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, URL, bytes.NewBuffer(out))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := httpClient.Do(req.WithContext(ctx))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		data, err := io.ReadAll(resp.Body)
		if err != nil {
			return err
		}
		return fmt.Errorf("failed to send usage stats: %s  body: %s", resp.Status, string(data))
	}
	return nil
}

// buildReport builds the report to be sent to the stats server
func buildReport(seed *ClusterSeed, interval time.Time) Report {
	var (
		targetName  string
		editionName string
	)
	if target := expvar.Get(statsPrefix + targetKey); target != nil {
		if target, ok := target.(*expvar.String); ok {
			targetName = target.Value()
		}
	}
	if edition := expvar.Get(statsPrefix + editionKey); edition != nil {
		if edition, ok := edition.(*expvar.String); ok {
			editionName = edition.Value()
		}
	}

	return Report{
		ClusterID:         seed.UID,
		PrometheusVersion: build.GetVersion(),
		CreatedAt:         seed.CreatedAt,
		Interval:          interval,
		IntervalPeriod:    reportInterval.Seconds(),
		Os:                runtime.GOOS,
		Arch:              runtime.GOARCH,
		Target:            targetName,
		Edition:           editionName,
		Metrics:           buildMetrics(),
	}
}

// buildMetrics builds the metrics part of the report to be sent to the stats server
func buildMetrics() map[string]interface{} {
	result := map[string]interface{}{
		"memstats":      memstats(),
		"num_cpu":       runtime.NumCPU(),
		"num_goroutine": runtime.NumGoroutine(),
	}
	expvar.Do(func(kv expvar.KeyValue) {
		if !strings.HasPrefix(kv.Key, statsPrefix) || kv.Key == statsPrefix+targetKey || kv.Key == statsPrefix+editionKey {
			return
		}
		var value interface{}
		switch v := kv.Value.(type) {
		case *expvar.Int:
			value = v.Value()
		case *expvar.Float:
			value = v.Value()
		case *expvar.String:
			value = v.Value()
		case *Statistics:
			value = v.Value()
		case *Counter:
			v.updateRate()
			value = v.Value()
			v.reset()
		case *WordCounter:
			value = v.Value()
		default:
			value = v.String()
		}
		result[strings.TrimPrefix(kv.Key, statsPrefix)] = value
	})
	return result
}

func memstats() interface{} {
	stats := new(runtime.MemStats)
	runtime.ReadMemStats(stats)
	return map[string]interface{}{
		"alloc":           stats.Alloc,
		"total_alloc":     stats.TotalAlloc,
		"sys":             stats.Sys,
		"heap_alloc":      stats.HeapAlloc,
		"heap_inuse":      stats.HeapInuse,
		"stack_inuse":     stats.StackInuse,
		"pause_total_ns":  stats.PauseTotalNs,
		"num_gc":          stats.NumGC,
		"gc_cpu_fraction": stats.GCCPUFraction,
	}
}

// NewFloat returns a new Float stats object.
// If a Float stats object with the same name already exists it is returned.
func NewFloat(name string) *expvar.Float {
	existing := expvar.Get(statsPrefix + name)
	if existing != nil {
		if f, ok := existing.(*expvar.Float); ok {
			return f
		}
		panic(fmt.Sprintf("%v is set to a non-float value", name))
	}
	return expvar.NewFloat(statsPrefix + name)
}

// NewInt returns a new Int stats object.
// If an Int stats object object with the same name already exists it is returned.
func NewInt(name string) *expvar.Int {
	existing := expvar.Get(statsPrefix + name)
	if existing != nil {
		if i, ok := existing.(*expvar.Int); ok {
			return i
		}
		panic(fmt.Sprintf("%v is set to a non-int value", name))
	}
	return expvar.NewInt(statsPrefix + name)
}

// NewString returns a new String stats object.
// If a String stats object with the same name already exists it is returned.
func NewString(name string) *expvar.String {
	existing := expvar.Get(statsPrefix + name)
	if existing != nil {
		if s, ok := existing.(*expvar.String); ok {
			return s
		}
		panic(fmt.Sprintf("%v is set to a non-string value", name))
	}
	return expvar.NewString(statsPrefix + name)
}

// Target sets the target name. This can be set multiple times.
func Target(target string) {
	NewString(targetKey).Set(target)
}

// Edition sets the edition name. This can be set multiple times.
func Edition(edition string) {
	NewString(editionKey).Set(edition)
}

type Statistics struct {
	min   *atomic.Float64
	max   *atomic.Float64
	count *atomic.Int64

	avg *atomic.Float64

	// require for stddev and stdvar
	mean  *atomic.Float64
	value *atomic.Float64
}

// NewStatistics returns a new Statistics object.
// Statistics object is thread-safe and compute statistics on the fly based on sample recorded.
// Available statistics are:
// - min
// - max
// - avg
// - count
// - stddev
// - stdvar
// If a Statistics object with the same name already exists it is returned.
func NewStatistics(name string) *Statistics {
	s := &Statistics{
		min:   atomic.NewFloat64(math.Inf(0)),
		max:   atomic.NewFloat64(math.Inf(-1)),
		count: atomic.NewInt64(0),
		avg:   atomic.NewFloat64(0),
		mean:  atomic.NewFloat64(0),
		value: atomic.NewFloat64(0),
	}
	existing := expvar.Get(statsPrefix + name)
	if existing != nil {
		if s, ok := existing.(*Statistics); ok {
			return s
		}
		panic(fmt.Sprintf("%v is set to a non-Statistics value", name))
	}
	expvar.Publish(statsPrefix+name, s)
	return s
}

func (s *Statistics) String() string {
	b, _ := json.Marshal(s.Value())
	return string(b)
}

func (s *Statistics) Value() map[string]interface{} {
	stdvar := s.value.Load() / float64(s.count.Load())
	stddev := math.Sqrt(stdvar)
	min := s.min.Load()
	max := s.max.Load()
	result := map[string]interface{}{
		"avg":   s.avg.Load(),
		"count": s.count.Load(),
	}
	if !math.IsInf(min, 0) {
		result["min"] = min
	}
	if !math.IsInf(max, 0) {
		result["max"] = s.max.Load()
	}
	if !math.IsNaN(stddev) {
		result["stddev"] = stddev
	}
	if !math.IsNaN(stdvar) {
		result["stdvar"] = stdvar
	}
	return result
}

func (s *Statistics) Record(v float64) {
	for {
		min := s.min.Load()
		if min <= v {
			break
		}
		if s.min.CompareAndSwap(min, v) {
			break
		}
	}
	for {
		max := s.max.Load()
		if max >= v {
			break
		}
		if s.max.CompareAndSwap(max, v) {
			break
		}
	}
	for {
		avg := s.avg.Load()
		count := s.count.Load()
		mean := s.mean.Load()
		value := s.value.Load()

		delta := v - mean
		newCount := count + 1
		newMean := mean + (delta / float64(newCount))
		newValue := value + (delta * (v - newMean))
		newAvg := avg + ((v - avg) / float64(newCount))
		if s.avg.CompareAndSwap(avg, newAvg) && s.count.CompareAndSwap(count, newCount) && s.mean.CompareAndSwap(mean, newMean) && s.value.CompareAndSwap(value, newValue) {
			break
		}
	}
}

type Counter struct {
	total *atomic.Int64
	rate  *atomic.Float64

	resetTime time.Time
}

// NewCounter returns a new Counter stats object.
// If a Counter stats object with the same name already exists it is returned.
func NewCounter(name string) *Counter {
	c := &Counter{
		total:     atomic.NewInt64(0),
		rate:      atomic.NewFloat64(0),
		resetTime: time.Now(),
	}
	existing := expvar.Get(statsPrefix + name)
	if existing != nil {
		if c, ok := existing.(*Counter); ok {
			return c
		}
		panic(fmt.Sprintf("%v is set to a non-Counter value", name))
	}
	expvar.Publish(statsPrefix+name, c)
	return c
}

func (c *Counter) updateRate() {
	total := c.total.Load()
	c.rate.Store(float64(total) / time.Since(c.resetTime).Seconds())
}

func (c *Counter) reset() {
	c.total.Store(0)
	c.rate.Store(0)
	c.resetTime = time.Now()
}

func (c *Counter) Inc(i int64) {
	c.total.Add(i)
}

func (c *Counter) String() string {
	b, _ := json.Marshal(c.Value())
	return string(b)
}

func (c *Counter) Value() map[string]interface{} {
	return map[string]interface{}{
		"total": c.total.Load(),
		"rate":  c.rate.Load(),
	}
}

type WordCounter struct {
	words sync.Map
	count *atomic.Int64
}

// NewWordCounter returns a new WordCounter stats object.
// The WordCounter object is thread-safe and counts the number of words recorded.
// If a WordCounter stats object with the same name already exists it is returned.
func NewWordCounter(name string) *WordCounter {
	c := &WordCounter{
		count: atomic.NewInt64(0),
		words: sync.Map{},
	}
	existing := expvar.Get(statsPrefix + name)
	if existing != nil {
		if w, ok := existing.(*WordCounter); ok {
			return w
		}
		panic(fmt.Sprintf("%v is set to a non-WordCounter value", name))
	}
	expvar.Publish(statsPrefix+name, c)
	return c
}

func (w *WordCounter) Add(word string) {
	if _, loaded := w.words.LoadOrStore(xxhash.Sum64String(word), struct{}{}); !loaded {
		w.count.Add(1)
	}
}

func (w *WordCounter) String() string {
	b, _ := json.Marshal(w.Value())
	return string(b)
}

func (w *WordCounter) Value() int64 {
	return w.count.Load()
}
