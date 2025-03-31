package builder

import (
	"os"
	"testing"

	"github.com/prometheus/common/model"

	"github.com/grafana/loki/v3/pkg/storage"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client/local"
	"github.com/grafana/loki/v3/pkg/storage/config"
)

var metrics *storage.ClientMetrics

func NewTestStorage(t testing.TB) (*MultiStore, error) {
	if metrics == nil {
		m := storage.NewClientMetrics()
		metrics = &m
	}
	dir := t.TempDir()
	t.Cleanup(func() {
		os.RemoveAll(dir)
		metrics.Unregister()
	})
	cfg := storage.Config{
		FSConfig: local.FSConfig{
			Directory: dir,
		},
	}
	return NewMultiStore([]config.PeriodConfig{
		{
			From:       config.DayTime{Time: model.Now()},
			ObjectType: "filesystem",
		},
	}, cfg, *metrics)
}
