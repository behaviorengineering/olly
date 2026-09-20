# Agents

This module is a portable OpenTelemetry helper. Humans read [README.md](README.md).

**Load:** [ai-copilots/skills/olly-ops/SKILL.md](ai-copilots/skills/olly-ops/SKILL.md)

Wire with [ai-copilots/BOOTSTRAP.md](ai-copilots/BOOTSTRAP.md).

```bash
go list -m -f '{{.Dir}}' github.com/behaviorengineering/olly
```

## Package layout

- Public API lives under `pkg/<domain>` (for example `pkg/olly`, `pkg/dump`, `pkg/errors`, `pkg/http`, `pkg/cli`).
- MUST NOT scatter loose implementation `.go` files at the module root.
- Hidden helpers (when needed) belong under `internal/<area>`.
