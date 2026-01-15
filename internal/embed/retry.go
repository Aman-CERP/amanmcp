package embed

import (
	"context"
	"fmt"
	"time"
)

// RetryConfig configures retry behavior for model downloads.
type RetryConfig struct {
	MaxRetries   int           // Maximum number of retry attempts (not including initial attempt)
	InitialDelay time.Duration // Delay before first retry
	MaxDelay     time.Duration // Maximum delay between retries
	Multiplier   float64       // Multiplier for exponential backoff
}

// DefaultRetryConfig returns the default retry configuration.
func DefaultRetryConfig() RetryConfig {
	return RetryConfig{
		MaxRetries:   3,
		InitialDelay: 1 * time.Second,
		MaxDelay:     16 * time.Second,
		Multiplier:   2.0,
	}
}

// DownloadWithRetry executes a function with exponential backoff retry logic.
// It retries the function up to MaxRetries times if it fails.
// The delay between retries grows exponentially, capped at MaxDelay.
// If the context is cancelled, it returns the context error immediately.
func DownloadWithRetry(ctx context.Context, cfg RetryConfig, fn func() error) error {
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

			// Wait before retrying (with context cancellation support)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(delay):
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
