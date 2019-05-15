package cache

import (
	"flag"
	"fmt"
	"net"
	"sort"
	"sync"
	"time"

	"github.com/bradfitz/gomemcache/memcache"
	"github.com/cortexproject/cortex/pkg/util"
	"github.com/go-kit/kit/log/level"
)

// MemcachedClient interface exists for mocking memcacheClient.
type MemcachedClient interface {
	GetMulti(keys []string) (map[string]*memcache.Item, error)
	Set(item *memcache.Item) error
}

// memcachedClient is a memcache client that gets its server list from SRV
// records, and periodically updates that ServerList.
type memcachedClient struct {
	*memcache.Client
	serverList *memcache.ServerList
	hostname   string
	service    string

	quit chan struct{}
	wait sync.WaitGroup
}

// MemcachedClientConfig defines how a MemcachedClient should be constructed.
type MemcachedClientConfig struct {
	Host           string        `yaml:"host,omitempty"`
	Service        string        `yaml:"service,omitempty"`
	Timeout        time.Duration `yaml:"timeout,omitempty"`
	MaxIdleConns   int           `yaml:"max_idle_conns,omitempty"`
	UpdateInterval time.Duration `yaml:"update_interval,omitempty"`
}

// RegisterFlagsWithPrefix adds the flags required to config this to the given FlagSet
func (cfg *MemcachedClientConfig) RegisterFlagsWithPrefix(prefix, description string, f *flag.FlagSet) {
	f.StringVar(&cfg.Host, prefix+"memcached.hostname", "", description+"Hostname for memcached service to use when caching chunks. If empty, no memcached will be used.")
	f.StringVar(&cfg.Service, prefix+"memcached.service", "memcached", description+"SRV service used to discover memcache servers.")
	f.IntVar(&cfg.MaxIdleConns, prefix+"memcached.max-idle-conns", 16, description+"Maximum number of idle connections in pool.")
	f.DurationVar(&cfg.Timeout, prefix+"memcached.timeout", 100*time.Millisecond, description+"Maximum time to wait before giving up on memcached requests.")
	f.DurationVar(&cfg.UpdateInterval, prefix+"memcached.update-interval", 1*time.Minute, description+"Period with which to poll DNS for memcache servers.")
}

// NewMemcachedClient creates a new MemcacheClient that gets its server list
// from SRV and updates the server list on a regular basis.
func NewMemcachedClient(cfg MemcachedClientConfig) MemcachedClient {
	var servers memcache.ServerList
	client := memcache.NewFromSelector(&servers)
	client.Timeout = cfg.Timeout
	client.MaxIdleConns = cfg.MaxIdleConns

	newClient := &memcachedClient{
		Client:     client,
		serverList: &servers,
		hostname:   cfg.Host,
		service:    cfg.Service,
		quit:       make(chan struct{}),
	}
	err := newClient.updateMemcacheServers()
	if err != nil {
		level.Error(util.Logger).Log("msg", "error setting memcache servers to host", "host", cfg.Host, "err", err)
	}

	newClient.wait.Add(1)
	go newClient.updateLoop(cfg.UpdateInterval)
	return newClient
}

// Stop the memcache client.
func (c *memcachedClient) Stop() {
	close(c.quit)
	c.wait.Wait()
}

func (c *memcachedClient) updateLoop(updateInterval time.Duration) error {
	defer c.wait.Done()
	ticker := time.NewTicker(updateInterval)
	var err error
	for {
		select {
		case <-ticker.C:
			err = c.updateMemcacheServers()
			if err != nil {
				level.Warn(util.Logger).Log("msg", "error updating memcache servers", "err", err)
			}
		case <-c.quit:
			ticker.Stop()
		}
	}
}

// updateMemcacheServers sets a memcache server list from SRV records. SRV
// priority & weight are ignored.
func (c *memcachedClient) updateMemcacheServers() error {
	_, addrs, err := net.LookupSRV(c.service, "tcp", c.hostname)
	if err != nil {
		return err
	}
	var servers []string
	for _, srv := range addrs {
		servers = append(servers, fmt.Sprintf("%s:%d", srv.Target, srv.Port))
	}
	// ServerList deterministically maps keys to _index_ of the server list.
	// Since DNS returns records in different order each time, we sort to
	// guarantee best possible match between nodes.
	sort.Strings(servers)
	return c.serverList.SetServers(servers...)
}
