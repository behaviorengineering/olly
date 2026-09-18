// Package cli provides stdlib helpers to wire OpenTelemetry into Go CLIs
// with minimal boilerplate. There is no cobra dependency; see README for a
// PersistentPreRunE / PersistentPostRunE snippet.
package cli

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/behaviorengineering/olly"
	ollyerrors "github.com/behaviorengineering/olly/errors"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

const (
	exitOK    = 0
	exitError = 1

	defaultShutdownTimeout = 5 * time.Second
)

// AddFlags registers OpenTelemetry flags on fs that mutate cfg.
// Call after constructing cfg (e.g. from ConfigFromEnv) and before Parse.
func AddFlags(fs *flag.FlagSet, cfg *olly.Config) {
	if fs == nil || cfg == nil {
		return
	}
	fs.BoolVar(&cfg.Enabled, "otel-enabled", cfg.Enabled, "enable OpenTelemetry tracing")
	fs.StringVar(&cfg.OTLPEndpoint, "otel-endpoint", cfg.OTLPEndpoint, "OTLP endpoint (host:port or URL)")
	fs.Func("otel-protocol", "OTLP protocol: grpc or http/protobuf", func(s string) error {
		cfg.Protocol = olly.Protocol(s)
		return nil
	})
	fs.StringVar(&cfg.ServiceName, "otel-service", cfg.ServiceName, "OTEL service.name")
	fs.Float64Var(&cfg.SampleRatio, "otel-sample-ratio", cfg.SampleRatio, "trace sample ratio (0 or 1 = always)")
	fs.StringVar(&cfg.Dump.Dir, "otel-dump-dir", cfg.Dump.Dir, "local failure-trace dump directory")
}

// ConfigFromEnv builds a Config from the OpenTelemetry environment contract.
// serviceName is used when OTEL_SERVICE_NAME is unset.
//
// Enabled is true unless OTEL_SDK_DISABLED is truthy. When enabled and no OTLP
// endpoint is set via env, OTLPEndpoint defaults to olly.DefaultOTLPEndpoint.
func ConfigFromEnv(serviceName string) olly.Config {
	cfg := olly.Config{
		Enabled:     true,
		ServiceName: serviceName,
	}.Resolve()
	if disabled, ok := parseSDKDisabled(); ok && disabled {
		cfg.Enabled = false
		cfg.OTLPEndpoint = ""
		return cfg
	}
	if cfg.ServiceName == "" {
		cfg.ServiceName = serviceName
	}
	if cfg.Enabled && cfg.OTLPEndpoint == "" {
		cfg.OTLPEndpoint = olly.DefaultOTLPEndpoint
	}
	cfg.AllowOTLPFailure = true
	return cfg
}

func parseSDKDisabled() (bool, bool) {
	raw := os.Getenv("OTEL_SDK_DISABLED")
	if raw == "" {
		return false, false
	}
	switch raw {
	case "1", "true", "TRUE", "True", "yes", "on":
		return true, true
	case "0", "false", "FALSE", "False", "no", "off":
		return false, true
	default:
		return false, false
	}
}

// Run initializes olly, runs fn under a command span, flushes and shuts down.
// Returns 0 on success and 1 on error. Cancels ctx on SIGINT/SIGTERM.
func Run(ctx context.Context, cfg olly.Config, fn func(ctx context.Context) error) int {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	shutdown, err := olly.Init(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "olly: init: %v\n", err)
		return exitError
	}
	defer func() {
		sctx, cancel := context.WithTimeout(context.Background(), defaultShutdownTimeout)
		defer cancel()
		_ = olly.Flush(sctx)
		_ = shutdown(sctx)
	}()

	op := cfg.ServiceName
	if op == "" {
		op = "command"
	}
	var runErr error
	ctx, span := WithCommandSpan(ctx, cfg.ServiceName, op)
	defer func() { EndWithErr(span, runErr) }()

	runErr = fn(ctx)
	if runErr != nil {
		fmt.Fprintf(os.Stderr, "%v\n", runErr)
		return exitError
	}
	return exitOK
}

// WithCommandSpan starts a SERVER span for a CLI command entrypoint.
func WithCommandSpan(ctx context.Context, tracerName, op string) (context.Context, trace.Span) {
	if tracerName == "" {
		tracerName = "olly/cli"
	}
	if op == "" {
		op = "command"
	}
	tr := olly.Tracer(tracerName)
	ctx, span := tr.Start(ctx, op, trace.WithSpanKind(trace.SpanKindServer))
	span.SetAttributes(
		attribute.String("command.operation", op),
		attribute.String("service.name", tracerName),
	)
	return ctx, span
}

// EndWithErr records status from err and ends the span.
func EndWithErr(span trace.Span, err error) {
	ollyerrors.EndWithStatus(span, err)
}

// Lifecycle holds Init/Shutdown for frameworks (e.g. cobra) that need hooks.
type Lifecycle struct {
	shutdown func(context.Context) error
}

// Start initializes olly from cfg. Pair with Stop in a post-run hook.
func Start(cfg olly.Config) (*Lifecycle, error) {
	shutdown, err := olly.Init(cfg)
	if err != nil {
		return nil, err
	}
	return &Lifecycle{shutdown: shutdown}, nil
}

// Stop flushes and shuts down the provider.
func (l *Lifecycle) Stop(ctx context.Context) error {
	if l == nil || l.shutdown == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	sctx, cancel := context.WithTimeout(ctx, defaultShutdownTimeout)
	defer cancel()
	_ = olly.Flush(sctx)
	return l.shutdown(sctx)
}
