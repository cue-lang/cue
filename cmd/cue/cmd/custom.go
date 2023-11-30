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
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/spf13/cobra"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/internal/core/adt"
	"cuelang.org/go/internal/cueexperiment"
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
	str, err := obj.LookupPath(cue.MakePath(cue.Str(key))).String()
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
		sub, err := customCommand(c, commandSection, fields.Selector().Unquoted(), tools)
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
	cmds := tools.Lookup(typ)
	o := cmds.Lookup(name)
	if !o.Exists() {
		return nil, o.Err()
	}

	// Ensure there is at least one tool file.
	// TODO: remove this block to allow commands to be defined in any file.
	_, w := value.ToInternal(cmds)
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
	if !hasToolFile {
		// Note that earlier versions of this code checked cmds.Err in this scenario,
		// but it isn't clear why that was done, and we had no tests covering it.
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
		// Note that we don't use mkRunE here, as the parent func is already wrapped by
		// another mkRunE call, and all Command initialization has already happened.
		RunE: func(cmd *cobra.Command, args []string) error {
			// TODO:
			// - parse flags and env vars
			// - constrain current config with config section

			return doTasks(c, name, tools)
		},
	}

	// TODO: implement var/flag handling.
	return sub, nil
}

func doTasks(cmd *Command, command string, root *cue.Instance) error {
	cmdPath := cue.MakePath(cue.Str(commandSection), cue.Str(command))
	cfg := &flow.Config{
		Root:           cmdPath,
		InferTasks:     true,
		IgnoreConcrete: true,
	}

	// Command and task discovery
	//
	// It should clearly be an error if we attempt to invoke cue cmd with a path
	// expression that does not exist within the command struct:
	//
	//    $ cue cmd doesNotExist
	//    command: field not found: doesNotExist
	//
	// Less clear is the case that the command struct value referenced itself is
	// in error, for example when a top level field in that struct has an
	// incomplete field name:
	//
	//    input: string
	//    command: willFail: {
	//    	"\(input)": exec.Run & {cmd: "true"}
	//    }
	//    $ cue cmd willFail
	//    command.willFail: invalid interpolation: non-concrete value string (type string)
	//
	// For now, this is an error, but previous discussions (captured in
	// https://cuelang.org/issue/1325) have explored whether we can
	// safely/sensibly allow this, subject to the point/caveat below.
	//
	// It should also be an error when the command we have invoked declares no
	// tasks. It might be the command declares literally no tasks, or that the
	// CUE value it references is sufficiently incomplete:
	//
	//    input: string
	//    command: shouldFail: NESTED: {
	//    	"\(input)": exec.Run & {cmd: "true"}
	//    }
	//    $ cue cmd shouldFail
	//    command.shouldFail: no tasks found
	//
	// Note that the case of the RHS of a task being incomplete is different. We
	// care simply at this stage that tasks are discovered, sufficient for the
	// tools/flow runner to attempt to do some work. If none of the tasks can
	// proceed, that's a different kind of error. As a rather brute-force
	// measure until we address https://cuelang.org/issue/1325 we simply error
	// in case that we don't find any tasks.
	//
	// In case a command genuinely has no work to do, and wants to "do nothing"
	// we could trivially introduce a no-op task.
	//
	// All of this leaves a rather large grey space of cases/situations where
	// tasks are expected to be found and run, but don't because of missing
	// data, etc. https://cuelang.org/issue/1325 is being used as a place to
	// capture the nuance of those situations, and ways in which this UX could
	// be improved.

	ctx := itask.Context{
		TaskKey: taskKey,
		Root:    root.Value(),
		Stdin:   cmd.InOrStdin(),
		Stdout:  cmd.OutOrStdout(),
		Stderr:  cmd.OutOrStderr(),
	}

	var didWork atomic.Bool
	c := flow.New(cfg, root, ctx.TaskFunc(&didWork))

	// Return early if anything was in error
	if err := c.Run(cmd.Context()); err != nil {
		return err
	}

	if !didWork.Load() {
		return fmt.Errorf("%v: no tasks found", cmdPath)
	}

	return nil
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

	id := v.LookupPath(cue.MakePath(cue.Str("$id")))

	cueexperiment.Init()
	if !cueexperiment.Flags.CmdReferencePkg {
		// In the old mode, $id or kind being present is enough.
		if id.Exists() {
			return true
		}
		// Is it an existing legacy kind.
		str, err := v.Lookup("kind").String()
		_, ok := legacyKinds[str]
		return err == nil && ok
	}

	// In the new mode, $id must exist and be a reference to the hidden _id field from a tool package.
	// TODO: surely we can check this via id.BuildInstance().ImportPath, but it's not obvious how to do so.
	// Or perhaps add a method on cue.Value to get the package info directly, like the import path.
	if !id.Exists() {
		return false
	}
	fromToolsPackage := false
	id.Walk(func(v cue.Value) bool {
		if strings.HasPrefix(v.Pos().Filename(), "tool/") {
			fromToolsPackage = true
		}
		return true
	}, nil)
	return fromToolsPackage
}

var legacyKinds = map[string]string{
	"exec":       "tool/exec.Run",
	"http":       "tool/http.Do",
	"print":      "tool/cli.Print",
	"testserver": "cmd/cue/cmd.Test",
}

func taskKey(v cue.Value) (string, error) {
	if !isTask(v) {
		return "", nil
	}

	kind, err := v.Lookup("$id").String()
	if err != nil {
		// Lookup kind for backwards compatibility.
		// This should not be supported for cue run.
		var err1 error
		kind, err1 = v.Lookup("kind").String()
		if err1 != nil || legacyKinds[kind] == "" {
			return "", errors.Promote(err1, "newTask")
		}
	}

	if k, ok := legacyKinds[kind]; ok {
		kind = k
		err = itask.IsLegacy
	}
	return kind, err
}

func init() {
	itask.Register("cmd/cue/cmd.Test", newTestServerCmd)
}

var testServerOnce = sync.OnceValue(func() string {
	s := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, req *http.Request) {
			data, _ := io.ReadAll(req.Body)
			d := map[string]string{
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
