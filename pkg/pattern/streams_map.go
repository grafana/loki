package pattern

import (
	"sync"

	"github.com/prometheus/common/model"
	"go.uber.org/atomic"
)

type streamsMap struct {
	consistencyMtx sync.RWMutex // Keep read/write consistency between other fields
	streams        *sync.Map    // map[string]*stream
	streamsByFP    *sync.Map    // map[model.Fingerprint]*stream
	streamsCounter *atomic.Int64
}

func newStreamsMap() *streamsMap {
	return &streamsMap{
		consistencyMtx: sync.RWMutex{},
		streams:        &sync.Map{},
		streamsByFP:    &sync.Map{},
		streamsCounter: atomic.NewInt64(0),
	}
}

// Load is lock-free. If usage of the stream is consistency sensitive, must be called inside WithRLock at least
func (m *streamsMap) Load(key string) (*stream, bool) {
	return m.load(m.streams, key)
}

// LoadByFP is lock-free. If usage of the stream is consistency sensitive, must be called inside WithRLock at least
func (m *streamsMap) LoadByFP(fp model.Fingerprint) (*stream, bool) {
	return m.load(m.streamsByFP, fp)
}

// Store must be called inside WithLock
func (m *streamsMap) Store(key string, s *stream) {
	m.store(key, s)
}

// StoreByFP must be called inside WithLock
func (m *streamsMap) StoreByFP(fp model.Fingerprint, s *stream) {
	m.store(fp, s)
}

// Delete must be called inside WithLock
func (m *streamsMap) Delete(s *stream) bool {
	_, loaded := m.streams.LoadAndDelete(s.labelsString)
	if loaded {
		m.streamsByFP.Delete(s.fp)
		m.streamsCounter.Dec()
		return true
	}
	return false
}

// LoadOrStoreNew already has lock inside, do NOT call inside WithLock or WithRLock
func (m *streamsMap) LoadOrStoreNew(key string, newStreamFn func() (*stream, error), postLoadFn func(*stream) error) (*stream, bool, error) {
	return m.loadOrStoreNew(m.streams, key, newStreamFn, postLoadFn)
}

// LoadOrStoreNewByFP already has lock inside, do NOT call inside WithLock or WithRLock
func (m *streamsMap) LoadOrStoreNewByFP(fp model.Fingerprint, newStreamFn func() (*stream, error), postLoadFn func(*stream) error) (*stream, bool, error) {
	return m.loadOrStoreNew(m.streamsByFP, fp, newStreamFn, postLoadFn)
}

// WithLock is a helper function to execute write operations
func (m *streamsMap) WithLock(fn func()) {
	m.consistencyMtx.Lock()
	defer m.consistencyMtx.Unlock()
	fn()
}

// WithRLock is a helper function to execute consistency sensitive read operations.
// Generally, if a stream loaded from streamsMap will have its chunkMtx locked, chunkMtx.Lock is supposed to be called
// within this function.
func (m *streamsMap) WithRLock(fn func()) {
	m.consistencyMtx.RLock()
	defer m.consistencyMtx.RUnlock()
	fn()
}

func (m *streamsMap) ForEach(fn func(s *stream) (bool, error)) error {
	var c bool
	var err error
	m.streams.Range(func(_, value interface{}) bool {
		c, err = fn(value.(*stream))
		return c
	})
	return err
}

func (m *streamsMap) Len() int {
	return int(m.streamsCounter.Load())
}

func (m *streamsMap) load(mp *sync.Map, key interface{}) (*stream, bool) {
	if v, ok := mp.Load(key); ok {
		return v.(*stream), true
	}
	return nil, false
}

func (m *streamsMap) store(key interface{}, s *stream) {
	if labelsString, ok := key.(string); ok {
		m.streams.Store(labelsString, s)
	} else {
		m.streams.Store(s.labelsString, s)
	}
	m.streamsByFP.Store(s.fp, s)
	m.streamsCounter.Inc()
}

// newStreamFn: Called if not loaded, with consistencyMtx locked. Must not be nil
// postLoadFn: Called if loaded, with consistencyMtx read-locked at least. Can be nil
func (m *streamsMap) loadOrStoreNew(mp *sync.Map, key interface{}, newStreamFn func() (*stream, error), postLoadFn func(*stream) error) (*stream, bool, error) {
	var s *stream
	var loaded bool
	var err error
	m.WithRLock(func() {
		if s, loaded = m.load(mp, key); loaded {
			if postLoadFn != nil {
				err = postLoadFn(s)
			}
		}
	})

	if loaded {
		return s, true, err
	}

	m.WithLock(func() {
		// Double check
		if s, loaded = m.load(mp, key); loaded {
			if postLoadFn != nil {
				err = postLoadFn(s)
			}
			return
		}

		s, err = newStreamFn()
		if err != nil {
			return
		}
		m.store(key, s)
	})

	return s, loaded, err
}
