package worker

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"sync"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/grpcutil"
)

type schedulerLookup struct {
	// logger to log messages with.
	logger log.Logger

	// Watcher emits events when schedulers are found. Each found address must
	// be an IP address.
	watcher grpcutil.Watcher

	closeOnce sync.Once
}

type handleScheduler func(ctx context.Context, addr net.Addr)

func newSchedulerLookup(logger log.Logger, address string, lookupInterval time.Duration) (*schedulerLookup, error) {
	if logger == nil {
		logger = log.NewNopLogger()
	}

	resolver, err := grpcutil.NewDNSResolverWithFreq(lookupInterval, logger)
	if err != nil {
		return nil, fmt.Errorf("creating DNS resolver: %w", err)
	}

	watcher, err := resolver.Resolve(address, "")
	if err != nil {
		return nil, fmt.Errorf("creating DNS watcher: %w", err)
	}

	return &schedulerLookup{
		logger:  logger,
		watcher: watcher,
	}, nil
}

func (l *schedulerLookup) Run(ctx context.Context, handlerFunc handleScheduler) error {
	// Hook into context cancellation to close the watcher. We need to do this
	// because the watcher doesn't accept a custom context when polling for
	// changes.
	stop := context.AfterFunc(ctx, l.closeWatcher)
	defer stop()

	var handlerWg sync.WaitGroup
	defer handlerWg.Wait()

	type handlerContext struct {
		Context context.Context
		Cancel  context.CancelFunc
	}
	handlers := make(map[string]handlerContext)

	// Create a new context so we can cancel all handlers at once.
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	for {
		updates, err := l.watcher.Next()
		if err != nil && ctx.Err() == nil {
			return fmt.Errorf("finding schedulers: %w", err)
		} else if ctx.Err() != nil {
			// The context was canceled, we can exit gracefully.
			return nil
		}

		for _, update := range updates {
			switch update.Op {
			case grpcutil.Add:
				if _, exist := handlers[update.Addr]; exist {
					// Ignore duplicate handlers.
					level.Warn(l.logger).Log("msg", "ignoring duplicate scheduler", "addr", update.Addr)
					continue
				}

				addr, err := parseTCPAddr(update.Addr)
				if err != nil {
					level.Warn(l.logger).Log("msg", "failed to parse scheduler address", "addr", update.Addr, "err", err)
					continue
				}

				var handler handlerContext
				handler.Context, handler.Cancel = context.WithCancel(ctx)
				handlers[update.Addr] = handler

				handlerWg.Add(1)
				go func() {
					defer handlerWg.Done()
					handlerFunc(handler.Context, addr)
				}()

			case grpcutil.Delete:
				handler, exist := handlers[update.Addr]
				if !exist {
					level.Warn(l.logger).Log("msg", "ignoring unrecognized scheduler", "addr", update.Addr)
					continue
				}
				handler.Cancel()
				delete(handlers, update.Addr)

			default:
				level.Warn(l.logger).Log("msg", "unknown scheduler update operation", "op", update.Op)
			}
		}
	}
}

func (l *schedulerLookup) closeWatcher() {
	l.closeOnce.Do(func() { l.watcher.Close() })
}

// parseTCPAddr parses a TCP address string into a [net.TCPAddr]. It doesn't do
// any name resolution: the addr must be a numeric pair of IP and port.
func parseTCPAddr(addr string) (*net.TCPAddr, error) {
	ap, err := netip.ParseAddrPort(addr)
	if err != nil {
		return nil, err
	}

	return net.TCPAddrFromAddrPort(ap), nil
}
