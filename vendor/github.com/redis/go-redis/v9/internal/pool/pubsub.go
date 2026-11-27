package pool

import (
	"context"
	"net"
	"runtime"
	"sync"
	"sync/atomic"
	"weak"
)

// PubSubStats contains pub/sub connection pool stats.
//
// TODO(cxl): the uint32 fields below will be changed to atomic.Uint32 in v10,
// which is a breaking API change.
type PubSubStats struct {
	Created   uint32
	Untracked uint32
	Active    uint32
}

// PubSubPool manages a pool of PubSub connections.
type PubSubPool struct {
	opt       *Options
	netDialer func(ctx context.Context, network, addr string) (net.Conn, error)

	// Map to track active PubSub connections
	activeConns sync.Map // map[uint64]*Conn (connID -> conn)
	// trackMu orders TrackConn's closed check with Close, so a connection
	// tracked during Close is either closed by Close or rejected.
	trackMu sync.Mutex
	closed  atomic.Bool
	stats   PubSubStats
}

// NewPubSubPool implements a pool for PubSub connections.
// It intentionally does not implement the Pooler interface
func NewPubSubPool(opt *Options, netDialer func(ctx context.Context, network, addr string) (net.Conn, error)) *PubSubPool {
	p := &PubSubPool{
		opt:       opt,
		netDialer: netDialer,
	}
	if opt.Metrics == nil {
		// Only pools on the global table are listed by GlobalPools.
		wp := weak.Make(p)
		livePoolsMu.Lock()
		livePubSubPools[wp] = struct{}{}
		livePoolsMu.Unlock()
		// Drops the entry for a pool that is collected without Close.
		runtime.AddCleanup(p, func(wp weak.Pointer[PubSubPool]) {
			livePoolsMu.Lock()
			delete(livePubSubPools, wp)
			livePoolsMu.Unlock()
		}, wp)
	}
	return p
}

func (p *PubSubPool) NewConn(ctx context.Context, network string, addr string, channels []string) (*Conn, error) {
	if p.closed.Load() {
		return nil, ErrClosed
	}

	netConn, err := p.netDialer(ctx, network, addr)
	if err != nil {
		return nil, err
	}
	cn := NewConnWithBufferSize(netConn, p.opt.ReadBufferSize, p.opt.WriteBufferSize)
	cn.pubsub = true
	// Set pool name for metrics
	cn.SetPoolName(p.opt.Name)
	cn.metrics = p.opt.Metrics
	atomic.AddUint32(&p.stats.Created, 1)
	return cn, nil
}

// TrackConn registers an initialized connection. If Close has already started,
// it closes the connection (including its initialization cleanup) and rejects it.
func (p *PubSubPool) TrackConn(cn *Conn) error {
	p.trackMu.Lock()
	if p.closed.Load() {
		// Conn.Close may run an onClose callback that reenters this pool.
		p.trackMu.Unlock()
		_ = cn.Close()
		return ErrClosed
	}

	atomic.AddUint32(&p.stats.Active, 1)
	p.activeConns.Store(cn.GetID(), cn)
	p.trackMu.Unlock()
	return nil
}

func (p *PubSubPool) UntrackConn(cn *Conn) {
	// LoadAndDelete ensures each connection is only decremented once,
	// guarding against double-decrement if Close() already untracked it.
	if _, loaded := p.activeConns.LoadAndDelete(cn.GetID()); !loaded {
		return
	}
	atomic.AddUint32(&p.stats.Active, ^uint32(0))
	atomic.AddUint32(&p.stats.Untracked, 1)
}

func (p *PubSubPool) Close() error {
	p.closed.Store(true)
	livePoolsMu.Lock()
	delete(livePubSubPools, weak.Make(p))
	livePoolsMu.Unlock()
	p.trackMu.Lock()
	var conns []*Conn
	p.activeConns.Range(func(key, value interface{}) bool {
		cn := value.(*Conn)
		// Use LoadAndDelete to atomically claim ownership of this entry.
		// If a concurrent UntrackConn already removed it, skip to avoid double-decrement.
		if _, loaded := p.activeConns.LoadAndDelete(key); !loaded {
			return true
		}
		atomic.AddUint32(&p.stats.Active, ^uint32(0))
		atomic.AddUint32(&p.stats.Untracked, 1)
		conns = append(conns, cn)
		return true
	})
	p.trackMu.Unlock()

	// Close outside trackMu: cn.Close runs the connection's onClose callback.
	for _, cn := range conns {
		_ = cn.Close()
	}
	return nil
}

// Name returns the pool's configured name, the one its connections carry (Conn.PoolName).
func (p *PubSubPool) Name() string { return p.opt.Name }

func (p *PubSubPool) Stats() *PubSubStats {
	// load stats atomically
	return &PubSubStats{
		Created:   atomic.LoadUint32(&p.stats.Created),
		Untracked: atomic.LoadUint32(&p.stats.Untracked),
		Active:    atomic.LoadUint32(&p.stats.Active),
	}
}
