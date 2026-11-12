// Copyright 2014 Manu Martinez-Almeida.  All rights reserved.
// Use of this source code is governed by a MIT style
// license that can be found in the LICENSE file.

package common

import (
	"io"
	"net/http"
	"strings"
)

// Server-Sent Events
// W3C Working Draft 29 October 2009
// http://www.w3.org/TR/2009/WD-eventsource-20091029/

var writeContentType = []string{"text/event-stream"}
var noCache = []string{"no-cache"}

// CustomEvent renders one server-sent event. Data is the pre-framed event
// body (for example "data: {...}"); the blank-line event terminator is
// appended only when Data starts with "data". Event, Id, and Retry are kept
// for API compatibility; current callers always emit fully framed payloads.
// CustomEvent does not synchronize writes to the response writer. Streaming
// callers must serialize event writes at the stream level.
type CustomEvent struct {
	Event string
	Id    string
	Retry uint
	Data  any
}

func (r CustomEvent) Render(w http.ResponseWriter) error {
	r.WriteContentType(w)
	text := r.Data.(string)
	remaining := text
	for {
		fragment, rest, found := strings.Cut(remaining, "\r")
		if fragment != "" {
			n, err := io.WriteString(w, fragment)
			if err != nil {
				return err
			}
			if n != len(fragment) {
				return io.ErrShortWrite
			}
		}
		if !found {
			break
		}
		n, err := io.WriteString(w, "\\r")
		if err != nil {
			return err
		}
		if n != len("\\r") {
			return io.ErrShortWrite
		}
		remaining = rest
	}
	if strings.HasPrefix(text, "data") {
		n, err := io.WriteString(w, "\n\n")
		if err != nil {
			return err
		}
		if n != len("\n\n") {
			return io.ErrShortWrite
		}
	}
	return nil
}

func (r CustomEvent) WriteContentType(w http.ResponseWriter) {
	header := w.Header()
	header["Content-Type"] = writeContentType

	if _, exist := header["Cache-Control"]; !exist {
		header["Cache-Control"] = noCache
	}
}
