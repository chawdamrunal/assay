package claude

import (
	"context"
	"errors"
	"log"
	"math/rand"
	"os"
	"strconv"
	"strings"
	"time"

	anthropic "github.com/anthropics/anthropic-sdk-go"
)

// debugHeaders is enabled via ASSAY_DEBUG_RATE_HEADERS=1. When on, every
// retryable error prints all rate-limit response headers to stderr — useful
// for diagnosing why a subscription bearer is throttled harder than expected.
var debugHeaders = os.Getenv("ASSAY_DEBUG_RATE_HEADERS") == "1"

// RetryConfig controls 429 / 5xx retry behavior. Zero value = sensible defaults
// for a single interactive scan (4 retries, ~3s → ~24s exponential backoff).
type RetryConfig struct {
	MaxAttempts  int           // total attempts including the first (default 5)
	Base         time.Duration // first backoff (default 3s)
	Max          time.Duration // backoff ceiling (default 60s)
	JitterFactor float64       // 0..1; randomizes each delay (default 0.25)

	// Notify is called once per retry with attempt number (1-based, excluding
	// the original try) and the wait duration. Used by serve to push SSE
	// events so the UI shows "rate-limited, retrying in Ns" instead of going
	// silent. Optional.
	Notify func(attempt int, wait time.Duration, err error)
}

// RetryingClient wraps a Client and transparently retries 429 + 5xx with
// exponential backoff. It honors the `retry-after` response header when the
// server provides one (Claude Code subscriptions almost always do).
//
// This is the headline fix for the "I'm using my Claude Code subscription
// and keep hitting 429" complaint: subscription bearers share per-minute
// caps, but a 3-15 s backoff almost always lands on a refilled bucket.
type RetryingClient struct {
	inner Client
	cfg   RetryConfig
}

// NewRetryingClient decorates inner with retry semantics. Pass an empty
// RetryConfig{} to get the defaults.
func NewRetryingClient(inner Client, cfg RetryConfig) *RetryingClient {
	if cfg.MaxAttempts <= 0 {
		cfg.MaxAttempts = 5
	}
	if cfg.Base <= 0 {
		cfg.Base = 3 * time.Second
	}
	if cfg.Max <= 0 {
		cfg.Max = 60 * time.Second
	}
	if cfg.JitterFactor < 0 {
		cfg.JitterFactor = 0
	}
	if cfg.JitterFactor == 0 {
		cfg.JitterFactor = 0.25
	}
	return &RetryingClient{inner: inner, cfg: cfg}
}

// Complete forwards to the wrapped client, retrying on retryable errors.
func (r *RetryingClient) Complete(ctx context.Context, req Request) (Response, error) {
	var lastErr error
	for attempt := 0; attempt < r.cfg.MaxAttempts; attempt++ {
		if attempt > 0 {
			wait := r.backoff(attempt, lastErr)
			if r.cfg.Notify != nil {
				r.cfg.Notify(attempt, wait, lastErr)
			}
			select {
			case <-ctx.Done():
				return Response{}, ctx.Err()
			case <-time.After(wait):
			}
		}
		resp, err := r.inner.Complete(ctx, req)
		if err == nil {
			return resp, nil
		}
		if !isRetryable(err) {
			return resp, err
		}
		if debugHeaders {
			logRateHeaders(err)
		}
		lastErr = err
	}
	return Response{}, lastErr
}

// isRetryable returns true for transient SDK errors: HTTP 429 (rate limit),
// 408 (request timeout), 5xx (server error). Anything else is permanent.
func isRetryable(err error) bool {
	var apierr *anthropic.Error
	if !errors.As(err, &apierr) {
		return false
	}
	switch apierr.StatusCode {
	case 408, 429:
		return true
	default:
		return apierr.StatusCode >= 500 && apierr.StatusCode < 600
	}
}

// backoff returns the wait duration for the given attempt. Honors the
// `retry-after` header if the last error carried one (in seconds — RFC 9110).
func (r *RetryingClient) backoff(attempt int, lastErr error) time.Duration {
	if hint := retryAfterFromError(lastErr); hint > 0 {
		if hint > r.cfg.Max {
			return r.cfg.Max
		}
		return hint
	}
	// Exponential: base * 2^(attempt-1).
	d := r.cfg.Base
	for i := 1; i < attempt; i++ {
		d *= 2
		if d >= r.cfg.Max {
			d = r.cfg.Max
			break
		}
	}
	// Jitter to spread retries from concurrent sub-agents.
	jitter := time.Duration(rand.Float64() * float64(d) * r.cfg.JitterFactor) // #nosec G404 -- jitter, not crypto
	return d + jitter
}

// logRateHeaders dumps every rate-limit-related response header to stderr so
// operators can see what the server actually advertised vs what our backoff
// chose. Enabled via ASSAY_DEBUG_RATE_HEADERS=1.
func logRateHeaders(err error) {
	var apierr *anthropic.Error
	if !errors.As(err, &apierr) || apierr.Response == nil {
		return
	}
	all := []string{}
	for name, vals := range apierr.Response.Header {
		all = append(all, name+"="+strings.Join(vals, ","))
	}
	log.Printf("retry: 429 status=%d ALL headers={%s}", apierr.StatusCode, strings.Join(all, " | "))
}

func retryAfterFromError(err error) time.Duration {
	var apierr *anthropic.Error
	if !errors.As(err, &apierr) || apierr.Response == nil {
		return 0
	}
	// Anthropic exposes the bucket-refill horizon via several headers.
	// Pick the most specific signal available, in priority order.
	hdrs := apierr.Response.Header
	if hdr := hdrs.Get("retry-after"); hdr != "" {
		if secs, perr := strconv.Atoi(hdr); perr == nil && secs > 0 {
			return time.Duration(secs) * time.Second
		}
		if t, perr := time.Parse(time.RFC1123, hdr); perr == nil {
			if d := time.Until(t); d > 0 {
				return d
			}
		}
	}
	// Anthropic-Ratelimit-*-Reset headers tell us when each bucket refills
	// (RFC 9457). When the model returns 429, at least one of these is the
	// limiter; pick the soonest non-zero refill window.
	resetHdrs := []string{
		"anthropic-ratelimit-requests-reset",
		"anthropic-ratelimit-input-tokens-reset",
		"anthropic-ratelimit-output-tokens-reset",
		"anthropic-ratelimit-tokens-reset",
	}
	var soonest time.Duration
	for _, h := range resetHdrs {
		v := hdrs.Get(h)
		if v == "" {
			continue
		}
		// Reset is an ISO-8601 timestamp.
		t, perr := time.Parse(time.RFC3339, v)
		if perr != nil {
			continue
		}
		d := time.Until(t)
		if d <= 0 {
			continue
		}
		if soonest == 0 || d < soonest {
			soonest = d
		}
	}
	return soonest
}
