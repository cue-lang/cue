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

package rpcfunc

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/rpc"
	"net/rpc/jsonrpc"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
)

// Client connects to a plugin server and registers its
// injections with a CUE context.
type Client struct {
	rpc *rpc.Client
}

// Dial connects to a plugin server at the given network address.
func Dial(network, address string) (*Client, error) {
	conn, err := net.Dial(network, address)
	if err != nil {
		return nil, err
	}
	return NewClient(conn), nil
}

// NewClient returns a new [Client] that communicates over conn.
func NewClient(conn io.ReadWriteCloser) *Client {
	return &Client{
		rpc: jsonrpc.NewClient(conn),
	}
}

// Close closes the connection to the server.
func (c *Client) Close() error {
	return c.rpc.Close()
}

// RegisterAll discovers all injections from the server and registers
// them with the given injector.
func (c *Client) RegisterAll(ctx *cue.Context, j *cuecontext.Injector) error {
	var reply InitReply
	if err := c.rpc.Call("Plugin.Init", struct{}{}, &reply); err != nil {
		return fmt.Errorf("rpcfunc: Init call failed: %v", err)
	}

	funcsByID := make(map[int]FuncInfo, len(reply.Funcs))
	for _, fi := range reply.Funcs {
		funcsByID[fi.ID] = fi
	}

	callerFunc := c.makeCaller(ctx, funcsByID)

	scope := ctx.CompileString("Caller: _")
	scope = scope.FillPath(cue.ParsePath("Caller"), callerFunc)

	for name, src := range reply.Injections {
		v := ctx.CompileString(src, cue.Scope(scope))
		if err := v.Err(); err != nil {
			return fmt.Errorf("rpcfunc: compiling injection %q: %v", name, err)
		}
		j.Register(name, v)
	}
	return nil
}

func (c *Client) makeCaller(ctx *cue.Context, funcs map[int]FuncInfo) cue.Value {
	return cue.PureFunc1(func(id int) (cue.Value, error) {
		fi, ok := funcs[id]
		if !ok {
			return cue.Value{}, fmt.Errorf("rpcfunc: unknown function ID %d", id)
		}
		return c.makeFunc(fi)
	})
}

func (c *Client) makeFunc(fi FuncInfo) (cue.Value, error) {
	call := func(args ...any) (any, error) {
		rawArgs := make([]json.RawMessage, len(args))
		for i, a := range args {
			data, err := json.Marshal(a)
			if err != nil {
				return nil, fmt.Errorf("rpcfunc: marshaling argument %d: %v", i, err)
			}
			rawArgs[i] = data
		}
		var reply CallReply
		if err := c.rpc.Call("Plugin.Call", &CallArgs{
			ID:   fi.ID,
			Args: rawArgs,
		}, &reply); err != nil {
			return nil, fmt.Errorf("rpcfunc: Call RPC failed: %v", err)
		}
		if reply.Error != "" {
			return nil, fmt.Errorf("%s", reply.Error)
		}
		var result any
		dec := json.NewDecoder(bytes.NewReader(reply.Result))
		dec.UseNumber()
		if err := dec.Decode(&result); err != nil {
			return nil, fmt.Errorf("rpcfunc: unmarshaling result: %v", err)
		}
		return convertNumbers(result), nil
	}

	switch fi.ArgCount {
	case 1:
		return cue.PureFunc1(func(a0 any) (any, error) {
			return call(a0)
		}), nil
	case 2:
		return cue.PureFunc2(func(a0, a1 any) (any, error) {
			return call(a0, a1)
		}), nil
	case 3:
		return cue.PureFunc3(func(a0, a1, a2 any) (any, error) {
			return call(a0, a1, a2)
		}), nil
	case 4:
		return cue.PureFunc4(func(a0, a1, a2, a3 any) (any, error) {
			return call(a0, a1, a2, a3)
		}), nil
	case 5:
		return cue.PureFunc5(func(a0, a1, a2, a3, a4 any) (any, error) {
			return call(a0, a1, a2, a3, a4)
		}), nil
	case 6:
		return cue.PureFunc6(func(a0, a1, a2, a3, a4, a5 any) (any, error) {
			return call(a0, a1, a2, a3, a4, a5)
		}), nil
	case 7:
		return cue.PureFunc7(func(a0, a1, a2, a3, a4, a5, a6 any) (any, error) {
			return call(a0, a1, a2, a3, a4, a5, a6)
		}), nil
	case 8:
		return cue.PureFunc8(func(a0, a1, a2, a3, a4, a5, a6, a7 any) (any, error) {
			return call(a0, a1, a2, a3, a4, a5, a6, a7)
		}), nil
	case 9:
		return cue.PureFunc9(func(a0, a1, a2, a3, a4, a5, a6, a7, a8 any) (any, error) {
			return call(a0, a1, a2, a3, a4, a5, a6, a7, a8)
		}), nil
	case 10:
		return cue.PureFunc10(func(a0, a1, a2, a3, a4, a5, a6, a7, a8, a9 any) (any, error) {
			return call(a0, a1, a2, a3, a4, a5, a6, a7, a8, a9)
		}), nil
	default:
		return cue.Value{}, fmt.Errorf("rpcfunc: unsupported argument count %d (max 10)", fi.ArgCount)
	}
}

// convertNumbers recursively converts json.Number values to int64 or float64.
func convertNumbers(v any) any {
	switch v := v.(type) {
	case json.Number:
		if n, err := v.Int64(); err == nil {
			return n
		}
		n, _ := v.Float64()
		return n
	case map[string]any:
		for k, val := range v {
			v[k] = convertNumbers(val)
		}
		return v
	case []any:
		for i, val := range v {
			v[i] = convertNumbers(val)
		}
		return v
	default:
		return v
	}
}
