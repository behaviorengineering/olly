# olly

Agents: start at [AGENTS.md](AGENTS.md). Skills: [ai-copilots/](ai-copilots/).

Portable OpenTelemetry helper for Behavior Engineering services.

Module: `github.com/behaviorengineering/olly`.

Apps map their own config and logger at the boundary, then call `olly.Init` once at process start. olly installs the global tracer provider, optional OTLP export, and an optional local failure-trace dump processor.

## What olly owns

| Package | Role |
|---------|------|
| `olly` | Provider lifecycle (`Init`, `Shutdown`, `Flush`), sampling, W3C propagation, env-aware OTLP (gRPC/HTTP) |
| `olly/dump` | Buffer spans by `trace_id`; write JSON when SERVER envelopes end with ERROR |
| `olly/errors` | Annotate spans from Go errors; expose trace/span IDs for logs |
| `olly/http` | `WrapHandler`, `Middleware`, `WrapTransport`, `Client` (otelhttp) |
| `olly/cli` | `AddFlags`, `ConfigFromEnv`, `Run`, `Lifecycle` (stdlib `flag` only) |

## What stays in the app

- Service names, operation taxonomies, product-specific span attributes
- Redaction of product-specific attributes (pass dump hooks)
- Business metrics and logger implementations
- Pipeline execution records (`strop/runreport`)

## Environment (OTLP)

When Config fields are empty, `Init` / `Resolve` read the OpenTelemetry env contract:

- `OTEL_SERVICE_NAME`
- `OTEL_RESOURCE_ATTRIBUTES` (comma-separated `k=v`)
- `OTEL_EXPORTER_OTLP_TRACES_ENDPOINT` (wins) / `OTEL_EXPORTER_OTLP_ENDPOINT`
- `OTEL_EXPORTER_OTLP_TRACES_PROTOCOL` (wins) / `OTEL_EXPORTER_OTLP_PROTOCOL` (`grpc` or `http/protobuf`)
- `OTEL_EXPORTER_OTLP_TRACES_HEADERS` (wins) / `OTEL_EXPORTER_OTLP_HEADERS` (comma-separated `k=v`)

Empty `OTLPEndpoint` after env resolution still means dump-only (no OTLP). Use constant `DefaultOTLPEndpoint` (`localhost:4319`, Polypus HyperDX gRPC) from apps or `cli.ConfigFromEnv`.

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
    OTLPEndpoint: olly.DefaultOTLPEndpoint, // empty = dump-only / no OTLP
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

### HTTP (one line)

```go
import ollyhttp "github.com/behaviorengineering/olly/http"

handler = ollyhttp.WrapHandler(mux, "my-service")
client := ollyhttp.Client(http.DefaultClient, "my-service")
```

### CLI (stdlib)

```go
import ollicli "github.com/behaviorengineering/olly/cli"

cfg := ollicli.ConfigFromEnv("my-cli")
ollicli.AddFlags(flag.CommandLine, &cfg)
flag.Parse()
os.Exit(ollicli.Run(context.Background(), cfg, run))
```

### Cobra (no dependency)

```go
var otelLife *ollicli.Lifecycle

root.PersistentPreRunE = func(cmd *cobra.Command, _ []string) error {
    var err error
    otelLife, err = ollicli.Start(ollicli.ConfigFromEnv(cmd.Root().Name()))
    return err
}
root.PersistentPostRunE = func(cmd *cobra.Command, _ []string) error {
    return otelLife.Stop(cmd.Context())
}
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
