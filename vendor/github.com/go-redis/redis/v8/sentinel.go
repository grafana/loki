package redis

import (
	"context"
	"crypto/tls"
	"errors"
	"math/rand"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/go-redis/redis/v8/internal"
	"github.com/go-redis/redis/v8/internal/pool"
)

//------------------------------------------------------------------------------

// FailoverOptions are used to configure a failover client and should
// be passed to NewFailoverClient.
type FailoverOptions struct {
	// The master name.
	MasterName string
	// A seed list of host:port addresses of sentinel nodes.
	SentinelAddrs []string
	// Sentinel password from "requirepass <password>" (if enabled) in Sentinel configuration
	SentinelPassword string

	// Enables read-only commands on slave nodes.
	ReadOnly bool

	// Following options are copied from Options struct.

	Dialer    func(ctx context.Context, network, addr string) (net.Conn, error)
	OnConnect func(ctx context.Context, cn *Conn) error

	Username string
	Password string
	DB       int

	MaxRetries      int
	MinRetryBackoff time.Duration
	MaxRetryBackoff time.Duration

	DialTimeout  time.Duration
	ReadTimeout  time.Duration
	WriteTimeout time.Duration

	PoolSize           int
	MinIdleConns       int
	MaxConnAge         time.Duration
	PoolTimeout        time.Duration
	IdleTimeout        time.Duration
	IdleCheckFrequency time.Duration

	TLSConfig *tls.Config
}

func (opt *FailoverOptions) options() *Options {
	return &Options{
		Addr: "FailoverClient",

		Dialer:    opt.Dialer,
		OnConnect: opt.OnConnect,

		DB:       opt.DB,
		Username: opt.Username,
		Password: opt.Password,

		MaxRetries:      opt.MaxRetries,
		MinRetryBackoff: opt.MinRetryBackoff,
		MaxRetryBackoff: opt.MaxRetryBackoff,

		DialTimeout:  opt.DialTimeout,
		ReadTimeout:  opt.ReadTimeout,
		WriteTimeout: opt.WriteTimeout,

		PoolSize:           opt.PoolSize,
		PoolTimeout:        opt.PoolTimeout,
		IdleTimeout:        opt.IdleTimeout,
		IdleCheckFrequency: opt.IdleCheckFrequency,
		MinIdleConns:       opt.MinIdleConns,
		MaxConnAge:         opt.MaxConnAge,

		TLSConfig: opt.TLSConfig,

		sentinelReadOnly: opt.ReadOnly,
	}
}

// NewFailoverClient returns a Redis client that uses Redis Sentinel
// for automatic failover. It's safe for concurrent use by multiple
// goroutines.
func NewFailoverClient(failoverOpt *FailoverOptions) *Client {
	opt := failoverOpt.options()
	opt.init()

	failover := &sentinelFailover{
		masterName:       failoverOpt.MasterName,
		sentinelAddrs:    failoverOpt.SentinelAddrs,
		sentinelPassword: failoverOpt.SentinelPassword,

		opt: opt,
	}

	c := Client{
		baseClient: newBaseClient(opt, failover.Pool()),
		ctx:        context.Background(),
	}
	c.cmdable = c.Process
	c.onClose = failover.Close

	return &c
}

//------------------------------------------------------------------------------

type SentinelClient struct {
	*baseClient
	ctx context.Context
}

func NewSentinelClient(opt *Options) *SentinelClient {
	opt.init()
	c := &SentinelClient{
		baseClient: &baseClient{
			opt:      opt,
			connPool: newConnPool(opt),
		},
		ctx: context.Background(),
	}
	return c
}

func (c *SentinelClient) Context() context.Context {
	return c.ctx
}

func (c *SentinelClient) WithContext(ctx context.Context) *SentinelClient {
	if ctx == nil {
		panic("nil context")
	}
	clone := *c
	clone.ctx = ctx
	return &clone
}

func (c *SentinelClient) Process(ctx context.Context, cmd Cmder) error {
	return c.baseClient.process(ctx, cmd)
}

func (c *SentinelClient) pubSub() *PubSub {
	pubsub := &PubSub{
		opt: c.opt,

		newConn: func(ctx context.Context, channels []string) (*pool.Conn, error) {
			return c.newConn(ctx)
		},
		closeConn: c.connPool.CloseConn,
	}
	pubsub.init()
	return pubsub
}

// Ping is used to test if a connection is still alive, or to
// measure latency.
func (c *SentinelClient) Ping(ctx context.Context) *StringCmd {
	cmd := NewStringCmd(ctx, "ping")
	_ = c.Process(ctx, cmd)
	return cmd
}

// Subscribe subscribes the client to the specified channels.
// Channels can be omitted to create empty subscription.
func (c *SentinelClient) Subscribe(ctx context.Context, channels ...string) *PubSub {
	pubsub := c.pubSub()
	if len(channels) > 0 {
		_ = pubsub.Subscribe(ctx, channels...)
	}
	return pubsub
}

// PSubscribe subscribes the client to the given patterns.
// Patterns can be omitted to create empty subscription.
func (c *SentinelClient) PSubscribe(ctx context.Context, channels ...string) *PubSub {
	pubsub := c.pubSub()
	if len(channels) > 0 {
		_ = pubsub.PSubscribe(ctx, channels...)
	}
	return pubsub
}

func (c *SentinelClient) GetMasterAddrByName(ctx context.Context, name string) *StringSliceCmd {
	cmd := NewStringSliceCmd(ctx, "sentinel", "get-master-addr-by-name", name)
	_ = c.Process(ctx, cmd)
	return cmd
}

func (c *SentinelClient) Sentinels(ctx context.Context, name string) *SliceCmd {
	cmd := NewSliceCmd(ctx, "sentinel", "sentinels", name)
	_ = c.Process(ctx, cmd)
	return cmd
}

// Failover forces a failover as if the master was not reachable, and without
// asking for agreement to other Sentinels.
func (c *SentinelClient) Failover(ctx context.Context, name string) *StatusCmd {
	cmd := NewStatusCmd(ctx, "sentinel", "failover", name)
	_ = c.Process(ctx, cmd)
	return cmd
}

// Reset resets all the masters with matching name. The pattern argument is a
// glob-style pattern. The reset process clears any previous state in a master
// (including a failover in progress), and removes every slave and sentinel
// already discovered and associated with the master.
func (c *SentinelClient) Reset(ctx context.Context, pattern string) *IntCmd {
	cmd := NewIntCmd(ctx, "sentinel", "reset", pattern)
	_ = c.Process(ctx, cmd)
	return cmd
}

// FlushConfig forces Sentinel to rewrite its configuration on disk, including
// the current Sentinel state.
func (c *SentinelClient) FlushConfig(ctx context.Context) *StatusCmd {
	cmd := NewStatusCmd(ctx, "sentinel", "flushconfig")
	_ = c.Process(ctx, cmd)
	return cmd
}

// Master shows the state and info of the specified master.
func (c *SentinelClient) Master(ctx context.Context, name string) *StringStringMapCmd {
	cmd := NewStringStringMapCmd(ctx, "sentinel", "master", name)
	_ = c.Process(ctx, cmd)
	return cmd
}

// Masters shows a list of monitored masters and their state.
func (c *SentinelClient) Masters(ctx context.Context) *SliceCmd {
	cmd := NewSliceCmd(ctx, "sentinel", "masters")
	_ = c.Process(ctx, cmd)
	return cmd
}

// Slaves shows a list of slaves for the specified master and their state.
func (c *SentinelClient) Slaves(ctx context.Context, name string) *SliceCmd {
	cmd := NewSliceCmd(ctx, "sentinel", "slaves", name)
	_ = c.Process(ctx, cmd)
	return cmd
}

// CkQuorum checks if the current Sentinel configuration is able to reach the
// quorum needed to failover a master, and the majority needed to authorize the
// failover. This command should be used in monitoring systems to check if a
// Sentinel deployment is ok.
func (c *SentinelClient) CkQuorum(ctx context.Context, name string) *StringCmd {
	cmd := NewStringCmd(ctx, "sentinel", "ckquorum", name)
	_ = c.Process(ctx, cmd)
	return cmd
}

// Monitor tells the Sentinel to start monitoring a new master with the specified
// name, ip, port, and quorum.
func (c *SentinelClient) Monitor(ctx context.Context, name, ip, port, quorum string) *StringCmd {
	cmd := NewStringCmd(ctx, "sentinel", "monitor", name, ip, port, quorum)
	_ = c.Process(ctx, cmd)
	return cmd
}

// Set is used in order to change configuration parameters of a specific master.
func (c *SentinelClient) Set(ctx context.Context, name, option, value string) *StringCmd {
	cmd := NewStringCmd(ctx, "sentinel", "set", name, option, value)
	_ = c.Process(ctx, cmd)
	return cmd
}

// Remove is used in order to remove the specified master: the master will no
// longer be monitored, and will totally be removed from the internal state of
// the Sentinel.
func (c *SentinelClient) Remove(ctx context.Context, name string) *StringCmd {
	cmd := NewStringCmd(ctx, "sentinel", "remove", name)
	_ = c.Process(ctx, cmd)
	return cmd
}

type sentinelFailover struct {
	sentinelAddrs    []string
	sentinelPassword string

	opt *Options

	pool     *pool.ConnPool
	poolOnce sync.Once

	mu          sync.RWMutex
	masterName  string
	_masterAddr string
	sentinel    *SentinelClient
	pubsub      *PubSub
}

func (c *sentinelFailover) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.sentinel != nil {
		return c.closeSentinel()
	}
	return nil
}

func (c *sentinelFailover) closeSentinel() error {
	firstErr := c.pubsub.Close()
	c.pubsub = nil

	err := c.sentinel.Close()
	if err != nil && firstErr == nil {
		firstErr = err
	}
	c.sentinel = nil

	return firstErr
}

func (c *sentinelFailover) Pool() *pool.ConnPool {
	c.poolOnce.Do(func() {
		opt := *c.opt
		opt.Dialer = c.dial
		c.pool = newConnPool(&opt)
	})
	return c.pool
}

func (c *sentinelFailover) dial(ctx context.Context, network, _ string) (net.Conn, error) {
	var addr string
	var err error

	if c.opt.sentinelReadOnly {
		addr, err = c.RandomSlaveAddr(ctx)
	} else {
		addr, err = c.MasterAddr(ctx)
	}

	if err != nil {
		return nil, err
	}
	if c.opt.Dialer != nil {
		return c.opt.Dialer(ctx, network, addr)
	}
	return net.DialTimeout("tcp", addr, c.opt.DialTimeout)
}

func (c *sentinelFailover) MasterAddr(ctx context.Context) (string, error) {
	addr, err := c.masterAddr(ctx)
	if err != nil {
		return "", err
	}
	c.switchMaster(ctx, addr)
	return addr, nil
}

func (c *sentinelFailover) RandomSlaveAddr(ctx context.Context) (string, error) {
	addresses, err := c.slaveAddresses(ctx)
	if err != nil {
		return "", err
	}
	if len(addresses) < 1 {
		return c.MasterAddr(ctx)
	}
	return addresses[rand.Intn(len(addresses))], nil
}

func (c *sentinelFailover) masterAddr(ctx context.Context) (string, error) {
	c.mu.RLock()
	sentinel := c.sentinel
	c.mu.RUnlock()

	if sentinel != nil {
		addr := c.getMasterAddr(ctx, sentinel)
		if addr != "" {
			return addr, nil
		}
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.sentinel != nil {
		addr := c.getMasterAddr(ctx, c.sentinel)
		if addr != "" {
			return addr, nil
		}
		_ = c.closeSentinel()
	}

	for i, sentinelAddr := range c.sentinelAddrs {
		sentinel := NewSentinelClient(&Options{
			Addr:   sentinelAddr,
			Dialer: c.opt.Dialer,

			Username: c.opt.Username,
			Password: c.opt.Password,

			MaxRetries: c.opt.MaxRetries,

			DialTimeout:  c.opt.DialTimeout,
			ReadTimeout:  c.opt.ReadTimeout,
			WriteTimeout: c.opt.WriteTimeout,

			PoolSize:           c.opt.PoolSize,
			PoolTimeout:        c.opt.PoolTimeout,
			IdleTimeout:        c.opt.IdleTimeout,
			IdleCheckFrequency: c.opt.IdleCheckFrequency,

			TLSConfig: c.opt.TLSConfig,
		})

		masterAddr, err := sentinel.GetMasterAddrByName(ctx, c.masterName).Result()
		if err != nil {
			internal.Logger.Printf(ctx, "sentinel: GetMasterAddrByName master=%q failed: %s",
				c.masterName, err)
			_ = sentinel.Close()
			continue
		}

		// Push working sentinel to the top.
		c.sentinelAddrs[0], c.sentinelAddrs[i] = c.sentinelAddrs[i], c.sentinelAddrs[0]
		c.setSentinel(ctx, sentinel)

		addr := net.JoinHostPort(masterAddr[0], masterAddr[1])
		return addr, nil
	}

	return "", errors.New("redis: all sentinels are unreachable")
}

func (c *sentinelFailover) slaveAddresses(ctx context.Context) ([]string, error) {
	c.mu.RLock()
	sentinel := c.sentinel
	c.mu.RUnlock()

	if sentinel != nil {
		addrs := c.getSlaveAddrs(ctx, sentinel)
		if len(addrs) > 0 {
			return addrs, nil
		}
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.sentinel != nil {
		addrs := c.getSlaveAddrs(ctx, c.sentinel)
		if len(addrs) > 0 {
			return addrs, nil
		}
		_ = c.closeSentinel()
	}

	for i, sentinelAddr := range c.sentinelAddrs {
		sentinel := NewSentinelClient(&Options{
			Addr:   sentinelAddr,
			Dialer: c.opt.Dialer,

			Username: c.opt.Username,
			Password: c.opt.Password,

			MaxRetries: c.opt.MaxRetries,

			DialTimeout:  c.opt.DialTimeout,
			ReadTimeout:  c.opt.ReadTimeout,
			WriteTimeout: c.opt.WriteTimeout,

			PoolSize:           c.opt.PoolSize,
			PoolTimeout:        c.opt.PoolTimeout,
			IdleTimeout:        c.opt.IdleTimeout,
			IdleCheckFrequency: c.opt.IdleCheckFrequency,

			TLSConfig: c.opt.TLSConfig,
		})

		slaves, err := sentinel.Slaves(ctx, c.masterName).Result()
		if err != nil {
			internal.Logger.Printf(ctx, "sentinel: Slaves master=%q failed: %s",
				c.masterName, err)
			_ = sentinel.Close()
			continue
		}

		// Push working sentinel to the top.
		c.sentinelAddrs[0], c.sentinelAddrs[i] = c.sentinelAddrs[i], c.sentinelAddrs[0]
		c.setSentinel(ctx, sentinel)

		addrs := parseSlaveAddresses(slaves)
		return addrs, nil
	}

	return []string{}, errors.New("redis: all sentinels are unreachable")
}

func (c *sentinelFailover) getMasterAddr(ctx context.Context, sentinel *SentinelClient) string {
	addr, err := sentinel.GetMasterAddrByName(ctx, c.masterName).Result()
	if err != nil {
		internal.Logger.Printf(ctx, "sentinel: GetMasterAddrByName name=%q failed: %s",
			c.masterName, err)
		return ""
	}
	return net.JoinHostPort(addr[0], addr[1])
}

func (c *sentinelFailover) getSlaveAddrs(ctx context.Context, sentinel *SentinelClient) []string {
	addrs, err := sentinel.Slaves(ctx, c.masterName).Result()
	if err != nil {
		internal.Logger.Printf(ctx, "sentinel: Slaves name=%q failed: %s",
			c.masterName, err)
		return []string{}
	}

	return parseSlaveAddresses(addrs)
}

func parseSlaveAddresses(addrs []interface{}) []string {
	nodes := []string{}

	for _, node := range addrs {
		ip := ""
		port := ""
		flags := []string{}
		lastkey := ""
		isDown := false

		for _, key := range node.([]interface{}) {
			switch lastkey {
			case "ip":
				ip = key.(string)
			case "port":
				port = key.(string)
			case "flags":
				flags = strings.Split(key.(string), ",")
			}
			lastkey = key.(string)
		}
		for _, flag := range flags {
			switch flag {
			case "s_down", "o_down", "disconnected":
				isDown = true
			}
		}
		if !isDown {
			nodes = append(nodes, net.JoinHostPort(ip, port))
		}
	}

	return nodes
}

func (c *sentinelFailover) switchMaster(ctx context.Context, addr string) {
	c.mu.RLock()
	masterAddr := c._masterAddr
	c.mu.RUnlock()
	if masterAddr == addr {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if c._masterAddr == addr {
		return
	}

	internal.Logger.Printf(ctx, "sentinel: new master=%q addr=%q",
		c.masterName, addr)
	_ = c.Pool().Filter(func(cn *pool.Conn) bool {
		return cn.RemoteAddr().String() != addr
	})
	c._masterAddr = addr
}

func (c *sentinelFailover) setSentinel(ctx context.Context, sentinel *SentinelClient) {
	if c.sentinel != nil {
		panic("not reached")
	}
	c.sentinel = sentinel
	c.discoverSentinels(ctx)

	c.pubsub = sentinel.Subscribe(ctx, "+switch-master")
	go c.listen(c.pubsub)
}

func (c *sentinelFailover) discoverSentinels(ctx context.Context) {
	sentinels, err := c.sentinel.Sentinels(ctx, c.masterName).Result()
	if err != nil {
		internal.Logger.Printf(ctx, "sentinel: Sentinels master=%q failed: %s", c.masterName, err)
		return
	}
	for _, sentinel := range sentinels {
		vals := sentinel.([]interface{})
		for i := 0; i < len(vals); i += 2 {
			key := vals[i].(string)
			if key == "name" {
				sentinelAddr := vals[i+1].(string)
				if !contains(c.sentinelAddrs, sentinelAddr) {
					internal.Logger.Printf(ctx, "sentinel: discovered new sentinel=%q for master=%q",
						sentinelAddr, c.masterName)
					c.sentinelAddrs = append(c.sentinelAddrs, sentinelAddr)
				}
			}
		}
	}
}

func (c *sentinelFailover) listen(pubsub *PubSub) {
	ch := pubsub.Channel()
	for {
		msg, ok := <-ch
		if !ok {
			break
		}

		if msg.Channel == "+switch-master" {
			parts := strings.Split(msg.Payload, " ")
			if parts[0] != c.masterName {
				internal.Logger.Printf(pubsub.getContext(), "sentinel: ignore addr for master=%q", parts[0])
				continue
			}
			addr := net.JoinHostPort(parts[3], parts[4])
			c.switchMaster(pubsub.getContext(), addr)
		}
	}
}

func contains(slice []string, str string) bool {
	for _, s := range slice {
		if s == str {
			return true
		}
	}
	return false
}
