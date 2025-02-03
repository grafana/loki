// This directory was copied and adapted from https://github.com/grafana/agent/tree/main/pkg/metrics.
// We cannot vendor the agent in since the agent vendors loki in, which would cause a cyclic dependency.
// NOTE: many changes have been made to the original code for our use-case.
package instance

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/go-kit/log"
	"github.com/prometheus/prometheus/scrape"
	"github.com/prometheus/prometheus/storage"
	"github.com/stretchr/testify/require"
)

func TestBasicManager_ApplyConfig(t *testing.T) {
	logger := log.NewLogfmtLogger(log.NewSyncWriter(os.Stderr))

	baseMock := mockInstance{
		RunFunc: func(ctx context.Context) error {
			logger.Log("msg", "starting an instance")
			<-ctx.Done()
			return nil
		},
		UpdateFunc: func(_ Config) error {
			return nil
		},
		TargetsActiveFunc: func() map[string][]*scrape.Target {
			return nil
		},
	}

	t.Run("dynamic update successful", func(t *testing.T) {
		spawnedCount := 0
		spawner := func(_ Config) (ManagedInstance, error) {
			spawnedCount++

			newMock := baseMock
			return &newMock, nil
		}

		cm := NewBasicManager(DefaultBasicManagerConfig, NewMetrics(nil), logger, spawner)

		for i := 0; i < 10; i++ {
			err := cm.ApplyConfig(Config{Name: "test"})
			require.NoError(t, err)
		}

		require.Equal(t, 1, spawnedCount)
	})

	t.Run("dynamic update unsuccessful", func(t *testing.T) {
		spawnedCount := 0
		spawner := func(_ Config) (ManagedInstance, error) {
			spawnedCount++

			newMock := baseMock
			newMock.UpdateFunc = func(_ Config) error {
				return ErrInvalidUpdate{
					Inner: fmt.Errorf("cannot dynamically update for testing reasons"),
				}
			}
			return &newMock, nil
		}

		cm := NewBasicManager(DefaultBasicManagerConfig, NewMetrics(nil), logger, spawner)

		for i := 0; i < 10; i++ {
			err := cm.ApplyConfig(Config{Name: "test"})
			require.NoError(t, err)
		}

		require.Equal(t, 10, spawnedCount)
	})

	t.Run("dynamic update errored", func(t *testing.T) {
		spawnedCount := 0
		spawner := func(_ Config) (ManagedInstance, error) {
			spawnedCount++

			newMock := baseMock
			newMock.UpdateFunc = func(_ Config) error {
				return fmt.Errorf("something really bad happened")
			}
			return &newMock, nil
		}

		cm := NewBasicManager(DefaultBasicManagerConfig, NewMetrics(nil), logger, spawner)

		// Creation should succeed
		err := cm.ApplyConfig(Config{Name: "test"})
		require.NoError(t, err)

		// ...but the update should fail
		err = cm.ApplyConfig(Config{Name: "test"})
		require.Error(t, err, "something really bad happened")
		require.Equal(t, 1, spawnedCount)
	})
}

type mockInstance struct {
	RunFunc              func(ctx context.Context) error
	UpdateFunc           func(c Config) error
	TargetsActiveFunc    func() map[string][]*scrape.Target
	StorageDirectoryFunc func() string
	AppenderFunc         func() storage.Appender
}

func (m mockInstance) Ready() bool {
	return true
}

func (m mockInstance) Stop() error {
	return nil
}

func (m mockInstance) Tenant() string {
	return ""
}

func (m mockInstance) Run(ctx context.Context) error {
	if m.RunFunc != nil {
		return m.RunFunc(ctx)
	}
	panic("RunFunc not provided")
}

func (m mockInstance) Update(c Config) error {
	if m.UpdateFunc != nil {
		return m.UpdateFunc(c)
	}
	panic("UpdateFunc not provided")
}

func (m mockInstance) TargetsActive() map[string][]*scrape.Target {
	if m.TargetsActiveFunc != nil {
		return m.TargetsActiveFunc()
	}
	panic("TargetsActiveFunc not provided")
}

func (m mockInstance) StorageDirectory() string {
	if m.StorageDirectoryFunc != nil {
		return m.StorageDirectoryFunc()
	}
	panic("StorageDirectoryFunc not provided")
}

func (m mockInstance) Appender(_ context.Context) storage.Appender {
	if m.AppenderFunc != nil {
		return m.AppenderFunc()
	}
	panic("AppenderFunc not provided")
}
