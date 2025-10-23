package workflow

import (
	"context"
	"math/rand/v2"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/atomic"
)

func TestTokenBucket_BasicFunctionality(t *testing.T) {
	bucket := newTokenBucket(10)

	// Test initial capacity
	require.Equal(t, uint(10), bucket.Available(), "expected 10 initial tokens")

	// Test allocation
	err := bucket.Claim(5)
	require.NoError(t, err)

	require.Equal(t, uint(5), bucket.Available(), "expected 5 tokens after allocating 5")

	// Test freeing - with max logic, freeing 3 tokens when we have 5 results in max(5+3, 10) = 10
	bucket.Return(3)
	require.Equal(t, uint(8), bucket.Available(), "expected 8 tokens after freeing 3")

	// Test freeing more tokens, tokens can exceed capacity
	bucket.Return(10)
	require.Equal(t, uint(10), bucket.Available(), "expected 10 tokens after freeing more than missing")
}

func TestTokenBucket_BlockingBehavior(t *testing.T) {
	bucket := newTokenBucket(5)

	// Claim some tokens
	_ = bucket.Claim(3)

	// Test that allocation blocks when not enough tokens
	done := make(chan bool, 1)
	go func() {
		_ = bucket.Claim(3) // Should block because only 2 tokens are left, ignore error for blocking test
		done <- true
	}()

	// Give some time for the goroutine to block
	select {
	case <-done:
		t.Error("Claim() should have blocked but didn't")
	case <-time.After(250 * time.Millisecond):
		// Expected behavior - claim is blocked
	}

	// Return a token to unblock the claim
	bucket.Return(2)

	select {
	case <-done:
		// Expected - claim completed
	case <-time.After(1 * time.Second):
		t.Error("Claim() should have completed after freeing tokens")
	}
}

func TestTokenBucket_ConcurrentClaimAndReturn(t *testing.T) {
	bucket := newTokenBucket(100)
	numGoroutines := 20
	tokensPerGoroutine := 8
	totalTokens := numGoroutines * tokensPerGoroutine

	// We want to allocate more tokens than initially available
	require.Greater(t, uint(totalTokens), bucket.Available())

	var wg sync.WaitGroup
	counter := atomic.NewInt64(0)

	// Start multiple goroutines trying to claim and return tokens
	// Each goroutine performs an Claim() before a Return() operation.
	for range numGoroutines {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := bucket.Claim(uint(tokensPerGoroutine))
			require.NoError(t, err)
			counter.Add(1)

			wg.Add(1)
			// Start multiple goroutines trying to free tokens
			go func() {
				defer wg.Done()
				time.Sleep(time.Duration(rand.Int64N(100)) * time.Millisecond) // wait before putting back the tokens
				bucket.Return(uint(tokensPerGoroutine))
				counter.Sub(1)
			}()

		}()
	}

	wg.Wait()

	require.Equal(t, 0, int(counter.Load()))        // numClaims == numReturns
	require.Equal(t, uint(100), bucket.Available()) // initial capacity
}

func TestTokenBucket_EdgeCases(t *testing.T) {
	t.Run("claiming 0 tokens returns an error", func(t *testing.T) {
		bucket := newTokenBucket(10)
		err := bucket.Claim(0)
		require.ErrorContains(t, err, "cannot claim less than one token: requested 0")
	})

	t.Run("returning 0 tokens is a no-op", func(t *testing.T) {
		bucket := newTokenBucket(10)
		for range 100 {
			bucket.Return(0)
			require.Equal(t, uint(10), bucket.Available())
		}
	})

	t.Run("claiming more than capacity returns an error", func(t *testing.T) {
		bucket := newTokenBucket(10)
		err := bucket.Claim(15)
		require.ErrorContains(t, err, "bucket has insufficient capacity: has 10, requested 15")
	})
}

func BenchmarkTokenBucket_ClaimReturn(b *testing.B) {
	bucket := newTokenBucket(1000)
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			if i%2 == 0 {
				_ = bucket.Claim(5) // Ignore errors in benchmark
			} else {
				bucket.Return(5)
			}
			i++
		}
	})
}

func TestTokenBucketWithContext_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	bucket := newTokenBucketWithContext(ctx, 5)

	// Claim some tokens
	_ = bucket.Claim(3)

	// Test that allocation blocks when not enough tokens
	done := make(chan error, 1)
	go func() {
		err := bucket.Claim(3) // Should block because only 2 tokens are left, ignore error for blocking test
		done <- err
	}()

	// Give some time for the goroutine to block
	select {
	case <-done:
		t.Error("Claim() should have blocked but didn't")
	case <-time.After(100 * time.Millisecond):
		// Expected behavior - allocation is blocked
	}

	// Cancel the context to unblock the Claim() call, which will return an error
	cancel()

	select {
	case err := <-done:
		// Claim() returned an error
		require.ErrorContains(t, err, context.Canceled.Error())
	case <-time.After(1 * time.Second):
		t.Error("Claim() should have completed after freeing tokens")
	}
}

func TestTokenBucketWithContext_ContextTimeout(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
	defer cancel()
	bucket := newTokenBucketWithContext(ctx, 5)

	// Claim some tokens
	_ = bucket.Claim(3)

	// Test that allocation blocks when not enough tokens
	done := make(chan error, 1)
	go func() {
		err := bucket.Claim(3) // Should block because only 2 tokens are left, ignore error for blocking test
		done <- err
	}()

	// Give some time for the goroutine to block
	select {
	case <-done:
		t.Error("Claim() should have blocked but didn't")
	case <-time.After(100 * time.Millisecond):
		// Expected behavior - claim is blocked
	}

	select {
	case err := <-done:
		// Claim() returned an error
		require.ErrorContains(t, err, context.DeadlineExceeded.Error())
	case <-time.After(1 * time.Second):
		t.Error("Claim() should have completed after freeing tokens")
	}
}

func TestBatchTokenBucket_BlockingBehavior(t *testing.T) {
	bucket := newBatchTokenBucket(5)

	_ = bucket.Claim(2) // does not block
	_ = bucket.Claim(2) // does not block

	// Test that allocation blocks when not enough tokens
	done := make(chan bool, 1)
	go func() {
		_ = bucket.Claim(2) // blocks because not enough tokens are available
		done <- true
	}()

	// Give some time for the goroutine to block
	select {
	case <-done:
		t.Error("Claim() should have blocked but didn't")
	case <-time.After(100 * time.Millisecond):
		t.Log("Claim() is still blocked ... OK")
	}

	// Return a single token, which would mean it could fulfill the blocked Claim(2).
	bucket.Return(1)

	// However, since the batch token bucket requires to be full again before it fulfills claims,
	// the original claim still blocks.
	select {
	case <-done:
		t.Error("Claim() should have blocked but didn't")
	case <-time.After(100 * time.Millisecond):
		t.Log("Claim() is still blocked ... OK")
	}

	// Return remaining tokens so bucket is full again.
	bucket.Return(3)

	select {
	case <-done:
		t.Log("Claim() has been fulfilled ... OK")
	case <-time.After(1 * time.Second):
		t.Error("Claim() should have completed after returning all tokens")
	}
}
