// Package olly is a portable OpenTelemetry helper for Go services.
//
// Boundary rules:
//  1. No imports of product application packages.
//  2. No product-specific service names, operation names, or metric names.
//  3. Apps own business span names and redaction policy.
//  4. olly owns provider lifecycle, optional OTLP export, local failure-trace
//     dumps, HTTP middleware, CLI Run helpers, and small span error helpers.
//
// Package layout:
//   - olly: Init, Config, Shutdown, Flush (env-aware OTLP)
//   - olly/dump: SpanProcessor that buffers by trace and writes JSON on envelope ERROR
//   - olly/errors: span annotation helpers (Record, EndWithStatus, IDs)
//   - olly/http: WrapHandler, Middleware, WrapTransport, Client
//   - olly/cli: AddFlags, ConfigFromEnv, Run, Lifecycle (stdlib flag; no cobra)
package olly
