package dump

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// TraceDocument is the JSON document written for a failed trace.
type TraceDocument struct {
	TraceID           string         `json:"trace_id"`
	DumpedAt          time.Time      `json:"dumped_at"`
	Reason            string         `json:"reason"`
	RootName          string         `json:"root_name,omitempty"`
	RootStatusMessage string         `json:"root_status_message,omitempty"`
	SpanCount         int            `json:"span_count"`
	Spans             []SpanDocument `json:"spans"`
}

// SpanDocument is one span in a TraceDocument.
type SpanDocument struct {
	TraceID       string         `json:"trace_id"`
	SpanID        string         `json:"span_id"`
	ParentSpanID  string         `json:"parent_span_id,omitempty"`
	Name          string         `json:"name"`
	Kind          string         `json:"kind,omitempty"`
	StatusCode    string         `json:"status_code"`
	StatusMessage string         `json:"status_message,omitempty"`
	StartTime     time.Time      `json:"start_time"`
	EndTime       time.Time      `json:"end_time"`
	Attributes    map[string]any `json:"attributes,omitempty"`
}

type traceBuffer struct {
	spans    []SpanDocument
	hasError bool
	inFlight int
}

// Processor writes a JSON dump when process envelopes end and the buffer saw ERROR.
// Inbound spans may have a remote parent (client traceparent), so we do not wait for IsRoot().
type Processor struct {
	cfg    Config
	mu     sync.Mutex
	traces map[string]*traceBuffer
}

// NewProcessor builds a SpanProcessor. cfg.Dir must be non-empty for writes.
func NewProcessor(cfg Config) *Processor {
	cfg = cfg.Defaults()
	return &Processor{
		cfg:    cfg,
		traces: make(map[string]*traceBuffer),
	}
}

// OnStart increments the envelope in-flight counter for SERVER (or configured) spans.
func (p *Processor) OnStart(_ context.Context, s sdktrace.ReadWriteSpan) {
	if p == nil || s == nil || s.SpanKind() != p.cfg.EnvelopeKind {
		return
	}
	tid := s.SpanContext().TraceID().String()
	if tid == "" {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	buf := p.traces[tid]
	if buf == nil {
		buf = &traceBuffer{}
		p.traces[tid] = buf
	}
	buf.inFlight++
}

// OnEnd records the span and dumps when envelopes complete with errors.
func (p *Processor) OnEnd(s sdktrace.ReadOnlySpan) {
	if p == nil || s == nil {
		return
	}
	snap := p.snapshot(s)
	if snap.TraceID == "" {
		return
	}
	isError := s.Status().Code == codes.Error
	isEnvelope := s.SpanKind() == p.cfg.EnvelopeKind

	p.mu.Lock()
	defer p.mu.Unlock()
	buf := p.traces[snap.TraceID]
	if buf == nil {
		buf = &traceBuffer{}
		p.traces[snap.TraceID] = buf
	}
	buf.spans = append(buf.spans, snap)
	if isError {
		buf.hasError = true
	}
	if isEnvelope {
		buf.inFlight--
		if buf.inFlight < 0 {
			buf.inFlight = 0
		}
	}
	if buf.inFlight > 0 {
		return
	}
	delete(p.traces, snap.TraceID)
	if !buf.hasError {
		return
	}
	p.writeLocked(TraceDocument{
		TraceID:           snap.TraceID,
		DumpedAt:          time.Now().UTC(),
		Reason:            "envelope_error",
		RootName:          snap.Name,
		RootStatusMessage: snap.StatusMessage,
		Spans:             buf.spans,
	})
}

// Shutdown dumps leftover error buffers.
func (p *Processor) Shutdown(context.Context) error {
	if p == nil {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	for tid, buf := range p.traces {
		if buf != nil && buf.hasError {
			p.writeLocked(TraceDocument{
				TraceID:  tid,
				DumpedAt: time.Now().UTC(),
				Reason:   "shutdown_with_error_spans",
				Spans:    buf.spans,
			})
		}
		delete(p.traces, tid)
	}
	return nil
}

// ForceFlush is a no-op; dumps happen on envelope completion.
func (p *Processor) ForceFlush(context.Context) error {
	return nil
}

func (p *Processor) writeLocked(doc TraceDocument) {
	if p.cfg.Dir == "" {
		return
	}
	sort.Slice(doc.Spans, func(i, j int) bool {
		return doc.Spans[i].StartTime.Before(doc.Spans[j].StartTime)
	})
	doc.SpanCount = len(doc.Spans)
	d := diag(p.cfg.Diagnostics)
	if err := os.MkdirAll(p.cfg.Dir, 0o750); err != nil {
		d.Printf("olly dump: mkdir: %v", err)
		return
	}
	path := filepath.Join(p.cfg.Dir, doc.TraceID+".json")
	tmp := path + ".tmp"
	body, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		d.Printf("olly dump: encode: %v", err)
		return
	}
	if err := os.WriteFile(tmp, body, 0o640); err != nil {
		d.Printf("olly dump: write: %v", err)
		return
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		d.Printf("olly dump: rename: %v", err)
		return
	}
	d.Printf("olly dump: failure dump written trace_id=%s path=%s span_count=%d",
		doc.TraceID, path, doc.SpanCount)
	if err := Prune(p.cfg.Dir, p.cfg.MaxAgeHours, p.cfg.MaxFiles); err != nil {
		d.Printf("olly dump: prune: %v", err)
	}
}

func (p *Processor) snapshot(s sdktrace.ReadOnlySpan) SpanDocument {
	sc := s.SpanContext()
	parentID := ""
	if s.Parent().IsValid() {
		parentID = s.Parent().SpanID().String()
	}
	status := s.Status()
	msg := status.Description
	if p.cfg.RedactStatusText != nil {
		msg = p.cfg.RedactStatusText(msg)
	}
	return SpanDocument{
		TraceID:       sc.TraceID().String(),
		SpanID:        sc.SpanID().String(),
		ParentSpanID:  parentID,
		Name:          s.Name(),
		Kind:          s.SpanKind().String(),
		StatusCode:    statusCodeString(status.Code),
		StatusMessage: msg,
		StartTime:     s.StartTime().UTC(),
		EndTime:       s.EndTime().UTC(),
		Attributes:    p.attributesToMap(s.Attributes()),
	}
}

func (p *Processor) attributesToMap(attrs []attribute.KeyValue) map[string]any {
	if len(attrs) == 0 {
		return nil
	}
	out := make(map[string]any, len(attrs))
	for _, attr := range attrs {
		key := string(attr.Key)
		value := attr.Value.AsInterface()
		if p.cfg.RedactAttribute != nil {
			value = p.cfg.RedactAttribute(key, value)
		}
		out[key] = value
	}
	return out
}

func statusCodeString(code codes.Code) string {
	switch code {
	case codes.Ok:
		return "OK"
	case codes.Error:
		return "ERROR"
	default:
		return "UNSET"
	}
}

// Prune removes old or excess dump JSON files.
func Prune(dir string, maxAgeHours, maxFiles int) error {
	if maxAgeHours <= 0 && maxFiles <= 0 {
		return nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	type dumpFile struct {
		path    string
		modTime time.Time
	}
	var kept []dumpFile
	now := time.Now()
	maxAge := time.Duration(maxAgeHours) * time.Hour
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(strings.ToLower(entry.Name()), ".json") {
			continue
		}
		info, infoErr := entry.Info()
		if infoErr != nil {
			return infoErr
		}
		path := filepath.Join(dir, entry.Name())
		if maxAgeHours > 0 && now.Sub(info.ModTime()) > maxAge {
			if removeErr := os.Remove(path); removeErr != nil && !os.IsNotExist(removeErr) {
				return removeErr
			}
			continue
		}
		kept = append(kept, dumpFile{path: path, modTime: info.ModTime()})
	}
	if maxFiles <= 0 || len(kept) <= maxFiles {
		return nil
	}
	sort.Slice(kept, func(i, j int) bool {
		return kept[i].modTime.After(kept[j].modTime)
	})
	for _, file := range kept[maxFiles:] {
		if removeErr := os.Remove(file.path); removeErr != nil && !os.IsNotExist(removeErr) {
			return removeErr
		}
	}
	return nil
}

var _ sdktrace.SpanProcessor = (*Processor)(nil)
