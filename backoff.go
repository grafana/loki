package chunk

import (
	"math/rand"
	"time"
)

const (
	// Backoff for dynamoDB requests, to match AWS lib - see:
	// https://github.com/aws/aws-sdk-go/blob/master/service/dynamodb/customizations.go
	minBackoff = 50 * time.Millisecond
	maxBackoff = 50 * time.Second
	maxRetries = 20
)

type backoff struct {
	numRetries int
	duration   time.Duration
}

func resetBackoff() backoff {
	return backoff{numRetries: 0, duration: minBackoff}
}

func (b backoff) finished() bool {
	return b.numRetries >= maxRetries
}

func (b *backoff) backoff() {
	b.numRetries++
	b.backoffWithoutCounting()
}

func (b *backoff) backoffWithoutCounting() {
	if !b.finished() {
		time.Sleep(b.duration)
	}
	// Based on the "Decorrelated Jitter" approach from https://www.awsarchitectureblog.com/2015/03/backoff.html
	// sleep = min(cap, random_between(base, sleep * 3))
	b.duration = minBackoff + time.Duration(rand.Int63n(int64((b.duration*3)-minBackoff)))
	if b.duration > maxBackoff {
		b.duration = maxBackoff
	}
}
