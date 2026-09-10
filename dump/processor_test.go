package dump_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/behaviorengineering/olly/dump"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

type recordingDiag struct {
	msgs []string
}

func (r *recordingDiag) Printf(format string, args ...any) {
	r.msgs = append(r.msgs, fmt.Sprintf(format, args...))
}

func TestProcessorIncludesEventsAndOnWrite(t *testing.T) {
	dir := t.TempDir()
	var wrote string
	proc := dump.NewProcessor(dump.Config{
		Dir: dir,
		OnWrite: func(path string) {
			wrote = path
		},
		RedactAttribute: func(key string, value any) any {
			if key == "token" {
				return "REDACTED"
			}
			return value
		},
		Diagnostics: &recordingDiag{},
	})
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(proc))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	tr := tp.Tracer("test")

	_, server := tr.Start(context.Background(), "cmd", trace.WithSpanKind(trace.SpanKindServer))
	server.AddEvent("exception", trace.WithAttributes(attribute.String("token", "secret")))
	server.SetStatus(codes.Error, "failed")
	server.End()
	_ = tp.ForceFlush(context.Background())

	if wrote == "" {
		t.Fatal("expected OnWrite path")
	}
	body, err := os.ReadFile(wrote)
	if err != nil {
		t.Fatal(err)
	}
	var doc dump.TraceDocument
	if err := json.Unmarshal(body, &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.Spans) != 1 || len(doc.Spans[0].Events) != 1 {
		t.Fatalf("spans/events = %+v", doc.Spans)
	}
	ev := doc.Spans[0].Events[0]
	if ev.Name != "exception" {
		t.Fatalf("event name=%q", ev.Name)
	}
	if ev.Attributes["token"] != "REDACTED" {
		t.Fatalf("token=%v", ev.Attributes["token"])
	}
}

func TestProcessorDumpsOnEnvelopeError(t *testing.T) {
	dir := t.TempDir()
	diag := &recordingDiag{}
	proc := dump.NewProcessor(dump.Config{
		Dir:         dir,
		MaxFiles:    10,
		Diagnostics: diag,
		RedactAttribute: func(key string, value any) any {
			if key == "secret" {
				return "REDACTED"
			}
			return value
		},
		RedactStatusText: func(text string) string {
			return strings.ReplaceAll(text, "token=abc", "token=REDACTED")
		},
	})

	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(proc))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	tr := tp.Tracer("test")

	ctx, server := tr.Start(context.Background(), "cmd", trace.WithSpanKind(trace.SpanKindServer))
	_, child := tr.Start(ctx, "work", trace.WithSpanKind(trace.SpanKindInternal))
	child.SetAttributes(attribute.String("secret", "value"))
	child.SetStatus(codes.Error, "boom token=abc")
	child.End()
	server.SetStatus(codes.Error, "failed")
	server.End()
	_ = tp.ForceFlush(context.Background())

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("dumps = %d, want 1; diag=%v", len(entries), diag.msgs)
	}
	body, err := os.ReadFile(filepath.Join(dir, entries[0].Name()))
	if err != nil {
		t.Fatal(err)
	}
	var doc dump.TraceDocument
	if err := json.Unmarshal(body, &doc); err != nil {
		t.Fatal(err)
	}
	if doc.SpanCount != 2 {
		t.Fatalf("span_count = %d, want 2", doc.SpanCount)
	}
	foundSecret := false
	for _, s := range doc.Spans {
		if s.Attributes != nil {
			if v, ok := s.Attributes["secret"]; ok {
				foundSecret = true
				if v != "REDACTED" {
					t.Fatalf("secret attr = %v, want REDACTED", v)
				}
			}
		}
		if strings.Contains(s.StatusMessage, "token=abc") {
			t.Fatalf("status not redacted: %q", s.StatusMessage)
		}
	}
	if !foundSecret {
		t.Fatal("expected secret attribute in dump")
	}
}

func TestProcessorSkipsSuccessfulTraces(t *testing.T) {
	dir := t.TempDir()
	proc := dump.NewProcessor(dump.Config{Dir: dir, Diagnostics: &recordingDiag{}})
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(proc))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	tr := tp.Tracer("test")

	_, server := tr.Start(context.Background(), "ok", trace.WithSpanKind(trace.SpanKindServer))
	server.SetStatus(codes.Ok, "")
	server.End()
	_ = tp.ForceFlush(context.Background())

	entries, err := os.ReadDir(dir)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("dumps = %d, want 0", len(entries))
	}
}

func TestProcessorSkipsRecoveredChildErrors(t *testing.T) {
	dir := t.TempDir()
	diag := &recordingDiag{}
	proc := dump.NewProcessor(dump.Config{Dir: dir, Diagnostics: diag})
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(proc))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	tr := tp.Tracer("test")

	ctx, server := tr.Start(context.Background(), "youtube.manage.add", trace.WithSpanKind(trace.SpanKindServer))
	_, failed := tr.Start(ctx, "Predict.Context", trace.WithSpanKind(trace.SpanKindClient))
	failed.SetStatus(codes.Error, "XML parsing failed")
	failed.End()
	_, okChild := tr.Start(ctx, "Predict.Context", trace.WithSpanKind(trace.SpanKindClient))
	okChild.SetStatus(codes.Ok, "")
	okChild.End()
	server.SetStatus(codes.Ok, "")
	server.End()
	_ = tp.ForceFlush(context.Background())

	entries, err := os.ReadDir(dir)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("dumps = %d, want 0 for recovered child errors; diag=%v", len(entries), diag.msgs)
	}
}

func TestProcessorRemoteParentStillDumps(t *testing.T) {
	dir := t.TempDir()
	proc := dump.NewProcessor(dump.Config{Dir: dir, Diagnostics: &recordingDiag{}})
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(proc))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	remoteSC := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    mustTraceID("0102030405060708090a0b0c0d0e0f10"),
		SpanID:     mustSpanID("0102030405060708"),
		TraceFlags: trace.FlagsSampled,
		Remote:     true,
	})
	ctx := trace.ContextWithRemoteSpanContext(context.Background(), remoteSC)
	tr := tp.Tracer("test")
	_, server := tr.Start(ctx, "gateway", trace.WithSpanKind(trace.SpanKindServer))
	if server.SpanContext().IsRemote() {
		t.Fatal("local envelope should not be remote")
	}
	if !server.SpanContext().HasTraceID() {
		t.Fatal("expected trace id")
	}
	server.SetStatus(codes.Error, "downstream failed")
	server.End()
	_ = tp.ForceFlush(context.Background())

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("dumps = %d, want 1", len(entries))
	}
}

func TestProcessorShutdownDumpsInFlightError(t *testing.T) {
	dir := t.TempDir()
	proc := dump.NewProcessor(dump.Config{Dir: dir, Diagnostics: &recordingDiag{}})
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(proc))
	tr := tp.Tracer("test")

	ctx, server := tr.Start(context.Background(), "cmd", trace.WithSpanKind(trace.SpanKindServer))
	_, child := tr.Start(ctx, "work", trace.WithSpanKind(trace.SpanKindInternal))
	child.SetStatus(codes.Error, "boom")
	child.End()
	_ = server // leave envelope open
	_ = tp.Shutdown(context.Background())

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("dumps = %d, want 1", len(entries))
	}
}

func TestPruneMaxFiles(t *testing.T) {
	dir := t.TempDir()
	ids := []string{
		"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		"cccccccccccccccccccccccccccccccc",
		"dddddddddddddddddddddddddddddddd",
		"eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee",
	}
	for _, id := range ids {
		name := filepath.Join(dir, id+".json")
		if err := os.WriteFile(name, []byte(`{}`), 0o640); err != nil {
			t.Fatal(err)
		}
		time.Sleep(5 * time.Millisecond)
	}
	if err := dump.Prune(dir, 0, 2); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("kept = %d, want 2", len(entries))
	}
}

func mustTraceID(hex32 string) trace.TraceID {
	id, err := trace.TraceIDFromHex(hex32)
	if err != nil {
		panic(err)
	}
	return id
}

func mustSpanID(hex16 string) trace.SpanID {
	id, err := trace.SpanIDFromHex(hex16)
	if err != nil {
		panic(err)
	}
	return id
}
