package common

import (
	"context"
	"crypto/tls"
	"net/http"
	"net/http/httptrace"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"
)

const relayTraceEventLimit = 96

type relayTraceContextKey struct{}

// RelayTraceEvent is one bounded, elapsed-time event in a relay trace.
type RelayTraceEvent struct {
	Stage     string `json:"stage"`
	ElapsedMS int64  `json:"elapsed_ms"`
	Attempt   int    `json:"attempt,omitempty"`
	Detail    string `json:"detail,omitempty"`
}

// RelayTraceSnapshot is an immutable copy of the events recorded for a request.
type RelayTraceSnapshot struct {
	Events    []RelayTraceEvent `json:"events"`
	Truncated bool              `json:"truncated"`
}

// RelayTrace records bounded timing events for one incoming /v1 request.
type RelayTrace struct {
	startedAt time.Time

	mu        sync.Mutex
	events    []RelayTraceEvent
	truncated bool
}

// StartRelayTrace initializes tracing only for /v1 requests when explicitly enabled.
func StartRelayTrace(c *gin.Context) {
	if !RelayTraceEnabled || c == nil || c.Request == nil || c.Request.URL == nil {
		return
	}
	path := c.Request.URL.Path
	if path != "/v1" && !strings.HasPrefix(path, "/v1/") {
		return
	}
	trace := &RelayTrace{
		startedAt: time.Now(),
		events:    make([]RelayTraceEvent, 0, relayTraceEventLimit),
	}
	ctx := context.WithValue(c.Request.Context(), relayTraceContextKey{}, trace)
	c.Request = c.Request.WithContext(ctx)
}

// GetRelayTrace returns the trace initialized for c, or nil when tracing is disabled.
func GetRelayTrace(c *gin.Context) *RelayTrace {
	if c == nil || c.Request == nil {
		return nil
	}
	trace, _ := c.Request.Context().Value(relayTraceContextKey{}).(*RelayTrace)
	return trace
}

// Mark records a timing event without attaching caller-provided details.
func (t *RelayTrace) Mark(stage string) {
	t.mark(stage, 0, "")
}

// MarkFirst records a stage only once and reports whether it was added.
func (t *RelayTrace) MarkFirst(stage string) bool {
	if t == nil || !validRelayTraceStage(stage) {
		return false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, event := range t.events {
		if event.Stage == stage {
			return false
		}
	}
	return t.appendLocked(stage, 0, "")
}

// MarkAttempt records a transport event associated with an attempt number.
func (t *RelayTrace) MarkAttempt(stage string, attempt int) {
	t.mark(stage, attempt, "")
}

// MarkDetail records a stage with a safe protocol, event type, reuse state, or error boolean.
func (t *RelayTrace) MarkDetail(stage, detail string) {
	t.mark(stage, 0, safeRelayTraceDetail(detail))
}

// MarkAttemptDetail records a transport event with an attempt and a safe detail value.
func (t *RelayTrace) MarkAttemptDetail(stage string, attempt int, detail string) {
	t.mark(stage, attempt, safeRelayTraceDetail(detail))
}

func (t *RelayTrace) mark(stage string, attempt int, detail string) {
	if t == nil || !validRelayTraceStage(stage) {
		return
	}
	t.mu.Lock()
	t.appendLocked(stage, attempt, detail)
	t.mu.Unlock()
}

func (t *RelayTrace) appendLocked(stage string, attempt int, detail string) bool {
	if len(t.events) >= relayTraceEventLimit {
		t.truncated = true
		return false
	}
	t.events = append(t.events, RelayTraceEvent{
		Stage: stage, ElapsedMS: time.Since(t.startedAt).Milliseconds(),
		Attempt: attempt, Detail: detail,
	})
	return true
}

// Snapshot returns a concurrency-safe copy of the recorded trace.
func (t *RelayTrace) Snapshot() *RelayTraceSnapshot {
	if t == nil {
		return nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return &RelayTraceSnapshot{
		Events:    append([]RelayTraceEvent(nil), t.events...),
		Truncated: t.truncated,
	}
}

// HTTPTrace adds transport timing hooks while preserving req's original context.
func (t *RelayTrace) HTTPTrace(req *http.Request) *http.Request {
	if t == nil || req == nil {
		return req
	}
	ctx := context.WithValue(req.Context(), relayTraceContextKey{}, t)
	var attempt atomic.Int64
	mark := func(stage, detail string) {
		currentAttempt := int(attempt.Load())
		if detail == "" {
			t.MarkAttempt(stage, currentAttempt)
		} else {
			t.MarkAttemptDetail(stage, currentAttempt, detail)
		}
	}
	clientTrace := &httptrace.ClientTrace{
		GetConn: func(string) {
			currentAttempt := int(attempt.Add(1))
			t.MarkAttempt("get_connection", currentAttempt)
		},
		DNSStart: func(httptrace.DNSStartInfo) { mark("dns_start", "") },
		DNSDone: func(info httptrace.DNSDoneInfo) {
			mark("dns_done", errorTraceDetail(info.Err != nil))
		},
		ConnectStart: func(string, string) { mark("connect_start", "") },
		ConnectDone: func(_, _ string, err error) {
			mark("connect_done", errorTraceDetail(err != nil))
		},
		TLSHandshakeStart: func() { mark("tls_start", "") },
		TLSHandshakeDone: func(_ tls.ConnectionState, err error) {
			mark("tls_done", errorTraceDetail(err != nil))
		},
		GotConn: func(info httptrace.GotConnInfo) {
			detail := "new"
			if info.Reused {
				detail = "reused"
			}
			mark("connection_acquired", detail)
		},
		WroteRequest: func(info httptrace.WroteRequestInfo) {
			mark("request_written", errorTraceDetail(info.Err != nil))
		},
		GotFirstResponseByte: func() { mark("upstream_first_byte", "") },
	}
	return req.WithContext(httptrace.WithClientTrace(ctx, clientTrace))
}

func validRelayTraceStage(stage string) bool {
	if stage == "" || len(stage) > 64 || stage[0] < 'a' || stage[0] > 'z' {
		return false
	}
	for i := 1; i < len(stage); i++ {
		ch := stage[i]
		if (ch < 'a' || ch > 'z') && (ch < '0' || ch > '9') && ch != '_' {
			return false
		}
	}
	return true
}

func safeRelayTraceDetail(detail string) string {
	switch detail {
	case "new", "reused", "HTTP/1.0", "HTTP/1.1", "HTTP/2.0", "HTTP/3.0", "error=true", "error=false", "codex.rate_limits":
		return detail
	}
	if !strings.HasPrefix(detail, "response.") || len(detail) > 64 || len(detail) == len("response.") || detail[len(detail)-1] == '.' {
		return ""
	}
	for i := len("response."); i < len(detail); i++ {
		ch := detail[i]
		if (ch < 'a' || ch > 'z') && (ch < '0' || ch > '9') && ch != '_' && ch != '.' {
			return ""
		}
	}
	return detail
}

func errorTraceDetail(isError bool) string {
	if isError {
		return "error=true"
	}
	return "error=false"
}
