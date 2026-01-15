package embed

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDownloadWithRetry_SuccessOnFirstTry(t *testing.T) {
	// Given: a function that succeeds immediately
	attempts := 0
	fn := func() error {
		attempts++
		return nil
	}

	// When: I call DownloadWithRetry
	err := DownloadWithRetry(context.Background(), DefaultRetryConfig(), fn)

	// Then: no error and only one attempt
	require.NoError(t, err)
	assert.Equal(t, 1, attempts)
}

func TestDownloadWithRetry_SuccessAfterRetries(t *testing.T) {
	// Given: a function that fails twice then succeeds
	attempts := 0
	fn := func() error {
		attempts++
		if attempts < 3 {
			return errors.New("temporary error")
		}
		return nil
	}

	// When: I call DownloadWithRetry with short delays
	cfg := RetryConfig{
		MaxRetries:   3,
		InitialDelay: 1 * time.Millisecond, // Very short for testing
		MaxDelay:     10 * time.Millisecond,
		Multiplier:   2.0,
	}
	err := DownloadWithRetry(context.Background(), cfg, fn)

	// Then: no error and 3 attempts
	require.NoError(t, err)
	assert.Equal(t, 3, attempts)
}

func TestDownloadWithRetry_FailureAfterMaxRetries(t *testing.T) {
	// Given: a function that always fails
	attempts := 0
	expectedErr := errors.New("permanent error")
	fn := func() error {
		attempts++
		return expectedErr
	}

	// When: I call DownloadWithRetry with short delays
	cfg := RetryConfig{
		MaxRetries:   3,
		InitialDelay: 1 * time.Millisecond,
		MaxDelay:     10 * time.Millisecond,
		Multiplier:   2.0,
	}
	err := DownloadWithRetry(context.Background(), cfg, fn)

	// Then: error returned after 4 attempts (initial + 3 retries)
	require.Error(t, err)
	assert.Equal(t, 4, attempts)
	assert.Contains(t, err.Error(), "failed after")
	assert.True(t, errors.Is(err, expectedErr))
}

func TestDownloadWithRetry_ContextCancellation(t *testing.T) {
	// Given: a function that fails and a context that gets cancelled
	attempts := 0
	fn := func() error {
		attempts++
		return errors.New("temporary error")
	}

	// Create a context that we'll cancel
	ctx, cancel := context.WithCancel(context.Background())

	// When: I call DownloadWithRetry and cancel after a short time
	cfg := RetryConfig{
		MaxRetries:   10, // High number - we'll cancel before reaching it
		InitialDelay: 50 * time.Millisecond,
		MaxDelay:     100 * time.Millisecond,
		Multiplier:   2.0,
	}

	// Cancel context after first attempt
	go func() {
		time.Sleep(10 * time.Millisecond)
		cancel()
	}()

	err := DownloadWithRetry(ctx, cfg, fn)

	// Then: context error returned
	require.Error(t, err)
	assert.True(t, errors.Is(err, context.Canceled))
	assert.LessOrEqual(t, attempts, 2, "should stop retrying after context cancellation")
}

func TestDownloadWithRetry_ExponentialBackoff(t *testing.T) {
	// Given: a function that records timing
	var timestamps []time.Time
	fn := func() error {
		timestamps = append(timestamps, time.Now())
		if len(timestamps) < 4 {
			return errors.New("retry")
		}
		return nil
	}

	// When: I call DownloadWithRetry
	cfg := RetryConfig{
		MaxRetries:   5,
		InitialDelay: 10 * time.Millisecond,
		MaxDelay:     100 * time.Millisecond,
		Multiplier:   2.0,
	}
	err := DownloadWithRetry(context.Background(), cfg, fn)

	// Then: delays should increase exponentially
	require.NoError(t, err)
	require.Len(t, timestamps, 4)

	// First call is immediate, then delays: 10ms, 20ms, 40ms
	delay1 := timestamps[1].Sub(timestamps[0])
	delay2 := timestamps[2].Sub(timestamps[1])
	delay3 := timestamps[3].Sub(timestamps[2])

	// Allow 50% variance for timing
	assert.InDelta(t, 10, delay1.Milliseconds(), 15, "first delay should be ~10ms")
	assert.InDelta(t, 20, delay2.Milliseconds(), 20, "second delay should be ~20ms")
	assert.InDelta(t, 40, delay3.Milliseconds(), 30, "third delay should be ~40ms")
}

func TestDefaultRetryConfig(t *testing.T) {
	cfg := DefaultRetryConfig()

	assert.Equal(t, 3, cfg.MaxRetries)
	assert.Equal(t, 1*time.Second, cfg.InitialDelay)
	assert.Equal(t, 16*time.Second, cfg.MaxDelay)
	assert.Equal(t, 2.0, cfg.Multiplier)
}

func TestDownloadWithRetry_MaxDelayRespected(t *testing.T) {
	// Given: a function that always fails
	var timestamps []time.Time
	fn := func() error {
		timestamps = append(timestamps, time.Now())
		return errors.New("fail")
	}

	// When: I call with a low max delay
	cfg := RetryConfig{
		MaxRetries:   5,
		InitialDelay: 5 * time.Millisecond,
		MaxDelay:     10 * time.Millisecond, // Low max
		Multiplier:   10.0,                  // High multiplier - would exceed max quickly
	}
	_ = DownloadWithRetry(context.Background(), cfg, fn)

	// Then: delays should not exceed max
	for i := 1; i < len(timestamps); i++ {
		delay := timestamps[i].Sub(timestamps[i-1])
		assert.LessOrEqual(t, delay.Milliseconds(), int64(30), // 10ms + 20ms buffer
			"delay %d should not exceed max delay", i)
	}
}
