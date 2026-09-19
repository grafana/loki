package bce

import (
	"context"
	"time"

	"github.com/baidubce/bce-sdk-go/rate"
)

const (
	RateLimiterSlotRx int = iota
	RateLimiterSlotTx
	RateLimiterSlots
)

type RateLimiter struct {
	Bandwidth int64 // Byte/S
	Limiter   *rate.Limiter
}

type RateLimiters [RateLimiterSlots]*RateLimiter

func newRateLimiter(bandwidth int64) *RateLimiter {
	return &RateLimiter{
		Bandwidth: bandwidth,
		Limiter:   newTokenBucket(bandwidth),
	}
}

func newTokenBucket(bandwidth int64) *rate.Limiter {
	const defaultMaxBurstSize = 4 * 1024 * 1024
	maxBurstSize := (bandwidth * defaultMaxBurstSize) / (256 * 1024 * 1024)
	if maxBurstSize < defaultMaxBurstSize {
		maxBurstSize = defaultMaxBurstSize
	}
	tb := rate.NewLimiter(rate.Limit(bandwidth), int(maxBurstSize))
	tb.AllowN(time.Now(), int(maxBurstSize))
	return tb
}

func (rl *RateLimiter) LimitBandwidth(n int) {
	rl.Limiter.WaitN(context.Background(), n)
}
