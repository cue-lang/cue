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

// Package rpcfunc enables CUE evaluations to call plugin functions
// served by an external process over JSON-RPC.
//
// A server registers functions and CUE source injections,
// then serves them over a connection. A client connects,
// discovers the available injections, and registers them
// with a [cuecontext.Injector] so that CUE code using
// @inject attributes can call the server-side functions.
package rpcfunc

import "encoding/json"

// FuncInfo describes a registered server-side function.
type FuncInfo struct {
	ID       int
	ArgCount int
}

// InitReply is the response to a Plugin.Init call.
type InitReply struct {
	Injections map[string]string
	Funcs      []FuncInfo
}

// CallArgs holds the arguments for a Plugin.Call RPC.
type CallArgs struct {
	ID   int
	Args []json.RawMessage
}

// CallReply holds the result of a Plugin.Call RPC.
type CallReply struct {
	Result json.RawMessage
	Error  string
}
