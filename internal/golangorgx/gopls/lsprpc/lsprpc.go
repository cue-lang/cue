// Copyright 2020 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package lsprpc implements a jsonrpc2.StreamServer that may be used to
// serve the LSP on a jsonrpc2 channel.
package lsprpc

import (
	"context"
	"fmt"
	"log"
	"strings"

	"cuelang.org/go/internal/golangorgx/gopls/protocol"
	"cuelang.org/go/internal/golangorgx/gopls/settings"
	"cuelang.org/go/internal/golangorgx/tools/jsonrpc2"
	"cuelang.org/go/internal/lsp/cache"
	"cuelang.org/go/internal/lsp/server"
)

// The streamServer type is a jsonrpc2.streamServer that handles incoming
// streams as a new LSP session, using a shared cache.
type streamServer struct {
	cache *cache.Cache
	// daemon controls whether or not to log new connections.
	daemon bool
}

// NewStreamServer creates a StreamServer using the shared cache.
func NewStreamServer(cache *cache.Cache, daemon bool) jsonrpc2.StreamServer {
	return &streamServer{cache: cache, daemon: daemon}
}

// ServeStream implements the jsonrpc2.StreamServer interface, by handling
// incoming streams using a new lsp server.
func (s *streamServer) ServeStream(ctx context.Context, conn jsonrpc2.Conn) error {
	client := protocol.ClientDispatcher(conn)
	options := settings.DefaultOptions()
	svr := server.New(s.cache, client, options)
	svrID := svr.ID()

	handlers, enqueue := protocol.HandlersWithEnqueue(
		svr.Handler(jsonrpc2.MethodNotFound))
	svr.SetEnqueuer(enqueue.Enqueue)

	// Clients may or may not send a shutdown message. Make sure the
	// server is shut down.
	defer enqueue.Enqueue(func() { svr.Shutdown(ctx) })

	conn.Go(ctx, handlers)
	if s.daemon {
		log.Printf("Server %s: connected", svrID)
		defer log.Printf("Server %s: exited", svrID)
	}
	<-conn.Done()
	return conn.Err()
}

// A forwarder is a jsonrpc2.StreamServer that handles an LSP stream
// by forwarding it to a remote. This is used when the cuelsp process
// started by the editor is in the `-remote` mode, which means it
// finds and connects to a separate cuelsp daemon.
type forwarder struct {
	dialer *autoDialer
}

// NewForwarder creates a new forwarder (a [jsonrpc2.StreamServer]),
// ready to forward connections to the
// remote server specified by rawAddr. If provided and rawAddr indicates an
// 'automatic' address (starting with 'auto;'), argFunc may be used to start a
// remote server for the auto-discovered address.
func NewForwarder(rawAddr string, argFunc func(network, address string) []string) (jsonrpc2.StreamServer, error) {
	dialer, err := newAutoDialer(rawAddr, argFunc)
	if err != nil {
		return nil, err
	}
	fwd := &forwarder{
		dialer: dialer,
	}
	return fwd, nil
}

// ServeStream dials the forwarder remote and binds the remote to serve the LSP
// on the incoming stream.
func (f *forwarder) ServeStream(ctx context.Context, clientConn jsonrpc2.Conn) error {
	client := protocol.ClientDispatcher(clientConn)

	netConn, err := f.dialer.dialNet(ctx)
	if err != nil {
		return fmt.Errorf("forwarder: connecting to remote: %w", err)
	}
	serverConn := jsonrpc2.NewConn(jsonrpc2.NewHeaderStream(netConn))
	server := protocol.ServerDispatcher(serverConn)

	// Forward between connections.
	serverConn.Go(ctx,
		protocol.Handlers(
			protocol.ClientHandler(client,
				jsonrpc2.MethodNotFound)))
	clientConn.Go(ctx,
		protocol.Handlers(
			protocol.ServerHandler(server, jsonrpc2.MethodNotFound)))

	select {
	case <-serverConn.Done():
		clientConn.Close()
	case <-clientConn.Done():
		serverConn.Close()
	}

	err = nil
	if serverConn.Err() != nil {
		err = fmt.Errorf("remote disconnected: %v", serverConn.Err())
	} else if clientConn.Err() != nil {
		err = fmt.Errorf("client disconnected: %v", clientConn.Err())
	}
	return err
}

// ParseAddr parses the address of a cuelsp remote.
// TODO(rFindley): further document this syntax, and allow URI-style remote
// addresses such as "auto://...".
func ParseAddr(listen string) (network string, address string) {
	// Allow passing just -remote=auto, as a shorthand for using automatic remote
	// resolution.
	if listen == autoNetwork {
		return autoNetwork, ""
	}
	if parts := strings.SplitN(listen, ";", 2); len(parts) == 2 {
		return parts[0], parts[1]
	}
	return "tcp", listen
}
