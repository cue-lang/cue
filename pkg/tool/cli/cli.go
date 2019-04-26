// Copyright 2019 CUE Authors
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

// Package cli provides tasks dealing with a console.
package cli

import (
	"fmt"

	"cuelang.org/go/cue"
	"cuelang.org/go/internal/task"
)

func init() {
	task.Register("tool/cli.Print", newPrintCmd)

	// For backwards compatibility.
	task.Register("print", newPrintCmd)
}

type printCmd struct{}

func newPrintCmd(v cue.Value) (task.Runner, error) {
	return &printCmd{}, nil
}

func (c *printCmd) Run(ctx *task.Context, v cue.Value) (res interface{}, err error) {
	str, err := v.Lookup("text").String()
	if err != nil {
		return nil, err
	}
	fmt.Fprintln(ctx.Stdout, str)
	return nil, nil
}
