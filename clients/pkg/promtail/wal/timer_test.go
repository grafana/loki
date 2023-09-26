package wal

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const (
	delta = time.Millisecond * 10
)

func TestBackoffTimer(t *testing.T) {
	var min = time.Millisecond * 300
	var max = time.Second
	timer := newBackoffTimer(min, max)

	now := time.Now()
	<-timer.C
	require.WithinDuration(t, now.Add(min), time.Now(), delta, "expected backing off timer to fire in the minimum")

	// backoff, and expect it will take twice the time
	now = time.Now()
	timer.backoff()
	<-timer.C
	require.WithinDuration(t, now.Add(min*2), time.Now(), delta, "expected backing off timer to fire in the twice the minimum")

	// backoff capped, backoff will actually be 1200ms, but capped at 1000
	now = time.Now()
	timer.backoff()
	<-timer.C
	require.WithinDuration(t, now.Add(max), time.Now(), delta, "expected backing off timer to fire in the max")
}
