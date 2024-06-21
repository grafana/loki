package ingester_rf1

import (
	"sync"
	"testing"
)

func BenchmarkRLock(b *testing.B) {
	mutex := sync.RWMutex{}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		mutex.RLock()
		mutex.RUnlock()
	}
}

func BenchmarkFlushCtx(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		allDone := sync.WaitGroup{}
		f := &flushCtx{
			lock:            &sync.RWMutex{},
			flushDone:       make(chan struct{}),
			newCtxAvailable: make(chan struct{}),
		}
		for i := 0; i < 1; i++ {
			allDone.Add(1)
			go func() {
				f.lock.RLock()
				f.lock.RUnlock()
				<-f.flushDone
				allDone.Done()
			}()
		}

		close(f.flushDone)
		allDone.Wait()
	}
}
