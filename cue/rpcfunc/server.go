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

//go:generate go run generate_server.go

import (
	"encoding/json"
	"fmt"
	"io"
	"net/rpc"
	"net/rpc/jsonrpc"
	"reflect"
)

// Server holds registered functions and injections for serving over RPC.
type Server struct {
	funcs      []serverFunc
	injections map[string]string
}

type serverFunc struct {
	argCount int
	call     func(argValues []json.RawMessage) (any, error)
}

// NewServer returns a new [Server].
func NewServer() *Server {
	return &Server{
		injections: make(map[string]string),
	}
}

func register[Args, R any](s *Server, f func(Args) (R, error)) int {
	argT := reflect.TypeFor[Args]()
	numArgs := argT.NumField()
	id := len(s.funcs)
	s.funcs = append(s.funcs, serverFunc{
		argCount: numArgs,
		call: func(argValues []json.RawMessage) (any, error) {
			if len(argValues) != numArgs {
				return nil, fmt.Errorf("function %d expects %d args, got %d", id, numArgs, len(argValues))
			}
			var args Args
			dstArgs := reflect.ValueOf(&args).Elem()
			for i, argv := range argValues {
				if err := json.Unmarshal(argv, dstArgs.Field(i).Addr().Interface()); err != nil {
					return nil, err
				}
			}
			return f(args)
		},
	})
	return id
}

// AddInjection associates CUE source with an injection name.
// The source should use Caller(ID) to reference registered functions.
func (s *Server) AddInjection(name, cueSource string) {
	s.injections[name] = cueSource
}

// Serve serves JSON-RPC on conn. It blocks until conn is closed.
func (s *Server) Serve(conn io.ReadWriteCloser) {
	srv := rpc.NewServer()
	srv.RegisterName("Plugin", &pluginService{server: s})
	srv.ServeCodec(jsonrpc.NewServerCodec(conn))
}

type pluginService struct {
	server *Server
}

func (p *pluginService) Init(_ struct{}, reply *InitReply) error {
	reply.Injections = p.server.injections
	reply.Funcs = make([]FuncInfo, len(p.server.funcs))
	for i, f := range p.server.funcs {
		reply.Funcs[i] = FuncInfo{
			ID:       i,
			ArgCount: f.argCount,
		}
	}
	return nil
}

func (p *pluginService) Call(args *CallArgs, reply *CallReply) error {
	if args.ID < 0 || args.ID >= len(p.server.funcs) {
		return fmt.Errorf("unknown function ID %d", args.ID)
	}
	f := p.server.funcs[args.ID]
	result, err := f.call(args.Args)
	if err != nil {
		reply.Error = err.Error()
		return nil
	}
	data, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf("marshaling result: %v", err)
	}
	reply.Result = data
	return nil
}
