// Copyright 2019 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package integration

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"sync"
	"time"

	"cuelang.org/go/internal/golangorgx/tools/jsonrpc2"
)

// loggingFramer records the LSP messages of the first stream it frames,
// which are only formatted when the log is printed.
type loggingFramer struct {
	mu     sync.Mutex
	framed bool
	msgs   []loggedMsg
}

type loggedMsg struct {
	msg    jsonrpc2.Message
	isRead bool
	time   time.Time
}

func (s *loggingFramer) framer(f jsonrpc2.Framer) jsonrpc2.Framer {
	return func(nc net.Conn) jsonrpc2.Stream {
		s.mu.Lock()
		first := !s.framed
		s.framed = true
		s.mu.Unlock()
		stream := f(nc)
		if !first {
			return stream
		}
		return &loggingStream{stream: stream, log: s}
	}
}

func (s *loggingFramer) add(msg jsonrpc2.Message, isRead bool) {
	if msg == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.msgs = append(s.msgs, loggedMsg{msg: msg, isRead: isRead, time: time.Now()})
}

const eor = "\r\n\r\n\r\n"

func (s *loggingFramer) printBuffers(testname string, w io.Writer) {
	s.mu.Lock()
	framed, msgs := s.framed, s.msgs
	s.mu.Unlock()
	if !framed {
		return
	}

	// Calls by direction and ID, to report how long their responses took.
	type callKey struct {
		isRead bool
		id     string
	}
	type call struct {
		method string
		start  time.Time
	}
	calls := make(map[callKey]call)

	fmt.Fprintf(os.Stderr, "#### Start Gopls Test Logs for %q\n", testname)
	bw := bufio.NewWriter(w)
	for _, m := range msgs {
		direction, pastTense := "Received", "Received"
		if m.isRead {
			direction, pastTense = "Sending", "Sent"
		}
		tmfmt := m.time.Format("15:04:05.000 PM")

		switch msg := m.msg.(type) {
		case *jsonrpc2.Call:
			id := fmt.Sprint(msg.ID())
			fmt.Fprintf(bw, "[Trace - %s] %s request '%s - (%s)'.\nParams: %s%s",
				tmfmt, direction, msg.Method(), id, msg.Params(), eor)
			calls[callKey{m.isRead, id}] = call{method: msg.Method(), start: m.time}
		case *jsonrpc2.Notification:
			fmt.Fprintf(bw, "[Trace - %s] %s notification '%s'.\nParams: %s%s",
				tmfmt, direction, msg.Method(), msg.Params(), eor)
		case *jsonrpc2.Response:
			id := fmt.Sprint(msg.ID())
			if err := msg.Err(); err != nil {
				fmt.Fprintf(bw, "[Error - %s] %s #%s %s%s", pastTense, tmfmt, id, err, eor)
				continue
			}
			key := callKey{!m.isRead, id}
			c := calls[key]
			delete(calls, key)
			elapsed := m.time.Sub(c.start)
			fmt.Fprintf(bw, "[Trace - %s] %s response '%s - (%s)' in %dms.\nResult: %s%s",
				tmfmt, direction, c.method, id, elapsed/time.Millisecond, msg.Result(), eor)
		}
	}
	bw.Flush()
	fmt.Fprintf(os.Stderr, "#### End Gopls Test Logs for %q\n", testname)
}

type loggingStream struct {
	stream jsonrpc2.Stream
	log    *loggingFramer
}

func (s *loggingStream) Read(ctx context.Context) (jsonrpc2.Message, int64, error) {
	msg, count, err := s.stream.Read(ctx)
	if err == nil {
		s.log.add(msg, true)
	}
	return msg, count, err
}

func (s *loggingStream) Write(ctx context.Context, msg jsonrpc2.Message) (int64, error) {
	s.log.add(msg, false)
	return s.stream.Write(ctx, msg)
}

func (s *loggingStream) Close() error {
	return s.stream.Close()
}
