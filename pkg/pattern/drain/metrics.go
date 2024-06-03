package drain

import "github.com/prometheus/client_golang/prometheus"

type Metrics struct {
	PatternsEvictedTotal  prometheus.Counter
	PatternsDetectedTotal prometheus.Counter
}
