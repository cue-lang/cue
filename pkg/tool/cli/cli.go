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

package cli

import (
	"fmt"
	"io"
	"strings"

	"cuelang.org/go/cue"
	"cuelang.org/go/internal/task"
)

func init() {
	task.Register("tool/cli.Print", newPrintCmd)
	task.Register("tool/cli.Ask", newAskCmd)

	// For backwards compatibility.
	task.Register("print", newPrintCmd)
}

type printCmd struct{}

func newPrintCmd(v cue.Value) (task.Runner, error) {
	return &printCmd{}, nil
}

func (c *printCmd) Run(ctx *task.Context) (res interface{}, err error) {
	str := ctx.String("text")
	if ctx.Err != nil {
		return nil, ctx.Err
	}
	fmt.Fprintln(ctx.Stdout, str)
	return nil, nil
}

type askCmd struct{}

func newAskCmd(v cue.Value) (task.Runner, error) {
	return &askCmd{}, nil
}

func (c *askCmd) Run(ctx *task.Context) (res interface{}, err error) {
	str := ctx.String("prompt")
	if ctx.Err != nil {
		return nil, ctx.Err
	}
	if str != "" {
		fmt.Fprint(ctx.Stdout, str+" ")
	}

	// Read a single line from stdin, one byte at a time, so that we do
	// not consume any input past the newline; stdin may be shared with
	// subsequent tasks such as another cli.Ask or an exec.Run.
	var line []byte
	for {
		var b [1]byte
		if _, err := io.ReadFull(ctx.Stdin, b[:]); err == io.EOF {
			break
		} else if err != nil {
			return nil, err
		}
		if b[0] == '\n' {
			break
		}
		line = append(line, b[0])
	}
	response := strings.TrimSuffix(string(line), "\r")

	update := map[string]interface{}{"response": response}

	switch v := ctx.Lookup("response"); v.IncompleteKind() {
	case cue.BoolKind:
		update["response"] = strings.ToLower(response) == "yes"
	case cue.StringKind:
		// already set above
	}
	return update, nil
}
