# BOOTSTRAP — olly ai-copilots

**Module path:** `github.com/behaviorengineering/olly`

```bash
MOD="$(go list -m -f '{{.Dir}}' github.com/behaviorengineering/olly)"
mkdir -p .cursor/skills
ln -snf "$MOD/ai-copilots/skills/olly-ops" .cursor/skills/olly-ops
test -f .cursor/skills/olly-ops/SKILL.md
```
