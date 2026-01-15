package errors

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TS02: Retry succeeds on transient error
func TestRetry_SucceedsAfterTransientError(t *testing.T) {
	// Given: a function that fails twice then succeeds
	attempts := 0
	fn := func() error {
		attempts++
		if attempts < 3 {
			return errors.New("transient error")
		}
		return nil
	}

	// When: retrying with default config
	cfg := DefaultRetryConfig()
	cfg.InitialDelay = 10 * time.Millisecond // Speed up test

	err := Retry(context.Background(), cfg, fn)

	// Then: succeeds after 3 attempts
	assert.NoError(t, err)
	assert.Equal(t, 3, attempts)
}

func TestRetry_FailsAfterMaxRetries(t *testing.T) {
	// Given: a function that always fails
	attempts := 0
	fn := func() error {
		attempts++
		return errors.New("persistent error")
	}

	// When: retrying with limited retries
	cfg := RetryConfig{
		MaxRetries:   2,
		InitialDelay: 10 * time.Millisecond,
		MaxDelay:     100 * time.Millisecond,
		Multiplier:   2.0,
	}

	err := Retry(context.Background(), cfg, fn)

	// Then: fails with wrapped error
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "after 2 retries")
	assert.Equal(t, 3, attempts) // Initial + 2 retries
}

func TestRetry_RespectsContextCancellation(t *testing.T) {
	// Given: a function that takes time
	fn := func() error {
		time.Sleep(100 * time.Millisecond)
		return errors.New("error")
	}

	// When: context is cancelled
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	cfg := DefaultRetryConfig()
	cfg.InitialDelay = 200 * time.Millisecond

	start := time.Now()
	err := Retry(ctx, cfg, fn)
	elapsed := time.Since(start)

	// Then: returns context error quickly
	assert.Error(t, err)
	assert.True(t, errors.Is(err, context.Canceled))
	assert.Less(t, elapsed, 500*time.Millisecond)
}

func TestRetry_RespectsContextDeadline(t *testing.T) {
	// Given: a context with deadline
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	fn := func() error {
		return errors.New("error")
	}

	cfg := RetryConfig{
		MaxRetries:   10,
		InitialDelay: 50 * time.Millisecond,
		MaxDelay:     1 * time.Second,
		Multiplier:   2.0,
	}

	// When: retrying
	err := Retry(ctx, cfg, fn)

	// Then: fails with deadline error
	assert.Error(t, err)
	assert.True(t, errors.Is(err, context.DeadlineExceeded))
}

func TestRetry_ExponentialBackoff(t *testing.T) {
	// Given: a function that records timing
	var timestamps []time.Time
	attempts := 0
	fn := func() error {
		timestamps = append(timestamps, time.Now())
		attempts++
		if attempts < 4 {
			return errors.New("error")
		}
		return nil
	}

	// When: retrying with specific backoff
	cfg := RetryConfig{
		MaxRetries:   5,
		InitialDelay: 20 * time.Millisecond,
		MaxDelay:     1 * time.Second,
		Multiplier:   2.0,
	}

	_ = Retry(context.Background(), cfg, fn)

	// Then: delays increase exponentially
	require.Len(t, timestamps, 4)

	// Delay between attempt 1 and 2 should be ~20ms
	delay1 := timestamps[1].Sub(timestamps[0])
	// Delay between attempt 2 and 3 should be ~40ms
	delay2 := timestamps[2].Sub(timestamps[1])
	// Delay between attempt 3 and 4 should be ~80ms
	delay3 := timestamps[3].Sub(timestamps[2])

	// Allow some timing variance
	assert.InDelta(t, 20, delay1.Milliseconds(), 15)
	assert.InDelta(t, 40, delay2.Milliseconds(), 20)
	assert.InDelta(t, 80, delay3.Milliseconds(), 40)
}

func TestRetry_CapsAtMaxDelay(t *testing.T) {
	// Given: a function that records timing
	var timestamps []time.Time
	attempts := 0
	fn := func() error {
		timestamps = append(timestamps, time.Now())
		attempts++
		if attempts < 5 {
			return errors.New("error")
		}
		return nil
	}

	// When: retrying with low max delay
	cfg := RetryConfig{
		MaxRetries:   10,
		InitialDelay: 20 * time.Millisecond,
		MaxDelay:     30 * time.Millisecond, // Cap at 30ms
		Multiplier:   2.0,
	}

	_ = Retry(context.Background(), cfg, fn)

	// Then: delays don't exceed max
	for i := 2; i < len(timestamps); i++ {
		delay := timestamps[i].Sub(timestamps[i-1])
		assert.LessOrEqual(t, delay.Milliseconds(), int64(50)) // Allow some variance
	}
}

func TestRetryWithResult_ReturnsValue(t *testing.T) {
	// Given: a function that returns a value
	attempts := 0
	fn := func() (int, error) {
		attempts++
		if attempts < 2 {
			return 0, errors.New("error")
		}
		return 42, nil
	}

	// When: retrying
	cfg := DefaultRetryConfig()
	cfg.InitialDelay = 10 * time.Millisecond

	result, err := RetryWithResult(context.Background(), cfg, fn)

	// Then: returns the value
	assert.NoError(t, err)
	assert.Equal(t, 42, result)
}

func TestRetryWithResult_ReturnsZeroOnFailure(t *testing.T) {
	// Given: a function that always fails
	fn := func() (string, error) {
		return "partial", errors.New("error")
	}

	// When: retrying
	cfg := RetryConfig{
		MaxRetries:   1,
		InitialDelay: 10 * time.Millisecond,
		MaxDelay:     100 * time.Millisecond,
		Multiplier:   2.0,
	}

	result, err := RetryWithResult(context.Background(), cfg, fn)

	// Then: returns zero value and error
	assert.Error(t, err)
	assert.Equal(t, "", result) // Zero value for string
}

func TestRetry_WithJitter(t *testing.T) {
	// Given: jitter is enabled
	cfg := RetryConfig{
		MaxRetries:   5,
		InitialDelay: 50 * time.Millisecond,
		MaxDelay:     1 * time.Second,
		Multiplier:   2.0,
		Jitter:       true,
	}

	// When: measuring multiple retry sequences
	var delays []time.Duration
	for i := 0; i < 3; i++ {
		var timestamps []time.Time
		attempts := 0
		fn := func() error {
			timestamps = append(timestamps, time.Now())
			attempts++
			if attempts < 3 {
				return errors.New("error")
			}
			return nil
		}
		_ = Retry(context.Background(), cfg, fn)
		if len(timestamps) >= 2 {
			delays = append(delays, timestamps[1].Sub(timestamps[0]))
		}
	}

	// Then: delays should vary (due to jitter)
	// Note: This is probabilistic, but with jitter the delays should not all be identical
	require.GreaterOrEqual(t, len(delays), 2)
	// Just verify that delays are within expected range
	for _, d := range delays {
		assert.GreaterOrEqual(t, d.Milliseconds(), int64(25))  // Min: 50% of 50ms
		assert.LessOrEqual(t, d.Milliseconds(), int64(100))    // Max: ~100ms with some variance
	}
}

func TestRetry_ImmediateSuccessNoDelay(t *testing.T) {
	// Given: a function that succeeds immediately
	fn := func() error {
		return nil
	}

	// When: retrying
	cfg := RetryConfig{
		MaxRetries:   5,
		InitialDelay: 1 * time.Second,
		MaxDelay:     10 * time.Second,
		Multiplier:   2.0,
	}

	start := time.Now()
	err := Retry(context.Background(), cfg, fn)
	elapsed := time.Since(start)

	// Then: returns immediately
	assert.NoError(t, err)
	assert.Less(t, elapsed, 100*time.Millisecond)
}

func TestRetry_Concurrent(t *testing.T) {
	// Given: concurrent retry operations
	var successCount atomic.Int32

	// When: running multiple retries concurrently
	for i := 0; i < 10; i++ {
		go func() {
			attempts := 0
			fn := func() error {
				attempts++
				if attempts < 2 {
					return errors.New("error")
				}
				return nil
			}

			cfg := RetryConfig{
				MaxRetries:   3,
				InitialDelay: 5 * time.Millisecond,
				MaxDelay:     20 * time.Millisecond,
				Multiplier:   2.0,
			}

			if err := Retry(context.Background(), cfg, fn); err == nil {
				successCount.Add(1)
			}
		}()
	}

	// Wait for all goroutines
	time.Sleep(200 * time.Millisecond)

	// Then: all should succeed
	assert.Equal(t, int32(10), successCount.Load())
}

func TestDefaultRetryConfig_HasSensibleDefaults(t *testing.T) {
	cfg := DefaultRetryConfig()

	assert.Equal(t, 3, cfg.MaxRetries)
	assert.Equal(t, 1*time.Second, cfg.InitialDelay)
	assert.Equal(t, 16*time.Second, cfg.MaxDelay)
	assert.Equal(t, 2.0, cfg.Multiplier)
}
