package logql

import (
	"fmt"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
)

func TestResult_RecordMetrics(t *testing.T) {
	for _, f := range prometheus.ExponentialBuckets(0.125, 2, 10) {
		// fmt.Println(humanize.Bytes(uint64(f)))
		fmt.Println(f)

	}
}
