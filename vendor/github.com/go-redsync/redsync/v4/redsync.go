package redsync

import (
	"math/rand"
	"time"

	"github.com/go-redsync/redsync/v4/redis"
)

const (
	minRetryDelayMilliSec = 50
	maxRetryDelayMilliSec = 250
)

// Redsync provides a simple method for creating distributed mutexes using multiple Redis connection pools.
type Redsync struct {
	pools []redis.Pool
}

// New creates and returns a new Redsync instance from given Redis connection pools.
func New(pools ...redis.Pool) *Redsync {
	return &Redsync{
		pools: pools,
	}
}

// NewMutex returns a new distributed mutex with given name.
func (r *Redsync) NewMutex(name string, options ...Option) *Mutex {
	m := &Mutex{
		name:   name,
		expiry: 8 * time.Second,
		tries:  32,
		delayFunc: func(tries int) time.Duration {
			return time.Duration(rand.Intn(maxRetryDelayMilliSec-minRetryDelayMilliSec)+minRetryDelayMilliSec) * time.Millisecond
		},
		genValueFunc:  genValue,
		driftFactor:   0.01,
		timeoutFactor: 0.05,
		quorum:        len(r.pools)/2 + 1,
		pools:         r.pools,
	}
	for _, o := range options {
		o.Apply(m)
	}
	if m.shuffle {
		randomPools(m.pools)
	}
	return m
}

// An Option configures a mutex.
type Option interface {
	Apply(*Mutex)
}

// OptionFunc is a function that configures a mutex.
type OptionFunc func(*Mutex)

// Apply calls f(mutex)
func (f OptionFunc) Apply(mutex *Mutex) {
	f(mutex)
}

// WithExpiry can be used to set the expiry of a mutex to the given value.
// The default is 8s.
func WithExpiry(expiry time.Duration) Option {
	return OptionFunc(func(m *Mutex) {
		m.expiry = expiry
	})
}

// WithTries can be used to set the number of times lock acquire is attempted.
// The default value is 32.
func WithTries(tries int) Option {
	return OptionFunc(func(m *Mutex) {
		m.tries = tries
	})
}

// WithRetryDelay can be used to set the amount of time to wait between retries.
// The default value is rand(50ms, 250ms).
func WithRetryDelay(delay time.Duration) Option {
	return OptionFunc(func(m *Mutex) {
		m.delayFunc = func(tries int) time.Duration {
			return delay
		}
	})
}

// WithSetNXOnExtend improves extending logic to extend the key if exist
// and if not, tries to set a new key in redis
// Useful if your redises restart often and you want to reduce the chances of losing the lock
// Read this MR for more info: https://github.com/go-redsync/redsync/pull/149
func WithSetNXOnExtend() Option {
	return OptionFunc(func(m *Mutex) {
		m.setNXOnExtend = true
	})
}

// WithRetryDelayFunc can be used to override default delay behavior.
func WithRetryDelayFunc(delayFunc DelayFunc) Option {
	return OptionFunc(func(m *Mutex) {
		m.delayFunc = delayFunc
	})
}

// WithDriftFactor can be used to set the clock drift factor.
// The default value is 0.01.
func WithDriftFactor(factor float64) Option {
	return OptionFunc(func(m *Mutex) {
		m.driftFactor = factor
	})
}

// WithTimeoutFactor can be used to set the timeout factor.
// The default value is 0.05.
func WithTimeoutFactor(factor float64) Option {
	return OptionFunc(func(m *Mutex) {
		m.timeoutFactor = factor
	})
}

// WithGenValueFunc can be used to set the custom value generator.
func WithGenValueFunc(genValueFunc func() (string, error)) Option {
	return OptionFunc(func(m *Mutex) {
		m.genValueFunc = genValueFunc
	})
}

// WithValue can be used to assign the random value without having to call lock.
// This allows the ownership of a lock to be "transferred" and allows the lock to be unlocked from elsewhere.
func WithValue(v string) Option {
	return OptionFunc(func(m *Mutex) {
		m.value = v
	})
}

// WithFailFast can be used to quickly acquire and release the lock.
// When some Redis servers are blocking, we do not need to wait for responses from all the Redis servers response.
// As long as the quorum is met, we can assume the lock is acquired. The effect of this parameter is to achieve low
// latency, avoid Redis blocking causing Lock/Unlock to not return for a long time.
func WithFailFast(b bool) Option {
	return OptionFunc(func(m *Mutex) {
		m.failFast = b
	})
}

// WithShufflePools can be used to shuffle Redis pools to reduce centralized access in concurrent scenarios.
func WithShufflePools(b bool) Option {
	return OptionFunc(func(m *Mutex) {
		m.shuffle = b
	})
}

// randomPools shuffles Redis pools.
func randomPools(pools []redis.Pool) {
	rand.Shuffle(len(pools), func(i, j int) {
		pools[i], pools[j] = pools[j], pools[i]
	})
}
