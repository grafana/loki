package retention

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func initAndFeedMarkerProcessor(t *testing.T, deleteWorkerCount int) *markerProcessor {
	t.Helper()
	minListMarkDelay = time.Second
	dir := t.TempDir()
	p, err := newMarkerStorageReader(dir, deleteWorkerCount, time.Second, sweepMetrics)
	require.NoError(t, err)
	go func() {
		w, err := NewMarkerStorageWriter(dir)
		require.NoError(t, err)

		require.NoError(t, w.Put([]byte("1")))
		require.NoError(t, w.Put([]byte("2")))
		require.NoError(t, w.Close())
		w, err = NewMarkerStorageWriter(dir)
		require.NoError(t, err)
		require.NoError(t, w.Put([]byte("3")))
		require.NoError(t, w.Put([]byte("4")))
		require.NoError(t, w.Close())
	}()
	return p
}

func Test_marlkerProcessor_Deadlock(t *testing.T) {
	dir := t.TempDir()
	p, err := newMarkerStorageReader(dir, 150, 0, sweepMetrics)
	require.NoError(t, err)
	w, err := NewMarkerStorageWriter(dir)
	require.NoError(t, err)
	for i := 0; i <= 2000; i++ {
		require.NoError(t, w.Put([]byte(fmt.Sprintf("%d", i))))
	}
	require.NoError(t, w.Close())
	paths, _, err := p.availablePath()
	require.NoError(t, err)
	for _, path := range paths {
		require.NoError(t, p.processPath(path, func(_ context.Context, _ []byte) error { return nil }))
		require.NoError(t, p.deleteEmptyMarks(path))
	}
	paths, _, err = p.availablePath()
	require.NoError(t, err)
	require.Len(t, paths, 0)
}

func Test_markerProcessor_StartRetryKey(t *testing.T) {
	p := initAndFeedMarkerProcessor(t, 5)
	defer p.Stop()
	counts := map[string]int{}
	l := sync.Mutex{}

	p.Start(func(_ context.Context, id []byte) error {
		l.Lock()
		defer l.Unlock()
		counts[string(id)]++
		return errors.New("don't delete")
	})

	require.Eventually(t, func() bool {
		l.Lock()
		defer l.Unlock()
		actual := []string{}
		expected := []string{"1", "2", "3", "4"}
		for k, v := range counts {
			if v <= 1 { // we expects value to come back more than once since we don't delete them.
				return false
			}
			actual = append(actual, k)
		}
		sort.Strings(actual)
		return assert.ObjectsAreEqual(expected, actual)
	}, 10*time.Second, 100*time.Microsecond)
}

func Test_markerProcessor_StartDeleteOnSuccess(t *testing.T) {
	p := initAndFeedMarkerProcessor(t, 5)
	defer p.Stop()
	counts := map[string]int{}
	l := sync.Mutex{}

	p.Start(func(_ context.Context, id []byte) error {
		l.Lock()
		defer l.Unlock()
		counts[string(id)]++
		return nil
	})

	require.Eventually(t, func() bool {
		l.Lock()
		defer l.Unlock()
		actual := []string{}
		expected := []string{"1", "2", "3", "4"}
		for k, v := range counts {
			if v > 1 { // we should see keys only once !
				return false
			}
			actual = append(actual, k)
		}
		sort.Strings(actual)
		return assert.ObjectsAreEqual(expected, actual)
	}, 10*time.Second, 100*time.Microsecond)
}

func Test_markerProcessor_availablePath(t *testing.T) {
	now := time.Now()
	for _, tt := range []struct {
		name     string
		expected func(dir string) ([]string, []time.Time)
	}{
		{"empty", func(_ string) ([]string, []time.Time) { return nil, nil }},
		{"skips bad files", func(dir string) ([]string, []time.Time) {
			_, _ = os.Create(filepath.Join(dir, "foo"))
			return nil, nil
		}},
		{"happy path", func(dir string) ([]string, []time.Time) {
			_, _ = os.Create(filepath.Join(dir, fmt.Sprintf("%d", now.UnixNano())))
			_, _ = os.Create(filepath.Join(dir, "foo"))
			_, _ = os.Create(filepath.Join(dir, fmt.Sprintf("%d", now.Add(-30*time.Minute).UnixNano())))
			_, _ = os.Create(filepath.Join(dir, fmt.Sprintf("%d", now.Add(-1*time.Hour).UnixNano())))
			_, _ = os.Create(filepath.Join(dir, fmt.Sprintf("%d", now.Add(-3*time.Hour).UnixNano())))
			_, _ = os.Create(filepath.Join(dir, fmt.Sprintf("%d", now.Add(-2*time.Hour).UnixNano())))
			_, _ = os.Create(filepath.Join(dir, fmt.Sprintf("%d", now.Add(-48*time.Hour).UnixNano())))
			return []string{
					filepath.Join(dir, fmt.Sprintf("%d", now.Add(-48*time.Hour).UnixNano())), // oldest should be first
					filepath.Join(dir, fmt.Sprintf("%d", now.Add(-3*time.Hour).UnixNano())),
					filepath.Join(dir, fmt.Sprintf("%d", now.Add(-2*time.Hour).UnixNano())),
				}, []time.Time{
					time.Unix(0, now.Add(-48*time.Hour).UnixNano()),
					time.Unix(0, now.Add(-3*time.Hour).UnixNano()),
					time.Unix(0, now.Add(-2*time.Hour).UnixNano()),
				}
		}},
	} {
		t.Run("", func(t *testing.T) {
			dir := t.TempDir()
			p, err := newMarkerStorageReader(dir, 5, 2*time.Hour, sweepMetrics)

			expectedPath, expectedTimes := tt.expected(p.folder)

			require.NoError(t, err)
			paths, times, err := p.availablePath()
			require.Nil(t, err)
			require.Equal(t, expectedPath, paths)
			require.Equal(t, expectedTimes, times)
		})
	}
}

func Test_MarkFileRotation(t *testing.T) {
	dir := t.TempDir()
	p, err := newMarkerStorageReader(dir, 150, 0, sweepMetrics)
	require.NoError(t, err)
	w, err := NewMarkerStorageWriter(dir)
	require.NoError(t, err)
	totalMarks := int64(2 * int(maxMarkPerFile))
	for i := int64(0); i < totalMarks; i++ {
		require.NoError(t, w.Put([]byte(fmt.Sprintf("%d", i))))
	}
	require.NoError(t, w.Close())
	paths, _, err := p.availablePath()
	require.NoError(t, err)

	require.Len(t, paths, 2)
	require.Equal(t, totalMarks, w.Count())
}
