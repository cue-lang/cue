// Copyright 2020 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package lsprpc implements a jsonrpc2.StreamServer that may be used to
// serve the LSP on a jsonrpc2 channel.
package lsprpc

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"cuelang.org/go/internal/golangorgx/gopls/protocol"
	"cuelang.org/go/internal/golangorgx/gopls/protocol/command"
	"cuelang.org/go/internal/golangorgx/gopls/settings"
	"cuelang.org/go/internal/golangorgx/tools/event"
	"cuelang.org/go/internal/golangorgx/tools/event/tag"
	"cuelang.org/go/internal/golangorgx/tools/jsonrpc2"
	"cuelang.org/go/internal/lsp/cache"
	"cuelang.org/go/internal/lsp/server"
)

// Unique identifiers for client/server.
var serverIndex int64

// The streamServer type is a jsonrpc2.streamServer that handles incoming
// streams as a new LSP session, using a shared cache.
type streamServer struct {
	cache *cache.Cache
	// daemon controls whether or not to log new connections.
	daemon bool

	// optionsOverrides is passed to newly created workspaces.
	optionsOverrides func(*settings.Options)

	// serverForTest may be set to a test fake for testing.
	serverForTest server.ServerWithID
}

// NewStreamServer creates a StreamServer using the shared cache.
func NewStreamServer(cache *cache.Cache, daemon bool, optionsFunc func(*settings.Options)) jsonrpc2.StreamServer {
	return &streamServer{cache: cache, daemon: daemon, optionsOverrides: optionsFunc}
}

// ServeStream implements the jsonrpc2.StreamServer interface, by handling
// incoming streams using a new lsp server.
func (s *streamServer) ServeStream(ctx context.Context, conn jsonrpc2.Conn) error {
	client := protocol.ClientDispatcher(conn)
	svr := s.serverForTest
	if svr == nil {
		options := settings.DefaultOptions(s.optionsOverrides)
		svr = server.New(s.cache, client, options)
	}
	svrID := svr.ID()
	// Clients may or may not send a shutdown message. Make sure the server is
	// shut down.
	//
	// TODO(ms): temporarily disabled because it introduces a
	// data-race: this is a moment of genuine concurrency. It would be
	// much better to inject the shutdown message onto the end of the
	// jsonrpc2 stream somehow. For now, there's nothing important to
	// do for shutdown, so disabling this is fine, and it solves the
	// data-race. It's possible we could get away with moving the
	// <-conn.Done() call within the defer, prior to the Shutdown call
	// - that would provide the necessary memory barries. TBD.
	//
	// defer svr.Shutdown(ctx)
	ctx = protocol.WithClient(ctx, client)
	conn.Go(ctx,
		protocol.Handlers(
			handshaker(svrID, s.daemon,
				protocol.ServerHandler(svr, jsonrpc2.MethodNotFound))))
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
// finds and connects to a separate cuelsp daemon. In these cases, we
// still want the forwarder cuelsp to in some cases hijack the
// jsonrpc2 connection with the daemon.
type forwarder struct {
	dialer *autoDialer

	mu sync.Mutex
	// Hold on to the server connection so that we can redo the handshake if any
	// information changes.
	serverConn jsonrpc2.Conn
	serverID   string
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

// QueryServerState returns a JSON-encodable struct describing the state of the named server.
func QueryServerState(ctx context.Context, addr string) (any, error) {
	serverConn, err := dialRemote(ctx, addr)
	if err != nil {
		return nil, err
	}
	var state serverState
	if err := protocol.Call(ctx, serverConn, serversMethod, nil, &state); err != nil {
		return nil, fmt.Errorf("querying server state: %w", err)
	}
	return &state, nil
}

// dialRemote is used for making calls into the cuelsp daemon. addr should be a
// URL, possibly on the synthetic 'auto' network (e.g. tcp://..., unix://...,
// or auto://...).
func dialRemote(ctx context.Context, addr string) (jsonrpc2.Conn, error) {
	network, address := ParseAddr(addr)
	if network == autoNetwork {
		cuePath, err := os.Executable()
		if err != nil {
			return nil, fmt.Errorf("getting cue path: %w", err)
		}
		network, address = autoNetworkAddress(cuePath, address)
	}
	netConn, err := net.DialTimeout(network, address, 5*time.Second)
	if err != nil {
		return nil, fmt.Errorf("dialing remote: %w", err)
	}
	serverConn := jsonrpc2.NewConn(jsonrpc2.NewHeaderStream(netConn))
	serverConn.Go(ctx, jsonrpc2.MethodNotFound)
	return serverConn, nil
}

// ExecuteCommand connects to the named server, sends it a
// workspace/executeCommand request (with command 'id' and arguments
// JSON encoded in 'request'), and populates the result variable.
func ExecuteCommand(ctx context.Context, addr string, id string, request, result any) error {
	serverConn, err := dialRemote(ctx, addr)
	if err != nil {
		return err
	}
	args, err := command.MarshalArgs(request)
	if err != nil {
		return err
	}
	params := protocol.ExecuteCommandParams{
		Command:   id,
		Arguments: args,
	}
	return protocol.Call(ctx, serverConn, "workspace/executeCommand", params, result)
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

	// Don't run the clientConn yet, so that we can complete the handshake before
	// processing any client messages.

	// Do a handshake with the server instance to exchange debug information.
	index := atomic.AddInt64(&serverIndex, 1)
	f.mu.Lock()
	f.serverConn = serverConn
	f.serverID = strconv.FormatInt(index, 10)
	f.mu.Unlock()
	f.handshake(ctx)
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
	event.Log(ctx, fmt.Sprintf("forwarder: exited with error: %v", err))
	return err
}

// TODO(rfindley): remove this handshaking in favor of middleware.
func (f *forwarder) handshake(ctx context.Context) {
	// This call to os.Executable is redundant, and will be eliminated by the
	// transition to the V2 API.
	hreq := handshakeRequest{ServerID: f.serverID}
	var hresp handshakeResponse
	if err := protocol.Call(ctx, f.serverConn, handshakeMethod, hreq, &hresp); err != nil {
		// TODO(rfindley): at some point in the future we should return an error
		// here.  Handshakes have become functional in nature.
		event.Error(ctx, "forwarder: cuelsp handshake failed", err)
	}
	event.Log(ctx, "New server",
		tag.NewServer.Of(f.serverID),
		tag.ServerID.Of(hresp.ServerID),
	)
}

func ConnectToRemote(ctx context.Context, addr string) (net.Conn, error) {
	dialer, err := newAutoDialer(addr, nil)
	if err != nil {
		return nil, err
	}
	return dialer.dialNet(ctx)
}

// A handshakeRequest identifies a client to the LSP server.
type handshakeRequest struct {
	// ServerID is the ID of the server on the client. This should
	// usually be 0.
	ServerID string `json:"serverID"`
}

// A handshakeResponse is returned by the LSP server to tell the LSP
// client information about its server.
type handshakeResponse struct {
	// ServerID is the server server associated with the client.
	ServerID string `json:"serverID"`
}

// clientServer identifies a current client LSP server on the
// server.
type clientServer struct {
	ServerID string `json:"serverID"`
}

// serverState holds information about the cuelsp daemon process.
type serverState struct {
	CurrentServerID string `json:"currentServerID"`
}

const (
	handshakeMethod = "cuelsp/handshake"
	serversMethod   = "cuelsp/servers"
)

func handshaker(svrID string, logHandshakes bool, handler jsonrpc2.Handler) jsonrpc2.Handler {
	return func(ctx context.Context, reply jsonrpc2.Replier, r jsonrpc2.Request) error {
		switch r.Method() {
		case handshakeMethod:
			// We log.Printf in this handler, rather than event.Log when we want logs
			// to go to the daemon log rather than being reflected back to the
			// client.
			var req handshakeRequest
			if err := json.Unmarshal(r.Params(), &req); err != nil {
				if logHandshakes {
					log.Printf("Error processing handshake for server %s: %v", svrID, err)
				}
				sendError(ctx, reply, err)
				return nil
			}
			if logHandshakes {
				log.Printf("Server %s: got handshake.", svrID)
			}
			event.Log(ctx, "Handshake server update",
				tag.ServerID.Of(req.ServerID),
			)
			resp := handshakeResponse{
				ServerID: svrID,
			}
			return reply(ctx, resp, nil)

		case serversMethod:
			resp := serverState{
				CurrentServerID: svrID,
			}
			return reply(ctx, resp, nil)
		}
		return handler(ctx, reply, r)
	}
}

func sendError(ctx context.Context, reply jsonrpc2.Replier, err error) {
	err = fmt.Errorf("%v: %w", err, jsonrpc2.ErrParse)
	if err := reply(ctx, nil, err); err != nil {
		event.Error(ctx, "", err)
	}
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
