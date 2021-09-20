package configstore

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/cortexproject/cortex/pkg/ring/kv"
	"github.com/go-kit/kit/log"
	"github.com/grafana/agent/pkg/metrics/instance"
	"github.com/grafana/agent/pkg/util"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
)

func TestRemote_List(t *testing.T) {
	remote, err := NewRemote(log.NewNopLogger(), prometheus.NewRegistry(), kv.Config{
		Store:  "inmemory",
		Prefix: "configs/",
	}, true)
	require.NoError(t, err)
	t.Cleanup(func() {
		err := remote.Close()
		require.NoError(t, err)
	})

	cfgs := []string{"a", "b", "c"}
	for _, cfg := range cfgs {
		err := remote.kv.CAS(context.Background(), cfg, func(in interface{}) (out interface{}, retry bool, err error) {
			return fmt.Sprintf("name: %s", cfg), false, nil
		})
		require.NoError(t, err)
	}

	list, err := remote.List(context.Background())
	require.NoError(t, err)
	sort.Strings(list)
	require.Equal(t, cfgs, list)
}

func TestRemote_Get(t *testing.T) {
	remote, err := NewRemote(log.NewNopLogger(), prometheus.NewRegistry(), kv.Config{
		Store:  "inmemory",
		Prefix: "configs/",
	}, true)
	require.NoError(t, err)
	t.Cleanup(func() {
		err := remote.Close()
		require.NoError(t, err)
	})

	err = remote.kv.CAS(context.Background(), "someconfig", func(in interface{}) (out interface{}, retry bool, err error) {
		return "name: someconfig", false, nil
	})
	require.NoError(t, err)

	cfg, err := remote.Get(context.Background(), "someconfig")
	require.NoError(t, err)

	expect := instance.DefaultConfig
	expect.Name = "someconfig"
	require.Equal(t, expect, cfg)
}

func TestRemote_Put(t *testing.T) {
	remote, err := NewRemote(log.NewNopLogger(), prometheus.NewRegistry(), kv.Config{
		Store:  "inmemory",
		Prefix: "configs/",
	}, true)
	require.NoError(t, err)
	t.Cleanup(func() {
		err := remote.Close()
		require.NoError(t, err)
	})

	cfg := instance.DefaultConfig
	cfg.Name = "newconfig"

	created, err := remote.Put(context.Background(), cfg)
	require.NoError(t, err)
	require.True(t, created)

	actual, err := remote.Get(context.Background(), "newconfig")
	require.NoError(t, err)
	require.Equal(t, cfg, actual)

	t.Run("Updating", func(t *testing.T) {
		cfg := instance.DefaultConfig
		cfg.Name = "newconfig"
		cfg.HostFilter = true

		created, err := remote.Put(context.Background(), cfg)
		require.NoError(t, err)
		require.False(t, created)
	})
}

func TestRemote_Put_NonUnique(t *testing.T) {
	var (
		conflictingA = util.Untab(`
name: conflicting-a
scrape_configs:
- job_name: foobar
		`)
		conflictingB = util.Untab(`
name: conflicting-b
scrape_configs:
- job_name: fizzbuzz
- job_name: foobar
		`)
	)

	conflictingACfg, err := instance.UnmarshalConfig(strings.NewReader(conflictingA))
	require.NoError(t, err)

	conflictingBCfg, err := instance.UnmarshalConfig(strings.NewReader(conflictingB))
	require.NoError(t, err)

	remote, err := NewRemote(log.NewNopLogger(), prometheus.NewRegistry(), kv.Config{
		Store:  "inmemory",
		Prefix: "configs/",
	}, true)
	require.NoError(t, err)
	t.Cleanup(func() {
		err := remote.Close()
		require.NoError(t, err)
	})

	created, err := remote.Put(context.Background(), *conflictingACfg)
	require.NoError(t, err)
	require.True(t, created)

	_, err = remote.Put(context.Background(), *conflictingBCfg)
	require.EqualError(t, err, fmt.Sprintf("failed to check uniqueness of config: found multiple scrape configs in config store with job name %q", "foobar"))
}

func TestRemote_Delete(t *testing.T) {
	remote, err := NewRemote(log.NewNopLogger(), prometheus.NewRegistry(), kv.Config{
		Store:  "inmemory",
		Prefix: "configs/",
	}, true)
	require.NoError(t, err)
	t.Cleanup(func() {
		err := remote.Close()
		require.NoError(t, err)
	})

	var cfg instance.Config
	cfg.Name = "deleteme"

	created, err := remote.Put(context.Background(), cfg)
	require.NoError(t, err)
	require.True(t, created)

	err = remote.Delete(context.Background(), "deleteme")
	require.NoError(t, err)

	_, err = remote.Get(context.Background(), "deleteme")
	require.EqualError(t, err, "configuration deleteme does not exist")

	err = remote.Delete(context.Background(), "deleteme")
	require.EqualError(t, err, "configuration deleteme does not exist")
}

func TestRemote_All(t *testing.T) {
	remote, err := NewRemote(log.NewNopLogger(), prometheus.NewRegistry(), kv.Config{
		Store:  "inmemory",
		Prefix: "all-configs/",
	}, true)
	require.NoError(t, err)
	t.Cleanup(func() {
		err := remote.Close()
		require.NoError(t, err)
	})

	cfgs := []string{"a", "b", "c"}
	for _, cfg := range cfgs {
		err := remote.kv.CAS(context.Background(), cfg, func(in interface{}) (out interface{}, retry bool, err error) {
			return fmt.Sprintf("name: %s", cfg), false, nil
		})
		require.NoError(t, err)
	}

	configCh, err := remote.All(context.Background(), nil)
	require.NoError(t, err)

	var gotConfigs []string
	for gotConfig := range configCh {
		gotConfigs = append(gotConfigs, gotConfig.Name)
	}
	sort.Strings(gotConfigs)

	require.Equal(t, cfgs, gotConfigs)
}

func TestRemote_Watch(t *testing.T) {
	remote, err := NewRemote(log.NewNopLogger(), prometheus.NewRegistry(), kv.Config{
		Store:  "inmemory",
		Prefix: "watch-configs/",
	}, true)
	require.NoError(t, err)
	t.Cleanup(func() {
		err := remote.Close()
		require.NoError(t, err)
	})

	_, err = remote.Put(context.Background(), instance.Config{Name: "watch"})
	require.NoError(t, err)

	select {
	case cfg := <-remote.Watch():
		require.Equal(t, "watch", cfg.Key)
		require.NotNil(t, cfg.Config)
		require.Equal(t, "watch", cfg.Config.Name)
	case <-time.After(3 * time.Second):
		require.FailNow(t, "failed to watch for config")
	}

	// Make sure Watch gets other updates.
	_, err = remote.Put(context.Background(), instance.Config{Name: "watch2"})
	require.NoError(t, err)

	select {
	case cfg := <-remote.Watch():
		require.Equal(t, "watch2", cfg.Key)
		require.NotNil(t, cfg.Config)
		require.Equal(t, "watch2", cfg.Config.Name)
	case <-time.After(3 * time.Second):
		require.FailNow(t, "failed to watch for config")
	}
}

func TestRemote_ApplyConfig(t *testing.T) {
	remote, err := NewRemote(log.NewNopLogger(), prometheus.NewRegistry(), kv.Config{
		Store:  "inmemory",
		Prefix: "test-applyconfig/",
	}, true)
	require.NoError(t, err)
	t.Cleanup(func() {
		err := remote.Close()
		require.NoError(t, err)
	})

	err = remote.ApplyConfig(kv.Config{
		Store:  "inmemory",
		Prefix: "test-applyconfig2/",
	}, true)
	require.NoError(t, err, "failed to apply a new config")

	err = remote.ApplyConfig(kv.Config{
		Store:  "inmemory",
		Prefix: "test-applyconfig2/",
	}, true)
	require.NoError(t, err, "failed to re-apply the current config")

	// Make sure watch still works
	_, err = remote.Put(context.Background(), instance.Config{Name: "watch"})
	require.NoError(t, err)

	select {
	case cfg := <-remote.Watch():
		require.Equal(t, "watch", cfg.Key)
		require.NotNil(t, cfg.Config)
		require.Equal(t, "watch", cfg.Config.Name)
	case <-time.After(3 * time.Second):
		require.FailNow(t, "failed to watch for config")
	}
}
