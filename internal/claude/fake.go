package claude

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

// FakeClient implements Client by returning responses from a queue.
type FakeClient struct {
	mu       sync.Mutex
	queued   []Response
	captured []Request
	errAfter int
}

// NewFakeClient returns an empty FakeClient.
func NewFakeClient() *FakeClient { return &FakeClient{} }

// Enqueue appends a response to the queue.
func (f *FakeClient) Enqueue(r Response) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.queued = append(f.queued, r)
}

// ErrAfter makes the n-th call return an error.
func (f *FakeClient) ErrAfter(n int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.errAfter = n
}

// Complete pops the next queued response.
func (f *FakeClient) Complete(_ context.Context, req Request) (Response, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.captured = append(f.captured, req)
	if f.errAfter > 0 && len(f.captured) == f.errAfter {
		return Response{}, fmt.Errorf("fake client: forced error at call %d", f.errAfter)
	}
	if len(f.queued) == 0 {
		return Response{}, errors.New("fake client: no enqueued response (test forgot to Enqueue)")
	}
	r := f.queued[0]
	f.queued = f.queued[1:]
	return r, nil
}

// Requests returns a copy of all captured requests in order.
func (f *FakeClient) Requests() []Request {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]Request, len(f.captured))
	copy(out, f.captured)
	return out
}

// Remaining returns how many responses are still queued.
func (f *FakeClient) Remaining() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.queued)
}
