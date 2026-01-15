package errors

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TS07: Circuit breaker opens after 5 failures
func TestCircuitBreaker_OpensAfterMaxFailures(t *testing.T) {
	// Given: a circuit breaker with max 3 failures
	cb := NewCircuitBreaker("test",
		WithMaxFailures(3),
		WithResetTimeout(1*time.Second),
	)

	// When: recording 3 failures
	for i := 0; i < 3; i++ {
		_ = cb.Execute(func() error {
			return errors.New("error")
		})
	}

	// Then: circuit is open
	assert.Equal(t, StateOpen, cb.State())

	// And: requests are rejected
	err := cb.Execute(func() error {
		return nil // Would succeed if called
	})
	assert.Error(t, err)
	assert.True(t, errors.Is(err, ErrCircuitOpen))
}

// TS08: Circuit breaker recovers after timeout
func TestCircuitBreaker_RecoversAfterTimeout(t *testing.T) {
	// Given: an open circuit breaker
	cb := NewCircuitBreaker("test",
		WithMaxFailures(2),
		WithResetTimeout(50*time.Millisecond),
	)

	// Trip the circuit
	for i := 0; i < 2; i++ {
		_ = cb.Execute(func() error {
			return errors.New("error")
		})
	}
	require.Equal(t, StateOpen, cb.State())

	// When: waiting for reset timeout
	time.Sleep(60 * time.Millisecond)

	// Then: circuit is half-open and allows one request
	executed := false
	err := cb.Execute(func() error {
		executed = true
		return nil
	})

	assert.NoError(t, err)
	assert.True(t, executed)
	assert.Equal(t, StateClosed, cb.State())
}

func TestCircuitBreaker_HalfOpenFailureReOpens(t *testing.T) {
	// Given: a circuit in half-open state
	cb := NewCircuitBreaker("test",
		WithMaxFailures(2),
		WithResetTimeout(50*time.Millisecond),
	)

	// Trip and wait for half-open
	for i := 0; i < 2; i++ {
		_ = cb.Execute(func() error { return errors.New("error") })
	}
	time.Sleep(60 * time.Millisecond)

	// When: the test request fails
	_ = cb.Execute(func() error {
		return errors.New("still failing")
	})

	// Then: circuit reopens
	assert.Equal(t, StateOpen, cb.State())
}

func TestCircuitBreaker_SuccessResetsClosed(t *testing.T) {
	// Given: a circuit breaker with some failures (but not tripped)
	cb := NewCircuitBreaker("test",
		WithMaxFailures(5),
		WithResetTimeout(1*time.Second),
	)

	// Record some failures
	for i := 0; i < 3; i++ {
		_ = cb.Execute(func() error { return errors.New("error") })
	}

	// When: a success occurs
	err := cb.Execute(func() error { return nil })

	// Then: failure count resets
	assert.NoError(t, err)
	assert.Equal(t, StateClosed, cb.State())
	assert.Equal(t, 0, cb.Failures())
}

func TestCircuitBreaker_ExecuteWithFallback(t *testing.T) {
	// Given: an open circuit breaker
	cb := NewCircuitBreaker("test",
		WithMaxFailures(1),
		WithResetTimeout(1*time.Second),
	)

	// Trip the circuit
	_ = cb.Execute(func() error { return errors.New("error") })

	// When: executing with fallback
	fallbackCalled := false
	result, err := cb.ExecuteWithResult(
		func() (string, error) {
			return "primary", nil
		},
		func() (string, error) {
			fallbackCalled = true
			return "fallback", nil
		},
	)

	// Then: fallback is used
	assert.NoError(t, err)
	assert.True(t, fallbackCalled)
	assert.Equal(t, "fallback", result)
}

func TestCircuitBreaker_Concurrent(t *testing.T) {
	// Given: a circuit breaker
	cb := NewCircuitBreaker("test",
		WithMaxFailures(10),
		WithResetTimeout(1*time.Second),
	)

	// When: concurrent requests
	var wg sync.WaitGroup
	var successCount atomic.Int32
	var failCount atomic.Int32

	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			err := cb.Execute(func() error {
				if i%2 == 0 {
					return nil
				}
				return errors.New("error")
			})
			if err == nil {
				successCount.Add(1)
			} else {
				failCount.Add(1)
			}
		}(i)
	}

	wg.Wait()

	// Then: all requests complete without panic
	assert.Equal(t, int32(20), successCount.Load()+failCount.Load())
}

func TestCircuitBreaker_Allow_WhenClosed(t *testing.T) {
	cb := NewCircuitBreaker("test")

	// When: circuit is closed
	allowed := cb.Allow()

	// Then: requests are allowed
	assert.True(t, allowed)
}

func TestCircuitBreaker_Allow_WhenOpen(t *testing.T) {
	cb := NewCircuitBreaker("test",
		WithMaxFailures(1),
		WithResetTimeout(1*time.Second),
	)

	// Trip the circuit
	_ = cb.Execute(func() error { return errors.New("error") })

	// When: circuit is open
	allowed := cb.Allow()

	// Then: requests are not allowed
	assert.False(t, allowed)
}

func TestCircuitBreaker_RecordSuccess(t *testing.T) {
	cb := NewCircuitBreaker("test", WithMaxFailures(5))

	// Record some failures
	cb.RecordFailure()
	cb.RecordFailure()
	assert.Equal(t, 2, cb.Failures())

	// When: recording success
	cb.RecordSuccess()

	// Then: failures reset
	assert.Equal(t, 0, cb.Failures())
	assert.Equal(t, StateClosed, cb.State())
}

func TestCircuitBreaker_RecordFailure(t *testing.T) {
	cb := NewCircuitBreaker("test", WithMaxFailures(3))

	// When: recording failures
	cb.RecordFailure()
	cb.RecordFailure()

	// Then: failure count increases
	assert.Equal(t, 2, cb.Failures())
	assert.Equal(t, StateClosed, cb.State())

	// And: one more trips it
	cb.RecordFailure()
	assert.Equal(t, StateOpen, cb.State())
}

func TestNewCircuitBreaker_DefaultValues(t *testing.T) {
	cb := NewCircuitBreaker("test-circuit")

	assert.Equal(t, "test-circuit", cb.Name())
	assert.Equal(t, 5, cb.maxFailures)
	assert.Equal(t, 30*time.Second, cb.resetTimeout)
	assert.Equal(t, StateClosed, cb.State())
}

func TestCircuitBreaker_Name(t *testing.T) {
	cb := NewCircuitBreaker("my-service")
	assert.Equal(t, "my-service", cb.Name())
}

func TestErrCircuitOpen_Error(t *testing.T) {
	err := ErrCircuitOpen
	assert.Equal(t, "circuit breaker is open", err.Error())
}
