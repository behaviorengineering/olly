package olly

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/behaviorengineering/olly/dump"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
)

var (
	providerMu sync.RWMutex
	provider   *sdktrace.TracerProvider
	shutdownFn func(context.Context) error
)

// Init installs the global TracerProvider, W3C propagator, optional OTLP exporter,
// and optional failure-dump processor. Returns a shutdown function.
//
// Environment fallbacks (when Config fields are empty) follow the OpenTelemetry
// spec names; see Config and Config.Resolve.
func Init(cfg Config) (func(context.Context) error, error) {
	cfg = cfg.Resolve()
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))
	if !cfg.Enabled {
		otel.SetTracerProvider(noop.NewTracerProvider())
		setShutdown(func(context.Context) error { return nil })
		return Shutdown, nil
	}

	serviceName := cfg.ServiceName
	if serviceName == "" {
		serviceName = "olly"
	}

	attrs := []attribute.KeyValue{
		attribute.String("service.name", serviceName),
	}
	for k, v := range cfg.ResourceAttributes {
		if k == "" || k == "service.name" {
			continue
		}
		attrs = append(attrs, attribute.String(k, v))
	}
	res, err := resource.Merge(
		resource.Default(),
		resource.NewWithAttributes("", attrs...),
	)
	if err != nil {
		return nil, fmt.Errorf("olly: resource: %w", err)
	}

	opts := []sdktrace.TracerProviderOption{
		sdktrace.WithResource(res),
		sdktrace.WithSampler(SamplerForRatio(cfg.SampleRatio)),
	}

	if cfg.Dump.Dir != "" {
		procCfg := cfg.Dump
		if procCfg.Diagnostics == nil {
			procCfg.Diagnostics = cfg.Diagnostics
		}
		opts = append(opts, sdktrace.WithSpanProcessor(dump.NewProcessor(procCfg)))
	}
	for _, p := range cfg.ExtraProcessors {
		if p != nil {
			opts = append(opts, sdktrace.WithSpanProcessor(p))
		}
	}

	if cfg.OTLPEndpoint != "" {
		ctx, cancel := context.WithTimeout(context.Background(), cfg.OTLPTimeout)
		defer cancel()
		exporter, expErr := newOTLPExporter(ctx, cfg)
		if expErr != nil {
			if !cfg.AllowOTLPFailure {
				return nil, fmt.Errorf("olly: otlp exporter: %w", expErr)
			}
			reportDiag(cfg.Diagnostics, fmt.Sprintf("olly: OTLP exporter failed (%v); dump-only tracing continues", expErr))
		} else {
			opts = append(opts, sdktrace.WithBatcher(
				exporter,
				sdktrace.WithBatchTimeout(cfg.BatchTimeout),
			))
		}
	}

	tp := sdktrace.NewTracerProvider(opts...)
	otel.SetTracerProvider(tp)
	setProvider(tp)
	setShutdown(tp.Shutdown)
	return Shutdown, nil
}

func newOTLPExporter(ctx context.Context, cfg Config) (*otlptrace.Exporter, error) {
	insecure := deriveInsecure(cfg.OTLPEndpoint, cfg.Insecure)
	switch cfg.Protocol {
	case ProtocolHTTP:
		opts := []otlptracehttp.Option{}
		ep := strings.TrimSpace(cfg.OTLPEndpoint)
		if strings.Contains(ep, "://") {
			opts = append(opts, otlptracehttp.WithEndpointURL(ep))
		} else {
			opts = append(opts, otlptracehttp.WithEndpoint(stripEndpointScheme(ep)))
			if insecure {
				opts = append(opts, otlptracehttp.WithInsecure())
			}
		}
		if len(cfg.Headers) > 0 {
			opts = append(opts, otlptracehttp.WithHeaders(cfg.Headers))
		}
		return otlptracehttp.New(ctx, opts...)
	default:
		opts := []otlptracegrpc.Option{
			otlptracegrpc.WithEndpoint(stripEndpointScheme(cfg.OTLPEndpoint)),
		}
		if insecure {
			opts = append(opts, otlptracegrpc.WithInsecure())
		}
		if len(cfg.Headers) > 0 {
			opts = append(opts, otlptracegrpc.WithHeaders(cfg.Headers))
		}
		return otlptracegrpc.New(ctx, opts...)
	}
}

// Shutdown shuts down the provider installed by Init.
func Shutdown(ctx context.Context) error {
	providerMu.RLock()
	fn := shutdownFn
	providerMu.RUnlock()
	if fn == nil {
		return nil
	}
	return fn(ctx)
}

// Flush flushes pending spans when an SDK provider is active.
func Flush(ctx context.Context) error {
	providerMu.RLock()
	tp := provider
	providerMu.RUnlock()
	if tp != nil {
		return tp.ForceFlush(ctx)
	}
	if p, ok := otel.GetTracerProvider().(*sdktrace.TracerProvider); ok {
		return p.ForceFlush(ctx)
	}
	return nil
}

// Tracer returns a named tracer from the global provider.
func Tracer(instrumentationName string) trace.Tracer {
	if instrumentationName == "" {
		instrumentationName = "github.com/behaviorengineering/olly"
	}
	return otel.Tracer(instrumentationName)
}

// Provider returns the SDK provider installed by Init, or nil.
func Provider() *sdktrace.TracerProvider {
	providerMu.RLock()
	defer providerMu.RUnlock()
	return provider
}

func setProvider(tp *sdktrace.TracerProvider) {
	providerMu.Lock()
	defer providerMu.Unlock()
	provider = tp
}

func setShutdown(fn func(context.Context) error) {
	providerMu.Lock()
	defer providerMu.Unlock()
	shutdownFn = fn
}

func reportDiag(d dump.Diagnostics, msg string) {
	if d != nil {
		d.Printf("%s", msg)
		return
	}
	dump.StderrDiagnostics{}.Printf("%s", msg)
}
