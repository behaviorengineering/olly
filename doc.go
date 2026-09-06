// Package olly is a portable OpenTelemetry helper for Go services.
//
// Boundary rules:
//  1. No imports of product application packages.
//  2. No product-specific service names, operation names, or metric names.
//  3. Apps own loggers, HTTP wrappers, and business instrumentation.
//  4. olly owns provider lifecycle, optional OTLP export, local failure-trace
//     dumps, and small span error helpers.
//
// Package layout:
//   - olly: Init, Config, Shutdown, Flush
//   - olly/dump: SpanProcessor that buffers by trace and writes JSON on envelope ERROR
//   - olly/errors: span annotation helpers (Record, EndWithStatus, TraceIDs)
package olly
