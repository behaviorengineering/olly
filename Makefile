.PHONY: help test vet fmt fmt-check tidy build

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
