// Copyright 2026 The CUE Authors
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

package cmd_test

import (
	"errors"
	"io"

	"github.com/rogpeppe/go-internal/testscript"

	"cuelang.org/go/internal/cuetestscript"
)

// This file hooks the environment that the cue command expects, as implemented
// by [cuetestscript], up to testscript itself.

// cuetestscriptCmds returns the commands from [cuetestscript.Cmds] in the form
// expected by [testscript.Params.Cmds].
func cuetestscriptCmds() map[string]func(ts *testscript.TestScript, neg bool, args []string) {
	tsCmds := make(map[string]func(ts *testscript.TestScript, neg bool, args []string))
	for name, cmd := range cuetestscript.Cmds() {
		tsCmds[name] = tsCmd(cmd)
	}
	return tsCmds
}

// tsCmd adapts cmd to testscript's own command signature, mapping a returned
// error onto the script's expectation of success or failure.
func tsCmd(cmd cuetestscript.Cmd) func(ts *testscript.TestScript, neg bool, args []string) {
	return func(ts *testscript.TestScript, neg bool, args []string) {
		err := cmd.Run(cmdEnv{ts}, args)
		if errors.Is(err, cuetestscript.ErrUsage) {
			ts.Fatalf("usage: %s", cmd.Usage)
		}
		if neg {
			if err == nil {
				ts.Fatalf("unexpected command success")
			}
			ts.Logf("[%v]\n", err)
			return
		}
		ts.Check(err)
	}
}

// setupEnv adapts [testscript.Env] to [cuetestscript.Env].
type setupEnv struct {
	e *testscript.Env
}

func (e setupEnv) WorkDir() string          { return e.e.WorkDir }
func (e setupEnv) Getenv(key string) string { return e.e.Getenv(key) }
func (e setupEnv) Setenv(key, value string) { e.e.Setenv(key, value) }
func (e setupEnv) Defer(f func())           { e.e.Defer(f) }

// cmdEnv adapts [testscript.TestScript] to [cuetestscript.CmdEnv].
type cmdEnv struct {
	ts *testscript.TestScript
}

func (e cmdEnv) WorkDir() string          { return e.ts.Getenv("WORK") }
func (e cmdEnv) Getenv(key string) string { return e.ts.Getenv(key) }
func (e cmdEnv) Setenv(key, value string) { e.ts.Setenv(key, value) }
func (e cmdEnv) Defer(f func())           { e.ts.Defer(f) }
func (e cmdEnv) MkAbs(path string) string { return e.ts.MkAbs(path) }
func (e cmdEnv) Stdout() io.Writer        { return e.ts.Stdout() }
func (e cmdEnv) Stderr() io.Writer        { return e.ts.Stderr() }
