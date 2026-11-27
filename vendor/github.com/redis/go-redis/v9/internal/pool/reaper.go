package pool

import (
	"context"
	"errors"
	"time"
	"weak"
)

// startLifetimeReaper runs reapExpiredIdle every ConnMaxLifetimeReapInterval
// until the pool is closed or collected. It is a no-op unless the interval is
// set and at least one of ConnMaxLifetime or ConnMaxIdleTime is set.
//
// The goroutine holds the pool through a weak pointer, so a pool that is
// dropped without Close can still be collected (see the AddCleanup in
// NewConnPool); the goroutine then exits.
func (p *ConnPool) startLifetimeReaper() {
	interval := p.cfg.ConnMaxLifetimeReapInterval
	if interval <= 0 || (p.cfg.ConnMaxLifetime <= 0 && p.cfg.ConnMaxIdleTime <= 0) {
		return
	}
	wp := weak.Make(p)
	closedCh := p.closedCh
	done := make(chan struct{})
	p.reaperDone = done
	go func() {
		defer close(done)
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-closedCh:
				return
			case <-t.C:
				pool := wp.Value()
				if pool == nil {
					return
				}
				pool.reapExpiredIdle(context.Background())
			}
		}
	}()
}

// expiredIdle mirrors the lifetime and idle-time checks in isHealthyConn.
func (p *ConnPool) expiredIdle(cn *Conn, nowNs int64) bool {
	if p.cfg.ConnMaxLifetime > 0 && cn.expiresAt.UnixNano() < nowNs {
		return true
	}
	return p.cfg.ConnMaxIdleTime > 0 && nowNs-cn.UsedAtNs() >= int64(p.cfg.ConnMaxIdleTime)
}

// takeExpiredIdleLocked removes expired idle conns from the pool and returns
// them. The caller holds connsMu and closes the conns after unlocking.
//
// A conn is claimed with TryAcquire, like popIdle does, so a conn that a
// re-auth or handoff worker already moved out of IDLE is left alone and a
// worker cannot claim it afterwards. The claim also covers CREATED conns
// (pooled before first use).
func (p *ConnPool) takeExpiredIdleLocked(nowNs int64) []*Conn {
	// One pass: keep survivors in order (FIFO/LIFO semantics depend on it),
	// drop expired ones. O(idle) however many expire.
	var removed []*Conn
	kept := p.idleConns[:0]
	for _, cn := range p.idleConns {
		// A conn with a handoff pending is not the reaper's to close: the
		// handoff worker re-initializes it in place and it shows IDLE while
		// OnConnect is still running. It keeps its original expiry, so once
		// the handoff clears it is reaped like any other expired conn.
		if p.expiredIdle(cn, nowNs) && !cn.HandoffPending() && cn.TryAcquire() {
			// Recheck after the claim: a re-auth worker may have used the conn
			// (refreshing its idle time) between the first check and the claim.
			// Undo the claim if it is no longer expired, or not tracked here.
			if p.expiredIdle(cn, nowNs) && p.forgetConn(cn) {
				removed = append(removed, cn)
				continue
			}
			cn.Release()
		}
		kept = append(kept, cn)
	}
	// Clear the tail so dropped conns are not kept alive by the backing array.
	clear(p.idleConns[len(kept):])
	p.idleConns = kept
	p.idleConnsLen.Add(-int32(len(removed)))
	return removed
}

// reapExpiredIdle closes idle connections past ConnMaxLifetime or idle for
// ConnMaxIdleTime, and refills the pool toward MinIdleConns (so the floor is
// recycled, not kept stale). Connections that are in use, or that a re-auth or
// handoff worker owns, are left alone. Returns the number closed.
func (p *ConnPool) reapExpiredIdle(ctx context.Context) int {
	nowNs := time.Now().UnixNano()

	p.connsMu.Lock()
	if p.closed() {
		p.connsMu.Unlock()
		return 0
	}
	removedConns := p.takeExpiredIdleLocked(nowNs)
	p.connsMu.Unlock()

	hookManager := p.hookManager.Load()
	for _, cn := range removedConns {
		// Mark it closed before the hooks run, so a hook that tracks
		// per-conn state (e.g. a re-auth marker) cannot re-create it after
		// OnRemove cleared it: those paths check IsClosed. Close still runs
		// its full teardown from CLOSED.
		cn.GetStateMachine().Transition(StateClosed)
		if hookManager != nil {
			hookManager.ProcessOnRemove(ctx, cn, errors.New(CloseReasonStale))
		}
		p.recordConnectionClosed(ctx, cn, CloseReasonStale)
		_ = p.closeConn(cn)
	}
	// Also on a pass that closed nothing: replacement dials from an earlier
	// pass may have failed, and with an empty pool no later tick would retry
	// them. Cheap when the idle floor is already met.
	p.checkMinIdleConns()
	return len(removedConns)
}
