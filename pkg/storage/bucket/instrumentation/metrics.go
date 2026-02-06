package instrumentation

import (
	"time"

	"github.com/cristalhq/hedgedhttp"
	"github.com/prometheus/client_golang/prometheus"
)

const (
	hedgedMetricsPublishDuration = 10 * time.Second
)

type diffCounter struct {
	previous uint64
	counter  prometheus.Counter
}

func (d *diffCounter) addAbsoluteToCounter(value uint64) {
	diff := float64(value - d.previous)
	if value < d.previous {
		diff = float64(value)
	}
	d.counter.Add(diff)
	d.previous = value
}

// statsProvider defines the interface that wraps hedgedhttp.Stats for ease of testing
type statsProvider interface {
	Snapshot() hedgedhttp.StatsSnapshot
}

// Publish flushes metrics from hedged requests every tickerDur
func publish(s *hedgedhttp.Stats, counter prometheus.Counter) {
	publishWithDuration(s, counter, hedgedMetricsPublishDuration)
}

func publishWithDuration(s statsProvider, counter prometheus.Counter, duration time.Duration) {
	ticker := time.NewTicker(duration)
	diff := &diffCounter{previous: 0, counter: counter}

	go func() {
		for range ticker.C {
			snap := s.Snapshot()

			hedgedRequests := snap.ActualRoundTrips - snap.RequestedRoundTrips
			diff.addAbsoluteToCounter(hedgedRequests)
		}
	}()
}
