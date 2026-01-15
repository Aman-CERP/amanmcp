package errors

import (
	"errors"
	"sync"
	"time"
)

// ErrCircuitOpen is returned when the circuit breaker is open.
var ErrCircuitOpen = errors.New("circuit breaker is open")

// State represents the circuit breaker state.
type State int

const (
	// StateClosed is the normal state where requests are allowed.
	StateClosed State = iota
	// StateOpen is when the circuit is tripped and requests are blocked.
	StateOpen
	// StateHalfOpen is when the circuit is testing if the service recovered.
	StateHalfOpen
)

// String returns a string representation of the state.
func (s State) String() string {
	switch s {
	case StateClosed:
		return "closed"
	case StateOpen:
		return "open"
	case StateHalfOpen:
		return "half-open"
	default:
		return "unknown"
	}
}

// CircuitBreaker implements the circuit breaker pattern.
// It protects against cascading failures by failing fast when a service is down.
type CircuitBreaker struct {
	name         string
	maxFailures  int
	resetTimeout time.Duration

	mu           sync.RWMutex
	state        State
	failures     int
	lastFailure  time.Time
}

// CircuitBreakerOption configures a CircuitBreaker.
type CircuitBreakerOption func(*CircuitBreaker)

// WithMaxFailures sets the number of failures before opening the circuit.
func WithMaxFailures(n int) CircuitBreakerOption {
	return func(cb *CircuitBreaker) {
		cb.maxFailures = n
	}
}

// WithResetTimeout sets the time to wait before attempting recovery.
func WithResetTimeout(d time.Duration) CircuitBreakerOption {
	return func(cb *CircuitBreaker) {
		cb.resetTimeout = d
	}
}

// NewCircuitBreaker creates a new circuit breaker with the given name.
// Default: 5 failures, 30 second reset timeout.
func NewCircuitBreaker(name string, opts ...CircuitBreakerOption) *CircuitBreaker {
	cb := &CircuitBreaker{
		name:         name,
		maxFailures:  5,
		resetTimeout: 30 * time.Second,
		state:        StateClosed,
	}

	for _, opt := range opts {
		opt(cb)
	}

	return cb
}

// Name returns the circuit breaker name.
func (cb *CircuitBreaker) Name() string {
	return cb.name
}

// State returns the current circuit breaker state.
func (cb *CircuitBreaker) State() State {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	return cb.currentState()
}

// currentState returns the state, checking for transition to half-open.
// Must be called with at least a read lock held.
func (cb *CircuitBreaker) currentState() State {
	if cb.state == StateOpen {
		if time.Since(cb.lastFailure) > cb.resetTimeout {
			return StateHalfOpen
		}
	}
	return cb.state
}

// Failures returns the current failure count.
func (cb *CircuitBreaker) Failures() int {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	return cb.failures
}

// Allow checks if a request should be allowed through.
func (cb *CircuitBreaker) Allow() bool {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	switch cb.currentState() {
	case StateClosed:
		return true
	case StateHalfOpen:
		return true
	default: // StateOpen
		return false
	}
}

// RecordSuccess records a successful request.
func (cb *CircuitBreaker) RecordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.failures = 0
	cb.state = StateClosed
}

// RecordFailure records a failed request.
func (cb *CircuitBreaker) RecordFailure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.failures++
	cb.lastFailure = time.Now()

	if cb.failures >= cb.maxFailures {
		cb.state = StateOpen
	}
}

// Execute runs a function through the circuit breaker.
// Returns ErrCircuitOpen if the circuit is open.
func (cb *CircuitBreaker) Execute(fn func() error) error {
	cb.mu.Lock()
	state := cb.currentState()

	switch state {
	case StateOpen:
		cb.mu.Unlock()
		return ErrCircuitOpen

	case StateHalfOpen:
		// Transition to half-open allows one test request
		cb.state = StateHalfOpen
		cb.mu.Unlock()

		err := fn()
		if err != nil {
			cb.mu.Lock()
			cb.state = StateOpen
			cb.lastFailure = time.Now()
			cb.mu.Unlock()
			return err
		}

		cb.RecordSuccess()
		return nil

	default: // StateClosed
		cb.mu.Unlock()

		err := fn()
		if err != nil {
			cb.RecordFailure()
			return err
		}

		cb.RecordSuccess()
		return nil
	}
}

// ExecuteWithResult runs a function that returns a value through the circuit breaker.
// If the circuit is open, the fallback function is called instead.
func (cb *CircuitBreaker) ExecuteWithResult(fn func() (string, error), fallback func() (string, error)) (string, error) {
	cb.mu.Lock()
	state := cb.currentState()

	switch state {
	case StateOpen:
		cb.mu.Unlock()
		return fallback()

	case StateHalfOpen:
		cb.state = StateHalfOpen
		cb.mu.Unlock()

		result, err := fn()
		if err != nil {
			cb.mu.Lock()
			cb.state = StateOpen
			cb.lastFailure = time.Now()
			cb.mu.Unlock()
			return fallback()
		}

		cb.RecordSuccess()
		return result, nil

	default: // StateClosed
		cb.mu.Unlock()

		result, err := fn()
		if err != nil {
			cb.RecordFailure()
			return result, err
		}

		cb.RecordSuccess()
		return result, nil
	}
}

// CircuitExecuteWithResult is a generic function for executing with fallback.
func CircuitExecuteWithResult[T any](cb *CircuitBreaker, fn func() (T, error), fallback func() (T, error)) (T, error) {
	cb.mu.Lock()
	state := cb.currentState()

	switch state {
	case StateOpen:
		cb.mu.Unlock()
		return fallback()

	case StateHalfOpen:
		cb.state = StateHalfOpen
		cb.mu.Unlock()

		result, err := fn()
		if err != nil {
			cb.mu.Lock()
			cb.state = StateOpen
			cb.lastFailure = time.Now()
			cb.mu.Unlock()
			return fallback()
		}

		cb.RecordSuccess()
		return result, nil

	default: // StateClosed
		cb.mu.Unlock()

		result, err := fn()
		if err != nil {
			cb.RecordFailure()
			return result, err
		}

		cb.RecordSuccess()
		return result, nil
	}
}
