package loki

import (
	"context"
	"flag"
	"io"
	"io/ioutil"
	"strings"
	"testing"
	"time"

	"github.com/cortexproject/cortex/pkg/util/runtimeconfig"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/validation"
)

func Test_LoadRetentionRules(t *testing.T) {
	overrides := newTestOverrides(t,
		`
overrides:
    "1":
        creation_grace_period: 48h
    "29":
        creation_grace_period: 48h
        ingestion_burst_size_mb: 140
        ingestion_rate_mb: 120
        max_concurrent_tail_requests: 1000
        max_global_streams_per_user: 100000
        max_label_names_per_series: 30
        max_query_parallelism: 256
        split_queries_by_interval: 15m
        retention_period: 1440h
        retention_stream:
            - selector: '{app="foo"}'
              period: 48h
              priority: 10
            - selector: '{namespace="bar", cluster=~"fo.*|b.+|[1-2]"}'
              period: 24h
              priority: 5
`)
	require.Equal(t, 31*24*time.Hour, overrides.RetentionPeriod("1"))    // default
	require.Equal(t, 2*30*24*time.Hour, overrides.RetentionPeriod("29")) // overrides
	require.Equal(t, []validation.StreamRetention(nil), overrides.StreamRetention("1"))
	require.Equal(t, []validation.StreamRetention{
		{Period: model.Duration(48 * time.Hour), Priority: 10, Selector: `{app="foo"}`, Matchers: []*labels.Matcher{
			labels.MustNewMatcher(labels.MatchEqual, "app", "foo"),
		}},
		{Period: model.Duration(24 * time.Hour), Priority: 5, Selector: `{namespace="bar", cluster=~"fo.*|b.+|[1-2]"}`, Matchers: []*labels.Matcher{
			labels.MustNewMatcher(labels.MatchEqual, "namespace", "bar"),
			labels.MustNewMatcher(labels.MatchRegexp, "cluster", "fo.*|b.+|[1-2]"),
		}},
	}, overrides.StreamRetention("29"))
}

func Test_ValidateRules(t *testing.T) {
	_, err := loadRuntimeConfig(strings.NewReader(
		`
overrides:
    "29":
        retention_stream:
            - selector: '{app=foo"}'
              period: 48h
              priority: 10
            - selector: '{namespace="bar", cluster=~"fo.*|b.+|[1-2]"}'
              period: 24h
              priority: 10
`))
	require.Equal(t, "invalid override for tenant 29: invalid labels matchers: parse error at line 1, col 6: syntax error: unexpected IDENTIFIER, expecting STRING", err.Error())
	_, err = loadRuntimeConfig(strings.NewReader(
		`
overrides:
    "29":
        retention_stream:
            - selector: '{app="foo"}'
              period: 5h
              priority: 10
`))
	require.Equal(t, "invalid override for tenant 29: retention period must be >= 24h was 5h", err.Error())
}

func newTestOverrides(t *testing.T, yaml string) *validation.Overrides {
	t.Helper()
	f, err := ioutil.TempFile(t.TempDir(), "bar")
	require.NoError(t, err)
	path := f.Name()
	// fake loader to load from string instead of file.
	loader := func(_ io.Reader) (interface{}, error) {
		return loadRuntimeConfig(strings.NewReader(yaml))
	}
	cfg := runtimeconfig.ManagerConfig{
		ReloadPeriod: 1 * time.Second,
		Loader:       loader,
		LoadPath:     path,
	}
	flagset := flag.NewFlagSet("", flag.PanicOnError)
	var defaults validation.Limits
	defaults.RegisterFlags(flagset)
	require.NoError(t, flagset.Parse(nil))
	validation.SetDefaultLimitsForYAMLUnmarshalling(defaults)

	runtimeConfig, err := runtimeconfig.NewRuntimeConfigManager(cfg, prometheus.DefaultRegisterer)
	require.NoError(t, err)

	require.NoError(t, runtimeConfig.StartAsync(context.Background()))
	require.NoError(t, runtimeConfig.AwaitRunning(context.Background()))
	defer func() {
		runtimeConfig.StopAsync()
		require.NoError(t, runtimeConfig.AwaitTerminated(context.Background()))
	}()

	overrides, err := validation.NewOverrides(defaults, newtenantLimitsFromRuntimeConfig(runtimeConfig))
	require.NoError(t, err)
	return overrides
}

func Test_NoOverrides(t *testing.T) {
	flagset := flag.NewFlagSet("", flag.PanicOnError)

	var defaults validation.Limits
	defaults.RegisterFlags(flagset)
	require.NoError(t, flagset.Parse(nil))
	validation.SetDefaultLimitsForYAMLUnmarshalling(defaults)
	overrides, err := validation.NewOverrides(defaults, newtenantLimitsFromRuntimeConfig(nil))
	require.NoError(t, err)
	require.Equal(t, time.Duration(defaults.QuerySplitDuration), overrides.QuerySplitDuration("foo"))
}
