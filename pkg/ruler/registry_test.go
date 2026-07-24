package ruler

import (
	"context"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/user"
	commonconfig "github.com/prometheus/common/config"
	"github.com/prometheus/common/model"
	promconfig "github.com/prometheus/prometheus/config"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/model/relabel"
	"github.com/prometheus/sigv4"
	"github.com/stretchr/testify/require"
	"go.uber.org/atomic"

	rulerconfig "github.com/grafana/loki/v3/pkg/ruler/config"
	"github.com/grafana/loki/v3/pkg/ruler/storage/instance"
	"github.com/grafana/loki/v3/pkg/util/test"
	"github.com/grafana/loki/v3/pkg/validation"
)

const enabledRWTenant = "enabled"
const disabledRWTenant = "disabled"
const additionalHeadersRWTenant = "additional-headers"
const headersRaceTenant = "headers-race-tenant"
const noHeadersRWTenant = "no-headers"
const customRelabelsTenant = "custom-relabels"
const nilRelabelsTenant = "nil-relabels"
const emptySliceRelabelsTenant = "empty-slice-relabels"
const sigV4ConfigTenant = "sigv4"
const multiRemoteWriteTenant = "multi-remote-write-tenant"
const sigV4TenantRegion = "us-east-2"

const defaultCapacity = 1000
const capacity = 1500

const remote1 = "remote-1"
const remote2 = "remote-2"

var remoteURL, _ = url.Parse("http://remote-write")
var backCompatCfg = Config{
	RemoteWrite: RemoteWriteConfig{
		AddOrgIDHeader: true,
		Client: &promconfig.RemoteWriteConfig{
			URL: &commonconfig.URL{URL: remoteURL},
			QueueConfig: promconfig.QueueConfig{
				Capacity: defaultCapacity,
			},
			HTTPClientConfig: commonconfig.HTTPClientConfig{
				BasicAuth: &commonconfig.BasicAuth{
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
		Clients: map[string]promconfig.RemoteWriteConfig{
			"default": {
				URL: &commonconfig.URL{URL: remoteURL},
				QueueConfig: promconfig.QueueConfig{
					Capacity: defaultCapacity,
				},
				HTTPClientConfig: commonconfig.HTTPClientConfig{
					BasicAuth: &commonconfig.BasicAuth{
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
		Clients: map[string]promconfig.RemoteWriteConfig{
			remote1: {
				URL: &commonconfig.URL{URL: remoteURL},
				QueueConfig: promconfig.QueueConfig{
					Capacity: defaultCapacity,
				},
				HTTPClientConfig: commonconfig.HTTPClientConfig{
					BasicAuth: &commonconfig.BasicAuth{
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
				URL: &commonconfig.URL{URL: remoteURL2},
				QueueConfig: promconfig.QueueConfig{
					Capacity: capacity,
				},
				HTTPClientConfig: commonconfig.HTTPClientConfig{
					BasicAuth: &commonconfig.BasicAuth{
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

var newRemoteURL2, _ = url.Parse("http://new-remote-write2")

func newFakeLimits() fakeLimits {
	regex, _ := relabel.NewRegexp(".+:.+")
	regexCluster, _ := relabel.NewRegexp("__cluster__")
	return fakeLimits{
		limits: map[string]*validation.Limits{
			enabledRWTenant: {
				RulerRemoteWriteConfig: map[string]rulerconfig.RemoteWriteConfig{
					remote1: {
						QueueConfig: promconfig.QueueConfig{Capacity: 987},
					},
				},
			},
			disabledRWTenant: {
				RulerRemoteWriteDisabled: true,
			},
			additionalHeadersRWTenant: {
				RulerRemoteWriteConfig: map[string]rulerconfig.RemoteWriteConfig{
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
				RulerRemoteWriteConfig: map[string]rulerconfig.RemoteWriteConfig{
					remote1: {
						Headers: map[string]string{},
					},
				},
			},
			customRelabelsTenant: {
				RulerRemoteWriteConfig: map[string]rulerconfig.RemoteWriteConfig{
					remote1: {
						WriteRelabelConfigs: []*relabel.Config{
							{
								Regex:        regex,
								SourceLabels: model.LabelNames{"__name__"},
								Action:       "drop",
							},
							{
								Regex:       regexCluster,
								Action:      "labeldrop",
								Separator:   relabel.DefaultRelabelConfig.Separator,
								Replacement: relabel.DefaultRelabelConfig.Replacement,
							},
						},
					},
				},
			},
			nilRelabelsTenant: {},
			emptySliceRelabelsTenant: {
				RulerRemoteWriteConfig: map[string]rulerconfig.RemoteWriteConfig{
					remote1: {
						WriteRelabelConfigs: []*relabel.Config{},
					},
				},
			},
			sigV4ConfigTenant: {
				RulerRemoteWriteConfig: map[string]rulerconfig.RemoteWriteConfig{
					remote1: {
						SigV4Config: &sigv4.SigV4Config{
							Region: sigV4TenantRegion,
						},
					},
				},
			},
			multiRemoteWriteTenant: {
				RulerRemoteWriteConfig: map[string]rulerconfig.RemoteWriteConfig{
					remote1: {
						QueueConfig:   promconfig.QueueConfig{Capacity: 987},
						RemoteTimeout: model.Duration(42),
						HTTPClientConfig: commonconfig.HTTPClientConfig{
							BearerToken: "test-token",
						},
					},
					remote2: {
						QueueConfig:   promconfig.QueueConfig{Capacity: 800},
						RemoteTimeout: model.Duration(10),
						URL:           &commonconfig.URL{URL: newRemoteURL2},
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

func TestTenantMultiRemoteWriteConfigWithoutOverride(t *testing.T) {
	reg := setupRegistry(t, cfg, newFakeLimits())

	tenantCfg, err := reg.getTenantConfig(multiRemoteWriteTenant)
	require.NoError(t, err)

	require.Len(t, tenantCfg.RemoteWrite, 2)

	// Both remote clients have their queue capacity and timeout overwritten
	expectedCap := []int{
		987,
		800,
	}
	actualCap := []int{}
	for _, rw := range tenantCfg.RemoteWrite {
		actualCap = append(actualCap, rw.QueueConfig.Capacity)
	}

	require.ElementsMatch(t, actualCap, expectedCap, "QueueConfig capacity do not match")

	expectedDurations := []model.Duration{
		model.Duration(10),
		model.Duration(42),
	}

	actualDurations := []model.Duration{}
	for _, rw := range tenantCfg.RemoteWrite {
		actualDurations = append(actualDurations, rw.RemoteTimeout)
	}
	require.ElementsMatch(t, actualDurations, expectedDurations, "RemoteTimeouts do not match")

	// First remote client's HTTPClientConfig is overrwritten
	expected := []commonconfig.HTTPClientConfig{
		{
			BearerToken: "test-token",
			BasicAuth: &commonconfig.BasicAuth{
				Password: "bar",
				Username: "foo",
			}},
		{
			BasicAuth: &commonconfig.BasicAuth{
				Password: "bar2",
				Username: "foo2",
			}},
	}
	actual := []commonconfig.HTTPClientConfig{}
	for _, rw := range tenantCfg.RemoteWrite {
		actual = append(actual, rw.HTTPClientConfig)
	}

	require.ElementsMatch(t, actual, expected, "HTTPClientConfig do not match")

	// Second remote client's URL is overrwritten
	expectedURLs := []commonconfig.URL{
		{URL: newRemoteURL2},
		{URL: remoteURL},
	}

	actualURLs := []commonconfig.URL{}
	for _, rw := range tenantCfg.RemoteWrite {
		actualURLs = append(actualURLs, *rw.URL)
	}
	require.ElementsMatch(t, actualURLs, expectedURLs, "URLs do not match")
}

func TestTenantRemoteWriteHeadersNotMutateOverrides(t *testing.T) {
	sharedHeaders := map[string]string{
		"Additional": "Header",
	}
	snapshot := maps.Clone(sharedHeaders)

	limits := fakeLimits{
		limits: map[string]*validation.Limits{
			additionalHeadersRWTenant: {},
		},
	}

	reg := setupRegistry(t, backCompatCfg, limits)

	tenantCfg, err := reg.getTenantConfig(additionalHeadersRWTenant)
	require.NoError(t, err)
	require.Len(t, tenantCfg.RemoteWrite, 1)

	require.Equal(t, snapshot, sharedHeaders, "getTenantConfig must not mutate the limits override headers map")
	require.NotEqual(t, fmt.Sprintf("%p", sharedHeaders), fmt.Sprintf("%p", tenantCfg.RemoteWrite[0].Headers),
		"remote write headers must be a copy of the override map, not the same map instance")

	_, err = reg.getTenantConfig(additionalHeadersRWTenant)
	require.NoError(t, err)
	require.Equal(t, snapshot, sharedHeaders, "repeated getTenantConfig must not mutate the limits override headers map")
}

func TestTenantRemoteWriteHeadersConcurrentRefresh(t *testing.T) {
	var requests atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)

	remoteWriteURL, err := url.Parse(srv.URL)
	require.NoError(t, err)

	raceCfg := Config{
		RemoteWrite: RemoteWriteConfig{
			AddOrgIDHeader:      true,
			Enabled:             true,
			ConfigRefreshPeriod: time.Hour,
			Clients: map[string]promconfig.RemoteWriteConfig{
				"default": {
					URL: &commonconfig.URL{URL: remoteWriteURL},
					QueueConfig: promconfig.QueueConfig{
						Capacity:          10000,
						MinShards:         4,
						MaxShards:         8,
						MaxSamplesPerSend: 100,
						BatchSendDeadline: model.Duration(100 * time.Millisecond),
					},
				},
			},
		},
	}

	limits := fakeLimits{
		limits: map[string]*validation.Limits{
			headersRaceTenant: {},
		},
	}

	reg := setupRegistry(t, raceCfg, limits)
	reg.configureTenantStorage(headersRaceTenant)

	test.Poll(t, 5*time.Second, true, func() interface{} {
		return reg.isReady(headersRaceTenant)
	})

	ctx := user.InjectOrgID(context.Background(), headersRaceTenant)
	stop := make(chan struct{})
	var wg sync.WaitGroup

	for i := range 4 {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := range 500 {
				select {
				case <-stop:
					return
				default:
					app := reg.Appender(ctx)
					_, appendErr := app.Append(
						0,
						labels.FromStrings("__name__", "test_metric", "goroutine", fmt.Sprintf("%d", id), "iter", fmt.Sprintf("%d", j)),
						time.Now().UnixMilli(),
						float64(j),
					)
					if appendErr == nil {
						_ = app.Commit()
					}
				}
			}
		}(i)
	}

	time.Sleep(3 * time.Second)
	close(stop)
	wg.Wait()

	require.Positive(t, requests.Load(), "expected remote write requests to exercise header injection")

	// Refresh after writers finish. Concurrent ApplyConfig during active remote-write
	// shards triggers an unrelated data race in vendored Prometheus SetClient handling.
	for range 10 {
		reg.configureTenantStorage(headersRaceTenant)
	}
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
	require.Truef(t, ok, "instance is not of expected type")

	// if remote-write is disabled, setup a null registry
	regDisabled := newWALRegistry(log.NewNopLogger(), nil, Config{
		RemoteWrite: RemoteWriteConfig{
			Enabled: false,
		},
	}, overrides)

	_, ok = regDisabled.(nullRegistry)
	require.Truef(t, ok, "instance is not of expected type")
}

func TestWALRegistryWipeOnStartup(t *testing.T) {
	overrides, err := validation.NewOverrides(validation.Limits{}, nil)
	require.NoError(t, err)

	configWithDir := func(dir string, remoteWriteEnabled bool) Config {
		cfg := Config{
			RemoteWrite: RemoteWriteConfig{Enabled: remoteWriteEnabled},
		}
		cfg.WAL.Dir = dir
		return cfg
	}

	// seedWAL creates a WAL directory containing leftover per-tenant WAL data,
	// mimicking what a previous ruler run would have left on disk.
	seedWAL := func(t *testing.T) string {
		t.Helper()
		walDir := filepath.Join(t.TempDir(), "ruler-wal")
		segment := filepath.Join(walDir, "tenant", "wal", "00000000")
		require.NoError(t, os.MkdirAll(filepath.Dir(segment), 0o755))
		require.NoError(t, os.WriteFile(segment, []byte("stale"), 0o644))
		return walDir
	}

	t.Run("wipes the WAL directory on startup", func(t *testing.T) {
		walDir := seedWAL(t)

		reg := newWALRegistry(log.NewNopLogger(), nil, configWithDir(walDir, true), overrides)
		t.Cleanup(reg.stop)

		_, statErr := os.Stat(walDir)
		require.Truef(t, os.IsNotExist(statErr), "expected WAL dir to be wiped, stat err: %v", statErr)
	})

	t.Run("does not wipe when remote-write is disabled", func(t *testing.T) {
		walDir := seedWAL(t)

		// With remote-write disabled the WAL is never used, so newWALRegistry
		// short-circuits to a null registry and must not touch the directory.
		newWALRegistry(log.NewNopLogger(), nil, configWithDir(walDir, false), overrides)

		_, statErr := os.Stat(filepath.Join(walDir, "tenant", "wal", "00000000"))
		require.NoError(t, statErr, "expected WAL contents to be preserved when remote-write is disabled")
	})
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
