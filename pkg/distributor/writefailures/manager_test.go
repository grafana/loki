package writefailures

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/runtime"
	"github.com/grafana/loki/v3/pkg/util/flagext"
)

func TestWriteFailuresLogging(t *testing.T) {
	t.Run("it only logs for the configured tenants and is disabled by default", func(t *testing.T) {
		buf := bytes.NewBuffer(nil)
		logger := log.NewLogfmtLogger(buf)

		f := &providerMock{
			tenantConfig: func(tenantID string) *runtime.Config {
				if tenantID == "good-tenant" {
					return &runtime.Config{
						LimitedLogPushErrors: true,
					}
				}
				if tenantID == "bad-tenant" {
					return &runtime.Config{
						LimitedLogPushErrors: false,
					}
				}
				return &runtime.Config{}
			},
		}

		runtimeCfg, err := runtime.NewTenantConfigs(f)
		require.NoError(t, err)

		manager := NewManager(logger, prometheus.NewRegistry(), Cfg{LogRate: flagext.ByteSize(1000)}, runtimeCfg, "ingester")

		manager.Log("bad-tenant", fmt.Errorf("bad-tenant contains invalid entry"))
		manager.Log("good-tenant", fmt.Errorf("good-tenant contains invalid entry"))
		manager.Log("unknown-tenant", fmt.Errorf("unknown-tenant contains invalid entry"))

		content := buf.String()
		require.NotEmpty(t, content)
		require.Contains(t, content, "good-tenant")
		require.NotContains(t, content, "bad-tenant")
		require.NotContains(t, content, "unknown-tenant")
	})
}

func TestWriteFailuresRateLimiting(t *testing.T) {
	buf := bytes.NewBuffer(nil)
	logger := log.NewLogfmtLogger(buf)

	provider := &providerMock{
		tenantConfig: func(_ string) *runtime.Config {
			return &runtime.Config{
				LimitedLogPushErrors: true,
			}
		},
	}
	runtimeCfg, err := runtime.NewTenantConfigs(provider)
	require.NoError(t, err)

	t.Run("with zero rate limiting", func(t *testing.T) {
		manager := NewManager(logger, prometheus.NewRegistry(), Cfg{LogRate: flagext.ByteSize(0)}, runtimeCfg, "distributor")

		manager.Log("known-tenant", fmt.Errorf("known-tenant entry error"))

		content := buf.String()
		require.Empty(t, content)
	})

	t.Run("bytes exceeded on single message", func(t *testing.T) {
		manager := NewManager(logger, prometheus.NewRegistry(), Cfg{LogRate: flagext.ByteSize(1000)}, runtimeCfg, "distributor")

		errorStr := strings.Builder{}
		for i := 0; i < 1001; i++ {
			errorStr.WriteRune('z')
		}

		manager.Log("known-tenant", fmt.Errorf("%s", errorStr.String()))

		content := buf.String()
		require.Empty(t, content)
	})

	t.Run("valid bytes", func(t *testing.T) {
		manager := NewManager(logger, prometheus.NewRegistry(), Cfg{LogRate: flagext.ByteSize(1002)}, runtimeCfg, "ingester")

		errorStr := strings.Builder{}
		for i := 0; i < 1001; i++ {
			errorStr.WriteRune('z')
		}

		manager.Log("known-tenant", fmt.Errorf("%s", errorStr.String()))

		content := buf.String()
		require.NotEmpty(t, content)
		require.Contains(t, content, errorStr.String())
	})

	t.Run("limit is reset after a second", func(t *testing.T) {
		manager := NewManager(logger, prometheus.NewRegistry(), Cfg{LogRate: flagext.ByteSize(1000)}, runtimeCfg, "ingester")

		errorStr1 := strings.Builder{}
		errorStr2 := strings.Builder{}
		errorStr3 := strings.Builder{}
		for i := 0; i < 999; i++ {
			errorStr1.WriteRune('z')
			errorStr2.WriteRune('w')
			errorStr2.WriteRune('y')
		}

		manager.Log("known-tenant", fmt.Errorf("%s", errorStr1.String()))
		manager.Log("known-tenant", fmt.Errorf("%s", errorStr2.String())) // more than 1KB/s
		time.Sleep(time.Second)
		manager.Log("known-tenant", fmt.Errorf("%s", errorStr3.String()))

		content := buf.String()
		require.NotEmpty(t, content)
		require.Contains(t, content, errorStr1.String())
		require.NotContains(t, content, errorStr2.String())
		require.Contains(t, content, errorStr3.String())
	})

	t.Run("limit is per-tenant", func(t *testing.T) {
		runtimeCfg, err := runtime.NewTenantConfigs(provider)
		require.NoError(t, err)
		manager := NewManager(logger, prometheus.NewRegistry(), Cfg{LogRate: flagext.ByteSize(1000)}, runtimeCfg, "ingester")

		errorStr1 := strings.Builder{}
		errorStr2 := strings.Builder{}
		errorStr3 := strings.Builder{}
		for i := 0; i < 998; i++ {
			errorStr1.WriteRune('z')
			errorStr2.WriteRune('w')
			errorStr3.WriteRune('y')
		}

		manager.Log("tenant1", fmt.Errorf("1%s", errorStr1.String()))
		manager.Log("tenant2", fmt.Errorf("2%s", errorStr1.String()))

		manager.Log("tenant1", fmt.Errorf("1%s", errorStr2.String())) // limit exceeded for tenant1, Str2 shouldn't be present.
		manager.Log("tenant3", fmt.Errorf("3%s", errorStr1.String())) // all fine with tenant3.

		time.Sleep(time.Second)
		manager.Log("tenant1", fmt.Errorf("1%s", errorStr3.String())) // tenant1 is fine again.
		manager.Log("tenant3", fmt.Errorf("3%s", errorStr1.String())) // all fine with tenant3.

		content := buf.String()
		require.NotEmpty(t, content)
		require.Contains(t, content, "1z")
		require.Contains(t, content, "2z")

		require.NotContains(t, content, "1w") // Str2
		require.Contains(t, content, "3z")

		require.Contains(t, content, "1y")
		require.Contains(t, content, "3z")
	})
}

type providerMock struct {
	tenantConfig func(string) *runtime.Config
}

func (m *providerMock) TenantConfig(userID string) *runtime.Config {
	return m.tenantConfig(userID)
}
