package compactor

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/services"
	"github.com/stretchr/testify/require"
)

// TestCompactor_BootShutdown boots the scaffold with an in-process-only
// scheduler (empty AdvertiseAddr), waits for it to reach Running, then
// stops it. It must transition cleanly with no error.
func TestCompactor_BootShutdown(t *testing.T) {
	cfg := Config{
		Enabled: true,
		Scheduler: SchedulerConfig{
			Endpoint: defaultEndpoint,
			// AdvertiseAddr left empty -> scheduler runs in-process only.
		},
	}

	c, err := New(cfg, log.NewNopLogger())
	require.NoError(t, err)
	require.NotNil(t, c.Scheduler(), "scheduler must be constructed")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	require.NoError(t, services.StartAndAwaitRunning(ctx, c))
	require.Equal(t, services.Running, c.State())

	require.NoError(t, services.StopAndAwaitTerminated(ctx, c))
	require.Equal(t, services.Terminated, c.State())
}

// TestConfig_Validate_DisabledIsNoop captures the default-behaviour
// invariant: a Config with Enabled=false validates regardless of any
// other fields (including ones that would be invalid when enabled).
func TestConfig_Validate_DisabledIsNoop(t *testing.T) {
	cfg := Config{
		Enabled:                   false,
		MaxRunningCompactionTasks: -1, // would fail when enabled
	}
	require.NoError(t, cfg.Validate())
}

// TestConfig_Validate_EnabledRejectsBadValues captures the validation
// surface that gets exercised when the operator turns the compactor on.
func TestConfig_Validate_EnabledRejectsBadValues(t *testing.T) {
	t.Run("negative max_running_compaction_tasks", func(t *testing.T) {
		cfg := Config{
			Enabled:                   true,
			MaxRunningCompactionTasks: -1,
			Scheduler:                 SchedulerConfig{Endpoint: defaultEndpoint},
		}
		err := cfg.Validate()
		require.Error(t, err)
		require.ErrorIs(t, err, errInvalidMaxRunningCompactionTasks)
	})

	t.Run("empty scheduler endpoint", func(t *testing.T) {
		cfg := Config{
			Enabled:   true,
			Scheduler: SchedulerConfig{Endpoint: ""},
		}
		err := cfg.Validate()
		require.Error(t, err)
		require.True(t, errors.Is(err, errEmptySchedulerEndpoint))
	})

	t.Run("happy path", func(t *testing.T) {
		cfg := Config{
			Enabled:                   true,
			MaxRunningCompactionTasks: 16,
			Scheduler:                 SchedulerConfig{Endpoint: defaultEndpoint},
		}
		require.NoError(t, cfg.Validate())
	})
}

// TestNew_InvalidAdvertiseAddr exercises the constructor error path
// when an unparseable advertise address is supplied. Pins the error
// wrapping in resolveAdvertiseAddr.
func TestNew_InvalidAdvertiseAddr(t *testing.T) {
	cfg := Config{
		Enabled: true,
		Scheduler: SchedulerConfig{
			AdvertiseAddr: "not-a-valid-host:port:::",
			Endpoint:      defaultEndpoint,
		},
	}
	_, err := New(cfg, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "resolve scheduler advertise address",
		"error must mention the resolution step for operator clarity, got: %v", err)
}
