package discovery

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	dskitdns "github.com/grafana/dskit/dns"
	"github.com/prometheus/client_golang/prometheus"
)

type DNS interface {
	Addresses() []string
	Stop()
}

type dns struct {
	logger        log.Logger
	cleanupPeriod time.Duration
	addresses     []string
	stop          chan struct{}
	done          sync.WaitGroup
	once          sync.Once
	dnsProvider   *dskitdns.Provider
}

func NewDNS(logger log.Logger, cleanupPeriod time.Duration, address string, reg prometheus.Registerer) DNS {
	dnsProvider := dskitdns.NewProvider(dskitdns.GolangResolverType, 0, logger, reg)
	d := &dns{
		logger:        logger,
		cleanupPeriod: cleanupPeriod,
		addresses:     strings.Split(address, ","),
		stop:          make(chan struct{}),
		done:          sync.WaitGroup{},
		dnsProvider:   dnsProvider,
	}
	d.runDiscovery() // Make an attempt to do one DNS lookup so we can start with addresses
	go d.discoveryLoop()
	d.done.Add(1)
	return d
}

func (d *dns) Addresses() []string {
	return d.dnsProvider.Addresses()
}

func (d *dns) Stop() {
	// Integration tests were calling Stop() multiple times, so we need to make sure
	// that we only close the stop channel once.
	d.once.Do(func() { close(d.stop) })
	d.done.Wait()
}

func (d *dns) discoveryLoop() {
	ticker := time.NewTicker(d.cleanupPeriod)
	defer func() {
		ticker.Stop()
		d.done.Done()
	}()

	for {
		select {
		case <-ticker.C:
			d.runDiscovery()
		case <-d.stop:
			return
		}
	}
}

func (d *dns) runDiscovery() {
	ctx, cancel := context.WithTimeoutCause(context.Background(), 5*time.Second, fmt.Errorf("DNS lookup timeout: %v", d.addresses))
	defer cancel()
	err := d.dnsProvider.Resolve(ctx, d.addresses)
	if err != nil {
		level.Error(d.logger).Log("msg", "failed to resolve server addresses", "err", err)
	}
}
