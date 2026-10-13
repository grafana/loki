package ingester

import (
	"hash/fnv"
	"sync"
)

// stripeLock is taken from ruler/storage/wal/series.go
type stripeLock struct {
	sync.RWMutex
	// Padding to avoid multiple locks being on the same cache line.
	_ [40]byte
}

type keyedLock struct {
	locks []stripeLock
}

func newKeyedLock(n int) *keyedLock {
	return &keyedLock{
		locks: make([]stripeLock, n),
	}
}

func (l *keyedLock) getKey(s string) int {
	h := fnv.New32()
	_, _ = h.Write([]byte(s))
	return int(h.Sum32() % uint32(len(l.locks)))

}

func (l *keyedLock) forKey(s string) *stripeLock {
	return &l.locks[l.getKey(s)]
}
