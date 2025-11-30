package worker

import (
	"context"
	"net"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/go-kit/log"
	"github.com/stretchr/testify/require"
	"go.uber.org/atomic"
)

func Test_schedulerLookup(t *testing.T) {
	// NOTE(rfratto): synctest makes it possible to reliably test asynchronous
	// code with time.Sleep.
	synctest.Test(t, func(t *testing.T) {
		var wg sync.WaitGroup
		defer wg.Wait()

		// Provide 10 addresses to start with.
		addrs := []string{
			"127.0.0.1:8080", "127.0.0.2:8080", "127.0.0.3:8080", "127.0.0.4:8080", "127.0.0.5:8080",
			"127.0.0.6:8080", "127.0.0.7:8080", "127.0.0.8:8080", "127.0.0.9:8080", "127.0.0.10:8080",
		}

		fr := &fakeProvider{
			resolveFunc: func(_ context.Context, _ []string) ([]string, error) { return addrs, nil },
		}

		// Manually create a schedulerLookup so we can hook in a custom
		// implementation of [grpcutil.Watcher].
		disc := &schedulerLookup{
			logger:   log.NewNopLogger(),
			watcher:  newDNSWatcher("example.com", fr),
			interval: 1 * time.Minute,
		}

		var handlers atomic.Int64

		lookupContext, lookupCancel := context.WithCancel(t.Context())
		defer lookupCancel()

		wg.Go(func() {
			_ = disc.Run(lookupContext, func(ctx context.Context, _ net.Addr) {
				context.AfterFunc(ctx, func() { handlers.Dec() })
				handlers.Inc()
			})
		})

		// There should immediately be running handlers without needing to wait
		// for the discovery interval.
		synctest.Wait()
		require.Equal(t, int64(10), handlers.Load(), "should have 10 running handlers")

		// Remove all the addresses from discovery; after the next interval, all
		// handlers should be removed.
		addrs = addrs[:0]
		time.Sleep(disc.interval + time.Second)
		require.Equal(t, int64(0), handlers.Load(), "should have no running handlers")
	})
}

type fakeProvider struct {
	resolveFunc func(ctx context.Context, addrs []string) ([]string, error)

	cached []string
}

func (fp *fakeProvider) Resolve(ctx context.Context, addrs []string) error {
	resolved, err := fp.resolveFunc(ctx, addrs)
	if err != nil {
		return err
	}
	fp.cached = resolved
	return nil
}

func (fp *fakeProvider) Addresses() []string {
	return fp.cached
}
