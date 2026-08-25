package ewma_test

import (
	"context"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/engine/internal/util/ewma"
)

func Test(t *testing.T) {
	var idx int
	observations := []float64{
		1, 2, 3, 4, 5, 6, 7, 8, 9, 10,
		11, 12, 13, 14, 15, 16, 17, 18, 19, 20,
	}

	source := ewma.SourceFunc(func() float64 {
		if idx >= len(observations) {
			<-t.Context().Done()
			return 0
		}

		res := observations[idx]
		idx++
		return res
	})

	synctest.Test(t, func(t *testing.T) {
		reg := prometheus.NewRegistry()

		m, err := ewma.New(ewma.Options{
			Name:            "my_ewma_metric",
			UpdateFrequency: time.Minute,
			Windows:         []time.Duration{time.Minute, 5 * time.Minute, 15 * time.Minute},
		}, source)
		require.NoError(t, err)

		reg.MustRegister(m)

		var wg sync.WaitGroup
		defer wg.Wait()

		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()

		wg.Go(func() { _ = m.Monitor(ctx) })

		// Wait for all observations to have been made.
		time.Sleep(time.Duration(len(observations)) * time.Minute)
		synctest.Wait()

		expect := `
# HELP
# TYPE my_ewma_metric gauge
my_ewma_metric{window="15m"} 9.581664816797675
my_ewma_metric{window="1m"} 19.41802329639137
my_ewma_metric{window="5m"} 15.584385505095714
`
		require.NoError(t, testutil.GatherAndCompare(reg, strings.NewReader(expect)))
	})
}
