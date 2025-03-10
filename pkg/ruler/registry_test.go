package ruler

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/user"
	promConfig "github.com/prometheus/common/config"
	"github.com/prometheus/common/model"
	common_sigv4 "github.com/prometheus/common/sigv4"
	"github.com/prometheus/prometheus/config"
	"github.com/prometheus/prometheus/model/relabel"
	prom_sigv4 "github.com/prometheus/sigv4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/ruler/storage/instance"
	"github.com/grafana/loki/v3/pkg/ruler/util"
	"github.com/grafana/loki/v3/pkg/util/test"
	"github.com/grafana/loki/v3/pkg/validation"
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

const remote1 = "remote-1"
const remote2 = "remote-2"

var remoteURL, _ = url.Parse("http://remote-write")
var backCompatCfg = Config{
	RemoteWrite: RemoteWriteConfig{
		AddOrgIDHeader: true,
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
		AddOrgIDHeader: true,
		Clients: map[string]config.RemoteWriteConfig{
			remote1: {
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
			remote2: {
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
				RulerRemoteWriteSigV4Config: &common_sigv4.SigV4Config{
					Region: sigV4TenantRegion,
				},
			},
		},
	}
}

var newRemoteURL2, _ = url.Parse("http://new-remote-write2")

func newFakeLimits() fakeLimits {
	regex, _ := relabel.NewRegexp(".+:.+")
	regexCluster, _ := relabel.NewRegexp("__cluster__")
	return fakeLimits{
		limits: map[string]*validation.Limits{
			enabledRWTenant: {
				RulerRemoteWriteConfig: map[string]config.RemoteWriteConfig{
					remote1: {
						QueueConfig: config.QueueConfig{Capacity: 987},
					},
				},
			},
			disabledRWTenant: {
				RulerRemoteWriteDisabled: true,
			},
			additionalHeadersRWTenant: {
				RulerRemoteWriteConfig: map[string]config.RemoteWriteConfig{
					remote1: {
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
					remote1: {
						Headers: map[string]string{},
					},
				},
			},
			customRelabelsTenant: {
				RulerRemoteWriteConfig: map[string]config.RemoteWriteConfig{
					remote1: {
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
					remote1: {
						WriteRelabelConfigs: []*relabel.Config{},
					},
				},
			},
			sigV4ConfigTenant: {
				RulerRemoteWriteConfig: map[string]config.RemoteWriteConfig{
					remote1: {
						SigV4Config: &prom_sigv4.SigV4Config{
							Region: sigV4TenantRegion,
						},
					},
				},
			},
			multiRemoteWriteTenant: {
				RulerRemoteWriteConfig: map[string]config.RemoteWriteConfig{
					remote1: {
						QueueConfig:   config.QueueConfig{Capacity: 987},
						RemoteTimeout: model.Duration(42),
						HTTPClientConfig: promConfig.HTTPClientConfig{
							BearerToken: "test-token",
						},
					},
					remote2: {
						QueueConfig:   config.QueueConfig{Capacity: 800},
						RemoteTimeout: model.Duration(10),
						URL:           &promConfig.URL{URL: newRemoteURL2},
					},
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
		clt.SigV4Config = &prom_sigv4.SigV4Config{
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
	// second client remains unchanged
	expected := []int{
		987,
		capacity,
	}
	actual := []int{}
	for _, rw := range tenantCfg.RemoteWrite {
		actual = append(actual, rw.QueueConfig.Capacity)
	}

	assert.ElementsMatch(t, actual, expected, "QueueConfig capacity do not match")
}

func TestTenantRemoteWriteConfigWithOverrideConcurrentAccess(t *testing.T) {
	require.NotPanics(t, func() {
		reg := setupRegistry(t, cfg, newFakeLimits())
		var wg sync.WaitGroup
		for i := 0; i < 1000; i++ {
			wg.Add(1)
			go func(reg *walRegistry) {
				defer wg.Done()

				_, err := reg.getTenantConfig(enabledRWTenant)
				require.NoError(t, err)
			}(reg)

			wg.Add(1)
			go func(reg *walRegistry) {
				defer wg.Done()

				_, err := reg.getTenantConfig(additionalHeadersRWTenant)
				require.NoError(t, err)
			}(reg)
		}

		wg.Wait()
	})
}

func TestAppenderConcurrentAccess(t *testing.T) {
	require.NotPanics(t, func() {
		reg := setupRegistry(t, cfg, newFakeLimits())
		var wg sync.WaitGroup
		for i := 0; i < 1000; i++ {
			wg.Add(1)
			go func(reg *walRegistry) {
				defer wg.Done()

				_ = reg.Appender(user.InjectOrgID(context.Background(), enabledRWTenant))
			}(reg)

			wg.Add(1)
			go func(reg *walRegistry) {
				defer wg.Done()

				_ = reg.Appender(user.InjectOrgID(context.Background(), additionalHeadersRWTenant))
			}(reg)
		}

		wg.Wait()
	})
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

	reg = setupRegistry(t, cfg, newFakeLimits())

	// this tenant has no overrides, so will get defaults
	tenantCfg, err = reg.getTenantConfig("unknown")
	require.NoError(t, err)

	// tenant has not disable remote-write so will inherit the global one
	assert.Len(t, tenantCfg.RemoteWrite, 2)
	// but the tenant has an override for the queue capacity for the first client
	expected := []int{
		defaultCapacity,
		capacity,
	}
	actual := []int{}
	for _, rw := range tenantCfg.RemoteWrite {
		actual = append(actual, rw.QueueConfig.Capacity)
	}

	assert.ElementsMatch(t, actual, expected, "QueueConfig capacity do not match")
}

func TestTenantMultiRemoteWriteConfigWithoutOverride(t *testing.T) {
	reg := setupRegistry(t, cfg, newFakeLimits())

	tenantCfg, err := reg.getTenantConfig(multiRemoteWriteTenant)
	require.NoError(t, err)

	assert.Len(t, tenantCfg.RemoteWrite, 2)

	// Both remote clients have their queue capacity and timeout overwritten
	expectedCap := []int{
		987,
		800,
	}
	actualCap := []int{}
	for _, rw := range tenantCfg.RemoteWrite {
		actualCap = append(actualCap, rw.QueueConfig.Capacity)
	}

	assert.ElementsMatch(t, actualCap, expectedCap, "QueueConfig capacity do not match")

	expectedDurations := []model.Duration{
		model.Duration(10),
		model.Duration(42),
	}

	actualDurations := []model.Duration{}
	for _, rw := range tenantCfg.RemoteWrite {
		actualDurations = append(actualDurations, rw.RemoteTimeout)
	}
	assert.ElementsMatch(t, actualDurations, expectedDurations, "RemoteTimeouts do not match")

	// First remote client's HTTPClientConfig is overrwritten
	expected := []promConfig.HTTPClientConfig{
		{
			BearerToken: "test-token",
			BasicAuth: &promConfig.BasicAuth{
				Password: "bar",
				Username: "foo",
			}},
		{
			BasicAuth: &promConfig.BasicAuth{
				Password: "bar2",
				Username: "foo2",
			}},
	}
	actual := []promConfig.HTTPClientConfig{}
	for _, rw := range tenantCfg.RemoteWrite {
		actual = append(actual, rw.HTTPClientConfig)
	}

	assert.ElementsMatch(t, actual, expected, "HTTPClientConfig do not match")

	// Second remote client's URL is overrwritten
	expectedURLs := []promConfig.URL{
		{URL: newRemoteURL2},
		{URL: remoteURL},
	}

	actualURLs := []promConfig.URL{}
	for _, rw := range tenantCfg.RemoteWrite {
		actualURLs = append(actualURLs, *rw.URL)
	}
	assert.ElementsMatch(t, actualURLs, expectedURLs, "URLs do not match")
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
	// ensure sigv4 config is not nil and not overwritten for second client
	expected := []string{
		sigV4TenantRegion,
		sigV4GlobalRegion,
	}
	actual := []string{}
	for _, rw := range tenantCfg.RemoteWrite {
		actual = append(actual, rw.SigV4Config.Region)
	}

	assert.ElementsMatch(t, actual, expected, "SigV4Config regions do not match")
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

	reg = setupSigV4Registry(t, cfg, newFakeLimits())

	// this tenant has no overrides, so will get defaults
	tenantCfg, err = reg.getTenantConfig("unknown")
	require.NoError(t, err)

	// tenant has not disable remote-write so will inherit the global one
	assert.Len(t, tenantCfg.RemoteWrite, 2)
	// ensure sigv4 config is not nil and the global value
	if assert.NotNil(t, tenantCfg.RemoteWrite[0].SigV4Config) {
		assert.Equal(t, tenantCfg.RemoteWrite[0].SigV4Config.Region, sigV4GlobalRegion)
	}
	if assert.NotNil(t, tenantCfg.RemoteWrite[1].SigV4Config) {
		assert.Equal(t, tenantCfg.RemoteWrite[1].SigV4Config.Region, sigV4GlobalRegion)
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
	expected := []promConfig.HTTPClientConfig{
		{
			BasicAuth: &promConfig.BasicAuth{
				Username: "foo",
				Password: promConfig.Secret("bar"),
			},
		},
		{
			BasicAuth: &promConfig.BasicAuth{
				Username: "foo2",
				Password: promConfig.Secret("bar2"),
			},
		},
	}

	actual := []promConfig.HTTPClientConfig{}
	for _, rw := range tenantCfg.RemoteWrite {
		actual = append(actual, rw.HTTPClientConfig)
	}

	assert.ElementsMatch(t, actual, expected, "HTTPClientConfigs do not match")
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

	reg = setupRegistry(t, cfg, newFakeLimits())

	tenantCfg, err = reg.getTenantConfig(additionalHeadersRWTenant)
	require.NoError(t, err)

	assert.Len(t, tenantCfg.RemoteWrite[0].Headers, 2)
	assert.Len(t, tenantCfg.RemoteWrite[1].Headers, 2)

	// Ensure that overrides take plus but that tenant cannot override X-Scope-OrgId header
	expected := []map[string]string{
		{
			user.OrgIDHeaderName: additionalHeadersRWTenant,
			"Additional":         "Header",
		},
		{
			user.OrgIDHeaderName: additionalHeadersRWTenant,
			"Base":               "value2",
		},
	}

	actual := []map[string]string{}
	for _, rw := range tenantCfg.RemoteWrite {
		actual = append(actual, rw.Headers)
	}

	assert.ElementsMatch(t, actual, expected, "Headers do not match")

	tenantCfg, err = reg.getTenantConfig(enabledRWTenant)
	require.NoError(t, err)

	// and a user who didn't set any header overrides still gets the X-Scope-OrgId header
	assert.Equal(t, tenantCfg.RemoteWrite[0].Headers[user.OrgIDHeaderName], enabledRWTenant)
	assert.Equal(t, tenantCfg.RemoteWrite[1].Headers[user.OrgIDHeaderName], enabledRWTenant)
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

	reg = setupRegistry(t, cfg, newFakeLimits())

	tenantCfg, err = reg.getTenantConfig(noHeadersRWTenant)
	require.NoError(t, err)

	// Ensure that overrides take plus but that tenant cannot override X-Scope-OrgId header
	expected := []map[string]string{
		{
			user.OrgIDHeaderName: noHeadersRWTenant,
		},
		{
			user.OrgIDHeaderName: noHeadersRWTenant,
			"Base":               "value2",
		},
	}

	actual := []map[string]string{}
	for _, rw := range tenantCfg.RemoteWrite {
		actual = append(actual, rw.Headers)
	}

	assert.ElementsMatch(t, actual, expected, "Headers do not match")
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

	reg = setupRegistry(t, cfg, newFakeLimits())

	tenantCfg, err = reg.getTenantConfig(enabledRWTenant)
	require.NoError(t, err)

	// Ensure that overrides take plus but that tenant cannot override X-Scope-OrgId header
	expected := []map[string]string{
		{
			user.OrgIDHeaderName: enabledRWTenant,
			"Base":               "value",
		},
		{
			user.OrgIDHeaderName: enabledRWTenant,
			"Base":               "value2",
		},
	}

	actual := []map[string]string{}
	for _, rw := range tenantCfg.RemoteWrite {
		actual = append(actual, rw.Headers)
	}

	assert.ElementsMatch(t, actual, expected, "Headers do not match")
}

func TestTenantRemoteWriteHeadersNoOrgIDHeader(t *testing.T) {
	backCompatCfg.RemoteWrite.AddOrgIDHeader = false
	reg := setupRegistry(t, backCompatCfg, newFakeLimitsBackwardCompat())

	tenantCfg, err := reg.getTenantConfig(enabledRWTenant)
	require.NoError(t, err)

	assert.Len(t, tenantCfg.RemoteWrite[0].Headers, 1)
	// ensure that X-Scope-OrgId header is missing
	assert.Equal(t, tenantCfg.RemoteWrite[0].Headers[user.OrgIDHeaderName], "")
	// the original header must be present
	assert.Equal(t, tenantCfg.RemoteWrite[0].Headers["Base"], "value")

	cfg.RemoteWrite.AddOrgIDHeader = false
	reg = setupRegistry(t, cfg, newFakeLimits())

	tenantCfg, err = reg.getTenantConfig(enabledRWTenant)
	require.NoError(t, err)

	// Ensure that overrides take plus and that X-Scope-OrgID header is still missing
	expected := []map[string]string{
		{
			"Base": "value",
		},
		{
			"Base": "value2",
		},
	}

	actual := []map[string]string{}
	for _, rw := range tenantCfg.RemoteWrite {
		actual = append(actual, rw.Headers)
	}

	assert.ElementsMatch(t, actual, expected, "Headers do not match")
}

func TestRelabelConfigOverrides(t *testing.T) {
	reg := setupRegistry(t, backCompatCfg, newFakeLimitsBackwardCompat())

	tenantCfg, err := reg.getTenantConfig(customRelabelsTenant)
	require.NoError(t, err)

	// it should also override the default label configs
	assert.Len(t, tenantCfg.RemoteWrite[0].WriteRelabelConfigs, 2)

	reg = setupRegistry(t, cfg, newFakeLimits())

	tenantCfg, err = reg.getTenantConfig(customRelabelsTenant)
	require.NoError(t, err)

	// It should also override the default label configs for the first client only
	expected := [][]string{
		{
			"__name__",
			"",
		},
		{
			"__name2__",
		},
	}

	actual := [][]string{{}, {}}
	for i, rw := range tenantCfg.RemoteWrite {
		for _, wrc := range rw.WriteRelabelConfigs {
			actual[i] = append(actual[i], wrc.SourceLabels.String())
		}
	}

	assert.ElementsMatch(t, actual, expected, "Headers do not match")
}

func TestRelabelConfigOverridesNilWriteRelabels(t *testing.T) {
	reg := setupRegistry(t, backCompatCfg, newFakeLimitsBackwardCompat())

	tenantCfg, err := reg.getTenantConfig(nilRelabelsTenant)
	require.NoError(t, err)

	// if there are no relabel configs defined for the tenant, it should not override
	assert.Equal(t, tenantCfg.RemoteWrite[0].WriteRelabelConfigs, reg.config.RemoteWrite.Client.WriteRelabelConfigs)

	reg = setupRegistry(t, cfg, newFakeLimits())

	tenantCfg, err = reg.getTenantConfig(nilRelabelsTenant)
	require.NoError(t, err)

	// if there are no relabel configs defined for the tenant, it should not override
	actual := [][]*relabel.Config{}
	for _, rw := range tenantCfg.RemoteWrite {
		actual = append(actual, rw.WriteRelabelConfigs)
	}

	expected := [][]*relabel.Config{
		reg.config.RemoteWrite.Clients[remote1].WriteRelabelConfigs,
		reg.config.RemoteWrite.Clients[remote2].WriteRelabelConfigs,
	}

	assert.ElementsMatch(t, actual, expected, "WriteRelabelConfigs do not match")
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

	// same test with regular config
	reg = setupRegistry(t, cfg, newFakeLimits())
	reg.configureTenantStorage(disabledRWTenant)

	app = reg.Appender(user.InjectOrgID(context.Background(), disabledRWTenant))
	_, ok = app.(discardingAppender)
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
