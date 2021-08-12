package prom

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-kit/kit/log"
	"github.com/grafana/loki/pkg/ruler/prom/instance"
	"github.com/stretchr/testify/require"
)

func TestWALCleaner_getAllStorageNoRoot(t *testing.T) {
	walRoot := filepath.Join(os.TempDir(), "getAllStorageNoRoot")
	logger := log.NewLogfmtLogger(os.Stderr)
	cleaner := NewWALCleaner(
		logger,
		&instance.MockManager{},
		walRoot,
		DefaultCleanupAge,
		DefaultCleanupPeriod,
	)

	// Bogus WAL root that doesn't exist. Method should return no results
	wals := cleaner.getAllStorage()

	require.Empty(t, wals)
}

func TestWALCleaner_getAllStorageSuccess(t *testing.T) {
	walRoot, err := ioutil.TempDir(os.TempDir(), "getAllStorageSuccess")
	require.NoError(t, err)
	defer os.RemoveAll(walRoot)

	walDir := filepath.Join(walRoot, "instance-1")
	err = os.MkdirAll(walDir, 0755)
	require.NoError(t, err)

	logger := log.NewLogfmtLogger(os.Stderr)
	cleaner := NewWALCleaner(
		logger,
		&instance.MockManager{},
		walRoot,
		DefaultCleanupAge,
		DefaultCleanupPeriod,
	)
	wals := cleaner.getAllStorage()

	require.Equal(t, []string{walDir}, wals)
}

func TestWALCleaner_getAbandonedStorageBeforeCutoff(t *testing.T) {
	walRoot, err := ioutil.TempDir(os.TempDir(), "getAbandonedStorageBeforeCutoff")
	require.NoError(t, err)
	defer os.RemoveAll(walRoot)

	walDir := filepath.Join(walRoot, "instance-1")
	err = os.MkdirAll(walDir, 0755)
	require.NoError(t, err)

	all := []string{walDir}
	managed := make(map[string]bool)
	now := time.Now()

	logger := log.NewLogfmtLogger(os.Stderr)
	cleaner := NewWALCleaner(
		logger,
		&instance.MockManager{},
		walRoot,
		5*time.Minute,
		DefaultCleanupPeriod,
	)

	cleaner.walLastModified = func(path string) (time.Time, error) {
		return now, nil
	}

	// Last modification time on our WAL directory is the same as "now"
	// so there shouldn't be any results even though it's not part of the
	// set of "managed" directories.
	abandoned := cleaner.getAbandonedStorage(all, managed, now)
	require.Empty(t, abandoned)
}

func TestWALCleaner_getAbandonedStorageAfterCutoff(t *testing.T) {
	walRoot, err := ioutil.TempDir(os.TempDir(), "getAbandonedStorageAfterCutoff")
	require.NoError(t, err)
	defer os.RemoveAll(walRoot)

	walDir := filepath.Join(walRoot, "instance-1")
	err = os.MkdirAll(walDir, 0755)
	require.NoError(t, err)

	all := []string{walDir}
	managed := make(map[string]bool)
	now := time.Now()

	logger := log.NewLogfmtLogger(os.Stderr)
	cleaner := NewWALCleaner(
		logger,
		&instance.MockManager{},
		walRoot,
		5*time.Minute,
		DefaultCleanupPeriod,
	)

	cleaner.walLastModified = func(path string) (time.Time, error) {
		return now.Add(-30 * time.Minute), nil
	}

	// Last modification time on our WAL directory is 30 minutes in the past
	// compared to "now" and we've set the cutoff for our cleaner to be 5
	// minutes: our WAL directory should show up as abandoned
	abandoned := cleaner.getAbandonedStorage(all, managed, now)
	require.Equal(t, []string{walDir}, abandoned)
}

func TestWALCleaner_cleanup(t *testing.T) {
	walRoot, err := ioutil.TempDir(os.TempDir(), "cleanup")
	require.NoError(t, err)
	defer os.RemoveAll(walRoot)

	walDir := filepath.Join(walRoot, "instance-1")
	err = os.MkdirAll(walDir, 0755)
	require.NoError(t, err)

	now := time.Now()
	logger := log.NewLogfmtLogger(os.Stderr)
	manager := &instance.MockManager{}
	manager.ListInstancesFunc = func() map[string]instance.ManagedInstance {
		return make(map[string]instance.ManagedInstance)
	}

	cleaner := NewWALCleaner(
		logger,
		manager,
		walRoot,
		5*time.Minute,
		DefaultCleanupPeriod,
	)

	cleaner.walLastModified = func(path string) (time.Time, error) {
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
