package worker

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/grpcutil"
	"github.com/stretchr/testify/require"
	"go.uber.org/atomic"
)

func Test_schedulerLookup(t *testing.T) {
	var wg sync.WaitGroup
	defer wg.Wait()

	fw := &fakeWatcher{
		ctx: t.Context(),
		ch:  make(chan *grpcutil.Update, 1),
	}

	// Manually create a schedulerLookup so we can hook in a custom
	// implementation of [grpcutil.Watcher].
	disc := &schedulerLookup{
		logger:  log.NewNopLogger(),
		watcher: fw,
	}

	var handlers atomic.Int64

	lookupContext, lookupCancel := context.WithCancel(t.Context())
	defer lookupCancel()

	wg.Add(1)
	go func() {
		// Decrement the wait group once Run exits. Run won't exit until all
		// handlers have terminated, so this validates that logic.
		defer wg.Done()

		_ = disc.Run(lookupContext, func(ctx context.Context, _ net.Addr) {
			context.AfterFunc(ctx, func() { handlers.Dec() })
			handlers.Inc()
		})
	}()

	// Emit 10 schedulers, then wait for there to be one handler per
	// scheduler.
	for i := range 10 {
		addr := fmt.Sprintf("127.0.0.%d:8080", i+1)
		fw.ch <- &grpcutil.Update{Op: grpcutil.Add, Addr: addr}
	}

	require.Eventually(t, func() bool {
		return handlers.Load() == 10
	}, time.Minute, time.Millisecond*10, "should have 10 running handlers, ended with %d", handlers.Load())

	// Delete all the schedulers, then wait for all handlers to terminate (by
	// context).
	for i := range 10 {
		addr := fmt.Sprintf("127.0.0.%d:8080", i+1)
		fw.ch <- &grpcutil.Update{Op: grpcutil.Delete, Addr: addr}
	}

	require.Eventually(t, func() bool {
		return handlers.Load() == 0
	}, time.Minute, time.Millisecond*10, "should have no handlers running, ended with %d", handlers.Load())
}

type fakeWatcher struct {
	ctx context.Context
	ch  chan *grpcutil.Update
}

func (fw fakeWatcher) Next() ([]*grpcutil.Update, error) {
	select {
	case <-fw.ctx.Done():
		return nil, fw.ctx.Err()
	case update, ok := <-fw.ch:
		if !ok {
			return nil, errors.New("closed")
		}
		return []*grpcutil.Update{update}, nil
	}
}

func (fw fakeWatcher) Close() { close(fw.ch) }
