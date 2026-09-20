// Package http wraps net/http handlers and clients with OpenTelemetry spans.
//
// Apps call WrapHandler or Middleware once at the server edge, and WrapTransport
// or Client for outbound calls. Span naming and health-check skip lists are
// opinionated defaults; override with options.
package http

import (
	"net/http"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

// DefaultSkipPaths are not traced on the server side.
var DefaultSkipPaths = []string{"/health", "/healthz", "/metrics"}

// Option configures WrapHandler, Middleware, WrapTransport, and Client.
type Option func(*options)

type options struct {
	skipPaths []string
	spanNamer func(operation string, r *http.Request) string
	attrs     []trace.SpanStartOption
}

// WithSkipPaths replaces the default server skip list.
func WithSkipPaths(paths ...string) Option {
	return func(o *options) {
		o.skipPaths = append([]string(nil), paths...)
	}
}

// WithSpanNamer overrides span name formatting. The operation argument is the
// otelhttp operation name (often the service name for servers).
func WithSpanNamer(fn func(operation string, r *http.Request) string) Option {
	return func(o *options) {
		o.spanNamer = fn
	}
}

// WithSpanOptions appends span start options (attributes, kind overrides).
func WithSpanOptions(opts ...trace.SpanStartOption) Option {
	return func(o *options) {
		o.attrs = append(o.attrs, opts...)
	}
}

func applyOptions(opts []Option) options {
	o := options{
		skipPaths: append([]string(nil), DefaultSkipPaths...),
	}
	for _, opt := range opts {
		if opt != nil {
			opt(&o)
		}
	}
	return o
}

func defaultPropagator() propagation.TextMapPropagator {
	return propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	)
}

func serverSpanName(service string, r *http.Request) string {
	if r == nil || r.URL == nil {
		return service + " SERVER HTTP"
	}
	return service + " SERVER " + r.Method + " " + r.URL.Path
}

func clientSpanName(service string, r *http.Request) string {
	if r == nil || r.URL == nil {
		return service + " CLIENT HTTP"
	}
	return service + " CLIENT " + r.Method + " " + r.URL.Host + r.URL.Path
}

// WrapHandler extracts W3C context and creates SERVER spans for inbound requests.
func WrapHandler(h http.Handler, serviceName string, opts ...Option) http.Handler {
	if h == nil {
		h = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "no handler", http.StatusInternalServerError)
		})
	}
	if serviceName == "" {
		serviceName = "olly"
	}
	o := applyOptions(opts)
	namer := o.spanNamer
	if namer == nil {
		label := serviceName
		namer = func(_ string, r *http.Request) string {
			return serverSpanName(label, r)
		}
	}
	otelOpts := []otelhttp.Option{
		otelhttp.WithPropagators(defaultPropagator()),
		otelhttp.WithFilter(func(r *http.Request) bool {
			if r == nil || r.URL == nil {
				return true
			}
			for _, p := range o.skipPaths {
				if r.URL.Path == p {
					return false
				}
			}
			return true
		}),
		otelhttp.WithSpanNameFormatter(namer),
	}
	if len(o.attrs) > 0 {
		otelOpts = append(otelOpts, otelhttp.WithSpanOptions(o.attrs...))
	}
	return otelhttp.NewHandler(h, serviceName, otelOpts...)
}

// Middleware returns a constructor suitable for chi/mux middleware chains.
func Middleware(serviceName string, opts ...Option) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return WrapHandler(next, serviceName, opts...)
	}
}

// WrapTransport injects W3C context on outbound HTTP and creates CLIENT spans.
func WrapTransport(base http.RoundTripper, serviceName string, opts ...Option) http.RoundTripper {
	if base == nil {
		base = http.DefaultTransport
	}
	if _, ok := base.(*otelhttp.Transport); ok {
		return base
	}
	if serviceName == "" {
		serviceName = "olly"
	}
	o := applyOptions(opts)
	namer := o.spanNamer
	if namer == nil {
		label := serviceName
		namer = func(_ string, r *http.Request) string {
			return clientSpanName(label, r)
		}
	}
	otelOpts := []otelhttp.Option{
		otelhttp.WithPropagators(defaultPropagator()),
		otelhttp.WithSpanNameFormatter(namer),
	}
	if len(o.attrs) > 0 {
		otelOpts = append(otelOpts, otelhttp.WithSpanOptions(o.attrs...))
	}
	return otelhttp.NewTransport(base, otelOpts...)
}

// Client returns a shallow copy of c with an instrumented Transport.
// A nil client is treated as &http.Client{}.
func Client(c *http.Client, serviceName string, opts ...Option) *http.Client {
	if c == nil {
		c = &http.Client{}
	}
	out := *c
	out.Transport = WrapTransport(c.Transport, serviceName, opts...)
	return &out
}
