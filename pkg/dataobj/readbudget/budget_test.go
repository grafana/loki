package readbudget_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/atomic"

	"github.com/grafana/loki/v3/pkg/dataobj/readbudget"
)

type reservation struct {
	lease *readbudget.Lease
	err   error
}

// reserveAsync starts a reservation on its own goroutine and returns the channel of its result.
func reserveAsync(ctx context.Context, b *readbudget.Budget, object string, section int, bytes int64) <-chan reservation {
	done := make(chan reservation, 1)
	go func() {
		lease, err := b.Section(object, section).Reserve(ctx, bytes)
		done <- reservation{lease: lease, err: err}
	}()
	return done
}

// requireAdmitted waits until every goroutine is blocked, then returns the lease of an admitted
// reservation.
func requireAdmitted(t *testing.T, done <-chan reservation) *readbudget.Lease {
	t.Helper()

	synctest.Wait()
	select {
	case r := <-done:
		require.NoError(t, r.err)
		return r.lease
	default:
		require.FailNow(t, "the reservation is still waiting")
		return nil
	}
}

// requireWaiting waits until every goroutine is blocked, then checks that the reservation waits.
func requireWaiting(t *testing.T, done <-chan reservation) {
	t.Helper()

	synctest.Wait()
	select {
	case <-done:
		require.FailNow(t, "the reservation was admitted")
	default:
	}
}

type observation struct {
	bytes  int64
	waited time.Duration
}

type recorder struct {
	mu        sync.Mutex
	admitted  []observation
	withdrawn []time.Duration
}

func (r *recorder) Admitted(bytes int64, waited time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.admitted = append(r.admitted, observation{bytes: bytes, waited: waited})
}

func (r *recorder) Withdrawn(waited time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.withdrawn = append(r.withdrawn, waited)
}

func TestBudget(t *testing.T) {
	t.Run("admits a reservation that fits without waiting", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			b := readbudget.New(100, nil)

			lease := requireAdmitted(t, reserveAsync(t.Context(), b, "object", 0, 60))

			require.NotNil(t, lease)
			require.Equal(t, int64(60), b.Held())
		})
	})

	t.Run("holds a reservation back until a release makes room", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			b := readbudget.New(100, nil)
			first := requireAdmitted(t, reserveAsync(t.Context(), b, "object", 0, 70))

			second := reserveAsync(t.Context(), b, "object", 1, 50)
			requireWaiting(t, second)

			first.Release(10)
			requireWaiting(t, second)

			first.Release(10)
			requireAdmitted(t, second)
			require.Equal(t, int64(100), b.Held())
		})
	})

	t.Run("admits a section larger than the limit when nothing is held", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			b := readbudget.New(100, nil)

			requireAdmitted(t, reserveAsync(t.Context(), b, "object", 0, 250))

			require.Equal(t, int64(250), b.Held())
		})
	})

	t.Run("holds back a section larger than the limit while another section holds bytes", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			b := readbudget.New(100, nil)
			small := requireAdmitted(t, reserveAsync(t.Context(), b, "object", 0, 10))

			large := reserveAsync(t.Context(), b, "object", 1, 250)
			requireWaiting(t, large)

			small.Close()
			requireAdmitted(t, large)
		})
	})

	t.Run("admits the sections of one object by index when bytes free up", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			b := readbudget.New(100, nil)
			holder := requireAdmitted(t, reserveAsync(t.Context(), b, "object", 9, 100))

			late := reserveAsync(t.Context(), b, "object", 3, 60)
			early := reserveAsync(t.Context(), b, "object", 1, 60)
			requireWaiting(t, late)
			requireWaiting(t, early)

			holder.Close()

			requireAdmitted(t, early)
			requireWaiting(t, late)
		})
	})

	t.Run("prefers the object that the budget saw first, whatever its name", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			b := readbudget.New(100, nil)
			holder := requireAdmitted(t, reserveAsync(t.Context(), b, "zulu", 9, 100))

			later := reserveAsync(t.Context(), b, "alpha", 0, 60)
			requireWaiting(t, later)
			earlier := reserveAsync(t.Context(), b, "zulu", 5, 60)
			requireWaiting(t, earlier)

			holder.Close()

			requireAdmitted(t, earlier)
			requireWaiting(t, later)
		})
	})

	t.Run("admits reservations for the same section in arrival order", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			b := readbudget.New(100, nil)
			holder := requireAdmitted(t, reserveAsync(t.Context(), b, "object", 9, 100))

			first := reserveAsync(t.Context(), b, "object", 3, 60)
			requireWaiting(t, first)
			second := reserveAsync(t.Context(), b, "object", 3, 60)
			requireWaiting(t, second)

			holder.Close()

			requireAdmitted(t, first)
			requireWaiting(t, second)
		})
	})

	t.Run("skips a waiter that does not fit and admits a smaller one behind it", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			b := readbudget.New(100, nil)
			holder := requireAdmitted(t, reserveAsync(t.Context(), b, "object", 0, 80))

			large := reserveAsync(t.Context(), b, "object", 1, 50)
			requireWaiting(t, large)
			small := reserveAsync(t.Context(), b, "object", 2, 10)
			requireAdmitted(t, small)
			requireWaiting(t, large)

			holder.Close()
			requireAdmitted(t, large)
		})
	})

	t.Run("returns the bytes of a lease when it closes, and ignores a second close", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			b := readbudget.New(100, nil)
			lease := requireAdmitted(t, reserveAsync(t.Context(), b, "object", 0, 60))
			other := requireAdmitted(t, reserveAsync(t.Context(), b, "object", 1, 30))

			lease.Close()
			lease.Close()

			require.Equal(t, int64(30), b.Held())
			other.Close()
			require.Equal(t, int64(0), b.Held())
		})
	})

	t.Run("releases no more than the lease holds", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			b := readbudget.New(100, nil)
			lease := requireAdmitted(t, reserveAsync(t.Context(), b, "object", 0, 50))
			other := requireAdmitted(t, reserveAsync(t.Context(), b, "object", 1, 40))

			lease.Release(80)
			lease.Release(-5)

			require.Equal(t, int64(40), b.Held())
			other.Close()
		})
	})

	t.Run("withdraws a waiter whose context ends and leaves the held bytes alone", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			b := readbudget.New(100, nil)
			holder := requireAdmitted(t, reserveAsync(t.Context(), b, "object", 0, 100))

			ctx, cancel := context.WithCancel(t.Context())
			waiting := reserveAsync(ctx, b, "object", 1, 50)
			requireWaiting(t, waiting)

			cancel()
			synctest.Wait()
			r := <-waiting
			require.ErrorIs(t, r.err, context.Canceled)
			require.Nil(t, r.lease)
			require.Equal(t, int64(100), b.Held())

			holder.Close()
			require.Equal(t, int64(0), b.Held())
		})
	})

	t.Run("returns the bytes of a waiter that the budget admits as its context ends", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			for range 64 {
				b := readbudget.New(100, nil)
				holder := requireAdmitted(t, reserveAsync(t.Context(), b, "object", 0, 100))

				ctx, cancel := context.WithCancel(t.Context())
				waiting := reserveAsync(ctx, b, "object", 1, 50)
				requireWaiting(t, waiting)

				cancel()
				holder.Close()
				synctest.Wait()

				if r := <-waiting; r.lease != nil {
					r.lease.Close()
				}
				require.Zero(t, b.Held())
			}
		})
	})

	t.Run("admits the waiter behind one that withdrew", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			b := readbudget.New(100, nil)
			holder := requireAdmitted(t, reserveAsync(t.Context(), b, "object", 0, 60))

			ctx, cancel := context.WithCancel(t.Context())
			withdrawn := reserveAsync(ctx, b, "object", 1, 50)
			requireWaiting(t, withdrawn)
			cancel()
			synctest.Wait()
			require.ErrorIs(t, (<-withdrawn).err, context.Canceled)

			holder.Close()
			requireAdmitted(t, reserveAsync(t.Context(), b, "object", 2, 100))
		})
	})

	t.Run("admits every waiter that fits when one release frees room for several", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			b := readbudget.New(100, nil)
			holder := requireAdmitted(t, reserveAsync(t.Context(), b, "object", 0, 100))
			first := reserveAsync(t.Context(), b, "object", 1, 30)
			second := reserveAsync(t.Context(), b, "object", 2, 30)
			third := reserveAsync(t.Context(), b, "object", 3, 30)
			requireWaiting(t, first)
			requireWaiting(t, second)
			requireWaiting(t, third)

			holder.Close()

			requireAdmitted(t, first)
			requireAdmitted(t, second)
			requireAdmitted(t, third)
			require.Equal(t, int64(90), b.Held())
		})
	})

	t.Run("fails with an error that matches the context error and keeps its cause", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			errTimeout := errors.New("query timeout reached")
			b := readbudget.New(100, nil)
			requireAdmitted(t, reserveAsync(t.Context(), b, "object", 0, 100))

			ctx, cancel := context.WithTimeoutCause(t.Context(), time.Second, errTimeout)
			defer cancel()
			waiting := reserveAsync(ctx, b, "object", 1, 50)
			requireWaiting(t, waiting)
			time.Sleep(2 * time.Second)
			synctest.Wait()

			r := <-waiting
			require.ErrorIs(t, r.err, context.DeadlineExceeded)
			require.ErrorIs(t, r.err, errTimeout)

			_, err := b.Section("object", 2).Reserve(ctx, 10)
			require.ErrorIs(t, err, context.DeadlineExceeded)
			require.ErrorIs(t, err, errTimeout)
		})
	})

	t.Run("treats a negative reservation as zero bytes", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			b := readbudget.New(100, nil)
			requireAdmitted(t, reserveAsync(t.Context(), b, "object", 0, 60))

			requireAdmitted(t, reserveAsync(t.Context(), b, "object", 1, -40))

			require.Equal(t, int64(60), b.Held())
		})
	})

	t.Run("fails at once when the context has already ended", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			b := readbudget.New(100, nil)
			ctx, cancel := context.WithCancel(t.Context())
			cancel()

			lease, err := b.Section("object", 0).Reserve(ctx, 10)

			require.ErrorIs(t, err, context.Canceled)
			require.Nil(t, lease)
			require.Equal(t, int64(0), b.Held())
		})
	})

	t.Run("never blocks a reservation of zero bytes", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			b := readbudget.New(100, nil)
			requireAdmitted(t, reserveAsync(t.Context(), b, "object", 0, 250))
			requireAdmitted(t, reserveAsync(t.Context(), b, "object", 1, 0))
		})
	})

	t.Run("admits one section at a time when the limit is below one byte", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			b := readbudget.New(0, nil)
			first := requireAdmitted(t, reserveAsync(t.Context(), b, "object", 0, 10))

			second := reserveAsync(t.Context(), b, "object", 1, 10)
			requireWaiting(t, second)

			first.Close()
			requireAdmitted(t, second)
		})
	})

	t.Run("does nothing for a nil budget, a nil lease or a zero lease", func(t *testing.T) {
		var b *readbudget.Budget
		require.Nil(t, b.Section("object", 0))
		require.Zero(t, b.Held())

		var lease *readbudget.Lease
		lease.Release(10)
		lease.Close()

		zero := new(readbudget.Lease)
		zero.Release(10)
		zero.Close()
	})

	t.Run("reports each admitted reservation with the time it waited", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			rec := &recorder{}
			b := readbudget.New(100, rec)
			holder := requireAdmitted(t, reserveAsync(t.Context(), b, "object", 0, 100))

			waiting := reserveAsync(t.Context(), b, "object", 1, 40)
			requireWaiting(t, waiting)
			time.Sleep(3 * time.Second)
			holder.Close()
			requireAdmitted(t, waiting)

			require.Equal(t, []observation{
				{bytes: 100, waited: 0},
				{bytes: 40, waited: 3 * time.Second},
			}, rec.admitted)
			require.Empty(t, rec.withdrawn)
		})
	})

	t.Run("reports a reservation that gave up with the time it waited", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			rec := &recorder{}
			b := readbudget.New(100, rec)
			requireAdmitted(t, reserveAsync(t.Context(), b, "object", 0, 100))

			ctx, cancel := context.WithCancel(t.Context())
			waiting := reserveAsync(ctx, b, "object", 1, 40)
			requireWaiting(t, waiting)
			time.Sleep(5 * time.Second)
			cancel()
			synctest.Wait()

			require.Equal(t, []time.Duration{5 * time.Second}, rec.withdrawn)
			require.Len(t, rec.admitted, 1, "only the first reservation was admitted")
		})
	})

	t.Run("never holds more than the limit when every section fits", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			const limit = 100
			b := readbudget.New(limit, nil)

			var inFlight, peak atomic.Int64
			var wg sync.WaitGroup
			errs := make(chan error, 40)
			for i := range 40 {
				wg.Add(1)
				go func() {
					defer wg.Done()

					bytes := int64(10 + i%5*10)
					lease, err := b.Section("object", i).Reserve(t.Context(), bytes)
					if err != nil {
						errs <- err
						return
					}

					now := inFlight.Add(bytes)
					for {
						old := peak.Load()
						if now <= old || peak.CompareAndSwap(old, now) {
							break
						}
					}
					time.Sleep(time.Duration(1+i%3) * time.Millisecond)
					inFlight.Add(-bytes / 2)
					lease.Release(bytes / 2)
					time.Sleep(time.Millisecond)
					inFlight.Add(-(bytes - bytes/2))
					lease.Close()
				}()
			}
			wg.Wait()

			close(errs)
			for err := range errs {
				require.NoError(t, err)
			}
			require.LessOrEqual(t, peak.Load(), int64(limit))
			require.Equal(t, int64(0), b.Held())
		})
	})
}
