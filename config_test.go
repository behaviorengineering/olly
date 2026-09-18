package olly_test

import (
	"testing"

	"github.com/behaviorengineering/olly"
)

func TestResolveTracesEndpointWins(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "localhost:4317")
	t.Setenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", "localhost:4319")
	cfg := olly.Config{Enabled: true}.Resolve()
	if cfg.OTLPEndpoint != "localhost:4319" {
		t.Fatalf("endpoint=%q", cfg.OTLPEndpoint)
	}
}

func TestResolveExplicitEndpointWinsOverEnv(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "localhost:4317")
	cfg := olly.Config{Enabled: true, OTLPEndpoint: "explicit:9999"}.Resolve()
	if cfg.OTLPEndpoint != "explicit:9999" {
		t.Fatalf("endpoint=%q", cfg.OTLPEndpoint)
	}
}

func TestResolveProtocolHTTP(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_PROTOCOL", "http/protobuf")
	cfg := olly.Config{Enabled: true}.Resolve()
	if cfg.Protocol != olly.ProtocolHTTP {
		t.Fatalf("protocol=%q", cfg.Protocol)
	}
}

func TestResolveTracesProtocolWins(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_PROTOCOL", "grpc")
	t.Setenv("OTEL_EXPORTER_OTLP_TRACES_PROTOCOL", "http/protobuf")
	cfg := olly.Config{Enabled: true}.Resolve()
	if cfg.Protocol != olly.ProtocolHTTP {
		t.Fatalf("protocol=%q", cfg.Protocol)
	}
}

func TestResolveHeadersMerge(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_HEADERS", "authorization=env-key,x-from=env")
	cfg := olly.Config{
		Enabled: true,
		Headers: map[string]string{"authorization": "explicit-key", "x-extra": "1"},
	}.Resolve()
	if cfg.Headers["authorization"] != "explicit-key" {
		t.Fatalf("authorization=%q", cfg.Headers["authorization"])
	}
	if cfg.Headers["x-from"] != "env" {
		t.Fatalf("x-from=%q", cfg.Headers["x-from"])
	}
	if cfg.Headers["x-extra"] != "1" {
		t.Fatalf("x-extra=%q", cfg.Headers["x-extra"])
	}
}

func TestResolveServiceNameAndResourceAttrs(t *testing.T) {
	t.Setenv("OTEL_SERVICE_NAME", "from-env")
	t.Setenv("OTEL_RESOURCE_ATTRIBUTES", "deployment.environment=dev,service.name=ignored")
	cfg := olly.Config{
		Enabled:            true,
		ResourceAttributes: map[string]string{"team": "platform"},
	}.Resolve()
	if cfg.ServiceName != "from-env" {
		t.Fatalf("service=%q", cfg.ServiceName)
	}
	if cfg.ResourceAttributes["deployment.environment"] != "dev" {
		t.Fatalf("attrs=%v", cfg.ResourceAttributes)
	}
	if cfg.ResourceAttributes["team"] != "platform" {
		t.Fatalf("attrs=%v", cfg.ResourceAttributes)
	}
}
