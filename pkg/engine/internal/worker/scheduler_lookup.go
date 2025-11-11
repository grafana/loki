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
	"github.com/grafana/dskit/dns"
	"github.com/grafana/dskit/grpcutil"
)

type schedulerLookup struct {
	logger   log.Logger
	watcher  *dnsWatcher
	interval time.Duration
}

type handleScheduler func(ctx context.Context, addr net.Addr)

func newSchedulerLookup(logger log.Logger, address string, lookupInterval time.Duration) (*schedulerLookup, error) {
	if logger == nil {
		logger = log.NewNopLogger()
	}

	provider := dns.NewProvider(logger, nil, dns.GolangResolverType)

	return &schedulerLookup{
		logger:   logger,
		watcher:  newDNSWatcher(address, provider),
		interval: lookupInterval,
	}, nil
}

func (l *schedulerLookup) Run(ctx context.Context, handlerFunc handleScheduler) error {
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

	// Initially set our timer with no delay so we process the first update
	// immediately. We'll give it a real duration after each tick.
	timer := time.NewTimer(0)
	defer timer.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil

		case <-timer.C:
			timer.Reset(l.interval)

			updates, err := l.watcher.Poll(ctx)
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

type provider interface {
	Resolve(ctx context.Context, addrs []string) error
	Addresses() []string
}

type dnsWatcher struct {
	addr     string
	provider provider

	cached map[string]struct{}
}

func newDNSWatcher(addr string, provider provider) *dnsWatcher {
	return &dnsWatcher{
		addr:     addr,
		provider: provider,

		cached: make(map[string]struct{}),
	}
}

// Poll polls for changes in the DNS records.
func (w *dnsWatcher) Poll(ctx context.Context) ([]*grpcutil.Update, error) {
	if err := w.provider.Resolve(ctx, []string{w.addr}); err != nil {
		return nil, err
	}

	actual := w.discovered()

	var updates []*grpcutil.Update
	for addr := range actual {
		if _, exists := w.cached[addr]; exists {
			continue
		}

		w.cached[addr] = struct{}{}
		updates = append(updates, &grpcutil.Update{
			Addr: addr,
			Op:   grpcutil.Add,
		})
	}

	for addr := range w.cached {
		if _, exists := actual[addr]; !exists {
			delete(w.cached, addr)
			updates = append(updates, &grpcutil.Update{
				Addr: addr,
				Op:   grpcutil.Delete,
			})
		}
	}

	return updates, nil
}

func (w *dnsWatcher) discovered() map[string]struct{} {
	slice := w.provider.Addresses()

	res := make(map[string]struct{}, len(slice))
	for _, addr := range slice {
		res[addr] = struct{}{}
	}
	return res
}
