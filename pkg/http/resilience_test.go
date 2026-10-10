package http

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func resilientClient(service string, opts ...Option) *http.Client {
	all := append([]Option{WithResilience()}, opts...)
	return Client(nil, service, all...)
}

func TestResilientTransport_requiresDeadline(t *testing.T) {
	resetBreakersForTest()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := resilientClient("test")
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Do(req)
	if !errors.Is(err, ErrMissingDeadline) {
		t.Fatalf("got %v", err)
	}
}

func TestResilientTransport_retries503Then200(t *testing.T) {
	resetBreakersForTest()
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	client := resilientClient("retry503")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
	if calls.Load() != 2 {
		t.Fatalf("calls %d", calls.Load())
	}
}

func TestResilientTransport_doesNotRetry400(t *testing.T) {
	resetBreakersForTest()
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer srv.Close()

	client := resilientClient("no400")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status %d", resp.StatusCode)
	}
	if calls.Load() != 1 {
		t.Fatalf("calls %d", calls.Load())
	}
}

func TestResilientTransport_retries429(t *testing.T) {
	resetBreakersForTest()
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := resilientClient("retry429")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK || calls.Load() != 2 {
		t.Fatalf("status %d calls %d", resp.StatusCode, calls.Load())
	}
}

func TestResilientTransport_replaysPOSTBody(t *testing.T) {
	resetBreakersForTest()
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if calls.Add(1) == 1 {
			if string(body) != "payload" {
				t.Errorf("first body %q", body)
			}
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		if string(body) != "payload" {
			t.Errorf("second body %q", body)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := resilientClient("post")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, srv.URL, strings.NewReader("payload"))
	if err != nil {
		t.Fatal(err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK || calls.Load() != 2 {
		t.Fatalf("status %d calls %d", resp.StatusCode, calls.Load())
	}
}

func TestResilientTransport_breakerOpens(t *testing.T) {
	resetBreakersForTest()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	policy := defaultResiliencePolicy()
	policy.maxRetries = 0
	policy.failureThreshold = 1
	policy.breakerDelay = time.Minute

	client := Client(nil, "breaker-test", WithResilience(), withResiliencePolicyForTest(policy))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Trip breaker.
	req1, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	resp1, err1 := client.Do(req1)
	if resp1 != nil {
		resp1.Body.Close()
	}
	if err1 != nil && !errors.Is(err1, ErrCircuitOpen) {
		// exhaustion may return last 503
	}

	req2, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	_, err2 := client.Do(req2)
	if !errors.Is(err2, ErrCircuitOpen) {
		t.Fatalf("expected circuit open, got %v", err2)
	}
}

func TestClientWithoutResilience_single503Attempt(t *testing.T) {
	resetBreakersForTest()
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	client := Client(nil, "trace-only")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if calls.Load() != 1 {
		t.Fatalf("calls %d", calls.Load())
	}
}

func TestResilientTransport_nonReplayableBody(t *testing.T) {
	resetBreakersForTest()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	client := resilientClient("nobody")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, srv.URL, bytes.NewReader([]byte("x")))
	req.GetBody = nil
	req.ContentLength = 1
	_, err := client.Do(req)
	if !errors.Is(err, ErrNonReplayableBody) {
		t.Fatalf("got %v", err)
	}
}
