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

//go:generate go run gen.go
//go:generate gofmt -s -w .

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

// readLine is akin to bufio.Reader.ReadString('\n'),
// but it avoids the need for a buffered reader.
// This is important because we want to use os.Stdin directly for os/exec,
// as without an os.File it will copy bytes in a goroutine,
// causing problems like https://go.dev/issue/7990.
//
// Since we don't need '\n' to be included in the resulting string,
// we exclude it directly, unlike bufio.Reader.ReadString.
// For consistent behavior on Windows, we also skip '\r'.
func readLine(r io.Reader) (string, error) {
	var p [1]byte
	var b strings.Builder
	for {
		n, err := r.Read(p[:])
		if n == 1 {
			if p[0] == '\n' {
				s := b.String()
				if s[len(s)-1] == '\r' {
					s = s[:len(s)-1] // treat CRLF line endings as if they were LF
				}
				return s, err // err might or might not be nil
			}
			b.WriteByte(p[0])
		}
		if err != nil {
			return b.String(), err
		}
	}
}

func (c *askCmd) Run(ctx *task.Context) (res interface{}, err error) {
	str := ctx.String("prompt")
	if ctx.Err != nil {
		return nil, ctx.Err
	}
	if str != "" {
		fmt.Fprint(ctx.Stdout, str+" ")
	}

	response, err := readLine(ctx.Stdin)
	if err != nil && err != io.EOF { // we are fine with an answer ending with EOF
		return nil, err
	}

	update := map[string]interface{}{"response": response}

	switch v := ctx.Lookup("response"); v.IncompleteKind() {
	case cue.BoolKind:
		switch strings.ToLower(response) {
		case "yes":
			update["response"] = true
		default:
			update["response"] = false
		}
	case cue.StringKind:
		// already set above
	}
	return update, nil
}
