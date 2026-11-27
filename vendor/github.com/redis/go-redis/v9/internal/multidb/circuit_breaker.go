// Package multidb provides internal components for multi-database support.
package multidb

import (
	"time"

	"github.com/redis/go-redis/v9/internal/circuitbreaker"
)

// CircuitState represents the state of a circuit breaker.
// This is an alias to the internal circuitbreaker package.
type CircuitState = circuitbreaker.State

// Reservation is a circuit-breaker admission handle returned by AllowReserve.
// This is an alias to the internal circuitbreaker package.
type Reservation = circuitbreaker.Reservation

const (
	// CircuitClosed indicates the circuit is closed and requests are allowed.
	CircuitClosed = circuitbreaker.StateClosed
	// CircuitOpen indicates the circuit is open and requests are blocked.
	CircuitOpen = circuitbreaker.StateOpen
	// CircuitHalfOpen indicates the circuit is testing if the service has recovered.
	CircuitHalfOpen = circuitbreaker.StateHalfOpen
)

// CircuitBreakerConfig holds configuration for a circuit breaker.
type CircuitBreakerConfig struct {
	// FailureThreshold is the number of failures before opening the circuit.
	FailureThreshold int
	// SuccessThreshold is the number of successes in half-open state before closing.
	SuccessThreshold int
	// GracePeriod is how long to wait before transitioning from open to half-open.
	// This grace period gives a failed database time to self-heal before it is
	// probed again. Default: 60 seconds.
	GracePeriod time.Duration
}

// DefaultCircuitBreakerConfig returns the default circuit breaker configuration.
func DefaultCircuitBreakerConfig() CircuitBreakerConfig {
	return CircuitBreakerConfig{
		FailureThreshold: 5,
		SuccessThreshold: 2,
		GracePeriod:      60 * time.Second,
	}
}

// CircuitBreakerCallback is called when the circuit breaker state changes.
type CircuitBreakerCallback func(oldState, newState CircuitState)

// CircuitBreaker wraps the internal circuitbreaker.CircuitBreaker.
//
// MultiDB feeds it two kinds of outcome: admitted commands (the reservation
// API) and health probes (the ForReset API). The base breaker keeps them as
// two consecutive-failure streaks, and Stats() reports both: Failures is the
// command streak, ExternalFailures the probe streak. Either reaching
// FailureThreshold opens the circuit; a successful command ends only the
// first, a passing probe only the second.
type CircuitBreaker struct {
	*circuitbreaker.CircuitBreaker
	config CircuitBreakerConfig
}

// NewCircuitBreaker creates a new circuit breaker with the given configuration.
// Zero-valued fields are replaced with their defaults before the config is
// stored, so Config() always reports the values the breaker actually runs with.
func NewCircuitBreaker(config CircuitBreakerConfig) *CircuitBreaker {
	def := DefaultCircuitBreakerConfig()
	if config.FailureThreshold <= 0 {
		config.FailureThreshold = def.FailureThreshold
	}
	if config.SuccessThreshold <= 0 {
		config.SuccessThreshold = def.SuccessThreshold
	}
	if config.GracePeriod <= 0 {
		config.GracePeriod = def.GracePeriod
	}
	return &CircuitBreaker{
		CircuitBreaker: circuitbreaker.New(circuitbreaker.Config{
			FailureThreshold:    config.FailureThreshold,
			SuccessThreshold:    config.SuccessThreshold,
			MaxHalfOpenRequests: config.SuccessThreshold,
			OpenTimeout:         config.GracePeriod,
		}),
		config: config,
	}
}

// OnStateChange registers a callback to be called when the state changes.
func (cb *CircuitBreaker) OnStateChange(callback CircuitBreakerCallback) {
	cb.CircuitBreaker.OnStateChange(func(oldState, newState circuitbreaker.State, _ circuitbreaker.Stats) {
		callback(oldState, newState)
	})
}

// Config returns the circuit breaker configuration.
func (cb *CircuitBreaker) Config() CircuitBreakerConfig {
	return cb.config
}
