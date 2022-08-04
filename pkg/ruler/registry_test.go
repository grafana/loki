package ruler

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/go-kit/log"
	promConfig "github.com/prometheus/common/config"
	"github.com/prometheus/common/model"
	"github.com/prometheus/common/sigv4"
	"github.com/prometheus/prometheus/config"
	"github.com/prometheus/prometheus/model/relabel"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/weaveworks/common/user"

	"github.com/grafana/loki/pkg/ruler/storage/instance"
	"github.com/grafana/loki/pkg/ruler/util"
	"github.com/grafana/loki/pkg/util/test"
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
const sigV4ConfigTenant = "sigv4"
const multiRemoteWriteTenant = "multi-remote-write-tenant"
const sigV4GlobalRegion = "us-east-1"
const sigV4TenantRegion = "us-east-2"

const defaultCapacity = 1000
const capacity = 1500

var remoteURL, _ = url.Parse("http://remote-write")
var backCompatCfg = Config{
	RemoteWrite: RemoteWriteConfig{
		Client: &config.RemoteWriteConfig{
			URL: &promConfig.URL{URL: remoteURL},
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
		Clients: map[string]config.RemoteWriteConfig{
			"default": {
				URL: &promConfig.URL{URL: remoteURL},
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
		},
		Enabled:             true,
		ConfigRefreshPeriod: 5 * time.Second,
	},
}

var remoteURL2, _ = url.Parse("http://remote-write2")
var cfg = Config{
	RemoteWrite: RemoteWriteConfig{
		Clients: map[string]config.RemoteWriteConfig{
			"remote-1": {
				URL: &promConfig.URL{URL: remoteURL},
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
			"remote-2": {
				URL: &promConfig.URL{URL: remoteURL2},
				QueueConfig: config.QueueConfig{
					Capacity: capacity,
				},
				HTTPClientConfig: promConfig.HTTPClientConfig{
					BasicAuth: &promConfig.BasicAuth{
						Password: "bar2",
						Username: "foo2",
					},
				},
				Headers: map[string]string{
					"Base": "value2",
				},
				WriteRelabelConfigs: []*relabel.Config{
					{
						SourceLabels: []model.LabelName{"__name2__"},
						Regex:        relabel.MustNewRegexp("ALERTS.*"),
						Action:       "drop",
						Separator:    ":",
						Replacement:  "$1",
					},
				},
			},
		},
		Enabled:             true,
		ConfigRefreshPeriod: 5 * time.Second,
	},
}

func newFakeLimitsBackwardCompat() fakeLimits {
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
			sigV4ConfigTenant: {
				RulerRemoteWriteSigV4Config: &sigv4.SigV4Config{
					Region: sigV4TenantRegion,
				},
			},
		},
	}
}

func newFakeLimits() fakeLimits {
	regex, _ := relabel.NewRegexp(".+:.+")
	regexCluster, _ := relabel.NewRegexp("__cluster__")
	return fakeLimits{
		limits: map[string]*validation.Limits{
			enabledRWTenant: {
				RulerRemoteWriteConfig: map[string]config.RemoteWriteConfig{
					"remote-1": {
						QueueConfig: config.QueueConfig{Capacity: 987},
					},
				},
			},
			disabledRWTenant: {
				RulerRemoteWriteDisabled: true,
			},
			additionalHeadersRWTenant: {
				RulerRemoteWriteConfig: map[string]config.RemoteWriteConfig{
					"remote-1": {
						Headers: map[string]string{
							user.OrgIDHeaderName:                         "overridden",
							fmt.Sprintf("   %s  ", user.OrgIDHeaderName): "overridden",
							strings.ToLower(user.OrgIDHeaderName):        "overridden-lower",
							strings.ToUpper(user.OrgIDHeaderName):        "overridden-upper",
							"Additional":                                 "Header",
						},
					},
				},
			},
			noHeadersRWTenant: {
				RulerRemoteWriteConfig: map[string]config.RemoteWriteConfig{
					"remote-1": {
						Headers: map[string]string{},
					},
				},
			},
			customRelabelsTenant: {
				RulerRemoteWriteConfig: map[string]config.RemoteWriteConfig{
					"remote-1": {
						WriteRelabelConfigs: []*relabel.Config{
							{
								Regex:        regex,
								SourceLabels: model.LabelNames{"__name__"},
								Action:       "drop",
							},
							{
								Regex:  regexCluster,
								Action: "labeldrop",
							},
						},
					},
				},
			},
			nilRelabelsTenant: {},
			emptySliceRelabelsTenant: {
				RulerRemoteWriteConfig: map[string]config.RemoteWriteConfig{
					"remote-1": {
						WriteRelabelConfigs: []*relabel.Config{},
					},
				},
			},
			badRelabelsTenant: {
				RulerRemoteWriteConfig: map[string]config.RemoteWriteConfig{
					"remote-1": {
						WriteRelabelConfigs: []*relabel.Config{
							{
								SourceLabels: model.LabelNames{"__cluster__"},
								Action:       "labeldrop",
							},
						},
					},
				},
			},
			sigV4ConfigTenant: {
				RulerRemoteWriteConfig: map[string]config.RemoteWriteConfig{
					"remote-1": {
						SigV4Config: &sigv4.SigV4Config{
							Region: sigV4TenantRegion,
						},
					},
				},
			},
			multiRemoteWriteTenant: {
				RulerRemoteWriteConfig: map[string]config.RemoteWriteConfig{
					//TODO
				},
			},
		},
	}
}

func setupRegistry(t *testing.T, cfg Config, limits fakeLimits) *walRegistry {
	c, err := cfg.RemoteWrite.Clone()
	require.NoError(t, err)

	cfg.RemoteWrite = *c

	// TempDir adds RemoveAll to c.Cleanup
	walDir := t.TempDir()
	cfg.WAL = instance.Config{
		Dir: walDir,
	}

	overrides, err := validation.NewOverrides(validation.Limits{}, limits)
	require.NoError(t, err)

	reg := newWALRegistry(log.NewNopLogger(), nil, cfg, overrides)
	require.NoError(t, err)

	//stops the registry before the directory is cleaned up
	t.Cleanup(reg.stop)
	return reg.(*walRegistry)
}

func setupSigV4Registry(t *testing.T, cfg Config, limits fakeLimits) *walRegistry {
	// Get the global config and override it
	reg := setupRegistry(t, cfg, limits)

	// Remove the basic auth config and replace with sigv4
	for id, clt := range reg.config.RemoteWrite.Clients {
		clt.HTTPClientConfig.BasicAuth = nil
		clt.SigV4Config = &sigv4.SigV4Config{
			Region: sigV4GlobalRegion,
		}
		reg.config.RemoteWrite.Clients[id] = clt
	}

	return reg
}

func TestTenantRemoteWriteConfigWithOverride(t *testing.T) {
	reg := setupRegistry(t, backCompatCfg, newFakeLimitsBackwardCompat())

	tenantCfg, err := reg.getTenantConfig(enabledRWTenant)
	require.NoError(t, err)

	// tenant has not disable remote-write so will inherit the global one
	assert.Len(t, tenantCfg.RemoteWrite, 1)
	// but the tenant has an override for the queue capacity
	assert.Equal(t, tenantCfg.RemoteWrite[0].QueueConfig.Capacity, 987)

	reg = setupRegistry(t, cfg, newFakeLimits())

	tenantCfg, err = reg.getTenantConfig(enabledRWTenant)
	require.NoError(t, err)

	// tenant has not disable remote-write so will inherit the global one
	assert.Len(t, tenantCfg.RemoteWrite, 2)
	// but the tenant has an override for the queue capacity for the first client
	assert.Equal(t, tenantCfg.RemoteWrite[0].QueueConfig.Capacity, 987)
	// second client remains unchanged
	assert.Equal(t, tenantCfg.RemoteWrite[1].QueueConfig.Capacity, capacity)
}

func TestTenantRemoteWriteConfigWithoutOverride(t *testing.T) {
	reg := setupRegistry(t, backCompatCfg, newFakeLimitsBackwardCompat())

	// this tenant has no overrides, so will get defaults
	tenantCfg, err := reg.getTenantConfig("unknown")
	require.NoError(t, err)

	// tenant has not disable remote-write so will inherit the global one
	assert.Len(t, tenantCfg.RemoteWrite, 1)
	// but the tenant has an override for the queue capacity
	assert.Equal(t, tenantCfg.RemoteWrite[0].QueueConfig.Capacity, defaultCapacity)

	reg = setupRegistry(t, backCompatCfg, newFakeLimits())

	// this tenant has no overrides, so will get defaults
	tenantCfg, err = reg.getTenantConfig("unknown")
	require.NoError(t, err)

	// tenant has not disable remote-write so will inherit the global one
	assert.Len(t, tenantCfg.RemoteWrite, 1)
	// but the tenant has an override for the queue capacity
	assert.Equal(t, tenantCfg.RemoteWrite[0].QueueConfig.Capacity, defaultCapacity)
}

func TestRulerRemoteWriteSigV4ConfigWithOverrides(t *testing.T) {
	reg := setupSigV4Registry(t, backCompatCfg, newFakeLimitsBackwardCompat())

	tenantCfg, err := reg.getTenantConfig(sigV4ConfigTenant)
	require.NoError(t, err)

	// tenant has not disable remote-write so will inherit the global one
	assert.Len(t, tenantCfg.RemoteWrite, 1)
	// ensure sigv4 config is not nil and overwritten
	if assert.NotNil(t, tenantCfg.RemoteWrite[0].SigV4Config) {
		assert.Equal(t, sigV4TenantRegion, tenantCfg.RemoteWrite[0].SigV4Config.Region)
	}

	reg = setupSigV4Registry(t, cfg, newFakeLimits())

	tenantCfg, err = reg.getTenantConfig(sigV4ConfigTenant)
	require.NoError(t, err)

	// tenant has not disable remote-write so will inherit the global one
	assert.Len(t, tenantCfg.RemoteWrite, 2)
	// ensure sigv4 config is not nil and overwritten for first client
	if assert.NotNil(t, tenantCfg.RemoteWrite[0].SigV4Config) {
		assert.Equal(t, sigV4TenantRegion, tenantCfg.RemoteWrite[0].SigV4Config.Region)
	}
	// ensure sigv4 config is not nil and not overwritten for second client
	if assert.NotNil(t, tenantCfg.RemoteWrite[1].SigV4Config) {
		assert.Equal(t, sigV4GlobalRegion, tenantCfg.RemoteWrite[1].SigV4Config.Region)
	}
}

func TestRulerRemoteWriteSigV4ConfigWithoutOverrides(t *testing.T) {
	reg := setupSigV4Registry(t, backCompatCfg, newFakeLimitsBackwardCompat())

	// this tenant has no overrides, so will get defaults
	tenantCfg, err := reg.getTenantConfig("unknown")
	require.NoError(t, err)

	// tenant has not disable remote-write so will inherit the global one
	assert.Len(t, tenantCfg.RemoteWrite, 1)
	// ensure sigv4 config is not nil and the global value
	if assert.NotNil(t, tenantCfg.RemoteWrite[0].SigV4Config) {
		assert.Equal(t, tenantCfg.RemoteWrite[0].SigV4Config.Region, sigV4GlobalRegion)
	}
}

func TestTenantRemoteWriteConfigDisabled(t *testing.T) {
	reg := setupRegistry(t, backCompatCfg, newFakeLimitsBackwardCompat())

	tenantCfg, err := reg.getTenantConfig(disabledRWTenant)
	require.NoError(t, err)

	// this tenant has remote-write disabled
	assert.Len(t, tenantCfg.RemoteWrite, 0)

	reg = setupRegistry(t, cfg, newFakeLimits())

	tenantCfg, err = reg.getTenantConfig(disabledRWTenant)
	require.NoError(t, err)

	// this tenant has remote-write disabled
	assert.Len(t, tenantCfg.RemoteWrite, 0)
}

func TestTenantRemoteWriteHTTPConfigMaintained(t *testing.T) {
	reg := setupRegistry(t, backCompatCfg, newFakeLimitsBackwardCompat())

	tenantCfg, err := reg.getTenantConfig(enabledRWTenant)
	require.NoError(t, err)

	// HTTP client config is not currently overrideable, all tenants' configs should inherit base
	assert.Equal(t, "foo", tenantCfg.RemoteWrite[0].HTTPClientConfig.BasicAuth.Username)
	assert.Equal(t, promConfig.Secret("bar"), tenantCfg.RemoteWrite[0].HTTPClientConfig.BasicAuth.Password)

	reg = setupRegistry(t, cfg, newFakeLimits())

	tenantCfg, err = reg.getTenantConfig(enabledRWTenant)
	require.NoError(t, err)

	// HTTP client config is not currently overrideable, all tenants' configs should inherit base
	assert.Equal(t, "foo", tenantCfg.RemoteWrite[0].HTTPClientConfig.BasicAuth.Username)
	assert.Equal(t, promConfig.Secret("bar"), tenantCfg.RemoteWrite[0].HTTPClientConfig.BasicAuth.Password)
}

func TestTenantRemoteWriteHeaderOverride(t *testing.T) {
	reg := setupRegistry(t, backCompatCfg, newFakeLimitsBackwardCompat())

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
	reg := setupRegistry(t, backCompatCfg, newFakeLimitsBackwardCompat())

	tenantCfg, err := reg.getTenantConfig(noHeadersRWTenant)
	require.NoError(t, err)

	assert.Len(t, tenantCfg.RemoteWrite[0].Headers, 1)
	// ensure that tenant cannot override X-Scope-OrgId header
	assert.Equal(t, tenantCfg.RemoteWrite[0].Headers[user.OrgIDHeaderName], noHeadersRWTenant)
	// the original header must be removed
	assert.Equal(t, tenantCfg.RemoteWrite[0].Headers["Base"], "")
}

func TestTenantRemoteWriteHeadersNoOverride(t *testing.T) {
	reg := setupRegistry(t, backCompatCfg, newFakeLimitsBackwardCompat())

	tenantCfg, err := reg.getTenantConfig(enabledRWTenant)
	require.NoError(t, err)

	assert.Len(t, tenantCfg.RemoteWrite[0].Headers, 2)
	// ensure that tenant cannot override X-Scope-OrgId header
	assert.Equal(t, tenantCfg.RemoteWrite[0].Headers[user.OrgIDHeaderName], enabledRWTenant)
	// the original header must be present
	assert.Equal(t, tenantCfg.RemoteWrite[0].Headers["Base"], "value")
}

func TestRelabelConfigOverrides(t *testing.T) {
	reg := setupRegistry(t, backCompatCfg, newFakeLimitsBackwardCompat())

	tenantCfg, err := reg.getTenantConfig(customRelabelsTenant)
	require.NoError(t, err)

	// it should also override the default label configs
	assert.Len(t, tenantCfg.RemoteWrite[0].WriteRelabelConfigs, 2)
}

func TestRelabelConfigOverridesNilWriteRelabels(t *testing.T) {
	reg := setupRegistry(t, backCompatCfg, newFakeLimitsBackwardCompat())

	tenantCfg, err := reg.getTenantConfig(nilRelabelsTenant)
	require.NoError(t, err)

	// if there are no relabel configs defined for the tenant, it should not override
	assert.Equal(t, tenantCfg.RemoteWrite[0].WriteRelabelConfigs, reg.config.RemoteWrite.Client.WriteRelabelConfigs)
}

func TestRelabelConfigOverridesEmptySliceWriteRelabels(t *testing.T) {
	reg := setupRegistry(t, backCompatCfg, newFakeLimitsBackwardCompat())

	tenantCfg, err := reg.getTenantConfig(emptySliceRelabelsTenant)
	require.NoError(t, err)

	// if there is an empty slice of relabel configs, it should clear existing relabel configs
	assert.Len(t, tenantCfg.RemoteWrite[0].WriteRelabelConfigs, 0)
}

func TestRelabelConfigOverridesWithErrors(t *testing.T) {
	reg := setupRegistry(t, backCompatCfg, newFakeLimitsBackwardCompat())

	_, err := reg.getTenantConfig(badRelabelsTenant)

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
	reg := setupRegistry(t, backCompatCfg, newFakeLimitsBackwardCompat())

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
	reg := setupRegistry(t, backCompatCfg, newFakeLimitsBackwardCompat())

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
