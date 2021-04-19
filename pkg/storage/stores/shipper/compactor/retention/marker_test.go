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

func initAndFeedMarkerProcessor(t *testing.T) *markerProcessor {
	t.Helper()
	minListMarkDelay = time.Second
	dir := t.TempDir()
	p, err := newMarkerStorageReader(dir, 5, time.Second)
	require.NoError(t, err)
	defer p.Stop()
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

func Test_markerProcessor_StartRetryKey(t *testing.T) {
	p := initAndFeedMarkerProcessor(t)
	counts := map[string]int{}
	l := sync.Mutex{}

	p.Start(func(ctx context.Context, id []byte) error {
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
	p := initAndFeedMarkerProcessor(t)
	counts := map[string]int{}
	l := sync.Mutex{}

	p.Start(func(ctx context.Context, id []byte) error {
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
		name         string
		expectedPath func(dir string) []string
	}{
		{"empty", func(_ string) []string { return nil }},
		{"skips bad files", func(dir string) []string {
			_, _ = os.Create(filepath.Join(dir, "foo"))
			return nil
		}},
		{"happy path", func(dir string) []string {
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
			}
		}},
	} {
		t.Run("", func(t *testing.T) {
			dir := t.TempDir()
			p, err := newMarkerStorageReader(dir, 5, 2*time.Hour)

			expected := tt.expectedPath(p.folder)

			require.NoError(t, err)
			paths, err := p.availablePath()
			require.Nil(t, err)
			require.Equal(t, expected, paths)
		})
	}
}
