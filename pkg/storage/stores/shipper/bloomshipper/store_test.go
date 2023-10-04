package bloomshipper

import (
	"context"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/loki/pkg/storage/config"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"
)

func parseDayTime(s string) config.DayTime {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		panic(err)
	}
	return config.DayTime{
		Time: model.TimeFromUnix(t.Unix()),
	}
}

func TestNoopBloomShipper(t *testing.T) {
	logger := log.NewNopLogger()
	shipper, err := NewBloomShipper(logger)
	require.NoError(t, err)
	shipper.ForEachBlock(context.Background(), "fake", time.Time{}, time.Now(), []uint64{}, func(bq BlockQuerier) error {
		return nil
	})
}

func TestBloomStore(t *testing.T) {
}
