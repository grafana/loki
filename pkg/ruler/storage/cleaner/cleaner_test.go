// This directory was copied and adapted from https://github.com/grafana/agent/tree/main/pkg/metrics.
// We cannot vendor the agent in since the agent vendors loki in, which would cause a cyclic dependency.
// NOTE: many changes have been made to the original code for our use-case.
package cleaner

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/ruler/storage/instance"
)

func TestWALCleaner_getAllStorageNoRoot(t *testing.T) {
	walRoot := filepath.Join(os.TempDir(), "getAllStorageNoRoot")
	cleaner := newCleaner(walRoot, Config{})

	// Bogus WAL root that doesn't exist. Method should return no results
	wals := cleaner.getAllStorage()

	require.Empty(t, wals)
}

func TestWALCleaner_getAllStorageSuccess(t *testing.T) {
	walRoot := t.TempDir()

	walDir := filepath.Join(walRoot, "instance-1")
	err := os.MkdirAll(walDir, 0755)
	require.NoError(t, err)

	cleaner := newCleaner(walRoot, Config{})
	wals := cleaner.getAllStorage()

	require.Equal(t, []string{walDir}, wals)
}

func TestWALCleaner_getAbandonedStorageBeforeCutoff(t *testing.T) {
	walRoot := t.TempDir()

	walDir := filepath.Join(walRoot, "instance-1")
	err := os.MkdirAll(walDir, 0755)
	require.NoError(t, err)

	all := []string{walDir}
	managed := make(map[string]bool)
	now := time.Now()

	cleaner := newCleaner(walRoot, Config{})
	cleaner.walLastModified = func(_ string) (time.Time, error) {
		return now, nil
	}

	// Last modification time on our WAL directory is the same as "now"
	// so there shouldn't be any results even though it's not part of the
	// set of "managed" directories.
	abandoned := cleaner.getAbandonedStorage(all, managed, now)
	require.Empty(t, abandoned)
}

func TestWALCleaner_getAbandonedStorageAfterCutoff(t *testing.T) {
	walRoot := t.TempDir()

	walDir := filepath.Join(walRoot, "instance-1")
	err := os.MkdirAll(walDir, 0755)
	require.NoError(t, err)

	all := []string{walDir}
	managed := make(map[string]bool)
	now := time.Now()

	cleaner := newCleaner(walRoot, Config{
		MinAge: 5 * time.Minute,
	})

	cleaner.walLastModified = func(_ string) (time.Time, error) {
		return now.Add(-30 * time.Minute), nil
	}

	// Last modification time on our WAL directory is 30 minutes in the past
	// compared to "now" and we've set the cutoff for our cleaner to be 5
	// minutes: our WAL directory should show up as abandoned
	abandoned := cleaner.getAbandonedStorage(all, managed, now)
	require.Equal(t, []string{walDir}, abandoned)
}

func TestWALCleaner_cleanup(t *testing.T) {
	walRoot := t.TempDir()

	walDir := filepath.Join(walRoot, "instance-1")
	err := os.MkdirAll(walDir, 0755)
	require.NoError(t, err)

	now := time.Now()
	manager := &instance.MockManager{}
	manager.ListInstancesFunc = func() map[string]instance.ManagedInstance {
		return make(map[string]instance.ManagedInstance)
	}

	cleaner := newCleaner(walRoot, Config{
		MinAge: 5 * time.Minute,
	})
	cleaner.instanceManager = manager

	cleaner.walLastModified = func(_ string) (time.Time, error) {
		return now.Add(-30 * time.Minute), nil
	}

	// Last modification time on our WAL directory is 30 minutes in the past
	// compared to "now" and we've set the cutoff for our cleaner to be 5
	// minutes: our WAL directory should be removed since it's abandoned
	cleaner.cleanup()
	_, err = os.Stat(walDir)
	require.Error(t, err)
	require.True(t, os.IsNotExist(err))
}

func newCleaner(walRoot string, cfg Config) *WALCleaner {
	logger := log.NewLogfmtLogger(os.Stderr)
	cleaner := NewWALCleaner(
		logger,
		&instance.MockManager{},
		NewMetrics(nil),
		walRoot,
		cfg,
	)
	return cleaner
}
