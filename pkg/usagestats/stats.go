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

type Report struct {
	ClusterID              string    `json:"clusterID"`
	CreatedAt              time.Time `json:"createdAt"`
	Interval               time.Time `json:"interval"`
	Target                 string    `json:"target"`
	prom.PrometheusVersion `json:"version"`
	Os                     string                 `json:"os"`
	Arch                   string                 `json:"arch"`
	Edition                string                 `json:"edition"`
	Metrics                map[string]interface{} `json:"metrics"`
}

func sendReport(ctx context.Context, seed *ClusterSeed, interval time.Time) error {
	out, err := jsoniter.MarshalIndent(buildReport(seed, interval), "", " ")
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, usageStatsURL, bytes.NewBuffer(out))
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
		Os:                runtime.GOOS,
		Arch:              runtime.GOARCH,
		Target:            targetName,
		Edition:           editionName,
		Metrics:           buildMetrics(),
	}
}

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
		"mallocs":         stats.Mallocs,
		"frees":           stats.Frees,
		"heap_alloc":      stats.HeapAlloc,
		"heap_sys":        stats.HeapSys,
		"heap_idle":       stats.HeapIdle,
		"heap_inuse":      stats.HeapInuse,
		"heap_released":   stats.HeapReleased,
		"heap_objects":    stats.HeapObjects,
		"stack_inuse":     stats.StackInuse,
		"stack_sys":       stats.StackSys,
		"other_sys":       stats.OtherSys,
		"pause_total_ns":  stats.PauseTotalNs,
		"num_gc":          stats.NumGC,
		"gc_cpu_fraction": stats.GCCPUFraction,
	}
}

func NewFloat(name string) *expvar.Float {
	return expvar.NewFloat(statsPrefix + name)
}

func NewInt(name string) *expvar.Int {
	return expvar.NewInt(statsPrefix + name)
}

func NewString(name string) *expvar.String {
	return expvar.NewString(statsPrefix + name)
}

func Target(target string) {
	NewString(targetKey).Set(target)
}

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

func NewStatistics(name string) *Statistics {
	s := &Statistics{
		min:   atomic.NewFloat64(math.Inf(0)),
		max:   atomic.NewFloat64(math.Inf(-1)),
		count: atomic.NewInt64(0),
		avg:   atomic.NewFloat64(0),
		mean:  atomic.NewFloat64(0),
		value: atomic.NewFloat64(0),
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
	return map[string]interface{}{
		"min":    s.min.Load(),
		"max":    s.max.Load(),
		"avg":    s.avg.Load(),
		"count":  s.count.Load(),
		"stddev": math.Sqrt(stdvar),
		"stdvar": stdvar,
	}
}

func (s *Statistics) Record(v float64) {
	for {
		min := s.min.Load()
		if min <= v {
			break
		}
		if s.min.CAS(min, v) {
			break
		}
	}
	for {
		max := s.max.Load()
		if max >= v {
			break
		}
		if s.max.CAS(max, v) {
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
		if s.avg.CAS(avg, newAvg) && s.count.CAS(count, newCount) && s.mean.CAS(mean, newMean) && s.value.CAS(value, newValue) {
			break
		}
	}
}

type Counter struct {
	total *atomic.Int64
	rate  *atomic.Float64

	resetTime time.Time
}

func NewCounter(name string) *Counter {
	c := &Counter{
		total:     atomic.NewInt64(0),
		rate:      atomic.NewFloat64(0),
		resetTime: time.Now(),
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

func NewWordCounter(name string) *WordCounter {
	c := &WordCounter{
		count: atomic.NewInt64(0),
		words: sync.Map{},
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
