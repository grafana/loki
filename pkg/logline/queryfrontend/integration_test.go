package queryfrontend

import (
	"testing"

	"github.com/go-kit/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logline/store"
	"github.com/grafana/loki/v3/pkg/storage/bucket"
	"github.com/grafana/loki/v3/pkg/storage/bucket/filesystem"
	storageconfig "github.com/grafana/loki/v3/pkg/storage/config"
)

func TestWrapMiddlewareEnabledWithoutTenantSettings(t *testing.T) {
	cfg := Config{
		Enabled: true,
		Store: store.Config{
			MinDate: "2026-09-01",
		},
	}
	inputs := MiddlewareInputs{
		SchemaConfig: storageconfig.SchemaConfig{
			Configs: []storageconfig.PeriodConfig{{
				From:       storageconfig.DayTime{Time: 0},
				ObjectType: bucket.Filesystem,
			}},
		},
		ObjectStoreConfig: bucket.ConfigWithNamedStores{
			Config: bucket.Config{
				Filesystem: filesystem.Config{Directory: t.TempDir()},
			},
		},
	}

	wrapped, storeService, cleanup, err := WrapMiddleware(
		cfg,
		inputs,
		nil,
		nil,
		log.NewNopLogger(),
		prometheus.NewRegistry(),
	)
	require.NoError(t, err)
	require.NotNil(t, wrapped)
	require.NotNil(t, storeService)
	require.NotNil(t, cleanup)
	cleanup()
}
