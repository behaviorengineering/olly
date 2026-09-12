---
name: olly-ops
description: >-
  Operate the olly OpenTelemetry helper (github.com/behaviorengineering/olly).
  Use when Init/Shutdown, failure dumps, OTLP, or pinning olly as a dependency.
---

# olly

**Module:** `github.com/behaviorengineering/olly` · **README:** [README.md](../../../README.md)

Apps map config and logger at the boundary, then call `olly.Init` once. olly owns the global tracer provider, optional OTLP export, and optional local failure-trace dumps.

## Commands

```bash
make test
make vet
make build
make fmt-check
make tidy
```

## Integration sketch

```go
shutdown, err := olly.Init(olly.Config{
    Enabled:      true,
    ServiceName:  "my-service",
    OTLPEndpoint: "localhost:4317", // empty = dump-only
    Dump: dump.Config{Dir: "logs/failures", MaxAgeHours: 48, MaxFiles: 20},
})
```

## Pin after a release tag

```bash
git -C providers/olly fetch --tags origin
git -C providers/olly checkout "vX.Y.Z"
go get github.com/behaviorengineering/olly@vX.Y.Z
go mod tidy
```

Use `[skip release]` in a commit subject to opt out of auto-patch once. Docs/chore/ci-only subjects do not bump tags.
