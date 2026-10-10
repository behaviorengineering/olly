package http

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/failsafe-go/failsafe-go"
	"github.com/failsafe-go/failsafe-go/circuitbreaker"
	"github.com/failsafe-go/failsafe-go/retrypolicy"
)

// ErrMissingDeadline is returned when an outbound request context has no deadline.
var ErrMissingDeadline = errors.New("olly http: context missing deadline")

// ErrCircuitOpen is returned when the circuit breaker is open for the service.
var ErrCircuitOpen = errors.New("olly http: circuit open")

// ErrNonReplayableBody is returned when a retryable request body cannot be replayed.
var ErrNonReplayableBody = errors.New("olly http: request body not replayable")

// WithResilience enables failsafe retry and circuit breaking on Client / WrapOutboundTransport.
func WithResilience() Option {
	return func(o *options) {
		o.resilience = true
	}
}

type resiliencePolicy struct {
	maxRetries       uint
	backoffMin       time.Duration
	backoffMax       time.Duration
	jitter           float64
	failureThreshold uint
	breakerDelay     time.Duration
}

func defaultResiliencePolicy() resiliencePolicy {
	return resiliencePolicy{
		maxRetries:       2,
		backoffMin:       100 * time.Millisecond,
		backoffMax:       time.Second,
		jitter:           0.2,
		failureThreshold: 5,
		breakerDelay:     30 * time.Second,
	}
}

// WrapOutboundTransport stacks optional resilience (when WithResilience) on otel WrapTransport.
func WrapOutboundTransport(base http.RoundTripper, serviceName string, opts ...Option) http.RoundTripper {
	o := applyOptions(opts)
	inner := WrapTransport(base, serviceName, opts...)
	if !o.resilience {
		return inner
	}
	return newResilientTransport(inner, serviceName, o.resiliencePolicy)
}

type resilientTransport struct {
	inner   http.RoundTripper
	service string
	policy  resiliencePolicy
}

func newResilientTransport(inner http.RoundTripper, service string, policy resiliencePolicy) http.RoundTripper {
	if inner == nil {
		inner = http.DefaultTransport
	}
	if _, ok := inner.(*resilientTransport); ok {
		return inner
	}
	if service == "" {
		service = "olly"
	}
	return &resilientTransport{inner: inner, service: service, policy: policy}
}

func (t *resilientTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req == nil {
		return nil, fmt.Errorf("olly http: nil request")
	}
	ctx := req.Context()
	if ctx == nil {
		return nil, ErrMissingDeadline
	}
	if _, ok := ctx.Deadline(); !ok {
		return nil, ErrMissingDeadline
	}

	retry := retrypolicy.NewBuilder[*http.Response]().
		HandleIf(func(_ *http.Response, err error) bool { return isRetryableOutbound(err) }).
		WithBackoff(t.policy.backoffMin, t.policy.backoffMax).
		WithJitterFactor(t.policy.jitter).
		WithMaxRetries(int(t.policy.maxRetries)).
		Build()

	breaker := breakerFor(t.service, t.policy)

	var lastRetryableResp *http.Response

	resp, err := failsafe.With(breaker, retry).
		WithContext(ctx).
		Get(func() (*http.Response, error) {
			attemptReq, err := cloneRequestForRetry(req)
			if err != nil {
				return nil, err
			}
			attemptResp, err := t.inner.RoundTrip(attemptReq)
			if err != nil {
				if errors.Is(err, context.Canceled) {
					return nil, err
				}
				if isRetryableTransport(err) {
					return nil, err
				}
				return nil, err
			}
			if attemptResp != nil && isRetryableStatus(attemptResp.StatusCode) {
				lastRetryableResp = attemptResp
				drainResponseBody(attemptResp)
				return nil, &retryableStatusError{status: attemptResp.StatusCode}
			}
			return attemptResp, nil
		})

	if err == nil {
		return resp, nil
	}
	if errors.Is(err, circuitbreaker.ErrOpen) {
		return nil, fmt.Errorf("%w: %v", ErrCircuitOpen, err)
	}
	if lastRetryableResp != nil && isRetryableStatus(lastRetryableResp.StatusCode) {
		return lastRetryableResp, nil
	}
	return nil, err
}

type retryableStatusError struct {
	status int
}

func (e *retryableStatusError) Error() string {
	return fmt.Sprintf("olly http: retryable status %d", e.status)
}

func isRetryableOutbound(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) {
		return false
	}
	if _, ok := err.(*retryableStatusError); ok {
		return true
	}
	return isRetryableTransport(err)
}

func isRetryableTransport(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) {
		return false
	}
	if errors.Is(err, ErrMissingDeadline) || errors.Is(err, ErrNonReplayableBody) {
		return false
	}
	return true
}

func isRetryableStatus(code int) bool {
	if code == http.StatusRequestTimeout || code == http.StatusTooManyRequests {
		return true
	}
	return code >= 500
}

func cloneRequestForRetry(req *http.Request) (*http.Request, error) {
	if req.Body == nil {
		return req.Clone(req.Context()), nil
	}
	if req.GetBody != nil {
		body, err := req.GetBody()
		if err != nil {
			return nil, fmt.Errorf("olly http: get body: %w", err)
		}
		cloned := req.Clone(req.Context())
		cloned.Body = body
		return cloned, nil
	}
	if req.ContentLength == 0 {
		return req.Clone(req.Context()), nil
	}
	return nil, ErrNonReplayableBody
}

func drainResponseBody(resp *http.Response) {
	if resp == nil || resp.Body == nil {
		return
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
}

var (
	breakerMu sync.Mutex
	breakers  = map[string]circuitbreaker.CircuitBreaker[*http.Response]{}
)

func breakerFor(service string, policy resiliencePolicy) circuitbreaker.CircuitBreaker[*http.Response] {
	if service == "" {
		service = "olly"
	}
	breakerMu.Lock()
	defer breakerMu.Unlock()
	if b, ok := breakers[service]; ok {
		return b
	}
	b := circuitbreaker.NewBuilder[*http.Response]().
		HandleIf(func(_ *http.Response, err error) bool { return isRetryableOutbound(err) }).
		WithFailureThreshold(policy.failureThreshold).
		WithDelay(policy.breakerDelay).
		Build()
	breakers[service] = b
	return b
}

func withResiliencePolicyForTest(p resiliencePolicy) Option {
	return func(o *options) {
		o.resiliencePolicy = p
	}
}

// resetBreakersForTest clears the process-wide breaker map (tests only).
func resetBreakersForTest() {
	breakerMu.Lock()
	breakers = map[string]circuitbreaker.CircuitBreaker[*http.Response]{}
	breakerMu.Unlock()
}
