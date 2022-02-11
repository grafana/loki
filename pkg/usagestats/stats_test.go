package usagestats

import (
	"runtime"
	"testing"
	"time"

	"github.com/grafana/loki/pkg/util/build"

	"github.com/google/uuid"
	jsoniter "github.com/json-iterator/go"
	"github.com/stretchr/testify/require"
)

func Test_BuildReport(t *testing.T) {
	now := time.Now()
	seed := &ClusterSeed{
		UID:       uuid.New().String(),
		CreatedAt: now,
	}

	Edition("OSS")
	Target("compactor")
	NewString("compression").Set("lz4")
	NewInt("compression_ratio").Set(100)
	NewFloat("size_mb").Set(100.1)
	NewCounter("lines_written").Inc(200)
	s := NewStatistics("query_throughput")
	s.Record(300)
	s.Record(5)
	w := NewWordCounter("active_tenants")
	w.Add("foo")
	w.Add("bar")
	w.Add("foo")

	r := buildReport(seed, now.Add(time.Hour))
	require.Equal(t, r.Arch, runtime.GOARCH)
	require.Equal(t, r.Os, runtime.GOOS)
	require.Equal(t, r.PrometheusVersion, build.GetVersion())
	require.Equal(t, r.Edition, "OSS")
	require.Equal(t, r.Target, "compactor")
	require.Equal(t, r.Metrics["num_cpu"], runtime.NumCPU())
	require.Equal(t, r.Metrics["num_goroutine"], runtime.NumGoroutine())
	require.Equal(t, r.Metrics["compression"], "lz4")
	require.Equal(t, r.Metrics["compression_ratio"], int64(100))
	require.Equal(t, r.Metrics["size_mb"], 100.1)
	require.Equal(t, r.Metrics["lines_written"].(map[string]interface{})["total"], int64(200))
	require.Equal(t, r.Metrics["query_throughput"].(map[string]interface{})["min"], float64(5))
	require.Equal(t, r.Metrics["query_throughput"].(map[string]interface{})["max"], float64(300))
	require.Equal(t, r.Metrics["query_throughput"].(map[string]interface{})["count"], int64(2))
	require.Equal(t, r.Metrics["query_throughput"].(map[string]interface{})["avg"], float64(300+5)/2)
	require.Equal(t, r.Metrics["active_tenants"], int64(2))

	out, _ := jsoniter.MarshalIndent(r, "", " ")
	t.Log(string(out))
}

func TestCounter(t *testing.T) {
	c := NewCounter("test_counter")
	c.Inc(100)
	c.Inc(200)
	c.Inc(300)
	time.Sleep(1 * time.Second)
	c.updateRate()
	v := c.Value()
	require.Equal(t, int64(600), v["total"])
	require.GreaterOrEqual(t, v["rate"], float64(590))
	c.reset()
	require.Equal(t, int64(0), c.Value()["total"])
	require.Equal(t, float64(0), c.Value()["rate"])
}

func TestStatistic(t *testing.T) {
	s := NewStatistics("test_stats")
	s.Record(100)
	s.Record(200)
	s.Record(300)
	v := s.Value()
	require.Equal(t, float64(100), v["min"])
	require.Equal(t, float64(300), v["max"])
	require.Equal(t, int64(3), v["count"])
	require.Equal(t, float64(100+200+300)/3, v["avg"])
	require.Equal(t, float64(81.64965809277261), v["stddev"])
	require.Equal(t, float64(6666.666666666667), v["stdvar"])
}

func TestWordCounter(t *testing.T) {
	w := NewWordCounter("test_words_count")
	for i := 0; i < 100; i++ {
		go func() {
			w.Add("foo")
			w.Add("bar")
			w.Add("foo")
		}()
	}
	require.Equal(t, int64(2), w.Value())
}
