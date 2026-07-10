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

package cli

import (
	"crypto/rand"
	"encoding/hex"
	"os"
	"os/user"
	"runtime"
	"time"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/cueload"
)

// DefaultTagVars returns the standard side-effecting tag variables
// (now, os, arch, cwd, username, hostname, rand) for use as
// [cueload.Config].TagVars. They are deliberately not part of the
// hermetic core configuration: a loader only consults the environment
// when given these explicitly (typically via the -T flag; see
// [Command.ApplyToConfig]).
func DefaultTagVars() map[string]cueload.TagVar {
	return map[string]cueload.TagVar{
		"now": {
			Func: func() (ast.Expr, error) {
				return ast.NewString(time.Now().UTC().Format(time.RFC3339Nano)), nil
			},
			Description: "the current time in RFC 3339 format",
		},
		"os": {
			Func: func() (ast.Expr, error) {
				return ast.NewString(runtime.GOOS), nil
			},
			Description: "the operating system (GOOS)",
		},
		"arch": {
			Func: func() (ast.Expr, error) {
				return ast.NewString(runtime.GOARCH), nil
			},
			Description: "the processor architecture (GOARCH)",
		},
		"cwd": {
			Func: func() (ast.Expr, error) {
				return varToString(os.Getwd())
			},
			Description: "the process working directory",
		},
		"username": {
			Func: func() (ast.Expr, error) {
				u, err := user.Current()
				if err != nil {
					return nil, err
				}
				return ast.NewString(u.Username), nil
			},
			Description: "the current user name",
		},
		"hostname": {
			Func: func() (ast.Expr, error) {
				return varToString(os.Hostname())
			},
			Description: "the host name",
		},
		"rand": {
			Func: func() (ast.Expr, error) {
				var b [16]byte
				rand.Read(b[:])
				var hx [34]byte
				hx[0] = '0'
				hx[1] = 'x'
				hex.Encode(hx[2:], b[:])
				return ast.NewLit(token.INT, string(hx[:])), nil
			},
			Description: "a 128-bit random integer",
		},
	}
}

func varToString(s string, err error) (ast.Expr, error) {
	if err != nil {
		return nil, err
	}
	return ast.NewString(s), nil
}
