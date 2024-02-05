package bloomshipper

import (
	"os"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/loki/pkg/storage/chunk/client/testutils"
	"github.com/grafana/loki/pkg/storage/config"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"
)

const (
	day = 24 * time.Hour
)

func parseTime(s string) model.Time {
	t, err := time.Parse("2006-01-02 15:04", s)
	if err != nil {
		panic(err)
	}
	return model.TimeFromUnix(t.Unix())
}

func parseDayTime(s string) config.DayTime {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		panic(err)
	}
	return config.DayTime{
		Time: model.TimeFromUnix(t.Unix()),
	}
}

func newMockBloomClient(t *testing.T) (*BloomClient, string) {
	oc := testutils.NewInMemoryObjectClient()
	dir := t.TempDir()
	logger := log.NewLogfmtLogger(os.Stderr)
	cfg := bloomStoreConfig{
		workingDir: dir,
		numWorkers: 3,
	}
	client, err := NewBloomClient(cfg, oc, logger)
	require.NoError(t, err)
	return client, dir
}

func TestBloomClient_GetMeta(t *testing.T) {}

func TestBloomClient_GetMetas(t *testing.T) {}

func TestBloomClient_PutMeta(t *testing.T) {}

func TestBloomClient_DeleteMetas(t *testing.T) {}
