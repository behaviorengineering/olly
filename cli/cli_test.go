package cli_test

import (
	"context"
	"errors"
	"flag"
	"testing"

	"github.com/behaviorengineering/olly"
	ollicli "github.com/behaviorengineering/olly/cli"
)

func TestConfigFromEnvDefaultsEndpoint(t *testing.T) {
	t.Setenv("OTEL_SDK_DISABLED", "")
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
	t.Setenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", "")
	t.Setenv("OTEL_SERVICE_NAME", "")
	cfg := ollicli.ConfigFromEnv("my-cli")
	if !cfg.Enabled {
		t.Fatal("expected enabled")
	}
	if cfg.ServiceName != "my-cli" {
		t.Fatalf("service=%q", cfg.ServiceName)
	}
	if cfg.OTLPEndpoint != olly.DefaultOTLPEndpoint {
		t.Fatalf("endpoint=%q", cfg.OTLPEndpoint)
	}
}

func TestConfigFromEnvDisabled(t *testing.T) {
	t.Setenv("OTEL_SDK_DISABLED", "true")
	cfg := ollicli.ConfigFromEnv("my-cli")
	if cfg.Enabled {
		t.Fatal("expected disabled")
	}
	if cfg.OTLPEndpoint != "" {
		t.Fatalf("endpoint=%q", cfg.OTLPEndpoint)
	}
}

func TestConfigFromEnvRespectsEndpoint(t *testing.T) {
	t.Setenv("OTEL_SDK_DISABLED", "")
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "collector:4317")
	cfg := ollicli.ConfigFromEnv("my-cli")
	if cfg.OTLPEndpoint != "collector:4317" {
		t.Fatalf("endpoint=%q", cfg.OTLPEndpoint)
	}
}

func TestAddFlagsOverrides(t *testing.T) {
	cfg := ollicli.ConfigFromEnv("base")
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	ollicli.AddFlags(fs, &cfg)
	if err := fs.Parse([]string{
		"-otel-enabled=false",
		"-otel-service=flag-svc",
		"-otel-endpoint=localhost:9999",
		"-otel-dump-dir=/tmp/dumps",
	}); err != nil {
		t.Fatal(err)
	}
	if cfg.Enabled {
		t.Fatal("expected disabled via flag")
	}
	if cfg.ServiceName != "flag-svc" {
		t.Fatalf("service=%q", cfg.ServiceName)
	}
	if cfg.OTLPEndpoint != "localhost:9999" {
		t.Fatalf("endpoint=%q", cfg.OTLPEndpoint)
	}
	if cfg.Dump.Dir != "/tmp/dumps" {
		t.Fatalf("dump=%q", cfg.Dump.Dir)
	}
}

func TestRunSuccessAndError(t *testing.T) {
	cfg := olly.Config{Enabled: false, ServiceName: "run-test"}
	code := ollicli.Run(context.Background(), cfg, func(ctx context.Context) error {
		return nil
	})
	if code != 0 {
		t.Fatalf("code=%d", code)
	}
	code = ollicli.Run(context.Background(), cfg, func(ctx context.Context) error {
		return errors.New("boom")
	})
	if code != 1 {
		t.Fatalf("code=%d", code)
	}
}

func TestLifecycle(t *testing.T) {
	lc, err := ollicli.Start(olly.Config{Enabled: false, ServiceName: "lc"})
	if err != nil {
		t.Fatal(err)
	}
	if err := lc.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
}
