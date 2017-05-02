package chunk

import (
	"flag"
	"fmt"
	"net"
	"sort"
	"sync"
	"time"

	"github.com/bradfitz/gomemcache/memcache"
	"github.com/prometheus/common/log"
)

// MemcacheClient is a memcache client that gets its server list from SRV
// records, and periodically updates that ServerList.
type MemcacheClient struct {
	*memcache.Client
	serverList *memcache.ServerList
	hostname   string
	service    string

	quit chan struct{}
	wait sync.WaitGroup
}

// MemcacheConfig defines how a MemcacheClient should be constructed.
type MemcacheConfig struct {
	Host           string
	Service        string
	Timeout        time.Duration
	UpdateInterval time.Duration
}

// RegisterFlags adds the flags required to config this to the given FlagSet
func (cfg *MemcacheConfig) RegisterFlags(f *flag.FlagSet) {
	f.StringVar(&cfg.Host, "memcached.hostname", "", "Hostname for memcached service to use when caching chunks. If empty, no memcached will be used.")
	f.StringVar(&cfg.Service, "memcached.service", "memcached", "SRV service used to discover memcache servers.")
	f.DurationVar(&cfg.Timeout, "memcached.timeout", 100*time.Millisecond, "Maximum time to wait before giving up on memcached requests.")
	f.DurationVar(&cfg.UpdateInterval, "memcached.update-interval", 1*time.Minute, "Period with which to poll DNS for memcache servers.")
}

// NewMemcacheClient creates a new MemcacheClient that gets its server list
// from SRV and updates the server list on a regular basis.
func NewMemcacheClient(cfg MemcacheConfig) *MemcacheClient {
	var servers memcache.ServerList
	client := memcache.NewFromSelector(&servers)
	client.Timeout = cfg.Timeout

	newClient := &MemcacheClient{
		Client:     client,
		serverList: &servers,
		hostname:   cfg.Host,
		service:    cfg.Service,
		quit:       make(chan struct{}),
	}
	err := newClient.updateMemcacheServers()
	if err != nil {
		log.Errorf("Error setting memcache servers to '%v': %v", cfg.Host, err)
	}

	newClient.wait.Add(1)
	go newClient.updateLoop(cfg.UpdateInterval)
	return newClient
}

// Stop the memcache client.
func (c *MemcacheClient) Stop() {
	close(c.quit)
	c.wait.Wait()
}

func (c *MemcacheClient) updateLoop(updateInterval time.Duration) error {
	defer c.wait.Done()
	ticker := time.NewTicker(updateInterval)
	var err error
	for {
		select {
		case <-ticker.C:
			err = c.updateMemcacheServers()
			if err != nil {
				log.Warnf("Error updating memcache servers: %v", err)
			}
		case <-c.quit:
			ticker.Stop()
		}
	}
}

// updateMemcacheServers sets a memcache server list from SRV records. SRV
// priority & weight are ignored.
func (c *MemcacheClient) updateMemcacheServers() error {
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
