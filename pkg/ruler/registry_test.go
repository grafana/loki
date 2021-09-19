package ruler

import (
	"context"
	"fmt"
	"io/ioutil"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/cortexproject/cortex/pkg/util/test"
	"github.com/go-kit/log"
	promConfig "github.com/prometheus/common/config"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/config"
	"github.com/prometheus/prometheus/pkg/relabel"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/weaveworks/common/user"

	"github.com/grafana/loki/pkg/ruler/storage/instance"
	"github.com/grafana/loki/pkg/validation"
)

const enabledRWTenant = "12345"
const disabledRWTenant = "54321"

const defaultCapacity = 1000

func newFakeLimits() fakeLimits {
	return fakeLimits{
		limits: map[string]*validation.Limits{
			enabledRWTenant: {
				RulerRemoteWriteQueueCapacity:  987,
				RulerRemoteWriteRelabelConfigs: []*relabel.Config{},
			},
			disabledRWTenant: {
				RulerRemoteWriteDisabled: true,
			},
		},
	}
}

func setupRegistry(t *testing.T, dir string) *walRegistry {
	u, _ := url.Parse("http://remote-write")

	cfg := Config{
		RemoteWrite: RemoteWriteConfig{
			Client: config.RemoteWriteConfig{
				URL: &promConfig.URL{URL: u},
				QueueConfig: config.QueueConfig{
					Capacity: defaultCapacity,
				},
				WriteRelabelConfigs: []*relabel.Config{
					{
						SourceLabels: []model.LabelName{"__name__"},
						Regex:        relabel.MustNewRegexp("ALERTS.*"),
						Action:       "drop",
					},
				},
			},
			Enabled:             true,
			ConfigRefreshPeriod: 5 * time.Second,
		},
		WAL: instance.Config{
			Dir: dir,
		},
	}

	overrides, err := validation.NewOverrides(validation.Limits{}, newFakeLimits())
	require.NoError(t, err)

	reg := newWALRegistry(log.NewNopLogger(), nil, cfg, overrides)
	require.NoError(t, err)

	return reg.(*walRegistry)
}

func createTempWALDir() (string, error) {
	return ioutil.TempDir(os.TempDir(), "wal")
}

func TestTenantRemoteWriteConfigWithOverride(t *testing.T) {
	walDir, err := createTempWALDir()
	require.NoError(t, err)
	reg := setupRegistry(t, walDir)
	defer os.RemoveAll(walDir)

	tenantCfg, err := reg.getTenantConfig(enabledRWTenant)
	require.NoError(t, err)

	// tenant has not disable remote-write so will inherit the global one
	assert.Len(t, tenantCfg.RemoteWrite, 1)
	// but the tenant has an override for the queue capacity
	assert.Equal(t, tenantCfg.RemoteWrite[0].QueueConfig.Capacity, 987)
	// it should also override the default label configs (in this case by clearing them)
	assert.Len(t, tenantCfg.RemoteWrite[0].WriteRelabelConfigs, 0)
}

func TestTenantRemoteWriteConfigWithoutOverride(t *testing.T) {
	walDir, err := createTempWALDir()
	require.NoError(t, err)
	reg := setupRegistry(t, walDir)
	defer os.RemoveAll(walDir)

	// this tenant has no overrides, so will get defaults
	tenantCfg, err := reg.getTenantConfig("unknown")
	require.NoError(t, err)

	// tenant has not disable remote-write so will inherit the global one
	assert.Len(t, tenantCfg.RemoteWrite, 1)
	// but the tenant has an override for the queue capacity
	assert.Equal(t, tenantCfg.RemoteWrite[0].QueueConfig.Capacity, defaultCapacity)
}

func TestTenantRemoteWriteConfigDisabled(t *testing.T) {
	walDir, err := createTempWALDir()
	require.NoError(t, err)
	reg := setupRegistry(t, walDir)
	defer os.RemoveAll(walDir)

	tenantCfg, err := reg.getTenantConfig(disabledRWTenant)
	require.NoError(t, err)

	// this tenant has remote-write disabled
	assert.Len(t, tenantCfg.RemoteWrite, 0)
}

func TestWALRegistryCreation(t *testing.T) {
	overrides, err := validation.NewOverrides(validation.Limits{}, nil)
	require.NoError(t, err)

	regEnabled := newWALRegistry(log.NewNopLogger(), nil, Config{
		RemoteWrite: RemoteWriteConfig{
			Enabled: true,
		},
	}, overrides)

	_, ok := regEnabled.(*walRegistry)
	assert.Truef(t, ok, "instance is not of expected type")

	// if remote-write is disabled, setup a null registry
	regDisabled := newWALRegistry(log.NewNopLogger(), nil, Config{
		RemoteWrite: RemoteWriteConfig{
			Enabled: false,
		},
	}, overrides)

	_, ok = regDisabled.(nullRegistry)
	assert.Truef(t, ok, "instance is not of expected type")
}

func TestStorageSetup(t *testing.T) {
	walDir, err := createTempWALDir()
	require.NoError(t, err)
	reg := setupRegistry(t, walDir)
	defer os.RemoveAll(walDir)

	// once the registry is setup and we configure the tenant storage, we should be able
	// to acquire an appender for the WAL storage
	reg.configureTenantStorage(enabledRWTenant)

	test.Poll(t, 2*time.Second, true, func() interface{} {
		return reg.isReady(enabledRWTenant)
	})

	app := reg.Appender(user.InjectOrgID(context.Background(), enabledRWTenant))
	assert.Equalf(t, "*storage.fanoutAppender", fmt.Sprintf("%T", app), "instance is not of expected type")
}

func TestStorageSetupWithRemoteWriteDisabled(t *testing.T) {
	walDir, err := createTempWALDir()
	require.NoError(t, err)
	reg := setupRegistry(t, walDir)
	defer os.RemoveAll(walDir)

	// once the registry is setup and we configure the tenant storage, we should be able
	// to acquire an appender for the WAL storage
	reg.configureTenantStorage(disabledRWTenant)

	// if remote-write is disabled, we use a discardingAppender to not write to the WAL
	app := reg.Appender(user.InjectOrgID(context.Background(), disabledRWTenant))
	_, ok := app.(discardingAppender)
	assert.Truef(t, ok, "instance is not of expected type")
}

type fakeLimits struct {
	limits map[string]*validation.Limits
}

func (f fakeLimits) TenantLimits(userID string) *validation.Limits {
	limits, ok := f.limits[userID]
	if !ok {
		return &validation.Limits{}
	}

	return limits
}

func (f fakeLimits) ForEachTenantLimit(validation.ForEachTenantLimitCallback) {}
