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

package cuehub

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"strings"
	"sync"
	"time"

	"cuelang.org/go/unstable/lspaux/config"
	"cuelang.org/go/unstable/lspaux/protocol"
	"github.com/coder/websocket"
)

const wsPath = "/ws/lsp"

type CueHubClient interface {
	ChangeSignal(*protocol.ChangedMsg) error
	EvalResult(*protocol.EvalResultMsg) error
	EvalFinished(*protocol.EvalFinishedMsg) error
}

type cueHubConnection struct {
	serverUrl string
	ctx       context.Context
	handler   CueHubClient
	debugLog  func(msg string)

	lock      sync.Mutex
	conn      *websocket.Conn
	sendQueue [][]byte
}

func connect(serverUrl string, ctx context.Context, handler CueHubClient, debugLog func(msg string)) *cueHubConnection {
	if ctx == nil {
		ctx = context.Background()
	}

	serverUrl = strings.TrimRight(serverUrl, "/")

	conn := &cueHubConnection{
		serverUrl: serverUrl,
		ctx:       ctx,
		handler:   handler,
		debugLog:  debugLog,
	}
	go conn.connect()
	return conn
}

func (c *cueHubConnection) connect() {
	const minSleepDuration = 250 * time.Millisecond
	const maxSleepDuration = 15 * time.Second
	sleepDuration := minSleepDuration
	ctx := c.ctx

	for {
		var dialOpts *websocket.DialOptions
		cfg, err := config.Parse(config.ConfigPath())
		if err == nil && cfg != nil {
			for _, profile := range cfg.Profiles {
				profileUrl := strings.TrimRight(profile.ServerURL, "/")
				if profileUrl == c.serverUrl && profile.Token != "" {
					dialOpts = &websocket.DialOptions{
						HTTPHeader: http.Header{
							"Authorization": {"Bearer " + profile.Token},
						},
					}
					break
				}
			}
		}

		conn, resp, err := websocket.Dial(ctx, c.serverUrl+wsPath, dialOpts)

		if err == nil {
			c.debugLogf("cuehub: connected to %s", c.serverUrl)
			c.lock.Lock()
			c.conn = conn
			sendQueue := c.sendQueue
			c.sendQueue = nil
			c.lock.Unlock()

			c.receive(conn, sendQueue)

			c.lock.Lock()
			c.conn = nil
			c.lock.Unlock()

		} else if resp == nil {
			c.debugLogf("cuehub: error when dialing %s: %v", c.serverUrl, err)
		} else {
			c.debugLogf("cuehub: error when dialing %s: %v, http status: %v", c.serverUrl, err, resp.StatusCode)
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

func (c *cueHubConnection) requestEvaluation(msg *protocol.EvalRequestMsg) {
	data := msg.MarshalBytes()

	c.lock.Lock()
	defer c.lock.Unlock()

	conn := c.conn
	if conn != nil {
		err := conn.Write(c.ctx, websocket.MessageBinary, data)
		if err == nil {
			return
		} else {
			c.debugLogf("cuehub: error when writing to websocket: %v", err)
			conn.Close(websocket.StatusNormalClosure, "")
			c.conn = nil
		}
	}

	c.sendQueue = append(c.sendQueue, data)
}

func (c *cueHubConnection) debugLogf(format string, args ...any) {
	c.debugLog(fmt.Sprintf(format, args...))
}

func (c *cueHubConnection) receive(conn *websocket.Conn, sendQueue [][]byte) {
	defer conn.Close(websocket.StatusNormalClosure, "")
	conn.SetReadLimit(16 * 1024 * 1024) // 16MB

	ctx := c.ctx
	handler := c.handler

	for i, data := range sendQueue {
		err := conn.Write(ctx, websocket.MessageBinary, data)
		if err != nil {
			c.debugLogf("cuehub: error when writing to websocket: %v", err)
			sendQueue := sendQueue[i:]
			c.lock.Lock()
			c.sendQueue = append(c.sendQueue, sendQueue...)
			c.lock.Unlock()
			return
		}
	}

	var closeErr websocket.CloseError
	for {
		msgType, data, err := conn.Read(ctx)
		if errors.Is(err, io.EOF) || errors.As(err, &closeErr) || ctx.Err() != nil {
			return
		} else if err != nil {
			c.debugLogf("cuehub: error when reading from websocket: %v", err)
			return
		} else if msgType != websocket.MessageBinary {
			c.debugLog("cuehub: websocket received non-binary message")
			return
		}

		msgTypeProto, err := protocol.PeekMessageType(data)
		if err != nil {
			c.debugLogf("cuehub: protocol violation: %v", err)
			return
		}

		switch msgTypeProto {
		case protocol.MsgTypeChanged:
			msg := &protocol.ChangedMsg{}
			err = msg.UnmarshalBytes(data)
			if err == nil {
				err = handler.ChangeSignal(msg)
			}

		case protocol.MsgTypeEvalResult:
			msg := &protocol.EvalResultMsg{}
			err = msg.UnmarshalBytes(data)
			if err == nil {
				err = handler.EvalResult(msg)
			}

		case protocol.MsgTypeEvalFinished:
			msg := &protocol.EvalFinishedMsg{}
			err = msg.UnmarshalBytes(data)
			if err == nil {
				err = handler.EvalFinished(msg)
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
