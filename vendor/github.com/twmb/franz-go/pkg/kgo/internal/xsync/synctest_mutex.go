//go:build synctests

package xsync

import "sync"

// Mutex is a channel-based mutex for use with synctest.
// A goroutine blocked on a sync.Mutex is not "durably
// blocked" from synctest's perspective, which prevents time
// from advancing. Channel-based blocking is durably blocked.
type Mutex struct {
	once sync.Once
	ch   chan struct{}
}

func (m *Mutex) init() {
	m.once.Do(func() {
		m.ch = make(chan struct{}, 1)
		m.ch <- struct{}{}
	})
}

func (m *Mutex) Lock() {
	m.init()
	<-m.ch
}

func (m *Mutex) TryLock() bool {
	m.init()
	select {
	case <-m.ch:
		return true
	default:
		return false
	}
}

func (m *Mutex) Unlock() {
	m.init()
	select {
	case m.ch <- struct{}{}:
	default:
		panic("sync: unlock of unlocked mutex")
	}
}

// RWMutex is a channel-based readers-writer mutex with
// writer priority.
//
// The design uses a "gate token" pattern: a buffered-1
// channel holds a single token. Readers take the token and
// immediately return it (passing through the gate). Writers
// take the token and hold it, which blocks all new readers
// until the writer unlocks. This provides writer priority
// naturally — the moment a writer calls Lock, new RLock
// calls block on the gate.
//
// A separate writerSignal channel (buffered 1) is used by
// the last active reader to wake a waiting writer. A stale
// signal can accumulate if readers drain while no writer is
// waiting; Lock drains any stale signal before checking the
// reader count.
type RWMutex struct {
	once         sync.Once
	mu           Mutex
	gate         chan struct{}
	readerCount  int
	writerSignal chan struct{}
}

func (rw *RWMutex) init() {
	rw.once.Do(func() {
		rw.gate = make(chan struct{}, 1)
		rw.gate <- struct{}{}
		rw.writerSignal = make(chan struct{}, 1)
		rw.mu.init()
	})
}

func (rw *RWMutex) RLock() {
	rw.init()
	<-rw.gate
	rw.mu.Lock()
	rw.readerCount++
	rw.mu.Unlock()
	rw.gate <- struct{}{}
}

func (rw *RWMutex) TryRLock() bool {
	rw.init()
	select {
	case <-rw.gate:
	default:
		return false
	}
	rw.mu.Lock()
	rw.readerCount++
	rw.mu.Unlock()
	rw.gate <- struct{}{}
	return true
}

func (rw *RWMutex) RUnlock() {
	rw.mu.Lock()
	rw.readerCount--
	if rw.readerCount < 0 {
		rw.mu.Unlock()
		panic("sync: RUnlock of unlocked RWMutex")
	}
	if rw.readerCount == 0 {
		select {
		case rw.writerSignal <- struct{}{}:
		default:
		}
	}
	rw.mu.Unlock()
}

func (rw *RWMutex) Lock() {
	rw.init()
	<-rw.gate
	select {
	case <-rw.writerSignal:
	default:
	}
	rw.mu.Lock()
	if rw.readerCount > 0 {
		rw.mu.Unlock()
		<-rw.writerSignal
	} else {
		rw.mu.Unlock()
	}
}

func (rw *RWMutex) TryLock() bool {
	rw.init()
	select {
	case <-rw.gate:
	default:
		return false
	}
	select {
	case <-rw.writerSignal:
	default:
	}
	rw.mu.Lock()
	if rw.readerCount > 0 {
		rw.mu.Unlock()
		rw.gate <- struct{}{}
		return false
	}
	rw.mu.Unlock()
	return true
}

func (rw *RWMutex) Unlock() {
	select {
	case rw.gate <- struct{}{}:
	default:
		panic("sync: Unlock of unlocked RWMutex")
	}
}

func (rw *RWMutex) RLocker() sync.Locker { return (*rlocker)(rw) }

type rlocker RWMutex

func (r *rlocker) Lock()   { (*RWMutex)(r).RLock() }
func (r *rlocker) Unlock() { (*RWMutex)(r).RUnlock() }
