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

olly is a Go library. Releases are **source tags** (`v*`) plus a GitHub Release from GoReleaser (`builds.skip: true`).

**CI quality:** workflow `ci.yml` runs `go mod tidy` check, `gofmt`, `go vet`, `go test -race`, and `go build` on pushes and pull requests.

**Auto patch on `main`:** every push to `main` that is not docs/chore/ci-only creates `vX.Y.(Z+1)` and publishes a release (workflow `auto-patch-release.yml`). Put `[skip release]` in the commit subject to opt out once.

**Skip (no tag):** when every commit subject since the last `v*` tag is only `docs:`, `chore:`, or `ci:` (conventional prefixes).

**Manual minor/major:** run workflow **Auto patch release** with `bump=minor` or `bump=major` (or push a `v*` tag yourself). Use major only for breaking public API changes.

**After a new tag, consumer agents MUST pin:**

```bash
git -C providers/olly fetch --tags origin
git -C providers/olly checkout "vX.Y.Z"
go get github.com/behaviorengineering/olly@vX.Y.Z
go mod tidy
```
