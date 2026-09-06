// Package dump buffers OpenTelemetry spans and writes JSON when a process
// envelope ends with an ERROR somewhere in the trace.
package dump

import (
	"fmt"
	"os"

	"go.opentelemetry.io/otel/trace"
)

// DefaultEnvelopeKind is SpanKindServer: the process envelope for gateway and CLI.
const DefaultEnvelopeKind = trace.SpanKindServer

// Config controls the failure-dump SpanProcessor.
type Config struct {
	// Dir is the output directory. Empty disables the processor when used via olly.Init.
	Dir string
	// MaxAgeHours prunes dumps older than this. Zero skips age pruning.
	MaxAgeHours int
	// MaxFiles keeps the newest N JSON dumps. Zero skips count pruning.
	MaxFiles int
	// EnvelopeKind counts as the process envelope. Zero means SpanKindServer.
	EnvelopeKind trace.SpanKind
	// RedactAttribute transforms attribute values before write. Nil keeps values as-is.
	RedactAttribute func(key string, value any) any
	// RedactStatusText transforms status descriptions. Nil keeps text as-is.
	RedactStatusText func(text string) string
	// Diagnostics receives write/prune messages. Nil uses stderr.
	Diagnostics Diagnostics
}

// Defaults fills EnvelopeKind.
func (c Config) Defaults() Config {
	out := c
	if out.EnvelopeKind == 0 {
		out.EnvelopeKind = DefaultEnvelopeKind
	}
	return out
}

// Diagnostics receives non-fatal dump messages.
type Diagnostics interface {
	Printf(format string, args ...any)
}

// StderrDiagnostics writes to os.Stderr.
type StderrDiagnostics struct{}

// Printf implements Diagnostics.
func (StderrDiagnostics) Printf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
}

func diag(d Diagnostics) Diagnostics {
	if d == nil {
		return StderrDiagnostics{}
	}
	return d
}
