// Copyright 2020 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package jsonrpc2

import (
	"context"
	"math"
	"net"
	"os"
	"time"
)

// NOTE: This file provides an experimental API for serving multiple remote
// jsonrpc2 clients over the network. For now, it is intentionally similar to
// net/http, but that may change in the future as we figure out the correct
// semantics.

// A StreamServer is used to serve incoming jsonrpc2 clients communicating over
// a newly created connection.
type StreamServer interface {
	ServeStream(context.Context, Conn) error
}

// ListenAndServe starts an jsonrpc2 server on the given address.  If
// idleTimeout is non-zero, ListenAndServe exits after there are no clients for
// this duration, otherwise it exits only on error.
func ListenAndServe(ctx context.Context, network, addr string, server StreamServer, idleTimeout time.Duration) error {
	ln, err := net.Listen(network, addr)
	if err != nil {
		return err
	}
	defer ln.Close()
	if network == "unix" {
		defer os.Remove(addr)
	}
	return Serve(ctx, ln, server, idleTimeout)
}

// Serve accepts incoming connections from the network, and handles them using
// the provided server. If idleTimeout is non-zero, ListenAndServe exits after
// there are no clients for this duration, otherwise it exits only on error.
func Serve(ctx context.Context, ln net.Listener, server StreamServer, idleTimeout time.Duration) error {
	newConns := make(chan net.Conn)
	closedConns := make(chan error)
	activeConns := 0
	var acceptErr error
	go func() {
		defer close(newConns)
		for {
			var nc net.Conn
			nc, acceptErr = ln.Accept()
			if acceptErr != nil {
				return
			}
			newConns <- nc
		}
	}()

	ctx, cancel := context.WithCancel(ctx)
	defer func() {
		// Signal the Accept goroutine to stop immediately
		// and terminate all newly-accepted connections until it returns.
		ln.Close()
		for nc := range newConns {
			nc.Close()
		}
		// Cancel pending ServeStream callbacks and wait for them to finish.
		cancel()
		for activeConns > 0 {
			<-closedConns
			activeConns--
		}
	}()

	// Max duration: ~290 years; surely that's long enough.
	const forever = math.MaxInt64
	if idleTimeout <= 0 {
		idleTimeout = forever
	}
	connTimer := time.NewTimer(idleTimeout)
	defer connTimer.Stop()

	for {
		select {
		case netConn, ok := <-newConns:
			if !ok {
				return acceptErr
			}
			if activeConns == 0 {
				connTimer.Stop()
			}
			activeConns++
			stream := NewHeaderStream(netConn)
			go func() {
				conn := NewConn(stream)
				err := server.ServeStream(ctx, conn)
				stream.Close()
				closedConns <- err
			}()

		case <-closedConns:
			activeConns--
			if activeConns == 0 {
				connTimer.Reset(idleTimeout)
			}

		case <-connTimer.C:
			return ErrIdleTimeout

		case <-ctx.Done():
			return nil
		}
	}
}
