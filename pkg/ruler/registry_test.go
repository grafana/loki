package ruler

import (
	"context"
	"fmt"
	"io/ioutil"
	"net/url"
	"os"
	"strings"
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
	"github.com/grafana/loki/pkg/ruler/util"
	"github.com/grafana/loki/pkg/validation"
)

const enabledRWTenant = "enabled"
const disabledRWTenant = "disabled"
const additionalHeadersRWTenant = "additional-headers"
const noHeadersRWTenant = "no-headers"
const customRelabelsTenant = "custom-relabels"
const badRelabelsTenant = "bad-relabels"
const nilRelabelsTenant = "nil-relabels"
const emptySliceRelabelsTenant = "empty-slice-relabels"

const defaultCapacity = 1000

func newFakeLimits() fakeLimits {
	return fakeLimits{
		limits: map[string]*validation.Limits{
			enabledRWTenant: {
				RulerRemoteWriteQueueCapacity: 987,
			},
			disabledRWTenant: {
				RulerRemoteWriteDisabled: true,
			},
			additionalHeadersRWTenant: {
				RulerRemoteWriteHeaders: validation.NewOverwriteMarshalingStringMap(map[string]string{
					user.OrgIDHeaderName:                         "overridden",
					fmt.Sprintf("   %s  ", user.OrgIDHeaderName): "overridden",
					strings.ToLower(user.OrgIDHeaderName):        "overridden-lower",
					strings.ToUpper(user.OrgIDHeaderName):        "overridden-upper",
					"Additional":                                 "Header",
				}),
			},
			noHeadersRWTenant: {
				RulerRemoteWriteHeaders: validation.NewOverwriteMarshalingStringMap(map[string]string{}),
			},
			customRelabelsTenant: {
				RulerRemoteWriteRelabelConfigs: []*util.RelabelConfig{
					{
						Regex:        ".+:.+",
						SourceLabels: []string{"__name__"},
						Action:       "drop",
					},
					{
						Regex:  "__cluster__",
						Action: "labeldrop",
					},
				},
			},
			nilRelabelsTenant: {},
			emptySliceRelabelsTenant: {
				RulerRemoteWriteRelabelConfigs: []*util.RelabelConfig{},
			},
			badRelabelsTenant: {
				RulerRemoteWriteRelabelConfigs: []*util.RelabelConfig{
					{
						SourceLabels: []string{"__cluster__"},
						Action:       "labeldrop",
					},
				},
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
				HTTPClientConfig: promConfig.HTTPClientConfig{
					BasicAuth: &promConfig.BasicAuth{
						Password: "bar",
						Username: "foo",
					},
				},
				Headers: map[string]string{
					"Base": "value",
				},
				WriteRelabelConfigs: []*relabel.Config{
					{
						SourceLabels: []model.LabelName{"__name__"},
						Regex:        relabel.MustNewRegexp("ALERTS.*"),
						Action:       "drop",
						Separator:    ";",
						Replacement:  "$1",
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

func TestTenantRemoteWriteHTTPConfigMaintained(t *testing.T) {
	walDir, err := createTempWALDir()
	require.NoError(t, err)
	reg := setupRegistry(t, walDir)
	defer os.RemoveAll(walDir)

	tenantCfg, err := reg.getTenantConfig(enabledRWTenant)
	require.NoError(t, err)

	// HTTP client config is not currently overrideable, all tenants' configs should inherit base
	assert.Equal(t, tenantCfg.RemoteWrite[0].HTTPClientConfig.BasicAuth.Username, "foo")
	assert.Equal(t, tenantCfg.RemoteWrite[0].HTTPClientConfig.BasicAuth.Password, promConfig.Secret("<secret>"))
}

func TestTenantRemoteWriteHeaderOverride(t *testing.T) {
	walDir, err := createTempWALDir()
	require.NoError(t, err)
	reg := setupRegistry(t, walDir)
	defer os.RemoveAll(walDir)

	tenantCfg, err := reg.getTenantConfig(additionalHeadersRWTenant)
	require.NoError(t, err)

	assert.Len(t, tenantCfg.RemoteWrite[0].Headers, 2)
	// ensure that tenant cannot override X-Scope-OrgId header
	assert.Equal(t, tenantCfg.RemoteWrite[0].Headers[user.OrgIDHeaderName], additionalHeadersRWTenant)
	// but that the additional header defined is set
	assert.Equal(t, tenantCfg.RemoteWrite[0].Headers["Additional"], "Header")
	// the original header must be removed
	assert.Equal(t, tenantCfg.RemoteWrite[0].Headers["Base"], "")

	tenantCfg, err = reg.getTenantConfig(enabledRWTenant)
	require.NoError(t, err)

	// and a user who didn't set any header overrides still gets the X-Scope-OrgId header
	assert.Equal(t, tenantCfg.RemoteWrite[0].Headers[user.OrgIDHeaderName], enabledRWTenant)
}

func TestTenantRemoteWriteHeadersReset(t *testing.T) {
	walDir, err := createTempWALDir()
	require.NoError(t, err)
	reg := setupRegistry(t, walDir)
	defer os.RemoveAll(walDir)

	tenantCfg, err := reg.getTenantConfig(noHeadersRWTenant)
	require.NoError(t, err)

	assert.Len(t, tenantCfg.RemoteWrite[0].Headers, 1)
	// ensure that tenant cannot override X-Scope-OrgId header
	assert.Equal(t, tenantCfg.RemoteWrite[0].Headers[user.OrgIDHeaderName], noHeadersRWTenant)
	// the original header must be removed
	assert.Equal(t, tenantCfg.RemoteWrite[0].Headers["Base"], "")
}

func TestTenantRemoteWriteHeadersNoOverride(t *testing.T) {
	walDir, err := createTempWALDir()
	require.NoError(t, err)
	reg := setupRegistry(t, walDir)
	defer os.RemoveAll(walDir)

	tenantCfg, err := reg.getTenantConfig(enabledRWTenant)
	require.NoError(t, err)

	assert.Len(t, tenantCfg.RemoteWrite[0].Headers, 2)
	// ensure that tenant cannot override X-Scope-OrgId header
	assert.Equal(t, tenantCfg.RemoteWrite[0].Headers[user.OrgIDHeaderName], enabledRWTenant)
	// the original header must be present
	assert.Equal(t, tenantCfg.RemoteWrite[0].Headers["Base"], "value")
}

func TestRelabelConfigOverrides(t *testing.T) {
	walDir, err := createTempWALDir()
	require.NoError(t, err)
	reg := setupRegistry(t, walDir)
	defer os.RemoveAll(walDir)

	tenantCfg, err := reg.getTenantConfig(customRelabelsTenant)
	require.NoError(t, err)

	// it should also override the default label configs
	assert.Len(t, tenantCfg.RemoteWrite[0].WriteRelabelConfigs, 2)
}

func TestRelabelConfigOverridesNilWriteRelabels(t *testing.T) {
	walDir, err := createTempWALDir()
	require.NoError(t, err)
	reg := setupRegistry(t, walDir)
	defer os.RemoveAll(walDir)

	tenantCfg, err := reg.getTenantConfig(nilRelabelsTenant)
	require.NoError(t, err)

	// if there are no relabel configs defined for the tenant, it should not override
	assert.Equal(t, tenantCfg.RemoteWrite[0].WriteRelabelConfigs, reg.config.RemoteWrite.Client.WriteRelabelConfigs)
}

func TestRelabelConfigOverridesEmptySliceWriteRelabels(t *testing.T) {
	walDir, err := createTempWALDir()
	require.NoError(t, err)
	reg := setupRegistry(t, walDir)
	defer os.RemoveAll(walDir)

	tenantCfg, err := reg.getTenantConfig(emptySliceRelabelsTenant)
	require.NoError(t, err)

	// if there is an empty slice of relabel configs, it should clear existing relabel configs
	assert.Len(t, tenantCfg.RemoteWrite[0].WriteRelabelConfigs, 0)
}

func TestRelabelConfigOverridesWithErrors(t *testing.T) {
	walDir, err := createTempWALDir()
	require.NoError(t, err)
	reg := setupRegistry(t, walDir)
	defer os.RemoveAll(walDir)

	_, err = reg.getTenantConfig(badRelabelsTenant)

	// ensure that relabel validation is being applied
	require.EqualError(t, err, "failed to parse relabel configs: labeldrop action requires only 'regex', and no other fields")
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

func (f fakeLimits) AllByUserID() map[string]*validation.Limits {
	return f.limits
}
