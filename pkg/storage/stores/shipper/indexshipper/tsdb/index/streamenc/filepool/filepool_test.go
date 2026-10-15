// SPDX-License-Identifier: AGPL-3.0-only

package filepool

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

func newTestFile(t testing.TB) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "file")
	require.NoError(t, os.WriteFile(path, make([]byte, 4096), 0o600))
	return path
}

// isClosed reports whether f has been closed. It does not close f.
func isClosed(f *os.File) bool {
	_, err := f.Stat()
	return err != nil
}

func TestFilePool_ReusesHandles(t *testing.T) {
	p := NewFilePool(newTestFile(t), 4, NewFilePoolMetrics(nil))
	t.Cleanup(p.Stop)

	f1, err := p.Get()
	require.NoError(t, err)
	require.NoError(t, p.Put(f1))
	require.False(t, isClosed(f1), "handle returned to a pool with space must stay open")

	f2, err := p.Get()
	require.NoError(t, err)
	require.Same(t, f1, f2, "Get must reuse the pooled handle")
	require.NoError(t, p.Put(f2))
}

func TestFilePool_ZeroCapacity(t *testing.T) {
	p := NewFilePool(newTestFile(t), 0, NewFilePoolMetrics(nil))
	t.Cleanup(p.Stop)

	f1, err := p.Get()
	require.NoError(t, err)
	require.NoError(t, p.Put(f1))
	require.True(t, isClosed(f1), "a zero-capacity pool must close every handle on Put")

	f2, err := p.Get()
	require.NoError(t, err)
	require.NotSame(t, f1, f2)
	require.NoError(t, p.Put(f2))
}

func TestFilePool_FullPoolClosesHandle(t *testing.T) {
	const capacity = 2
	p := NewFilePool(newTestFile(t), capacity, NewFilePoolMetrics(nil))
	t.Cleanup(p.Stop)

	// Nothing is pooled yet, so these are all newly opened handles.
	var handles []*os.File
	for range capacity + 1 {
		f, err := p.Get()
		require.NoError(t, err)
		handles = append(handles, f)
	}
	for _, f := range handles {
		require.NoError(t, p.Put(f))
	}

	pooled := map[*os.File]bool{}
	for _, f := range handles {
		if !isClosed(f) {
			pooled[f] = true
		}
	}
	require.Len(t, pooled, capacity, "the pool must keep exactly capacity handles and close the rest")

	// The next capacity Gets return the pooled handles; the one after opens
	// a new handle.
	var got []*os.File
	for range capacity {
		f, err := p.Get()
		require.NoError(t, err)
		require.True(t, pooled[f], "expected a pooled handle")
		delete(pooled, f)
		got = append(got, f)
	}
	f, err := p.Get()
	require.NoError(t, err)
	for _, h := range handles {
		require.NotSame(t, h, f)
	}
	for _, f := range append(got, f) {
		require.NoError(t, p.Put(f))
	}
}

func TestFilePool_Stop(t *testing.T) {
	p := NewFilePool(newTestFile(t), 4, NewFilePoolMetrics(nil))

	idle, err := p.Get()
	require.NoError(t, err)
	inUse, err := p.Get()
	require.NoError(t, err)
	require.NoError(t, p.Put(idle))

	p.Stop()
	require.True(t, isClosed(idle), "Stop must close idle handles")
	require.False(t, isClosed(inUse), "Stop must not close handles that are in use")

	_, err = p.Get()
	require.ErrorIs(t, err, ErrPoolStopped)

	require.NoError(t, p.Put(inUse))
	require.True(t, isClosed(inUse), "Put after Stop must close the handle")
}

// TestFilePool_ConcurrentStop stops the pool while many goroutines are
// repeatedly running Get/Put.
// We expect all files to end up closed, and no errors to occur.
func TestFilePool_ConcurrentStop(t *testing.T) {
	path := newTestFile(t)

	for range 50 {
		p := NewFilePool(path, 4, NewFilePoolMetrics(nil))

		const workers = 16
		var (
			mtx     sync.Mutex
			handles = map[*os.File]struct{}{}
			wg      sync.WaitGroup
			// started is done once every worker has completed a Get/Put, so
			// that Stop runs while all workers are mid-loop.
			started sync.WaitGroup
			errs    = make(chan error, workers)
		)
		started.Add(workers)
		for range workers {
			wg.Add(1)
			go func() {
				defer wg.Done()
				signalStarted := sync.OnceFunc(started.Done)
				defer signalStarted()
				for {
					f, err := p.Get()
					if err != nil {
						if !errors.Is(err, ErrPoolStopped) {
							errs <- err
						}
						return
					}
					mtx.Lock()
					handles[f] = struct{}{}
					mtx.Unlock()
					if err := p.Put(f); err != nil {
						errs <- err
						return
					}
					signalStarted()
				}
			}()
		}

		started.Wait()
		p.Stop()
		wg.Wait()
		close(errs)

		for err := range errs {
			require.NoError(t, err)
		}

		require.NotEmpty(t, handles)
		for f := range handles {
			require.True(t, isClosed(f), "handle left open after Stop")
		}
	}
}
