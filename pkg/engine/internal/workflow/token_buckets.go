package workflow

import (
	"context"
	"fmt"
	"sync"
)

// Bucket represents a token bucket for rate limiting or resource management.
// It implements a bucket token algorithm where tokens represent available resources
// or permits that can be claimed and later returned.
//
// The bucket maintains a fixed capacity and allows consumers to claim tokens
// when available. If insufficient tokens are available, the Claim method will
// block until enough tokens become available through Return calls or return
// an error if the request cannot be satisfied.
type TokenBucket interface {
	// Claim asks the bucket for a number of resources.
	// The implementation must block and wait until the claim can be fulfilled
	// or return an error.
	Claim(uint) error

	// Return returns a number of claimed resources to the bucket.
	Return(uint)
}

// simpleTokenBucket is a basic implementation of the TokenBucket interface that
// provides thread-safe token-based resource management using a traditional token
// bucket algorithm.
//
// The bucket maintains a fixed capacity of tokens and allows consumers to claim
// tokens when available. If insufficient tokens are available for a claim, the
// Claim method will block the calling goroutine until enough tokens become
// available through Return calls from other goroutines.
type simpleTokenBucket struct {
	lock     *sync.RWMutex
	cond     *sync.Cond
	capacity uint
	tokens   uint
}

func newTokenBucket(capacity uint) *simpleTokenBucket {
	lock := new(sync.RWMutex)
	cond := sync.NewCond(lock)
	return &simpleTokenBucket{
		capacity: capacity,
		tokens:   capacity,
		lock:     lock,
		cond:     cond,
	}
}

func (b *simpleTokenBucket) Available() uint {
	b.lock.RLock()
	defer b.lock.RUnlock()
	return b.tokens
}

func (b *simpleTokenBucket) Claim(n uint) error {
	if n < 1 {
		return errTooFewTokens(n)
	}

	if n > b.capacity {
		return errInsufficientCapacity(b.capacity, n)
	}

	b.lock.Lock()
	defer b.lock.Unlock()

	for b.tokens < n {
		b.cond.Wait()
	}

	b.tokens -= n
	return nil
}

func (b *simpleTokenBucket) Return(n uint) {
	if n == 0 {
		return
	}
	b.lock.Lock()
	b.tokens = min(b.tokens+n, b.capacity)
	b.lock.Unlock()

	b.cond.Signal()
}

func errTooFewTokens(requested uint) error {
	return fmt.Errorf("cannot claim less than one token: requested %d", requested)
}

func errInsufficientCapacity(has, requested uint) error {
	return fmt.Errorf("bucket has insufficient capacity: has %d, requested %d", has, requested)
}

// tokenBucketWithContext is a wrapper around [simpleTokenBucket] that adds
// context-aware cancellation support. It allows token claims to be interrupted
// when the associated context is cancelled or times out.
//
// When a Claim() operation would normally block waiting for tokens to become
// available, this implementation will also monitor the context and return an
// error if the context is cancelled or expires before the claim can be fulfilled.
//
// The wrapper maintains all the functionality of the underlying [simpleTokenBucket]
// while adding context-aware error handling. If the context is already cancelled
// when Claim() is called, it returns immediately with the context's error.
type tokenBucketWithContext struct {
	ctx    context.Context
	bucket *simpleTokenBucket
}

func newTokenBucketWithContext(ctx context.Context, capacity uint) *tokenBucketWithContext {
	tb := &tokenBucketWithContext{
		bucket: newTokenBucket(capacity),
		ctx:    ctx,
	}

	// Wait for the context to be done and then send the signal
	// so the waiting claim can return an error.
	go func() {
		<-ctx.Done()
		tb.bucket.cond.Signal()
	}()
	return tb
}

func (b *tokenBucketWithContext) Available() uint {
	return b.bucket.Available()
}

func (b *tokenBucketWithContext) Claim(n uint) error {
	if n < 1 {
		return errTooFewTokens(n)
	}

	if n > b.bucket.capacity {
		return errInsufficientCapacity(b.bucket.capacity, n)
	}

	b.bucket.lock.Lock()
	defer b.bucket.lock.Unlock()

	if err := b.ctx.Err(); err != nil {
		return err
	}

	for b.bucket.tokens < n {
		b.bucket.cond.Wait()

		// Check if the context is done.
		// If so, return the error to the waiting caller.
		if err := b.ctx.Err(); err != nil {
			return err
		}
	}

	b.bucket.tokens -= n
	return nil
}

func (b *tokenBucketWithContext) Return(n uint) {
	b.bucket.Return(n)
}

// batchTokenBucket is a specialized token bucket implementation that enforces
// batch-like behavior for resource management. Unlike a regular token bucket,
// it only allows new claims when the bucket is in a "ready" state.
//
// The bucket becomes "ready" only when all tokens have been returned (tokens == capacity).
// Once tokens are claimed from a ready bucket, it becomes "not ready" and will block
// all subsequent claims until all outstanding tokens are returned.
//
// This behavior is useful for scenarios where you want to ensure that all resources
// from a previous batch are fully released before allowing a new batch to begin.
type batchTokenBucket struct {
	lock     *sync.RWMutex
	cond     *sync.Cond
	capacity uint
	tokens   uint
	ready    bool
}

func newBatchTokenBucket(capacity uint) *batchTokenBucket {
	lock := new(sync.RWMutex)
	cond := sync.NewCond(lock)
	return &batchTokenBucket{
		capacity: capacity,
		tokens:   capacity,
		lock:     lock,
		cond:     cond,
		ready:    true,
	}
}

func (b *batchTokenBucket) Available() uint {
	b.lock.RLock()
	defer b.lock.RUnlock()
	return b.tokens
}

func (b *batchTokenBucket) Claim(n uint) error {
	if n < 1 {
		return errTooFewTokens(n)
	}

	if n > b.capacity {
		return errInsufficientCapacity(b.capacity, n)
	}

	b.lock.Lock()
	defer b.lock.Unlock()

	// Wait if there are not enough tokens to fulfill the claim,
	// or if the bucket is not ready (not completely fill up).
	for b.tokens < n || !b.ready {
		b.ready = false
		b.cond.Wait()
	}

	b.tokens -= n
	return nil
}

func (b *batchTokenBucket) Return(n uint) {
	b.lock.Lock()
	b.tokens = min(b.tokens+n, b.capacity)
	// Become ready again once all tokens have been returned
	if b.tokens == b.capacity {
		b.ready = true
	}
	b.lock.Unlock()

	b.cond.Signal()
}

// batchTokenBucketWithContext is a wrapper around [batchTokenBucket] that combines
// the batch-like resource management behavior with context-aware cancellation support.
// It allows token claims to be interrupted when the associated context is cancelled
// or times out.
//
// When a Claim() operation would normally block waiting for tokens to become
// available, this implementation will also monitor the context and return an
// error if the context is cancelled or expires before the claim can be fulfilled.
//
// The wrapper maintains all the functionality of the underlying [batchTokenBucket]
// while adding context-aware error handling. If the context is already cancelled
// when Claim() is called, it returns immediately with the context's error.
type batchTokenBucketWithContext struct {
	inner *batchTokenBucket
	ctx   context.Context
}

func newBatchTokenBucketWithContext(ctx context.Context, capacity uint) *batchTokenBucketWithContext {
	return &batchTokenBucketWithContext{
		inner: newBatchTokenBucket(capacity),
		ctx:   ctx,
	}
}

func (b *batchTokenBucketWithContext) Available() uint {
	return b.inner.Available()
}

func (b *batchTokenBucketWithContext) Claim(n uint) error {
	if n < 1 {
		return errTooFewTokens(n)
	}

	if n > b.inner.capacity {
		return errInsufficientCapacity(b.inner.capacity, n)
	}

	b.inner.lock.Lock()
	defer b.inner.lock.Unlock()

	if err := b.ctx.Err(); err != nil {
		return err
	}

	// Wait if there are not enough tokens to fulfill the claim,
	// or if the bucket is not ready (not completely fill up).
	for b.inner.tokens < n || !b.inner.ready {
		b.inner.ready = false
		b.inner.cond.Wait()

		// Check if the context is done.
		// If so, return the error to the waiting caller.
		if err := b.ctx.Err(); err != nil {
			return err
		}
	}

	b.inner.tokens -= n
	return nil
}

func (b *batchTokenBucketWithContext) Return(n uint) {
	b.inner.Return(n)
}
