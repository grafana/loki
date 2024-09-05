package objstore

import (
	"os"
	"testing"

	"github.com/grafana/loki/v3/pkg/storage"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client/local"
	"github.com/grafana/loki/v3/pkg/storage/config"
	"github.com/prometheus/common/model"
)

var metrics = storage.NewClientMetrics()

func NewTestStorage(t testing.TB) (*Multi, error) {
	dir := t.TempDir()
	t.Cleanup(func() {
		os.RemoveAll(dir)
	})
	cfg := storage.Config{
		FSConfig: local.FSConfig{
			Directory: dir,
		},
	}
	return New([]config.PeriodConfig{
		{
			From:       config.DayTime{Time: model.Now()},
			ObjectType: "filesystem",
		},
	}, cfg, metrics)
}
