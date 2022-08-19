package scribe

import "sync/atomic"

type counter struct {
	n int64
}

func (c *counter) Next() int64 {
	n := atomic.LoadInt64(&c.n)
	atomic.AddInt64(&c.n, 1)
	return n
}
