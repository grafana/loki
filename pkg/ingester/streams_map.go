package ingester

import (
	"sync"

	"github.com/prometheus/common/model"
	"go.uber.org/atomic"
)

type streamsMap struct {
	consistencyMtx sync.Mutex
	streams        *sync.Map // map[string]*stream
	streamsByFP    *sync.Map // map[model.Fingerprint]*stream
	streamsCounter *atomic.Int64
}

func newStreamsMap() *streamsMap {
	return &streamsMap{
		consistencyMtx: sync.Mutex{},
		streams:        &sync.Map{},
		streamsByFP:    &sync.Map{},
		streamsCounter: atomic.NewInt64(0),
	}
}

func (m *streamsMap) Load(key string) (*stream, bool) {
	return m.load(m.streams, key)
}

func (m *streamsMap) LoadByFP(fp model.Fingerprint) (*stream, bool) {
	return m.load(m.streamsByFP, fp)
}

func (m *streamsMap) LoadOrStoreNew(key string, newStreamFn func() (*stream, error)) (*stream, bool, error) {
	return m.loadOrStoreNew(m.streams, key, newStreamFn)
}

func (m *streamsMap) LoadOrStoreNewByFP(fp model.Fingerprint, newStreamFn func() (*stream, error)) (*stream, bool, error) {
	return m.loadOrStoreNew(m.streamsByFP, fp, newStreamFn)
}

func (m *streamsMap) Delete(s *stream) bool {
	m.consistencyMtx.Lock()
	defer m.consistencyMtx.Unlock()
	_, loaded := m.streams.LoadAndDelete(s.labelsString)
	if loaded {
		m.streamsByFP.Delete(s.fp)
		m.streamsCounter.Dec()
		return true
	}
	return false
}

func (m *streamsMap) ForEach(fn func(s *stream) (bool, error)) error {
	var c bool
	var err error
	m.streams.Range(func(key, value interface{}) bool {
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
	} else {
		return nil, false
	}
}

func (m *streamsMap) loadOrStoreNew(mp *sync.Map, key interface{}, newStreamFn func() (*stream, error)) (*stream, bool, error) {
	if s, ok := m.load(mp, key); ok {
		return s, true, nil
	} else {
		m.consistencyMtx.Lock()
		defer m.consistencyMtx.Unlock()
		// Double check
		if s, ok := m.load(mp, key); ok {
			return s, true, nil
		} else {
			s, err := newStreamFn()
			if err != nil {
				return nil, false, err
			}
			if labelsString, ok := key.(string); ok {
				m.streams.Store(labelsString, s)
			} else {
				m.streams.Store(s.labelsString, s)
			}
			m.streamsByFP.Store(s.fp, s)
			m.streamsCounter.Inc()
			return s, false, nil
		}
	}
}
