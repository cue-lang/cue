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
	"context"
	"encoding/json"
	"io"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"

	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/internal"
	itask "cuelang.org/go/internal/task"
	"cuelang.org/go/internal/walk"
	_ "cuelang.org/go/pkg/tool/cli" // Register tasks
	_ "cuelang.org/go/pkg/tool/exec"
	_ "cuelang.org/go/pkg/tool/file"
	_ "cuelang.org/go/pkg/tool/http"
	_ "cuelang.org/go/pkg/tool/os"
)

const (
	commandSection = "command"
)

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

// Variables used for testing.
var (
	stdout io.Writer = os.Stdout
	stderr io.Writer = os.Stderr
)

func addCustom(c *Command, parent *cobra.Command, typ, name string, tools *cue.Instance) (*cobra.Command, error) {
	if tools == nil {
		return nil, errors.New("no commands defined")
	}

	// TODO: validate allowing incomplete.
	o := tools.Lookup(typ, name)
	if !o.Exists() {
		return nil, o.Err()
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
	parent.AddCommand(sub)

	// TODO: implement var/flag handling.
	return sub, nil
}

type customRunner struct {
	name string
	root *cue.Instance

	tasks []*task
	index map[taskKey]*task

	allErrors errors.Error
}

type taskKey string

func (r *customRunner) keyForTask(t *task) taskKey {
	return keyForReference(t.path...)
}

func keyForReference(ref ...string) (k taskKey) {
	return taskKey(strings.Join(ref, "\000") + "\000")
}

func doTasks(cmd *Command, typ, command string, root *cue.Instance) error {
	err := executeTasks(cmd, typ, command, root)
	exitIfErr(cmd, root, err, true)
	return err
}

func (r *customRunner) insert(stack []string, v cue.Value) *task {
	t, err := newTask(stack, v)
	if err != nil {
		r.allErrors = errors.Append(r.allErrors, err)
		return nil
	}
	t.index = len(r.tasks)
	r.tasks = append(r.tasks, t)
	r.index[r.keyForTask(t)] = t
	return t
}

func (r *customRunner) tagReference(t *task, ref cue.Value) error {
	inst, path := ref.Reference()
	if len(path) == 0 {
		return errors.Newf(ref.Pos(),
			"$after must be a reference or list of references, found %s", ref)
	}
	if inst != r.root {
		return errors.Newf(ref.Pos(),
			"reference in $after must refer to value in same package")
	}
	// TODO: allow referring to group of tasks.
	if !r.tagDependencies(t, path) {
		return errors.Newf(ref.Pos(),
			"reference %s does not refer to task or task group",
			strings.Join(path, "."), // TODO: more correct representation.
		)

	}
	return nil
}

// tagDependencies marks dependencies in t correpsoning to ref
func (r *customRunner) tagDependencies(t *task, ref []string) bool {
	found := false
	prefix := keyForReference(ref...)
	for key, task := range r.index {
		if strings.HasPrefix(string(key), string(prefix)) {
			found = true
			t.dep[task] = true
		}
	}
	if found {
		return true
	}

	v := r.root.Lookup(ref...)
	if isTask(v) {
		if task := r.insert(ref, v); task != nil {
			t.dep[task] = true
			return true
		}
	}

	return false
}

func (r *customRunner) findTask(ref []string) *task {
	for ref := ref; len(ref) > 0; ref = ref[:len(ref)-1] {
		if t := r.index[keyForReference(ref...)]; t != nil {
			return t
		}
	}
	for ref := ref; len(ref) > 0; ref = ref[:len(ref)-1] {
		v := r.root.Lookup(ref...)
		if isTask(v) {
			return r.insert(ref, v)
		}
	}
	return nil
}

func isTask(v cue.Value) bool {
	return v.Kind() == cue.StructKind &&
		(v.Lookup("$id").Exists() || v.Lookup("kind").Exists())
}

func (r *customRunner) getTasks(v cue.Value, stack []string) {
	// Allow non-task values, but do not allow errors.
	if err := v.Err(); err != nil {
		r.allErrors = errors.Append(r.allErrors, errors.Promote(err, "getTasks"))
		return
	}
	if v.Kind()&cue.StructKind == 0 {
		return
	}

	if isTask(v) {
		_ = r.insert(stack, v)
		return
	}

	for iter, _ := v.Fields(); iter.Next(); {
		l := iter.Label()
		if strings.HasPrefix(l, "$") || l == "command" || l == "commands" {
			continue
		}
		r.getTasks(iter.Value(), append(stack, l))
		if r.allErrors != nil {
			return
		}
	}
}

// executeTasks runs user-defined tasks as part of a user-defined command.
//
// All tasks are started at once, but will block until tasks that they depend
// on will continue.
func executeTasks(cmd *Command, typ, command string, inst *cue.Instance) (err error) {
	cr := &customRunner{
		name:  command,
		root:  inst,
		index: map[taskKey]*task{},
	}

	// Create task entries from spec.
	base := []string{commandSection, cr.name}
	cr.getTasks(cr.root.Lookup(base...), base)
	if cr.allErrors != nil {
		return cr.allErrors
	}

	// Mark dependencies for unresolved nodes. Note that cr.tasks may grow
	// during iteration, which is why we don't use range.
	for i := 0; i < len(cr.tasks); i++ {
		t := cr.tasks[i]

		task := cr.root.Lookup(t.path...)

		// Inject dependency in `$after` field
		after := task.Lookup("$after")
		if after.Err() == nil {
			if after.Kind() != cue.ListKind {
				err = cr.tagReference(t, after)
			} else {
				for iter, _ := after.List(); iter.Next(); {
					err = cr.tagReference(t, iter.Value())
					exitIfErr(cmd, inst, err, true)
				}
			}
			exitIfErr(cmd, inst, err, true)
		}

		visited := make(map[string]bool)
		task.Walk(func(v cue.Value) bool {
			if v == task {
				return true
			}

			// Prevent inifinite walks
			_, vPath := v.Reference()
			if vPath != nil {
				vPath := string(keyForReference(vPath...))
				_, isVisited := visited[vPath]
				if isVisited {
					return false
				}
				visited[vPath] = true
			}

			for _, r := range appendReferences(nil, cr.root, v) {
				if dep := cr.findTask(r); dep != nil && t != dep {
					// TODO(string): consider adding dependencies
					// unconditionally here.
					// Something like IsFinal would be the right semantics here.
					v := cr.root.Lookup(r...)
					if !v.IsConcrete() && v.Kind() != cue.StructKind {
						t.dep[dep] = true
					}
				}
			}
			return true
		}, nil)
	}
	if cr.allErrors != nil {
		return cr.allErrors
	}

	if isCyclic(cr.tasks) {
		return errors.New("cyclic dependency in tasks") // TODO: better message.
	}

	ctx := context.Background()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var m sync.Mutex

	g, ctx := errgroup.WithContext(ctx)
	for _, t := range cr.tasks {
		t := t
		g.Go(func() error {
			for d := range t.dep {
				<-d.done
			}
			defer close(t.done)
			// TODO: This can be done concurrently once it is verified that this
			// code does not look up new strings in the index and that the
			// full configuration, as used by the tasks, is pre-evaluated.
			m.Lock()
			obj := cr.root.Lookup(t.path...)
			// NOTE: ignore the linter warning for the following line:
			// itask.Context is an internal type and we want to break if any
			// fields are added.
			c := &itask.Context{ctx, stdin, stdout, stderr, obj, nil}
			update, err := t.Run(c)
			if c.Err != nil {
				err = c.Err
			}
			if err == nil && update != nil {
				cr.root, err = cr.root.Fill(update, t.path...)
			}
			m.Unlock()

			if err != nil {
				cancel()
			}
			return err
		})
	}
	return g.Wait()
}

func appendReferences(a [][]string, root *cue.Instance, v cue.Value) [][]string {
	inst, path := v.Reference()
	if path != nil && inst == root {
		a = append(a, path)
		return a
	}

	switch op, args := v.Expr(); op {
	case cue.NoOp:
		walk.Value(v, &walk.Config{
			Opts: []cue.Option{cue.All()},
			After: func(w cue.Value) {
				if v != w {
					a = appendReferences(a, root, w)
				}
			},
		})
	default:
		for _, arg := range args {
			a = appendReferences(a, root, arg)
		}
	}
	return a
}

func isCyclic(tasks []*task) bool {
	cc := cycleChecker{
		visited: make([]bool, len(tasks)),
		stack:   make([]bool, len(tasks)),
	}
	for _, t := range tasks {
		if cc.isCyclic(t) {
			return true
		}
	}
	return false
}

type cycleChecker struct {
	visited, stack []bool
}

func (cc *cycleChecker) isCyclic(t *task) bool {
	i := t.index
	if !cc.visited[i] {
		cc.visited[i] = true
		cc.stack[i] = true

		for d := range t.dep {
			if !cc.visited[d.index] && cc.isCyclic(d) {
				return true
			} else if cc.stack[d.index] {
				return true
			}
		}
	}
	cc.stack[i] = false
	return false
}

type task struct {
	itask.Runner

	index int
	path  []string
	done  chan error
	dep   map[*task]bool
}

var legacyKinds = map[string]string{
	"exec":       "tool/exec.Run",
	"http":       "tool/http.Do",
	"print":      "tool/cli.Print",
	"testserver": "cmd/cue/cmd.Test",
}

func newTask(path []string, v cue.Value) (*task, errors.Error) {
	kind, err := v.Lookup("$id").String()
	if err != nil {
		// Lookup kind for backwards compatibility.
		// TODO: consider at some point whether kind can be removed.
		var err1 error
		kind, err1 = v.Lookup("kind").String()
		if err1 != nil {
			return nil, errors.Promote(err1, "newTask")
		}
	}
	if k, ok := legacyKinds[kind]; ok {
		kind = k
	}
	rf := itask.Lookup(kind)
	if rf == nil {
		return nil, errors.Newf(v.Pos(), "runner of kind %q not found", kind)
	}

	// Verify entry against template.
	v = internal.UnifyBuiltin(v, kind).(cue.Value)
	if err := v.Err(); err != nil {
		return nil, errors.Promote(err, "newTask")
	}

	runner, err := rf(v)
	if err != nil {
		return nil, errors.Promote(err, "errors running task")
	}
	return &task{
		Runner: runner,
		path:   append([]string{}, path...), // make a copy.
		done:   make(chan error),
		dep:    make(map[*task]bool),
	}, nil
}

func init() {
	itask.Register("cmd/cue/cmd.Test", newTestServerCmd)
}

var testOnce sync.Once

func newTestServerCmd(v cue.Value) (itask.Runner, error) {
	server := ""
	testOnce.Do(func() {
		s := httptest.NewServer(http.HandlerFunc(
			func(w http.ResponseWriter, req *http.Request) {
				data, _ := ioutil.ReadAll(req.Body)
				d := map[string]interface{}{
					"data": string(data),
					"when": "now",
				}
				enc := json.NewEncoder(w)
				_ = enc.Encode(d)
			}))
		server = s.URL
	})
	return testServerCmd(server), nil
}

type testServerCmd string

func (s testServerCmd) Run(ctx *itask.Context) (x interface{}, err error) {
	return map[string]interface{}{"url": string(s)}, nil
}
