package scratch

import (
	"bytes"
	"io"
	"sync"
)

// Memory is an implementation of [Store] that retains data in memory.
type Memory struct {
	mut        sync.RWMutex
	nextHandle *uint64 // nextHandle is a pointer to allow multiple stores to synchronize handles
	data       map[Handle]*bytes.Buffer
}

var _ Store = (*Memory)(nil)

// NewMemory returns a new Memory store.
func NewMemory() *Memory {
	// Handles start at 1.
	nextHandle := uint64(1)
	return newMemoryWithNextHandle(&nextHandle)
}

// newMemoryWithNextHandle creates a new Memory store with a custom nextHandle.
func newMemoryWithNextHandle(nextHandle *uint64) *Memory {
	return &Memory{
		nextHandle: nextHandle,
		data:       make(map[Handle]*bytes.Buffer),
	}
}

// Put stores the contents of p into memory.
func (m *Memory) Put(p []byte) Handle {
	buf := bytes.NewBuffer(make([]byte, 0, len(p)))
	if _, err := buf.Write(p); err != nil {
		// buf.Write doesn't fail unless writing more than [math.MaxInt].
		panic(err)
	}

	m.mut.Lock()
	defer m.mut.Unlock()

	handle := m.getNextHandle()
	m.data[handle] = buf
	return handle
}

// getNextHandle returns the next unique handle. getNextHandle must be called
// while holding m.mut.
func (m *Memory) getNextHandle() Handle {
	handle := Handle(*m.nextHandle)
	*m.nextHandle++
	return handle
}

// Read returns a reader for h. Read returns [HandleNotFoundError] if h doesn't
// exist in memory.
func (m *Memory) Read(h Handle) (io.ReadSeekCloser, error) {
	buf, err := m.getBuffer(h)
	if err != nil {
		return nil, err
	}

	return nopReadSeekCloser{bytes.NewReader(buf.Bytes())}, nil
}

func (m *Memory) getBuffer(h Handle) (*bytes.Buffer, error) {
	m.mut.RLock()
	defer m.mut.RUnlock()

	if _, ok := m.data[h]; !ok {
		return nil, HandleNotFoundError(h)
	}
	return m.data[h], nil
}

// Remove removes h. Remove returns [HandleNotFoundError] if h doesn't exist in memory.
func (m *Memory) Remove(h Handle) error {
	m.mut.Lock()
	defer m.mut.Unlock()

	if _, ok := m.data[h]; !ok {
		return HandleNotFoundError(h)
	}

	delete(m.data, h)
	return nil
}

type nopReadSeekCloser struct {
	io.ReadSeeker
}

func (n nopReadSeekCloser) Close() error { return nil }
