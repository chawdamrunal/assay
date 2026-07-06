package claude

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"testing"
	"time"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// flakyClient returns a 429 for the first n calls, then the queued ok responses.
type flakyClient struct {
	failures   int
	calls      int
	ok         []Response
	retryAfter string
}

func (f *flakyClient) Complete(_ context.Context, _ Request) (Response, error) {
	f.calls++
	if f.calls <= f.failures {
		hdr := http.Header{}
		if f.retryAfter != "" {
			hdr.Set("retry-after", f.retryAfter)
		}
		return Response{}, &anthropic.Error{
			StatusCode: 429,
			Response: &http.Response{
				StatusCode: 429,
				Header:     hdr,
				Request:    &http.Request{Method: "POST", URL: &url.URL{Path: "/v1/messages"}},
			},
			Request: &http.Request{Method: "POST", URL: &url.URL{Path: "/v1/messages"}},
		}
	}
	if len(f.ok) == 0 {
		return Response{Text: "ok"}, nil
	}
	r := f.ok[0]
	f.ok = f.ok[1:]
	return r, nil
}

func TestRetryingClientRetriesOn429(t *testing.T) {
	flaky := &flakyClient{failures: 2, ok: []Response{{Text: "final"}}}
	notified := 0
	rc := NewRetryingClient(flaky, RetryConfig{
		MaxAttempts:  5,
		Base:         10 * time.Millisecond,
		Max:          30 * time.Millisecond,
		JitterFactor: 0.01,
		Notify:       func(_ int, _ time.Duration, _ error) { notified++ },
	})
	resp, err := rc.Complete(context.Background(), Request{})
	require.NoError(t, err)
	assert.Equal(t, "final", resp.Text)
	assert.Equal(t, 3, flaky.calls)
	assert.Equal(t, 2, notified)
}

func TestRetryingClientGivesUpAfterMaxAttempts(t *testing.T) {
	flaky := &flakyClient{failures: 100} // always 429s
	rc := NewRetryingClient(flaky, RetryConfig{
		MaxAttempts:  3,
		Base:         5 * time.Millisecond,
		JitterFactor: 0.01,
	})
	_, err := rc.Complete(context.Background(), Request{})
	require.Error(t, err)
	var apierr *anthropic.Error
	require.True(t, errors.As(err, &apierr))
	assert.Equal(t, 429, apierr.StatusCode)
	assert.Equal(t, 3, flaky.calls)
}

func TestRetryingClientRespectsRetryAfterHeader(t *testing.T) {
	flaky := &flakyClient{failures: 1, retryAfter: "1", ok: []Response{{Text: "ok"}}}
	rc := NewRetryingClient(flaky, RetryConfig{
		MaxAttempts:  3,
		Base:         500 * time.Millisecond, // bigger than retry-after
		Max:          5 * time.Second,
		JitterFactor: 0,
	})
	start := time.Now()
	_, err := rc.Complete(context.Background(), Request{})
	require.NoError(t, err)
	elapsed := time.Since(start)
	// Retry-after is 1s; we should wait ~1s, not the full 500ms base*2 etc.
	assert.GreaterOrEqual(t, elapsed, 900*time.Millisecond)
	assert.Less(t, elapsed, 2*time.Second, "should not exceed retry-after by much")
}

func TestRetryingClientPassesThroughNonRetryable(t *testing.T) {
	nonRetry := &nonRetryClient{}
	rc := NewRetryingClient(nonRetry, RetryConfig{MaxAttempts: 5, Base: time.Millisecond})
	_, err := rc.Complete(context.Background(), Request{})
	require.Error(t, err)
	assert.Equal(t, 1, nonRetry.calls, "non-retryable error must not be retried")
}

type nonRetryClient struct{ calls int }

func (n *nonRetryClient) Complete(_ context.Context, _ Request) (Response, error) {
	n.calls++
	return Response{}, &anthropic.Error{
		StatusCode: 400,
		Response: &http.Response{
			StatusCode: 400,
			Header:     http.Header{},
			Request:    &http.Request{Method: "POST", URL: &url.URL{Path: "/v1/messages"}},
		},
		Request: &http.Request{Method: "POST", URL: &url.URL{Path: "/v1/messages"}},
	}
}
