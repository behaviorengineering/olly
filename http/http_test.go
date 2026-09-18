package http_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	ollyhttp "github.com/behaviorengineering/olly/http"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

func installTP(t *testing.T) *tracetest.InMemoryExporter {
	t.Helper()
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	prev := otel.GetTracerProvider()
	otel.SetTracerProvider(tp)
	t.Cleanup(func() { otel.SetTracerProvider(prev) })
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))
	return exporter
}

func TestWrapHandlerSkipsHealth(t *testing.T) {
	exporter := installTP(t)
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := ollyhttp.WrapHandler(inner, "svc")
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/health", nil))
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/x", nil))

	sawHealth, sawAPI := false, false
	for _, sp := range exporter.GetSpans() {
		if sp.Name == "svc SERVER GET /health" {
			sawHealth = true
		}
		if sp.Name == "svc SERVER GET /api/x" {
			sawAPI = true
		}
	}
	if sawHealth {
		t.Fatal("health should be skipped")
	}
	if !sawAPI {
		t.Fatal("missing api span")
	}
}

func TestMiddlewareAndCustomSkip(t *testing.T) {
	exporter := installTP(t)
	mw := ollyhttp.Middleware("svc", ollyhttp.WithSkipPaths("/ready"))
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/ready", nil))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/health", nil))

	sawReady, sawHealth := false, false
	for _, sp := range exporter.GetSpans() {
		if strings.Contains(sp.Name, "/ready") {
			sawReady = true
		}
		if strings.Contains(sp.Name, "/health") {
			sawHealth = true
		}
	}
	if sawReady {
		t.Fatal("ready should be skipped")
	}
	if !sawHealth {
		t.Fatal("default /health skip was replaced; expect health span")
	}
}

func TestWrapTransportPropagatesTraceparent(t *testing.T) {
	_ = installTP(t)
	var gotParent string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotParent = r.Header.Get("traceparent")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "ok")
	}))
	t.Cleanup(server.Close)

	client := ollyhttp.Client(&http.Client{}, "svc")
	ctx, span := otel.Tracer("test").Start(context.Background(), "root")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/x", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	span.End()

	if gotParent == "" {
		t.Fatal("expected traceparent on outbound request")
	}
}

func TestClientNilSafe(t *testing.T) {
	c := ollyhttp.Client(nil, "svc")
	if c == nil || c.Transport == nil {
		t.Fatal("expected instrumented client")
	}
}

func TestWithSpanOptions(t *testing.T) {
	exporter := installTP(t)
	h := ollyhttp.WrapHandler(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}),
		"svc",
		ollyhttp.WithSkipPaths(),
		ollyhttp.WithSpanOptions(trace.WithAttributes(attribute.String("http.io", "server"))),
	)
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/z", nil))
	found := false
	for _, sp := range exporter.GetSpans() {
		for _, a := range sp.Attributes {
			if string(a.Key) == "http.io" && a.Value.AsString() == "server" {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("missing http.io attribute")
	}
}
