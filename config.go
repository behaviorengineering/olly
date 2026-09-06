package olly

import (
	"time"

	"github.com/behaviorengineering/olly/dump"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

// Config controls provider construction.
type Config struct {
	// Enabled installs a real TracerProvider. When false, Init installs noop.
	Enabled bool
	// ServiceName is set on the resource as service.name.
	ServiceName string
	// OTLPEndpoint is host:port for OTLP/gRPC (insecure). Empty skips OTLP;
	// dump-only tracing still works when Dump.Dir is set.
	OTLPEndpoint string
	// OTLPTimeout bounds exporter construction. Zero uses 10s.
	OTLPTimeout time.Duration
	// BatchTimeout is the OTLP batch export interval. Zero uses 3s.
	BatchTimeout time.Duration
	// SampleRatio is 0..1. Values <=0 or >=1 use AlwaysSample.
	SampleRatio float64
	// AllowOTLPFailure continues with dump-only tracing when OTLP setup fails.
	// When false, Init returns the exporter error.
	AllowOTLPFailure bool
	// Dump configures the local failure-trace processor. Empty Dir disables it.
	Dump dump.Config
	// ExtraProcessors are appended after the dump processor (if any).
	ExtraProcessors []sdktrace.SpanProcessor
	// ResourceAttributes are merged onto the default resource (service.name wins from ServiceName).
	ResourceAttributes map[string]string
	// Diagnostics receives dump write/prune messages. Nil uses stderr.
	Diagnostics dump.Diagnostics
}

// Defaults fills zero-value timeouts and dump retention.
func (c Config) Defaults() Config {
	out := c
	if out.OTLPTimeout <= 0 {
		out.OTLPTimeout = 10 * time.Second
	}
	if out.BatchTimeout <= 0 {
		out.BatchTimeout = 3 * time.Second
	}
	out.Dump = out.Dump.Defaults()
	return out
}

// SamplerForRatio returns AlwaysSample for local/full capture (ratio <=0 or >=1).
// Otherwise ParentBased TraceIDRatioBased for partial export.
func SamplerForRatio(ratio float64) sdktrace.Sampler {
	if ratio <= 0 || ratio >= 1 {
		return sdktrace.AlwaysSample()
	}
	return sdktrace.ParentBased(sdktrace.TraceIDRatioBased(ratio))
}

// EnvelopeKind is the default process envelope for failure dumps (SERVER).
func EnvelopeKind() trace.SpanKind {
	return dump.DefaultEnvelopeKind
}
