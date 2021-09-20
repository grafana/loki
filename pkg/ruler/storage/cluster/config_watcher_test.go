// This directory was copied and adapted from https://github.com/grafana/agent/tree/main/pkg/metrics.
// We cannot vendor the agent in since the agent vendors loki in, which would cause a cyclic dependency.
// NOTE: many changes have been made to the original code for our use-case.
package cluster

import (
	"context"
	"testing"
	"time"

	"github.com/grafana/agent/pkg/metrics/instance"
	"github.com/grafana/agent/pkg/metrics/instance/configstore"
	"github.com/grafana/agent/pkg/util"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func Test_configWatcher_Refresh(t *testing.T) {
	var (
		log = util.TestLogger(t)

		cfg   = DefaultConfig
		store = configstore.Mock{
			WatchFunc: func() <-chan configstore.WatchEvent {
				return make(chan configstore.WatchEvent)
			},
		}

		im mockConfigManager

		validate = func(*instance.Config) error { return nil }
		owned    = func(key string) (bool, error) { return true, nil }
	)
	cfg.Enabled = true
	cfg.ReshardInterval = time.Hour

	w, err := newConfigWatcher(log, cfg, &store, &im, owned, validate)
	require.NoError(t, err)
	t.Cleanup(func() { _ = w.Stop() })

	im.On("ApplyConfig", mock.Anything).Return(nil)
	im.On("DeleteConfig", mock.Anything).Return(nil)

	// First: return a "hello" config.
	store.AllFunc = func(ctx context.Context, keep func(key string) bool) (<-chan instance.Config, error) {
		ch := make(chan instance.Config)
		go func() {
			ch <- instance.Config{Name: "hello"}
			close(ch)
		}()
		return ch, nil
	}

	err = w.refresh(context.Background())
	require.NoError(t, err)

	// Then: return a "new" config.
	store.AllFunc = func(ctx context.Context, keep func(key string) bool) (<-chan instance.Config, error) {
		ch := make(chan instance.Config, 1)
		go func() {
			ch <- instance.Config{Name: "new"}
			close(ch)
		}()
		return ch, nil
	}

	err = w.refresh(context.Background())
	require.NoError(t, err)

	// "hello" and "new" should've been applied, and "hello" should've been deleted
	// from the second refresh.
	im.AssertCalled(t, "ApplyConfig", instance.Config{Name: "hello"})
	im.AssertCalled(t, "ApplyConfig", instance.Config{Name: "new"})
	im.AssertCalled(t, "DeleteConfig", "hello")
}

func Test_configWatcher_handleEvent(t *testing.T) {
	var (
		cfg   = DefaultConfig
		store = configstore.Mock{
			WatchFunc: func() <-chan configstore.WatchEvent {
				return make(chan configstore.WatchEvent)
			},
		}

		validate = func(*instance.Config) error { return nil }

		owned   = func(key string) (bool, error) { return true, nil }
		unowned = func(key string) (bool, error) { return false, nil }
	)
	cfg.Enabled = true

	t.Run("new owned config", func(t *testing.T) {
		var (
			log = util.TestLogger(t)
			im  mockConfigManager
		)

		w, err := newConfigWatcher(log, cfg, &store, &im, owned, validate)
		require.NoError(t, err)
		t.Cleanup(func() { _ = w.Stop() })

		im.On("ApplyConfig", mock.Anything).Return(nil)
		im.On("DeleteConfig", mock.Anything).Return(nil)

		err = w.handleEvent(configstore.WatchEvent{Key: "new", Config: &instance.Config{}})
		require.NoError(t, err)

		im.AssertNumberOfCalls(t, "ApplyConfig", 1)
	})

	t.Run("updated owned config", func(t *testing.T) {
		var (
			log = util.TestLogger(t)
			im  mockConfigManager
		)

		w, err := newConfigWatcher(log, cfg, &store, &im, owned, validate)
		require.NoError(t, err)
		t.Cleanup(func() { _ = w.Stop() })

		im.On("ApplyConfig", mock.Anything).Return(nil)
		im.On("DeleteConfig", mock.Anything).Return(nil)

		// One for create, one for update
		err = w.handleEvent(configstore.WatchEvent{Key: "update", Config: &instance.Config{}})
		require.NoError(t, err)

		err = w.handleEvent(configstore.WatchEvent{Key: "update", Config: &instance.Config{}})
		require.NoError(t, err)

		im.AssertNumberOfCalls(t, "ApplyConfig", 2)
	})

	t.Run("new unowned config", func(t *testing.T) {
		var (
			log = util.TestLogger(t)
			im  mockConfigManager
		)

		w, err := newConfigWatcher(log, cfg, &store, &im, unowned, validate)
		require.NoError(t, err)
		t.Cleanup(func() { _ = w.Stop() })

		im.On("ApplyConfig", mock.Anything).Return(nil)
		im.On("DeleteConfig", mock.Anything).Return(nil)

		// One for create, one for update
		err = w.handleEvent(configstore.WatchEvent{Key: "unowned", Config: &instance.Config{}})
		require.NoError(t, err)

		im.AssertNumberOfCalls(t, "ApplyConfig", 0)
	})

	t.Run("lost ownership", func(t *testing.T) {
		var (
			log = util.TestLogger(t)

			im mockConfigManager

			isOwned = true
			owns    = func(key string) (bool, error) { return isOwned, nil }
		)

		w, err := newConfigWatcher(log, cfg, &store, &im, owns, validate)
		require.NoError(t, err)
		t.Cleanup(func() { _ = w.Stop() })

		im.On("ApplyConfig", mock.Anything).Return(nil)
		im.On("DeleteConfig", mock.Anything).Return(nil)

		// One for create, then one for ownership change
		err = w.handleEvent(configstore.WatchEvent{Key: "disappear", Config: &instance.Config{}})
		require.NoError(t, err)

		// Mark the config as unowned. The re-apply should then delete it.
		isOwned = false

		err = w.handleEvent(configstore.WatchEvent{Key: "disappear", Config: &instance.Config{}})
		require.NoError(t, err)

		im.AssertNumberOfCalls(t, "ApplyConfig", 1)
		im.AssertNumberOfCalls(t, "DeleteConfig", 1)
	})

	t.Run("deleted running config", func(t *testing.T) {
		var (
			log = util.TestLogger(t)

			im mockConfigManager
		)

		w, err := newConfigWatcher(log, cfg, &store, &im, owned, validate)
		require.NoError(t, err)
		t.Cleanup(func() { _ = w.Stop() })

		im.On("ApplyConfig", mock.Anything).Return(nil)
		im.On("DeleteConfig", mock.Anything).Return(nil)

		// One for create, then one for deleted.
		err = w.handleEvent(configstore.WatchEvent{Key: "new-key", Config: &instance.Config{}})
		require.NoError(t, err)

		err = w.handleEvent(configstore.WatchEvent{Key: "new-key", Config: nil})
		require.NoError(t, err)

		im.AssertNumberOfCalls(t, "ApplyConfig", 1)
		im.AssertNumberOfCalls(t, "DeleteConfig", 1)
	})
}

func Test_configWatcher_nextReshard(t *testing.T) {
	watcher := &configWatcher{
		log: util.TestLogger(t),
		cfg: Config{ReshardInterval: time.Second},
	}

	t.Run("past time", func(t *testing.T) {
		select {
		case <-watcher.nextReshard(time.Time{}):
		case <-time.After(250 * time.Millisecond):
			require.FailNow(t, "nextReshard did not return an already ready channel")
		}
	})

	t.Run("future time", func(t *testing.T) {
		select {
		case <-watcher.nextReshard(time.Now()):
		case <-time.After(1500 * time.Millisecond):
			require.FailNow(t, "nextReshard took too long to return")
		}
	})

}

type mockConfigManager struct {
	mock.Mock
}

func (m *mockConfigManager) GetInstance(name string) (instance.ManagedInstance, error) {
	args := m.Mock.Called()
	return args.Get(0).(instance.ManagedInstance), args.Error(1)
}

func (m *mockConfigManager) ListInstances() map[string]instance.ManagedInstance {
	args := m.Mock.Called()
	return args.Get(0).(map[string]instance.ManagedInstance)
}

// ListConfigs implements Manager.
func (m *mockConfigManager) ListConfigs() map[string]instance.Config {
	args := m.Mock.Called()
	return args.Get(0).(map[string]instance.Config)
}

// ApplyConfig implements Manager.
func (m *mockConfigManager) ApplyConfig(c instance.Config) error {
	args := m.Mock.Called(c)
	return args.Error(0)
}

// DeleteConfig implements Manager.
func (m *mockConfigManager) DeleteConfig(name string) error {
	args := m.Mock.Called(name)
	return args.Error(0)
}

// Stop implements Manager.
func (m *mockConfigManager) Stop() {
	m.Mock.Called()
}
