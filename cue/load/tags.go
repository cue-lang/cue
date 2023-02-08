// Copyright 2020 CUE Authors
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

package load

import (
	"crypto/rand"
	"encoding/hex"
	"os"
	"os/user"
	"runtime"
	"time"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/token"
)

const rfc3339 = "2006-01-02T15:04:05.999999999Z"

// DefaultTagVars creates a new map with a set of supported injection variables.
func DefaultTagVars() map[string]TagVar {
	return map[string]TagVar{
		"now": {
			Func: func() (ast.Expr, error) {
				return ast.NewString(time.Now().UTC().Format(rfc3339)), nil
			},
		},
		"os": {
			Func: func() (ast.Expr, error) {
				return ast.NewString(runtime.GOOS), nil
			},
		},
		"cwd": {
			Func: func() (ast.Expr, error) {
				return varToString(os.Getwd())
			},
		},
		"username": {
			Func: func() (ast.Expr, error) {
				u, err := user.Current()
				return varToString(u.Username, err)
			},
		},
		"hostname": {
			Func: func() (ast.Expr, error) {
				return varToString(os.Hostname())
			},
		},
		"rand": {
			Func: func() (ast.Expr, error) {
				var b [16]byte
				_, err := rand.Read(b[:])
				if err != nil {
					return nil, err
				}
				var hx [34]byte
				hx[0] = '0'
				hx[1] = 'x'
				hex.Encode(hx[2:], b[:])
				return ast.NewLit(token.INT, string(hx[:])), nil
			},
		},
	}
}

func varToString(s string, err error) (ast.Expr, error) {
	if err != nil {
		return nil, err
	}
	x := ast.NewString(s)
	return x, nil
}
