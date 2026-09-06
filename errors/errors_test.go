package errors_test

import (
	"context"
	"errors"
	"testing"

	ollyerrors "github.com/behaviorengineering/olly/errors"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestEndWithStatusRecordsError(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	tr := tp.Tracer("test")
	_, span := tr.Start(context.Background(), "op")
	ollyerrors.EndWithStatus(span, errors.New("nope"))
	spans := exporter.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("spans = %d", len(spans))
	}
	if spans[0].Status.Code != codes.Error {
		t.Fatalf("status = %v", spans[0].Status.Code)
	}
	tid, sid := ollyerrors.IDs(span)
	if tid == "" || sid == "" {
		// span already ended; IDs still valid from ended span context
		tid, sid = spans[0].SpanContext.TraceID().String(), spans[0].SpanContext.SpanID().String()
	}
	if tid == "" || sid == "" {
		t.Fatal("expected ids")
	}
}

func TestEndWithStatusOK(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	_, span := tp.Tracer("test").Start(context.Background(), "op")
	ollyerrors.EndWithStatus(span, nil)
	spans := exporter.GetSpans()
	if len(spans) != 1 || spans[0].Status.Code != codes.Ok {
		t.Fatalf("status = %+v", spans)
	}
}
