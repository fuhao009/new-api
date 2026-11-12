package common

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCustomEventRenderWire locks the on-the-wire SSE framing of
// CustomEvent.Render: exact response bytes for Unicode payloads, CR escaping,
// LF passthrough, CR/LF and repeated-CR boundaries, empty payloads, and the
// blank-line terminator that is appended only to "data"-prefixed payloads.
func TestCustomEventRenderWire(t *testing.T) {
	tests := []struct {
		name     string
		event    CustomEvent
		wantBody string
		prefix   string
	}{
		{
			name:     "data event appends terminator",
			event:    CustomEvent{Data: "data: hello"},
			wantBody: "data: hello\n\n",
		},
		{
			name:     "unicode bytes preserved",
			event:    CustomEvent{Data: "data: 中文"},
			wantBody: "data: 中文\n\n",
		},
		{
			name:     "carriage return escaped literally",
			event:    CustomEvent{Data: "data: a\rb"},
			wantBody: "data: a\\rb\n\n",
		},
		{
			name:     "line feed unchanged",
			event:    CustomEvent{Data: "data: a\nid: 1"},
			wantBody: "data: a\nid: 1\n\n",
		},
		{
			name:     "crlf boundary keeps lf escapes cr",
			event:    CustomEvent{Data: "data: a\r\nb"},
			wantBody: "data: a\\r\nb\n\n",
		},
		{
			name:     "repeated carriage returns",
			event:    CustomEvent{Data: "data: \r\r\r"},
			wantBody: "data: \\r\\r\\r\n\n",
		},
		{
			name:     "empty data renders nothing",
			event:    CustomEvent{Data: ""},
			wantBody: "",
		},
		{
			name:     "bare data prefix gets terminator",
			event:    CustomEvent{Data: "data:"},
			wantBody: "data:\n\n",
		},
		{
			name:     "empty data field",
			event:    CustomEvent{Data: "data: "},
			wantBody: "data: \n\n",
		},
		{
			name:     "event followed by data",
			prefix:   "event: response.output_text.delta\n",
			event:    CustomEvent{Data: "data: {\"delta\":\"A\"}"},
			wantBody: "event: response.output_text.delta\ndata: {\"delta\":\"A\"}\n\n",
		},
		{
			name:     "event payload has no terminator",
			event:    CustomEvent{Data: "event: response.output_text.delta\n"},
			wantBody: "event: response.output_text.delta\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errors := make(chan error, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				if tt.prefix != "" {
					if err := (CustomEvent{Data: tt.prefix}).Render(w); err != nil {
						errors <- err
						return
					}
				}
				errors <- tt.event.Render(w)
			}))
			defer server.Close()
			response, err := server.Client().Get(server.URL)
			require.NoError(t, err)
			defer response.Body.Close()
			body, err := io.ReadAll(response.Body)
			require.NoError(t, err)
			require.NoError(t, <-errors)
			assert.Equal(t, tt.wantBody, string(body))
			assert.Equal(t, "text/event-stream", response.Header.Get("Content-Type"))
			assert.Equal(t, "no-cache", response.Header.Get("Cache-Control"))
		})
	}
}

var errWriterBroken = errors.New("writer broken")

// writeOutcome scripts one write call; negative n accepts the entire fragment.
// Short nil-error writes exercise the renderer's io.ErrShortWrite handling.
type writeOutcome struct {
	n   int
	err error
}

// renderWriterCore is a scriptable response-writer body sink. Outcomes are
// consumed in call order; once the script is exhausted, writes accept
// everything.
type renderWriterCore struct {
	outcomes []writeOutcome
	call     int
	written  strings.Builder
}

func (w *renderWriterCore) Header() http.Header { return http.Header{} }

func (w *renderWriterCore) WriteHeader(int) {}

func (w *renderWriterCore) accept(p string) (int, error) {
	w.call++
	scripted := w.call <= len(w.outcomes)
	var outcome writeOutcome
	if scripted {
		outcome = w.outcomes[w.call-1]
	}
	n := outcome.n
	if !scripted || n < 0 || n > len(p) {
		n = len(p)
	}
	w.written.WriteString(p[:n])
	if outcome.err != nil {
		return n, outcome.err
	}
	return n, nil
}

// plainResponseWriter only has Write, so io.WriteString must fall back to it.
type plainResponseWriter struct {
	renderWriterCore
}

var _ http.ResponseWriter = (*plainResponseWriter)(nil)

func (w *plainResponseWriter) Write(p []byte) (int, error) {
	return w.accept(string(p))
}

// stringResponseWriter additionally implements io.StringWriter, the direct
// no-copy path io.WriteString prefers.
type stringResponseWriter struct {
	renderWriterCore
}

var (
	_ http.ResponseWriter = (*stringResponseWriter)(nil)
	_ io.StringWriter     = (*stringResponseWriter)(nil)
)

func (w *stringResponseWriter) Write(p []byte) (int, error) {
	return w.accept(string(p))
}

func (w *stringResponseWriter) WriteString(s string) (int, error) {
	return w.accept(s)
}

// TestCustomEventRenderWriteFailures verifies that Render surfaces write
// errors and short writes immediately — including failures on the blank-line
// terminator — and that a failed payload write is never followed by the
// terminator. Every scripted case runs against a plain io.Writer and an
// io.StringWriter so both io.WriteString dispatch targets are exercised.
func TestCustomEventRenderWriteFailures(t *testing.T) {
	body := "data: chunk"
	tests := []struct {
		name     string
		body     string
		outcomes []writeOutcome
		wantErr  error
		wantBody string
	}{
		{
			name:     "payload immediate failure",
			outcomes: []writeOutcome{{n: 0, err: errWriterBroken}},
			wantErr:  errWriterBroken,
		},
		{
			name:     "payload short write",
			outcomes: []writeOutcome{{n: 5}},
			wantErr:  io.ErrShortWrite,
			wantBody: "data:",
		},
		{
			name:     "payload full write with error skips terminator",
			outcomes: []writeOutcome{{n: len(body), err: errWriterBroken}},
			wantErr:  errWriterBroken,
			wantBody: body,
		},
		{
			name:     "terminator short write",
			outcomes: []writeOutcome{{n: -1}, {n: 1}},
			wantErr:  io.ErrShortWrite,
			wantBody: body + "\n",
		},
		{
			name:     "terminator full write with error",
			outcomes: []writeOutcome{{n: -1}, {n: 2, err: errWriterBroken}},
			wantErr:  errWriterBroken,
			wantBody: body + "\n\n",
		},
		{
			name:     "terminator immediate failure",
			outcomes: []writeOutcome{{n: -1}, {n: 0, err: errWriterBroken}},
			wantErr:  errWriterBroken,
			wantBody: body,
		},
		{
			name:     "carriage return escape short write",
			body:     "data: a\rb",
			outcomes: []writeOutcome{{n: -1}, {n: 1}},
			wantErr:  io.ErrShortWrite,
			wantBody: "data: a\\",
		},
		{
			name:     "carriage return escape error",
			body:     "data: a\rb",
			outcomes: []writeOutcome{{n: -1}, {n: 0, err: errWriterBroken}},
			wantErr:  errWriterBroken,
			wantBody: "data: a",
		},
	}
	for _, tt := range tests {
		for _, kind := range []string{"plain", "string"} {
			t.Run(tt.name+"/"+kind, func(t *testing.T) {
				payload := tt.body
				if payload == "" {
					payload = body
				}
				var renderErr error
				var bodyWritten string
				if kind == "plain" {
					w := &plainResponseWriter{renderWriterCore: renderWriterCore{outcomes: tt.outcomes}}
					renderErr = (CustomEvent{Data: payload}).Render(w)
					bodyWritten = w.written.String()
				} else {
					w := &stringResponseWriter{renderWriterCore: renderWriterCore{outcomes: tt.outcomes}}
					renderErr = (CustomEvent{Data: payload}).Render(w)
					bodyWritten = w.written.String()
				}
				require.ErrorIs(t, renderErr, tt.wantErr)
				assert.Equal(t, tt.wantBody, bodyWritten)
			})
		}
	}
}
