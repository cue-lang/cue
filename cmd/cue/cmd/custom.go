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
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"

	"cuelang.org/go/cue"
	itask "cuelang.org/go/internal/task"
	_ "cuelang.org/go/pkg/tool/cli" // Register tasks
	_ "cuelang.org/go/pkg/tool/exec"
	_ "cuelang.org/go/pkg/tool/http"
	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"
)

const (
	commandSection = "command"
	taskSection    = "task"
)

func lookupString(obj cue.Value, key string) string {
	str, err := obj.Lookup(key).String()
	if err != nil {
		return ""
	}
	return str
}

// Variables used for testing.
var (
	stdout io.Writer = os.Stdout
	stderr io.Writer = os.Stderr
)

func addCustom(parent *cobra.Command, typ, name string, tools *cue.Instance) (*cobra.Command, error) {
	if tools == nil {
		return nil, errors.New("no commands defined")
	}

	// TODO: validate allowing incomplete.
	o := tools.Lookup(typ, name)
	if !o.Exists() {
		return nil, o.Err()
	}

	usage := lookupString(o, "usage")
	if usage == "" {
		usage = name
	}
	sub := &cobra.Command{
		Use:   usage,
		Short: lookupString(o, "short"),
		Long:  lookupString(o, "long"),
		RunE: func(cmd *cobra.Command, args []string) error {
			// TODO:
			// - parse flags and env vars
			// - constrain current config with config section

			return doTasks(cmd, typ, name, tools)
		},
	}
	parent.AddCommand(sub)

	// TODO: implement var/flag handling.
	return sub, nil
}

type taskKey struct {
	typ  string
	name string
	task string
}

func (k taskKey) keyForTask(taskName string) taskKey {
	k.task = taskName
	return k
}

func keyForReference(ref []string) (k taskKey) {
	// command <command> task <task>
	if len(ref) >= 4 && ref[2] == taskSection {
		k.typ = ref[0]
		k.name = ref[1]
		k.task = ref[3]
	}
	return k
}

func (k taskKey) taskPath(task string) []string {
	k.task = task
	return []string{k.typ, k.name, taskSection, task}
}

func (k *taskKey) lookupTasks(root *cue.Instance) cue.Value {
	return root.Lookup(k.typ, k.name, taskSection)
}

func doTasks(cmd *cobra.Command, typ, command string, root *cue.Instance) error {
	err := executeTasks(typ, command, root)
	exitIfErr(cmd, root, err, true)
	return err
}

// executeTasks runs user-defined tasks as part of a user-defined command.
//
// All tasks are started at once, but will block until tasks that they depend
// on will continue.
func executeTasks(typ, command string, root *cue.Instance) (err error) {
	spec := taskKey{typ, command, ""}
	tasks := spec.lookupTasks(root)

	index := map[taskKey]*task{}

	// Create task entries from spec.
	queue := []*task{}
	iter, err := tasks.Fields()
	if err != nil {
		return err
	}
	for i := 0; iter.Next(); i++ {
		t, err := newTask(i, iter.Label(), iter.Value())
		if err != nil {
			return err
		}
		queue = append(queue, t)
		index[spec.keyForTask(iter.Label())] = t
	}

	// Mark dependencies for unresolved nodes.
	for _, t := range queue {
		tasks.Lookup(t.name).Walk(func(v cue.Value) bool {
			// if v.IsIncomplete() {
			for _, r := range v.References() {
				if dep, ok := index[keyForReference(r)]; ok {
					v := root.Lookup(r...)
					if v.IsIncomplete() && v.Kind() != cue.StructKind {
						t.dep[dep] = true
					}
				}
			}
			// }
			return true
		}, nil)
	}

	if isCyclic(queue) {
		return errors.New("cyclic dependency in tasks") // TODO: better message.
	}

	ctx := context.Background()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var m sync.Mutex

	g, ctx := errgroup.WithContext(ctx)
	for _, t := range queue {
		t := t
		g.Go(func() error {
			for d := range t.dep {
				<-d.done
			}
			defer close(t.done)
			m.Lock()
			obj := tasks.Lookup(t.name)
			m.Unlock()
			update, err := t.Run(&itask.Context{ctx, stdout, stderr}, obj)
			if err == nil && update != nil {
				m.Lock()
				root, err = root.Fill(update, spec.taskPath(t.name)...)

				if err == nil {
					tasks = spec.lookupTasks(root)
				}
				m.Unlock()
			}
			if err != nil {
				cancel()
			}
			return err
		})
	}
	return g.Wait()
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
	name  string
	done  chan error
	dep   map[*task]bool
}

func newTask(index int, name string, v cue.Value) (*task, error) {
	kind, err := v.Lookup("kind").String()
	if err != nil {
		return nil, err
	}
	rf := itask.Lookup(kind)
	if rf == nil {
		return nil, fmt.Errorf("runner of kind %q not found", kind)
	}
	runner, err := rf(v)
	if err != nil {
		return nil, err
	}
	return &task{
		Runner: runner,
		index:  index,
		name:   name,
		done:   make(chan error),
		dep:    make(map[*task]bool),
	}, nil
}

func isValid(v cue.Value) bool {
	return v.Kind() == cue.BottomKind
}

func init() {
	itask.Register("testserver", newTestServerCmd)
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
				enc.Encode(d)
			}))
		server = s.URL
	})
	return testServerCmd(server), nil
}

type testServerCmd string

func (s testServerCmd) Run(ctx *itask.Context, v cue.Value) (x interface{}, err error) {
	return map[string]interface{}{"url": string(s)}, nil
}
