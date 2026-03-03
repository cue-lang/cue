// Copyright 2026 CUE Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package extvalidator

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"sync"
	"time"

	"cuelang.org/go/unstable/lspaux/protocol"
	"cuelang.org/go/unstable/lspaux/validatorconfig"
	"github.com/coder/websocket"
)

// wsPath is the suffix added to the serverUrl to give the full URL to
// the external validator's websocket acceptor.
const wsPath = "/ws/lsp"

type extValidatorClient interface {
	// Called each time a connection to the server is established.
	connected()
	// Called when the server indicates some external change has
	// occurred and re-evaluation is possible.
	changeSignal(*protocol.ChangedMsg) error
	// Called when the server sends a (possibly partial) result to an
	// evaluation request.
	evalResult(*protocol.EvalResultMsg) error
	// Called when the server indicates no more results will occur for
	// the indicated evaluation request.
	evalFinished(*protocol.EvalFinishedMsg) error
}

// conn models a connection to an external validator.
type conn struct {
	profile  *validatorconfig.Profile
	ctx      context.Context
	client   extValidatorClient
	debugLog func(msg string)

	mu   sync.Mutex
	conn *websocket.Conn
}

// connect creates a new connection and starts a go-routine to
// repeatedly connect to, and receive from the external validator.
func connect(profile *validatorconfig.Profile, ctx context.Context, client extValidatorClient, debugLog func(msg string)) *conn {
	if ctx == nil {
		ctx = context.Background()
	}

	conn := &conn{
		profile:  profile,
		ctx:      ctx,
		client:   client,
		debugLog: debugLog,
	}
	go conn.connect()
	return conn
}

// connect repeatedly attempts to connect to the external
// validator. Whenever the connection closes, or fails to connect, the
// go-routine sleeps, following a randomised binary exponential
// backoff schedule. The go-routine will only exit if the context
// supplied to [connect] errors.
func (c *conn) connect() {
	const minSleepDuration = 250 * time.Millisecond
	const maxSleepDuration = 15 * time.Second
	sleepDuration := minSleepDuration

	profile := c.profile
	serverUrl := profile.ServerURL
	ctx := c.ctx

	var dialOpts *websocket.DialOptions

	if profile.Token != "" {
		dialOpts = &websocket.DialOptions{
			HTTPHeader: http.Header{
				"Authorization": {"Bearer " + profile.Token},
			},
		}
	}

	for {
		conn, resp, err := websocket.Dial(ctx, serverUrl+wsPath, dialOpts)

		if err == nil {
			c.debugLogf("extValidator: connected to %s", serverUrl)
			c.mu.Lock()
			c.conn = conn
			c.mu.Unlock()

			c.receive(conn)

			c.mu.Lock()
			c.conn = nil
			c.mu.Unlock()
			sleepDuration = minSleepDuration

		} else if resp == nil {
			c.debugLogf("extValidator: error when dialing %s: %v", serverUrl, err)
		} else {
			c.debugLogf("extValidator: error when dialing %s: %v, http status: %v", serverUrl, err, resp.StatusCode)
		}

		if ctx.Err() != nil {
			return

		} else {
			time.Sleep(sleepDuration)
			sleepDuration += time.Duration(rand.Int64N(int64(sleepDuration)))
			sleepDuration = min(sleepDuration, maxSleepDuration)
		}
	}
}

// requestEvaluation sends the supplied [protocol.EvalRequestMsg] to
// the external validator. If the connection to the external validator
// exists and writing to it returns no error, then true is returned;
// otherwise false. However, as normal, just because the message was
// sent does not mean that it was received.
func (c *conn) requestEvaluation(msg *protocol.EvalRequestMsg) bool {
	// TODO: it would be better to use the websocket Writer as
	// MarshalBytes is really just making another exact copy of msg.
	data := msg.MarshalBytes()

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.conn != nil {
		err := c.conn.Write(c.ctx, websocket.MessageBinary, data)
		return err == nil
	}
	return false
}

// receive is the connection's receive-loop.
func (c *conn) receive(conn *websocket.Conn) {
	defer conn.Close(websocket.StatusNormalClosure, "")
	const readLimit = 16 * 1024 * 1024 // 16MB
	conn.SetReadLimit(readLimit)

	ctx := c.ctx
	client := c.client
	client.connected()

	var closeErr websocket.CloseError
	for {
		msgType, data, err := conn.Read(ctx)
		if errors.Is(err, io.EOF) || errors.As(err, &closeErr) || ctx.Err() != nil {
			return
		} else if err != nil {
			c.debugLogf("extValidator: error when reading from websocket: %v", err)
			return
		} else if msgType != websocket.MessageBinary {
			c.debugLog("extValidator: websocket received non-binary message")
			return
		}

		msgTypeProto, err := protocol.PeekMessageType(data)
		if err != nil {
			c.debugLogf("extValidator: protocol violation: %v", err)
			return
		}

		switch msgTypeProto {
		case protocol.MsgTypeChanged:
			msg := &protocol.ChangedMsg{}
			err = msg.UnmarshalBytes(data)
			if err == nil {
				err = client.changeSignal(msg)
			}

		case protocol.MsgTypeEvalResult:
			msg := &protocol.EvalResultMsg{}
			err = msg.UnmarshalBytes(data)
			if err == nil {
				err = client.evalResult(msg)
			}

		case protocol.MsgTypeEvalFinished:
			msg := &protocol.EvalFinishedMsg{}
			err = msg.UnmarshalBytes(data)
			if err == nil {
				err = client.evalFinished(msg)
			}

		default:
			c.debugLog("protocol violation")
			return
		}

		if err != nil {
			c.debugLog(err.Error())
			return
		}
	}
}

func (c *conn) debugLogf(format string, args ...any) {
	c.debugLog(fmt.Sprintf(format, args...))
}
