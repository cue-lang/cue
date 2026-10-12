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

package exec

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/internal/task"
)

func init() {
	task.Register("tool/exec.Run", newExecCmd)

	// For backwards compatibility.
	task.Register("exec", newExecCmd)
}

type execCmd struct{}

func newExecCmd(v cue.Value) (task.Runner, error) {
	return &execCmd{}, nil
}

func (c *execCmd) Run(ctx *task.Context) (res interface{}, err error) {
	cmd, doc, err := mkCommand(ctx)
	if err != nil {
		return cue.Value{}, err
	}

	// TODO: set environment variables, if defined.
	stream := func(name string) (stream cue.Value, ok bool, err error) {
		c := ctx.Obj.LookupPath(cue.ParsePath(name))
		// A field that is absent or null is simply not configured;
		// an error, such as a failure to compute its value, must not be ignored.
		if !c.Exists() || c.IsNull() {
			return cue.Value{}, false, nil
		}
		if err := c.Err(); err != nil {
			return cue.Value{}, false, err
		}
		return c, true, nil
	}

	if v, ok, err := stream("stdin"); err != nil {
		return nil, err
	} else if !ok {
		cmd.Stdin = ctx.Stdin
	} else if cmd.Stdin, err = v.Reader(); err != nil {
		return nil, errors.Wrapf(err, v.Pos(), "invalid input")
	}
	var stdout, stderr bytes.Buffer
	stdoutVal, captureOut, err := stream("stdout")
	if err != nil {
		return nil, err
	}
	if captureOut {
		cmd.Stdout = &stdout
	} else {
		cmd.Stdout = ctx.Stdout
	}
	stderrVal, captureErr, err := stream("stderr")
	if err != nil {
		return nil, err
	}
	if captureErr {
		cmd.Stderr = &stderr
	} else {
		cmd.Stderr = ctx.Stderr
	}

	v := ctx.Obj.LookupPath(cue.ParsePath("mustSucceed"))
	mustSucceed, err := v.Bool()
	if err != nil {
		return nil, errors.Wrapf(err, v.Pos(), "invalid bool value")
	}

	err = cmd.Run()

	update := map[string]interface{}{}
	if captureOut {
		update["stdout"] = output(stdoutVal, stdout.Bytes())
	}
	if captureErr {
		// An error other than a non-zero exit status, such as a missing
		// program, is reported via stderr too.
		if exit := (*exec.ExitError)(nil); err != nil && !errors.As(err, &exit) {
			stderr.WriteString(err.Error())
		}
		update["stderr"] = output(stderrVal, stderr.Bytes())
	}
	update["success"] = err == nil

	if err == nil || !mustSucceed {
		return update, nil
	}

	return nil, fmt.Errorf("command %q failed: %v", doc, err)
}

// output returns captured output as bytes if v is constrained to bytes,
// and as a string otherwise.
func output(v cue.Value, b []byte) any {
	if v.IncompleteKind() == cue.BytesKind {
		return b
	}
	return string(b)
}

// mkCommand builds an [exec.Cmd] from a CUE task value,
// also returning the full list of arguments as a string slice
// so that it can be used in error messages.
func mkCommand(ctx *task.Context) (c *exec.Cmd, doc []string, err error) {
	v := ctx.Lookup("cmd")
	if ctx.Err != nil {
		return nil, nil, ctx.Err
	}

	var bin string
	var args []string
	switch v.Kind() {
	case cue.StringKind:
		str, _ := v.String()
		if list := strings.Fields(str); len(list) > 0 {
			bin, args = list[0], list[1:]
		}

	case cue.ListKind:
		list, _ := v.List()
		if !list.Next() {
			return nil, nil, errors.New("empty command list")
		}
		bin, err = list.Value().String()
		if err != nil {
			return nil, nil, err
		}
		for list.Next() {
			str, err := list.Value().String()
			if err != nil {
				return nil, nil, err
			}
			args = append(args, str)
		}
	}

	if bin == "" {
		return nil, nil, errors.New("empty command")
	}

	cmd := exec.CommandContext(ctx.Context, bin, args...)

	if v := ctx.Obj.LookupPath(cue.ParsePath("dir")); v.Exists() {
		cmd.Dir, err = v.String()
		if err != nil {
			return nil, nil, err
		}
	}

	env := ctx.Obj.LookupPath(cue.ParsePath("env"))

	// List case.
	for iter, _ := env.List(); iter.Next(); {
		v, _ := iter.Value().Default()
		str, err := v.String()
		if err != nil {
			return nil, nil, errors.Wrapf(err, v.Pos(),
				"invalid environment variable value %q", v)
		}
		cmd.Env = append(cmd.Env, str)
	}

	// Struct case.
	for iter, _ := env.Fields(); iter.Next(); {
		label := iter.Selector().Unquoted()
		v, _ := iter.Value().Default()
		var str string
		switch v.Kind() {
		case cue.StringKind:
			str, _ = v.String()
		case cue.IntKind, cue.FloatKind, cue.NumberKind:
			str = fmt.Sprint(v)
		default:
			return nil, nil, errors.Newf(v.Pos(),
				"invalid environment variable value %q", v)
		}
		cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", label, str))
	}

	return cmd, append([]string{bin}, args...), nil
}
