.PHONY: help test vet fmt fmt-check tidy build hooks-install

.DEFAULT_GOAL := help

help:
	@echo "olly — portable OpenTelemetry helper"
	@echo ""
	@echo "  make test       go test -race ./..."
	@echo "  make vet        go vet ./..."
	@echo "  make fmt        gofmt -w ."
	@echo "  make fmt-check  fail if gofmt needed"
	@echo "  make tidy       go mod tidy"
	@echo "  make build      go build ./..."
	@echo "  make hooks-install  Install Lefthook git hooks (once per clone)"

test:
	go test -race -count=1 ./...

vet:
	go vet ./...

fmt:
	gofmt -w .

fmt-check:
	@unformatted=$$(gofmt -l .); \
	if [ -n "$$unformatted" ]; then \
		echo "gofmt needed:"; \
		echo "$$unformatted"; \
		exit 1; \
	fi

tidy:
	go mod tidy

build:
	go build ./...

hooks-install:
	@command -v lefthook >/dev/null 2>&1 || { \
		if command -v brew >/dev/null 2>&1; then brew install lefthook; \
		else go install github.com/evilmartians/lefthook@latest; fi; }
	@command -v lefthook >/dev/null 2>&1 || { echo "lefthook not on PATH; add $$(go env GOPATH)/bin"; exit 1; }
	lefthook install
