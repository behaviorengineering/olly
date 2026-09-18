package olly

import (
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/behaviorengineering/olly/dump"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

// DefaultOTLPEndpoint is the local Polypus HyperDX OTLP/gRPC host:port.
// Init does not apply it automatically when OTLPEndpoint is empty (empty still
// means dump-only). Apps and ConfigFromEnv may use this constant.
const DefaultOTLPEndpoint = "localhost:4319"

// Protocol selects the OTLP transport.
type Protocol string

const (
	// ProtocolGRPC is OTLP over gRPC (default).
	ProtocolGRPC Protocol = "grpc"
	// ProtocolHTTP is OTLP over HTTP protobuf (http/protobuf).
	ProtocolHTTP Protocol = "http/protobuf"
)

// Config controls provider construction.
type Config struct {
	// Enabled installs a real TracerProvider. When false, Init installs noop.
	Enabled bool
	// ServiceName is set on the resource as service.name.
	// Empty falls back to OTEL_SERVICE_NAME, then "olly".
	ServiceName string
	// OTLPEndpoint is host:port or URL for OTLP. Empty skips OTLP after env
	// fallback (dump-only tracing still works when Dump.Dir is set).
	// Env fallback: OTEL_EXPORTER_OTLP_TRACES_ENDPOINT, then OTEL_EXPORTER_OTLP_ENDPOINT.
	OTLPEndpoint string
	// Protocol is grpc or http/protobuf. Empty falls back to
	// OTEL_EXPORTER_OTLP_TRACES_PROTOCOL, then OTEL_EXPORTER_OTLP_PROTOCOL, then grpc.
	Protocol Protocol
	// Headers are sent with OTLP export (e.g. authorization for HyperDX cloud).
	// Env fallback: OTEL_EXPORTER_OTLP_TRACES_HEADERS, then OTEL_EXPORTER_OTLP_HEADERS
	// (comma-separated k=v). Explicit Headers win over env for the same key.
	Headers map[string]string
	// Insecure overrides TLS. Nil derives from the endpoint: localhost, 127.0.0.1,
	// or an http:// scheme uses insecure; otherwise TLS.
	Insecure *bool
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
	// Env OTEL_RESOURCE_ATTRIBUTES (comma-separated k=v) fill keys not set here.
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

// Resolve applies environment fallbacks onto a copy of Config without mutating c.
// Explicit non-empty Config fields win over env.
func (c Config) Resolve() Config {
	out := c.Defaults()
	if out.ServiceName == "" {
		out.ServiceName = strings.TrimSpace(os.Getenv("OTEL_SERVICE_NAME"))
	}
	if out.OTLPEndpoint == "" {
		out.OTLPEndpoint = firstNonEmpty(
			os.Getenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT"),
			os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"),
		)
	}
	if out.Protocol == "" {
		out.Protocol = Protocol(strings.TrimSpace(firstNonEmpty(
			os.Getenv("OTEL_EXPORTER_OTLP_TRACES_PROTOCOL"),
			os.Getenv("OTEL_EXPORTER_OTLP_PROTOCOL"),
		)))
	}
	out.Protocol = normalizeProtocol(out.Protocol)
	out.Headers = mergeHeaders(parseHeaderEnv(firstNonEmpty(
		os.Getenv("OTEL_EXPORTER_OTLP_TRACES_HEADERS"),
		os.Getenv("OTEL_EXPORTER_OTLP_HEADERS"),
	)), out.Headers)
	out.ResourceAttributes = mergeStringMap(parseResourceAttributesEnv(os.Getenv("OTEL_RESOURCE_ATTRIBUTES")), out.ResourceAttributes)
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

// BoolPtr returns a pointer to b for Config.Insecure.
func BoolPtr(b bool) *bool { return &b }

func normalizeProtocol(p Protocol) Protocol {
	switch strings.ToLower(strings.TrimSpace(string(p))) {
	case "http/protobuf", "http", "http/proto":
		return ProtocolHTTP
	case "grpc", "":
		return ProtocolGRPC
	default:
		return ProtocolGRPC
	}
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if s := strings.TrimSpace(v); s != "" {
			return s
		}
	}
	return ""
}

func parseHeaderEnv(raw string) map[string]string {
	return parseCommaKV(raw)
}

func parseResourceAttributesEnv(raw string) map[string]string {
	return parseCommaKV(raw)
}

func parseCommaKV(raw string) map[string]string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	out := make(map[string]string)
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		k, v, ok := strings.Cut(part, "=")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		if k == "" {
			continue
		}
		out[k] = v
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// mergeStringMap returns env keys first, then overlay wins on conflict.
func mergeStringMap(base, overlay map[string]string) map[string]string {
	if len(base) == 0 && len(overlay) == 0 {
		return nil
	}
	out := make(map[string]string, len(base)+len(overlay))
	for k, v := range base {
		out[k] = v
	}
	for k, v := range overlay {
		out[k] = v
	}
	return out
}

// mergeHeaders: env first, explicit overlay wins.
func mergeHeaders(env, explicit map[string]string) map[string]string {
	return mergeStringMap(env, explicit)
}

func deriveInsecure(endpoint string, override *bool) bool {
	if override != nil {
		return *override
	}
	ep := strings.TrimSpace(strings.ToLower(endpoint))
	if strings.HasPrefix(ep, "http://") {
		return true
	}
	if strings.HasPrefix(ep, "https://") {
		return false
	}
	host := ep
	if i := strings.Index(host, "://"); i >= 0 {
		host = host[i+3:]
	}
	if i := strings.IndexAny(host, "/?"); i >= 0 {
		host = host[:i]
	}
	if i := strings.LastIndex(host, ":"); i >= 0 {
		// strip port; handle [ipv6]
		if !strings.HasPrefix(host, "[") {
			host = host[:i]
		}
	}
	host = strings.Trim(host, "[]")
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}

func stripEndpointScheme(endpoint string) string {
	ep := strings.TrimSpace(endpoint)
	for _, p := range []string{"https://", "http://", "grpc://"} {
		if strings.HasPrefix(strings.ToLower(ep), p) {
			return ep[len(p):]
		}
	}
	return ep
}

// parseBoolEnv reads a truthy env var (1, true, yes, on).
func parseBoolEnv(key string) (value bool, ok bool) {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return false, false
	}
	v, err := strconv.ParseBool(raw)
	if err != nil {
		switch strings.ToLower(raw) {
		case "1", "yes", "on":
			return true, true
		case "0", "no", "off":
			return false, true
		default:
			return false, false
		}
	}
	return v, true
}
