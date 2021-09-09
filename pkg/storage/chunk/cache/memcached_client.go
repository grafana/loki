package cache

import (
	"context"
	"flag"
	"fmt"
	"net"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/bradfitz/gomemcache/memcache"
	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/grafana/dskit/dslog"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/sony/gobreaker"
	"github.com/thanos-io/thanos/pkg/discovery/dns"
)

// MemcachedClient interface exists for mocking memcacheClient.
type MemcachedClient interface {
	GetMulti(keys []string) (map[string]*memcache.Item, error)
	Set(item *memcache.Item) error
}

type serverSelector interface {
	memcache.ServerSelector
	SetServers(servers ...string) error
}

// memcachedClient is a memcache client that gets its server list from SRV
// records, and periodically updates that ServerList.
type memcachedClient struct {
	sync.Mutex
	name string
	*memcache.Client
	serverList serverSelector

	hostname string
	service  string

	addresses []string
	provider  *dns.Provider

	cbs        map[ /*address*/ string]*gobreaker.CircuitBreaker
	cbFailures uint
	cbTimeout  time.Duration
	cbInterval time.Duration

	maxItemSize int

	quit chan struct{}
	wait sync.WaitGroup

	numServers prometheus.Gauge
	skipped    prometheus.Counter

	logger log.Logger
}

// MemcachedClientConfig defines how a MemcachedClient should be constructed.
type MemcachedClientConfig struct {
	Host           string        `yaml:"host"`
	Service        string        `yaml:"service"`
	Addresses      string        `yaml:"addresses"` // EXPERIMENTAL.
	Timeout        time.Duration `yaml:"timeout"`
	MaxIdleConns   int           `yaml:"max_idle_conns"`
	MaxItemSize    int           `yaml:"max_item_size"`
	UpdateInterval time.Duration `yaml:"update_interval"`
	ConsistentHash bool          `yaml:"consistent_hash"`
	CBFailures     uint          `yaml:"circuit_breaker_consecutive_failures"`
	CBTimeout      time.Duration `yaml:"circuit_breaker_timeout"`  // reset error count after this long
	CBInterval     time.Duration `yaml:"circuit_breaker_interval"` // remain closed for this long after CBFailures errors
}

// RegisterFlagsWithPrefix adds the flags required to config this to the given FlagSet
func (cfg *MemcachedClientConfig) RegisterFlagsWithPrefix(prefix, description string, f *flag.FlagSet) {
	f.StringVar(&cfg.Host, prefix+"memcached.hostname", "", description+"Hostname for memcached service to use. If empty and if addresses is unset, no memcached will be used.")
	f.StringVar(&cfg.Service, prefix+"memcached.service", "memcached", description+"SRV service used to discover memcache servers.")
	f.StringVar(&cfg.Addresses, prefix+"memcached.addresses", "", description+"EXPERIMENTAL: Comma separated addresses list in DNS Service Discovery format: https://cortexmetrics.io/docs/configuration/arguments/#dns-service-discovery")
	f.IntVar(&cfg.MaxIdleConns, prefix+"memcached.max-idle-conns", 16, description+"Maximum number of idle connections in pool.")
	f.DurationVar(&cfg.Timeout, prefix+"memcached.timeout", 100*time.Millisecond, description+"Maximum time to wait before giving up on memcached requests.")
	f.DurationVar(&cfg.UpdateInterval, prefix+"memcached.update-interval", 1*time.Minute, description+"Period with which to poll DNS for memcache servers.")
	f.BoolVar(&cfg.ConsistentHash, prefix+"memcached.consistent-hash", true, description+"Use consistent hashing to distribute to memcache servers.")
	f.UintVar(&cfg.CBFailures, prefix+"memcached.circuit-breaker-consecutive-failures", 10, description+"Trip circuit-breaker after this number of consecutive dial failures (if zero then circuit-breaker is disabled).")
	f.DurationVar(&cfg.CBTimeout, prefix+"memcached.circuit-breaker-timeout", 10*time.Second, description+"Duration circuit-breaker remains open after tripping (if zero then 60 seconds is used).")
	f.DurationVar(&cfg.CBInterval, prefix+"memcached.circuit-breaker-interval", 10*time.Second, description+"Reset circuit-breaker counts after this long (if zero then never reset).")
	f.IntVar(&cfg.MaxItemSize, prefix+"memcached.max-item-size", 0, description+"The maximum size of an item stored in memcached. Bigger items are not stored. If set to 0, no maximum size is enforced.")
}

// NewMemcachedClient creates a new MemcacheClient that gets its server list
// from SRV and updates the server list on a regular basis.
func NewMemcachedClient(cfg MemcachedClientConfig, name string, r prometheus.Registerer, logger log.Logger) MemcachedClient {
	var selector serverSelector
	if cfg.ConsistentHash {
		selector = &MemcachedJumpHashSelector{}
	} else {
		selector = &memcache.ServerList{}
	}

	client := memcache.NewFromSelector(selector)
	client.Timeout = cfg.Timeout
	client.MaxIdleConns = cfg.MaxIdleConns

	dnsProviderRegisterer := prometheus.WrapRegistererWithPrefix("cortex_", prometheus.WrapRegistererWith(prometheus.Labels{
		"name": name,
	}, r))

	newClient := &memcachedClient{
		name:        name,
		Client:      client,
		serverList:  selector,
		hostname:    cfg.Host,
		service:     cfg.Service,
		logger:      logger,
		provider:    dns.NewProvider(logger, dnsProviderRegisterer, dns.GolangResolverType),
		cbs:         make(map[string]*gobreaker.CircuitBreaker),
		cbFailures:  cfg.CBFailures,
		cbInterval:  cfg.CBInterval,
		cbTimeout:   cfg.CBTimeout,
		maxItemSize: cfg.MaxItemSize,
		quit:        make(chan struct{}),

		numServers: promauto.With(r).NewGauge(prometheus.GaugeOpts{
			Namespace:   "loki",
			Name:        "memcache_client_servers",
			Help:        "The number of memcache servers discovered.",
			ConstLabels: prometheus.Labels{"name": name},
		}),

		skipped: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Namespace:   "loki",
			Name:        "memcache_client_set_skip_total",
			Help:        "Total number of skipped set operations because of the value is larger than the max-item-size.",
			ConstLabels: prometheus.Labels{"name": name},
		}),
	}
	if cfg.CBFailures > 0 {
		newClient.Client.DialTimeout = newClient.dialViaCircuitBreaker
	}

	if len(cfg.Addresses) > 0 {
		dslog.WarnExperimentalUse("DNS-based memcached service discovery", logger)
		newClient.addresses = strings.Split(cfg.Addresses, ",")
	}

	err := newClient.updateMemcacheServers()
	if err != nil {
		level.Error(logger).Log("msg", "error setting memcache servers to host", "host", cfg.Host, "err", err)
	}

	newClient.wait.Add(1)
	go newClient.updateLoop(cfg.UpdateInterval)
	return newClient
}

func (c *memcachedClient) circuitBreakerStateChange(name string, from gobreaker.State, to gobreaker.State) {
	level.Info(c.logger).Log("msg", "circuit-breaker state change", "name", name, "from-state", from, "to-state", to)
}

func (c *memcachedClient) dialViaCircuitBreaker(network, address string, timeout time.Duration) (net.Conn, error) {
	c.Lock()
	cb := c.cbs[address]
	if cb == nil {
		cb = gobreaker.NewCircuitBreaker(gobreaker.Settings{
			Name:          c.name + ":" + address,
			Interval:      c.cbInterval,
			Timeout:       c.cbTimeout,
			OnStateChange: c.circuitBreakerStateChange,
			ReadyToTrip: func(counts gobreaker.Counts) bool {
				return uint(counts.ConsecutiveFailures) > c.cbFailures
			},
		})
		c.cbs[address] = cb
	}
	c.Unlock()

	conn, err := cb.Execute(func() (interface{}, error) {
		return net.DialTimeout(network, address, timeout)
	})
	if err != nil {
		return nil, err
	}
	return conn.(net.Conn), nil
}

// Stop the memcache client.
func (c *memcachedClient) Stop() {
	close(c.quit)
	c.wait.Wait()
}

func (c *memcachedClient) Set(item *memcache.Item) error {
	// Skip hitting memcached at all if the item is bigger than the max allowed size.
	if c.maxItemSize > 0 && len(item.Value) > c.maxItemSize {
		c.skipped.Inc()
		return nil
	}

	err := c.Client.Set(item)
	if err == nil {
		return nil
	}

	// Inject the server address in order to have more information about which memcached
	// backend server failed. This is a best effort.
	addr, addrErr := c.serverList.PickServer(item.Key)
	if addrErr != nil {
		return err
	}

	return errors.Wrapf(err, "server=%s", addr)
}

func (c *memcachedClient) updateLoop(updateInterval time.Duration) {
	defer c.wait.Done()
	ticker := time.NewTicker(updateInterval)
	for {
		select {
		case <-ticker.C:
			err := c.updateMemcacheServers()
			if err != nil {
				level.Warn(c.logger).Log("msg", "error updating memcache servers", "err", err)
			}
		case <-c.quit:
			ticker.Stop()
			return
		}
	}
}

// updateMemcacheServers sets a memcache server list from SRV records. SRV
// priority & weight are ignored.
func (c *memcachedClient) updateMemcacheServers() error {
	var servers []string

	if len(c.addresses) > 0 {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if err := c.provider.Resolve(ctx, c.addresses); err != nil {
			return err
		}
		servers = c.provider.Addresses()
	} else {
		_, addrs, err := net.LookupSRV(c.service, "tcp", c.hostname)
		if err != nil {
			return err
		}
		for _, srv := range addrs {
			servers = append(servers, fmt.Sprintf("%s:%d", srv.Target, srv.Port))
		}
	}

	if len(servers) > 0 {
		// Copy across circuit-breakers for current set of addresses, thus
		// leaving behind any for servers we won't talk to again
		c.Lock()
		newCBs := make(map[string]*gobreaker.CircuitBreaker, len(servers))
		for _, address := range servers {
			if cb, exists := c.cbs[address]; exists {
				newCBs[address] = cb
			}
		}
		c.cbs = newCBs
		c.Unlock()
	}

	// ServerList deterministically maps keys to _index_ of the server list.
	// Since DNS returns records in different order each time, we sort to
	// guarantee best possible match between nodes.
	sort.Strings(servers)
	c.numServers.Set(float64(len(servers)))
	return c.serverList.SetServers(servers...)
}
