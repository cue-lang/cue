// Copyright 2018 The CUE Authors
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

package cmd

// This file contains code or initializing and running custom commands.

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"

	"github.com/spf13/cobra"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/internal/core/adt"
	itask "cuelang.org/go/internal/task"
	"cuelang.org/go/internal/value"
	_ "cuelang.org/go/pkg/tool/cli" // Register tasks
	_ "cuelang.org/go/pkg/tool/exec"
	_ "cuelang.org/go/pkg/tool/file"
	_ "cuelang.org/go/pkg/tool/http"
	_ "cuelang.org/go/pkg/tool/os"
	"cuelang.org/go/tools/flow"
)

const commandSection = "command"

func lookupString(obj cue.Value, key, def string) string {
	str, err := obj.Lookup(key).String()
	if err == nil {
		def = str
	}
	return strings.TrimSpace(def)
}

// splitLine splits the first line and the rest of the string.
func splitLine(s string) (line, tail string) {
	line = s
	if p := strings.IndexByte(s, '\n'); p >= 0 {
		line, tail = strings.TrimSpace(s[:p]), strings.TrimSpace(s[p+1:])
	}
	return
}

// addCustomCommands iterates over all commands defined under field typ
// and adds them as cobra subcommands to cmd.
// The func is only used in `cue help cmd`, which doesn't show errors.
func addCustomCommands(c *Command, cmd *cobra.Command, typ string, tools *cue.Instance) {
	commands := tools.Lookup(typ)
	if !commands.Exists() {
		return
	}
	fields, err := commands.Fields()
	if err != nil {
		return
	}
	for fields.Next() {
		sub, err := customCommand(c, commandSection, fields.Label(), tools)
		if err == nil {
			cmd.AddCommand(sub)
		}
	}
}

// customCommand creates a cobra.Command out of a CUE command definition.
func customCommand(c *Command, typ, name string, tools *cue.Instance) (*cobra.Command, error) {
	if tools == nil {
		return nil, errors.New("no commands defined")
	}

	// TODO: validate allowing incomplete.
	o := tools.Lookup(typ, name)
	if !o.Exists() {
		return nil, o.Err()
	}

	// Ensure there is at least one tool file.
	// TODO: remove this block to allow commands to be defined in any file.
	for _, v := range []cue.Value{tools.Lookup(typ), o} {
		_, w := value.ToInternal(v)
		hasToolFile := false
		w.VisitLeafConjuncts(func(c adt.Conjunct) bool {
			src := c.Source()
			if src == nil {
				return true
			}
			if strings.HasSuffix(src.Pos().Filename(), "_tool.cue") {
				hasToolFile = true
				return false
			}
			return true
		})
		if hasToolFile {
			break
		}
		if err := v.Err(); err != nil {
			return nil, err
		}
		return nil, errors.Newf(token.NoPos, "could not find command %q", name)
	}

	docs := o.Doc()
	var usage, short, long string
	if len(docs) > 0 {
		txt := docs[0].Text()
		short, txt = splitLine(txt)
		short = lookupString(o, "short", short)
		if strings.HasPrefix(txt, "Usage:") {
			usage, txt = splitLine(txt[len("Usage:"):])
		}
		usage = lookupString(o, "usage", usage)
		usage = lookupString(o, "$usage", usage)
		long = lookupString(o, "long", txt)
	}
	if !strings.HasPrefix(usage, name+" ") {
		usage = name
	}
	sub := &cobra.Command{
		Use:   usage,
		Short: lookupString(o, "$short", short),
		Long:  lookupString(o, "$long", long),
		RunE: mkRunE(c, func(cmd *Command, args []string) error {
			// TODO:
			// - parse flags and env vars
			// - constrain current config with config section

			return doTasks(cmd, typ, name, tools)
		}),
	}

	// TODO: implement var/flag handling.
	return sub, nil
}

func doTasks(cmd *Command, typ, command string, root *cue.Instance) error {
	cfg := &flow.Config{
		Root:           cue.MakePath(cue.Str(commandSection), cue.Str(command)),
		InferTasks:     true,
		IgnoreConcrete: true,
	}

	c := flow.New(cfg, root, newTaskFunc(cmd))

	err := c.Run(backgroundContext())
	exitOnErr(cmd, err, true)

	return err
}

// func (r *customRunner) tagReference(t *task, ref cue.Value) error {
// 	inst, path := ref.Reference()
// 	if len(path) == 0 {
// 		return errors.Newf(ref.Pos(),
// 			"$after must be a reference or list of references, found %s", ref)
// 	}
// 	if inst != r.root {
// 		return errors.Newf(ref.Pos(),
// 			"reference in $after must refer to value in same package")
// 	}
// 	// TODO: allow referring to group of tasks.
// 	if !r.tagDependencies(t, path) {
// 		return errors.Newf(ref.Pos(),
// 			"reference %s does not refer to task or task group",
// 			strings.Join(path, "."), // TODO: more correct representation.
// 		)

// 	}
// 	return nil
// }

func isTask(v cue.Value) bool {
	// This mimics the v0.2 behavior. The cutoff is really quite arbitrary. A
	// sane implementation should not use InferTasks, really.
	if len(v.Path().Selectors()) == 0 {
		return false
	}
	if v.Kind() != cue.StructKind {
		return false
	}
	if v.Lookup("$id").Exists() {
		return true
	}
	// Is it an existing legacy kind.
	str, err := v.Lookup("kind").String()
	_, ok := legacyKinds[str]
	return err == nil && ok
}

var legacyKinds = map[string]string{
	"exec":       "tool/exec.Run",
	"http":       "tool/http.Do",
	"print":      "tool/cli.Print",
	"testserver": "cmd/cue/cmd.Test",
}

func newTaskFunc(cmd *Command) flow.TaskFunc {
	return func(v cue.Value) (flow.Runner, error) {
		if !isTask(v) {
			return nil, nil
		}

		kind, err := v.Lookup("$id").String()
		if err != nil {
			// Lookup kind for backwards compatibility.
			// This should not be supported for cue run.
			var err1 error
			kind, err1 = v.Lookup("kind").String()
			if err1 != nil || legacyKinds[kind] == "" {
				return nil, errors.Promote(err1, "newTask")
			}
		}
		var isLegacy bool
		if k, ok := legacyKinds[kind]; ok {
			kind = k
			isLegacy = true
		}
		rf := itask.Lookup(kind)
		if rf == nil {
			return nil, errors.Newf(v.Pos(), "runner of kind %q not found", kind)
		}

		// Verify entry against template.
		v = value.UnifyBuiltin(v, kind)
		if err := v.Err(); err != nil {
			err = v.Validate()
			return nil, errors.Promote(err, "newTask")
		}

		runner, err := rf(v)
		if err != nil {
			return nil, errors.Promote(err, "errors running task")
		}

		return flow.RunnerFunc(func(t *flow.Task) error {
			obj := t.Value()

			if isLegacy {
				obj = obj.Unify(v)
			}
			c := &itask.Context{
				Context: t.Context(),
				Stdin:   cmd.InOrStdin(),
				Stdout:  cmd.OutOrStdout(),
				Stderr:  cmd.OutOrStderr(),
				Obj:     obj,
			}
			value, err := runner.Run(c)
			if err != nil {
				return err
			}
			if value != nil {
				_ = t.Fill(value)
			}
			return nil
		}), nil
	}
}

func init() {
	itask.Register("cmd/cue/cmd.Test", newTestServerCmd)
}

var testServerOnce = sync.OnceValue(func() string {
	s := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, req *http.Request) {
			data, _ := io.ReadAll(req.Body)
			d := map[string]interface{}{
				"data": string(data),
				"when": "now",
			}
			enc := json.NewEncoder(w)
			_ = enc.Encode(d)
		}))
	return s.URL
})

func newTestServerCmd(v cue.Value) (itask.Runner, error) {
	return testServerCmd(testServerOnce()), nil
}

type testServerCmd string

func (s testServerCmd) Run(ctx *itask.Context) (x interface{}, err error) {
	return map[string]interface{}{"url": string(s)}, nil
}
