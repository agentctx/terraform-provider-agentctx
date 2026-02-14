package target

import (
	"context"
	"errors"
	"io"
	"math"
	"math/rand"
	"time"
)

// RetryTarget wraps another Target and retries transient errors with configurable backoff.
type RetryTarget struct {
	inner      Target
	maxRetries int
	backoff    string // "exponential" or "linear"
}

// NewRetryTarget creates a Target that retries transient errors.
// backoff must be "exponential" or "linear". maxRetries is the maximum number
// of retry attempts (0 means no retries).
func NewRetryTarget(inner Target, maxRetries int, backoff string) Target {
	if backoff != "exponential" && backoff != "linear" {
		backoff = "exponential"
	}
	return &RetryTarget{
		inner:      inner,
		maxRetries: maxRetries,
		backoff:    backoff,
	}
}

func (r *RetryTarget) Name() string {
	return r.inner.Name()
}

func (r *RetryTarget) Put(ctx context.Context, key string, body io.Reader, opts PutOptions) error {
	return r.retryOp(ctx, func() error {
		return r.inner.Put(ctx, key, body, opts)
	})
}

func (r *RetryTarget) Get(ctx context.Context, key string) (io.ReadCloser, ObjectMeta, error) {
	var (
		rc   io.ReadCloser
		meta ObjectMeta
	)
	err := r.retryOp(ctx, func() error {
		var e error
		rc, meta, e = r.inner.Get(ctx, key)
		return e
	})
	return rc, meta, err
}

func (r *RetryTarget) Head(ctx context.Context, key string) (ObjectMeta, error) {
	var meta ObjectMeta
	err := r.retryOp(ctx, func() error {
		var e error
		meta, e = r.inner.Head(ctx, key)
		return e
	})
	return meta, err
}

func (r *RetryTarget) Delete(ctx context.Context, key string) error {
	return r.retryOp(ctx, func() error {
		return r.inner.Delete(ctx, key)
	})
}

func (r *RetryTarget) List(ctx context.Context, prefix string) ([]ObjectInfo, error) {
	var items []ObjectInfo
	err := r.retryOp(ctx, func() error {
		var e error
		items, e = r.inner.List(ctx, prefix)
		return e
	})
	return items, err
}

func (r *RetryTarget) ConditionalPut(ctx context.Context, key string, body io.Reader, condition WriteCondition, opts PutOptions) error {
	return r.retryOp(ctx, func() error {
		return r.inner.ConditionalPut(ctx, key, body, condition, opts)
	})
}

// isTransient returns true if the error is transient and should be retried.
// Non-retryable errors include ErrNotFound, ErrPreconditionFailed, and
// ConcurrentModificationError.
func isTransient(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, ErrNotFound) {
		return false
	}
	if errors.Is(err, ErrPreconditionFailed) {
		return false
	}
	var cme *ConcurrentModificationError
	if errors.As(err, &cme) {
		return false
	}
	return true
}

// retryOp executes the operation and retries on transient errors.
func (r *RetryTarget) retryOp(ctx context.Context, op func() error) error {
	var lastErr error
	for attempt := 0; attempt <= r.maxRetries; attempt++ {
		lastErr = op()
		if lastErr == nil {
			return nil
		}
		if !isTransient(lastErr) {
			return lastErr
		}
		if attempt == r.maxRetries {
			break
		}
		// Calculate sleep duration with jitter.
		sleepDur := r.calcBackoff(attempt)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(sleepDur):
		}
	}
	return lastErr
}

// calcBackoff computes the backoff duration for the given attempt number.
func (r *RetryTarget) calcBackoff(attempt int) time.Duration {
	const baseDelay = 100 * time.Millisecond
	const maxDelay = 30 * time.Second

	var delay time.Duration
	switch r.backoff {
	case "linear":
		delay = baseDelay * time.Duration(attempt+1)
	default: // "exponential"
		delay = baseDelay * time.Duration(math.Pow(2, float64(attempt)))
	}

	if delay > maxDelay {
		delay = maxDelay
	}

	// Add jitter: +/- 25% of the delay.
	jitter := time.Duration(rand.Int63n(int64(delay/2))) - delay/4
	delay += jitter

	if delay < 0 {
		delay = baseDelay
	}

	return delay
}
