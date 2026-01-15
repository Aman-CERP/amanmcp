package errors

import (
	"context"
	"fmt"
	"math/rand"
	"time"
)

// RetryConfig configures retry behavior.
type RetryConfig struct {
	// MaxRetries is the maximum number of retry attempts (not including initial attempt).
	MaxRetries int

	// InitialDelay is the delay before the first retry.
	InitialDelay time.Duration

	// MaxDelay is the maximum delay between retries.
	MaxDelay time.Duration

	// Multiplier is the factor by which delay increases after each retry.
	Multiplier float64

	// Jitter adds randomness to delay to prevent thundering herd.
	Jitter bool
}

// DefaultRetryConfig returns sensible default retry configuration.
func DefaultRetryConfig() RetryConfig {
	return RetryConfig{
		MaxRetries:   3,
		InitialDelay: 1 * time.Second,
		MaxDelay:     16 * time.Second,
		Multiplier:   2.0,
		Jitter:       false,
	}
}

// Retry executes a function with exponential backoff retry logic.
// It retries up to MaxRetries times if the function returns an error.
// The delay between retries grows exponentially, capped at MaxDelay.
// If the context is cancelled, it returns the context error immediately.
func Retry(ctx context.Context, cfg RetryConfig, fn func() error) error {
	delay := cfg.InitialDelay
	var lastErr error

	for attempt := 0; attempt <= cfg.MaxRetries; attempt++ {
		// Check context before attempting
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// Execute the function
		if err := fn(); err != nil {
			lastErr = err

			// If this was the last attempt, don't wait
			if attempt >= cfg.MaxRetries {
				break
			}

			// Calculate delay with optional jitter
			waitDelay := delay
			if cfg.Jitter {
				// Add jitter: delay * (0.5 + rand(0, 0.5))
				jitterFactor := 0.5 + rand.Float64()*0.5
				waitDelay = time.Duration(float64(delay) * jitterFactor)
			}

			// Wait before retrying (with context cancellation support)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(waitDelay):
			}

			// Calculate next delay with exponential backoff
			delay = time.Duration(float64(delay) * cfg.Multiplier)
			if delay > cfg.MaxDelay {
				delay = cfg.MaxDelay
			}
			continue
		}

		// Success
		return nil
	}

	return fmt.Errorf("failed after %d retries: %w", cfg.MaxRetries, lastErr)
}

// RetryWithResult executes a function that returns a value with retry logic.
// Similar to Retry but for functions that return both a result and an error.
func RetryWithResult[T any](ctx context.Context, cfg RetryConfig, fn func() (T, error)) (T, error) {
	var result T
	delay := cfg.InitialDelay
	var lastErr error

	for attempt := 0; attempt <= cfg.MaxRetries; attempt++ {
		// Check context before attempting
		select {
		case <-ctx.Done():
			return result, ctx.Err()
		default:
		}

		// Execute the function
		var err error
		result, err = fn()
		if err != nil {
			lastErr = err

			// If this was the last attempt, don't wait
			if attempt >= cfg.MaxRetries {
				break
			}

			// Calculate delay with optional jitter
			waitDelay := delay
			if cfg.Jitter {
				jitterFactor := 0.5 + rand.Float64()*0.5
				waitDelay = time.Duration(float64(delay) * jitterFactor)
			}

			// Wait before retrying (with context cancellation support)
			select {
			case <-ctx.Done():
				return result, ctx.Err()
			case <-time.After(waitDelay):
			}

			// Calculate next delay with exponential backoff
			delay = time.Duration(float64(delay) * cfg.Multiplier)
			if delay > cfg.MaxDelay {
				delay = cfg.MaxDelay
			}
			continue
		}

		// Success
		return result, nil
	}

	// Return zero value and error
	var zero T
	return zero, fmt.Errorf("failed after %d retries: %w", cfg.MaxRetries, lastErr)
}
