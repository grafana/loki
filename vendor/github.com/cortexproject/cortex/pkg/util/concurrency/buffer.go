package concurrency

import (
	"bytes"
	"sync"
)

type SyncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (sb *SyncBuffer) Write(p []byte) (n int, err error) {
	sb.mu.Lock()
	defer sb.mu.Unlock()

	return sb.buf.Write(p)
}

func (sb *SyncBuffer) String() string {
	sb.mu.Lock()
	defer sb.mu.Unlock()

	return sb.buf.String()
}
