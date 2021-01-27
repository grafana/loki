// Copyright (c) The Thanos Authors.
// Licensed under the Apache License 2.0.

package dns

import (
	"context"
	"net"
	"strings"
	"sync"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/thanos-io/thanos/pkg/discovery/dns/godns"
	"github.com/thanos-io/thanos/pkg/discovery/dns/miekgdns"
	"github.com/thanos-io/thanos/pkg/errutil"
	"github.com/thanos-io/thanos/pkg/extprom"
)

// Provider is a stateful cache for asynchronous DNS resolutions. It provides a way to resolve addresses and obtain them.
type Provider struct {
	sync.RWMutex
	resolver Resolver
	// A map from domain name to a slice of resolved targets.
	resolved map[string][]string
	logger   log.Logger

	resolverAddrs         *extprom.TxGaugeVec
	resolverLookupsCount  prometheus.Counter
	resolverFailuresCount prometheus.Counter
}

type ResolverType string

const (
	GolangResolverType   ResolverType = "golang"
	MiekgdnsResolverType ResolverType = "miekgdns"
)

func (t ResolverType) ToResolver(logger log.Logger) ipLookupResolver {
	var r ipLookupResolver
	switch t {
	case GolangResolverType:
		r = &godns.Resolver{Resolver: net.DefaultResolver}
	case MiekgdnsResolverType:
		r = &miekgdns.Resolver{ResolvConf: miekgdns.DefaultResolvConfPath}
	default:
		level.Warn(logger).Log("msg", "no such resolver type, defaulting to golang", "type", t)
		r = &godns.Resolver{Resolver: net.DefaultResolver}
	}
	return r
}

// NewProvider returns a new empty provider with a given resolver type.
// If empty resolver type is net.DefaultResolver.
func NewProvider(logger log.Logger, reg prometheus.Registerer, resolverType ResolverType) *Provider {
	p := &Provider{
		resolver: NewResolver(resolverType.ToResolver(logger), logger),
		resolved: make(map[string][]string),
		logger:   logger,
		resolverAddrs: extprom.NewTxGaugeVec(reg, prometheus.GaugeOpts{
			Name: "dns_provider_results",
			Help: "The number of resolved endpoints for each configured address",
		}, []string{"addr"}),
		resolverLookupsCount: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name: "dns_lookups_total",
			Help: "The number of DNS lookups resolutions attempts",
		}),
		resolverFailuresCount: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name: "dns_failures_total",
			Help: "The number of DNS lookup failures",
		}),
	}

	return p
}

// Clone returns a new provider from an existing one.
func (p *Provider) Clone() *Provider {
	return &Provider{
		resolver:              p.resolver,
		resolved:              make(map[string][]string),
		logger:                p.logger,
		resolverAddrs:         p.resolverAddrs,
		resolverLookupsCount:  p.resolverLookupsCount,
		resolverFailuresCount: p.resolverFailuresCount,
	}
}

// IsDynamicNode returns if the specified StoreAPI addr uses
// any kind of SD mechanism.
func IsDynamicNode(addr string) bool {
	qtype, _ := GetQTypeName(addr)
	return qtype != ""
}

// GetQTypeName splits the provided addr into two parts: the QType (if any)
// and the name.
func GetQTypeName(addr string) (qtype string, name string) {
	qtypeAndName := strings.SplitN(addr, "+", 2)
	if len(qtypeAndName) != 2 {
		return "", addr
	}
	return qtypeAndName[0], qtypeAndName[1]
}

// Resolve stores a list of provided addresses or their DNS records if requested.
// Addresses prefixed with `dns+` or `dnssrv+` will be resolved through respective DNS lookup (A/AAAA or SRV).
// For non-SRV records, it will return an error if a port is not supplied.
func (p *Provider) Resolve(ctx context.Context, addrs []string) error {
	resolvedAddrs := map[string][]string{}
	errs := errutil.MultiError{}

	for _, addr := range addrs {
		var resolved []string
		qtype, name := GetQTypeName(addr)
		if qtype == "" {
			resolvedAddrs[name] = []string{name}
			continue
		}

		resolved, err := p.resolver.Resolve(ctx, name, QType(qtype))
		p.resolverLookupsCount.Inc()
		if err != nil {
			// Append all the failed dns resolution in the error list.
			errs.Add(err)
			// The DNS resolution failed. Continue without modifying the old records.
			p.resolverFailuresCount.Inc()
			// Use cached values.
			p.RLock()
			resolved = p.resolved[addr]
			p.RUnlock()
		}
		resolvedAddrs[addr] = resolved
	}

	// All addresses have been resolved. We can now take an exclusive lock to
	// update the resolved addresses metric and update the local state.
	p.Lock()
	defer p.Unlock()

	p.resolverAddrs.ResetTx()
	for name, addrs := range resolvedAddrs {
		p.resolverAddrs.WithLabelValues(name).Set(float64(len(addrs)))
	}
	p.resolverAddrs.Submit()

	p.resolved = resolvedAddrs

	return errs.Err()
}

// Addresses returns the latest addresses present in the Provider.
func (p *Provider) Addresses() []string {
	p.RLock()
	defer p.RUnlock()

	var result []string
	for _, addrs := range p.resolved {
		result = append(result, addrs...)
	}
	return result
}
