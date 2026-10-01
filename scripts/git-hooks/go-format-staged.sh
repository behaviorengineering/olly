#!/usr/bin/env bash
set -euo pipefail

if [[ $# -eq 0 ]]; then
  exit 0
fi

formatter=""
if command -v gofumpt >/dev/null 2>&1; then
  formatter="gofumpt"
elif command -v gofmt >/dev/null 2>&1; then
  formatter="gofmt"
else
  echo "lefthook: gofumpt or gofmt required to format staged Go files"
  exit 1
fi

for f in "$@"; do
  if [[ ! -f "$f" ]]; then
    continue
  fi
  if [[ "$formatter" == "gofumpt" ]]; then
    gofumpt -w "$f"
  else
    gofmt -w "$f"
  fi
done

exit 0
