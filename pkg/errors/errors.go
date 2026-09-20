// Package errors annotates OpenTelemetry spans from Go errors.
package errors

import (
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// Record marks the span as Error and records err. Nil span or err is a no-op.
func Record(span trace.Span, err error) {
	if span == nil || err == nil {
		return
	}
	span.RecordError(err)
	span.SetStatus(codes.Error, err.Error())
}

// EndWithStatus sets OK or Error from err, then ends the span.
func EndWithStatus(span trace.Span, err error) {
	if span == nil {
		return
	}
	if err != nil {
		Record(span, err)
	} else {
		span.SetStatus(codes.Ok, "")
	}
	span.End()
}

// EndWithStatusPtr is like EndWithStatus but takes *error for defer patterns.
func EndWithStatusPtr(span trace.Span, err *error) {
	if err != nil {
		EndWithStatus(span, *err)
		return
	}
	EndWithStatus(span, nil)
}

// IDs returns hex trace_id and span_id for the current span context.
func IDs(span trace.Span) (traceID, spanID string) {
	if span == nil {
		return "", ""
	}
	sc := span.SpanContext()
	if !sc.IsValid() {
		return "", ""
	}
	return sc.TraceID().String(), sc.SpanID().String()
}
