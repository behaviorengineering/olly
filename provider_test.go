package olly_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/behaviorengineering/olly"
	"github.com/behaviorengineering/olly/dump"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
)

func TestInitDisabledUsesNoop(t *testing.T) {
	shutdown, err := olly.Init(olly.Config{Enabled: false})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = shutdown(context.Background()) })
	if _, ok := otel.GetTracerProvider().(*noop.TracerProvider); !ok {
		// noop provider type may be unexported wrapper; just ensure spans are not recording
		_, span := otel.Tracer("t").Start(context.Background(), "x")
		if span.IsRecording() {
			t.Fatal("expected non-recording span when disabled")
		}
		span.End()
	}
}

func TestInitDumpOnlyWithoutOTLP(t *testing.T) {
	dir := t.TempDir()
	shutdown, err := olly.Init(olly.Config{
		Enabled:          true,
		ServiceName:      "olly-test",
		AllowOTLPFailure: true,
		Dump: dump.Config{
			Dir:         dir,
			MaxFiles:    5,
			Diagnostics: dump.StderrDiagnostics{},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = shutdown(context.Background()) })

	tr := olly.Tracer("test")
	ctx, server := tr.Start(context.Background(), "cmd", trace.WithSpanKind(trace.SpanKindServer))
	_, child := tr.Start(ctx, "work", trace.WithSpanKind(trace.SpanKindInternal))
	child.SetStatus(codes.Error, "boom")
	child.End()
	server.SetStatus(codes.Error, "failed")
	server.End()
	_ = olly.Flush(context.Background())

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("dumps = %d, want 1", len(entries))
	}
	if filepath.Ext(entries[0].Name()) != ".json" {
		t.Fatalf("unexpected dump name %q", entries[0].Name())
	}
}

func TestSamplerForRatio(t *testing.T) {
	if olly.SamplerForRatio(0) == nil || olly.SamplerForRatio(1) == nil {
		t.Fatal("expected samplers")
	}
	if olly.SamplerForRatio(0.5) == nil {
		t.Fatal("expected ratio sampler")
	}
}
