package compute_test

import (
	"testing"

	"github.com/grafana/loki/v3/pkg/columnar"
)

func reportArrayBenchMetrics(b *testing.B, datums ...columnar.Datum) {
	b.Helper()

	totalSize := 0
	totalValues := 0
	for _, d := range datums {
		arr := d.(columnar.Array)
		totalSize += arr.Size()
		totalValues += arr.Len()
	}

	b.SetBytes(int64(totalSize))
	b.ReportMetric(float64(b.N*totalValues)/b.Elapsed().Seconds(), "values/s")

}
