# olly

Portable OpenTelemetry helper for Behavior Engineering services.

Module: `github.com/behaviorengineering/olly`.

Apps map their own config and logger at the boundary, then call `olly.Init` once at process start. olly installs the global tracer provider, optional OTLP export, and an optional local failure-trace dump processor.

## What olly owns

| Package | Role |
|---------|------|
| `olly` | Provider lifecycle (`Init`, `Shutdown`, `Flush`), sampling, W3C propagation |
| `olly/dump` | Buffer spans by `trace_id`; write JSON when SERVER envelopes end with ERROR |
| `olly/errors` | Annotate spans from Go errors; expose trace/span IDs for logs |

## What stays in the app

- Service names, operation taxonomies, HTTP wrappers
- Redaction of product-specific attributes (pass dump hooks)
- Business metrics and logger implementations
- Pipeline execution records (`strop/runreport`)

## Failure dumps

Default completion policy (one mode for gateway and CLI):

1. Buffer spans by `trace_id`.
2. Treat `SpanKindServer` as this process's envelope (not `IsRoot()`; inbound `traceparent` makes the local envelope a remote child).
3. Write JSON when envelope in-flight hits zero **and** any buffered span had ERROR.
4. On shutdown, dump leftover error buffers.

Successful traces are not written. Retention is by max age and max files.

## Quick start

```go
import (
    "context"

    "github.com/behaviorengineering/olly"
    "github.com/behaviorengineering/olly/dump"
)

shutdown, err := olly.Init(olly.Config{
    Enabled:      true,
    ServiceName:  "my-service",
    OTLPEndpoint: "localhost:4317", // empty = dump-only / no OTLP
    Dump: dump.Config{
        Dir:         "logs/failures",
        MaxAgeHours: 48,
        MaxFiles:    20,
    },
})
if err != nil {
    return err
}
defer shutdown(context.Background())
```

## Releases (for agents)

Default bump on each releasable merge to `main` is **patch** (`vX.Y.(Z+1)`). Skip docs/chore/ci-only ranges and `[skip release]`. Consumers pin both the git checkout and `go.mod` to the same `v*` tag.
