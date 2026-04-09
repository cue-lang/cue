// Copyright 2020 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package lsprpc implements a jsonrpc2.StreamServer that may be used to
// serve the LSP on a jsonrpc2 channel.
package lsprpc

import (
	"context"
	"log"

	"cuelang.org/go/internal/golangorgx/gopls/protocol"
	"cuelang.org/go/internal/golangorgx/gopls/settings"
	"cuelang.org/go/internal/golangorgx/tools/jsonrpc2"
	"cuelang.org/go/internal/lsp/cache"
	"cuelang.org/go/internal/lsp/server"
)

// The streamServer type is a jsonrpc2.StreamServer that handles incoming
// streams as a new LSP session, using a shared cache.
type streamServer struct {
	cache *cache.Cache
	// daemon controls whether or not to log new connections.
	daemon bool

	// optionsOverrides is passed to newly created workspaces.
	optionsOverrides func(*settings.Options)
}

// NewStreamServer creates a StreamServer using the shared cache.
func NewStreamServer(cache *cache.Cache, daemon bool, optionsFunc func(*settings.Options)) jsonrpc2.StreamServer {
	return &streamServer{cache: cache, daemon: daemon, optionsOverrides: optionsFunc}
}

// ServeStream implements the jsonrpc2.StreamServer interface, by handling
// incoming streams using a new lsp server.
func (s *streamServer) ServeStream(ctx context.Context, conn jsonrpc2.Conn) error {
	client := protocol.ClientDispatcher(conn)
	options := settings.DefaultOptions(s.optionsOverrides)
	svr := server.New(s.cache, client, options)
	svrID := svr.ID()

	handlers, enqueue := protocol.HandlersWithEnqueue(
		protocol.ServerHandler(svr, jsonrpc2.MethodNotFound))
	svr.SetEnqueuer(enqueue.Enqueue)

	// Clients may or may not send a shutdown message. Make sure the
	// server is shut down.
	defer enqueue.Enqueue(func() { svr.Shutdown(ctx) })

	ctx = protocol.WithClient(ctx, client)
	conn.Go(ctx, handlers)
	if s.daemon {
		log.Printf("Server %s: connected", svrID)
		defer log.Printf("Server %s: exited", svrID)
	}
	<-conn.Done()
	return conn.Err()
}
