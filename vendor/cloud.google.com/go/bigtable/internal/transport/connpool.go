// Copyright 2025 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package internal

import (
	"context"
	"fmt"
	"math/rand"
	"sync"
	"sync/atomic"

	gtransport "google.golang.org/api/transport/grpc"

	btopt "cloud.google.com/go/bigtable/internal/option"

	"google.golang.org/grpc"
)

var errNoConnections = fmt.Errorf("bigtable_connpool: no connections available in the pool")
var _ gtransport.ConnPool = &BigtableChannelPool{}

// BigtableChannelPool implements ConnPool and routes requests to the connection
// pool according to load balancing strategy.
//
// To benefit from automatic load tracking, use the Invoke and NewStream methods
// directly on the BigtableChannelPool instance.
type BigtableChannelPool struct {
	conns []*grpc.ClientConn
	load  []int64 // Tracks active requests per connection

	// Mutex is only used for selecting the least loaded connection.
	// The load array itself is manipulated using atomic operations.
	mu         sync.Mutex
	dial       func() (*grpc.ClientConn, error)
	strategy   btopt.LoadBalancingStrategy
	rrIndex    uint64              // For round-robin selection
	selectFunc func() (int, error) // Stored function for connection selection

}

// NewBigtableChannelPool creates a pool of connPoolSize and takes the dial func()
func NewBigtableChannelPool(connPoolSize int, strategy btopt.LoadBalancingStrategy, dial func() (*grpc.ClientConn, error)) (*BigtableChannelPool, error) {
	if connPoolSize <= 0 {
		return nil, fmt.Errorf("bigtable_connpool: connPoolSize must be positive")
	}

	if dial == nil {
		return nil, fmt.Errorf("bigtable_connpool: dial function cannot be nil")
	}
	pool := &BigtableChannelPool{
		dial:     dial,
		strategy: strategy,
		rrIndex:  0,
	}

	// Set the selection function based on the strategy
	switch strategy {
	case btopt.LeastInFlight:
		pool.selectFunc = pool.selectLeastLoaded
	case btopt.PowerOfTwoLeastInFlight:
		pool.selectFunc = pool.selectLeastLoadedRandomOfTwo
	default: // RoundRobin is the default
		pool.selectFunc = pool.selectRoundRobin
	}

	for i := 0; i < connPoolSize; i++ {
		conn, err := dial()
		if err != nil {
			defer pool.Close()
			return nil, err
		}
		pool.conns = append(pool.conns, conn)
		pool.load = append(pool.load, 0)

	}
	return pool, nil

}

// Num returns the number of connections in the pool.
func (p *BigtableChannelPool) Num() int {
	return len(p.conns)
}

// Close closes all connections in the pool.
func (p *BigtableChannelPool) Close() error {
	var errs multiError
	for _, conn := range p.conns {
		if err := conn.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) == 0 {
		return nil
	}
	return errs
}

// Invoke selects the least loaded connection and calls Invoke on it.
// This method provides automatic load tracking.
func (p *BigtableChannelPool) Invoke(ctx context.Context, method string, args interface{}, reply interface{}, opts ...grpc.CallOption) error {
	index, err := p.selectFunc()
	if err != nil {
		return err
	}
	conn := p.conns[index]

	atomic.AddInt64(&p.load[index], 1)
	defer atomic.AddInt64(&p.load[index], -1)

	return conn.Invoke(ctx, method, args, reply, opts...)
}

// Conn provides connbased on selectfunc()
func (p *BigtableChannelPool) Conn() *grpc.ClientConn {
	index, err := p.selectFunc()
	if err != nil {
		// no conn available
		return nil
	}
	return p.conns[index]
}

// NewStream selects the least loaded connection and calls NewStream on it.
// This method provides automatic load tracking via a wrapped stream.
func (p *BigtableChannelPool) NewStream(ctx context.Context, desc *grpc.StreamDesc, method string, opts ...grpc.CallOption) (grpc.ClientStream, error) {
	index, err := p.selectFunc()
	if err != nil {
		return nil, err
	}
	conn := p.conns[index]

	atomic.AddInt64(&p.load[index], 1)

	stream, err := conn.NewStream(ctx, desc, method, opts...)

	if err != nil {
		atomic.AddInt64(&p.load[index], -1) // Decrement if stream creation failed
		return nil, err
	}

	// Wrap the stream to decrement load when the stream finishes.
	return &refCountedStream{
		ClientStream: stream,
		pool:         p,
		connIndex:    index,
		once:         sync.Once{},
	}, nil
}

// selectLeastLoadedRandomOfTwo() returns the index of the connection via random of two
func (p *BigtableChannelPool) selectLeastLoadedRandomOfTwo() (int, error) {
	numConns := p.Num()
	if numConns == 0 {
		return -1, errNoConnections
	}
	if numConns == 1 {
		return 0, nil
	}

	// Pick two distinct random indices
	idx1 := rand.Intn(numConns)
	idx2 := rand.Intn(numConns)
	// Simple way to ensure they are different for small numConns.
	// For very large numConns, the chance of collision is low,
	// but a loop is safer.
	for idx2 == idx1 {
		idx2 = rand.Intn(numConns)
	}

	load1 := atomic.LoadInt64(&p.load[idx1])
	load2 := atomic.LoadInt64(&p.load[idx2])

	if load1 <= load2 {
		return idx1, nil
	}
	return idx2, nil
}

func (p *BigtableChannelPool) selectRoundRobin() (int, error) {
	numConns := p.Num()
	if numConns == 0 {
		return -1, errNoConnections
	}
	if numConns == 1 {
		return 0, nil
	}

	// Atomically increment and get the next index
	nextIndex := atomic.AddUint64(&p.rrIndex, 1) - 1
	return int(nextIndex % uint64(numConns)), nil
}

// selectLeastLoaded returns the index of the connection with the minimum load.
func (p *BigtableChannelPool) selectLeastLoaded() (int, error) {
	numConns := p.Num()

	if numConns == 0 {
		return -1, errNoConnections
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	minIndex := 0
	minLoad := atomic.LoadInt64(&p.load[0])

	for i := 1; i < p.Num(); i++ {
		currentLoad := atomic.LoadInt64(&p.load[i])
		if currentLoad < minLoad {
			minLoad = currentLoad
			minIndex = i
		}
	}
	return minIndex, nil
}

// refCountedStream wraps a grpc.ClientStream to decrement the load count when the stream is done.
// refCountedStream in this BigtableConnectionPool is to hook into the stream's lifecycle
// to decrement the load counter (s.pool.load[s.connIndex]) when the stream is no longer usable.
// This is primarily detected by errors occurring during SendMsg or RecvMsg (including io.EOF on RecvMsg).

// Another option would have been to use grpc.OnFinish for streams is about the timing of when the load should be considered "finished".
// The grpc.OnFinish callback is executed only when the entire stream is fully closed and the final status is determined.
type refCountedStream struct {
	grpc.ClientStream
	pool      *BigtableChannelPool
	connIndex int
	once      sync.Once
}

// SendMsg calls the embedded stream's SendMsg method.
func (s *refCountedStream) SendMsg(m interface{}) error {
	err := s.ClientStream.SendMsg(m)
	if err != nil {
		s.decrementLoad()
	}
	return err
}

// RecvMsg calls the embedded stream's RecvMsg method and decrements load on error.
func (s *refCountedStream) RecvMsg(m interface{}) error {
	err := s.ClientStream.RecvMsg(m)
	if err != nil { // io.EOF is also an error, indicating stream end.
		s.decrementLoad()
	}
	return err
}

// decrementLoad ensures the load count is decremented exactly once.
func (s *refCountedStream) decrementLoad() {
	s.once.Do(func() {
		atomic.AddInt64(&s.pool.load[s.connIndex], -1)
	})
}

type multiError []error

func (m multiError) Error() string {
	s, n := "", 0
	for _, e := range m {
		if e != nil {
			if n == 0 {
				s = e.Error()
			}
			n++
		}
	}
	switch n {
	case 0:
		return "(0 errors)"
	case 1:
		return s
	case 2:
		return s + " (and 1 other error)"
	}
	return fmt.Sprintf("%s (and %d other errors)", s, n-1)
}
