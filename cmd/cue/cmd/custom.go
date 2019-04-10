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
	"os/exec"
	"strings"
	"sync"

	"cuelang.org/go/cue"
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
	if err := executeTasks(typ, command, root); err != nil {
		exitIfErr(cmd, root, err, true)
	}
	return nil
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
			update, err := t.Run(ctx, obj)
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
	Runner

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
	rf, ok := runners[kind]
	if !ok {
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

// A Runner defines a command type.
type Runner interface {
	// Init is called with the original configuration before any task is run.
	// As a result, the configuration may be incomplete, but allows some
	// validation before tasks are kicked off.
	// Init(v cue.Value)

	// Runner runs given the current value and returns a new value which is to
	// be unified with the original result.
	Run(ctx context.Context, v cue.Value) (results interface{}, err error)
}

// A RunnerFunc creates a Runner.
type RunnerFunc func(v cue.Value) (Runner, error)

var runners = map[string]RunnerFunc{
	"print":      newPrintCmd,
	"exec":       newExecCmd,
	"http":       newHTTPCmd,
	"testserver": newTestServerCmd,
}

type printCmd struct{}

func newPrintCmd(v cue.Value) (Runner, error) {
	return &printCmd{}, nil
}

// TODO: get rid of this hack
var testOut io.Writer

func (c *printCmd) Run(ctx context.Context, v cue.Value) (res interface{}, err error) {
	str, err := v.Lookup("text").String()
	if err != nil {
		return nil, err
	}
	if testOut != nil {
		fmt.Fprintln(testOut, str)
	} else {
		fmt.Println(str)
	}
	return nil, nil
}

type execCmd struct{}

func newExecCmd(v cue.Value) (Runner, error) {
	return &execCmd{}, nil
}

func (c *execCmd) Run(ctx context.Context, v cue.Value) (res interface{}, err error) {
	// TODO: set environment variables, if defined.
	var bin string
	var args []string
	switch v := v.Lookup("cmd"); v.Kind() {
	case cue.StringKind:
		str, _ := v.String()
		if str == "" {
			return cue.Value{}, errors.New("empty command")
		}
		list := strings.Fields(str)
		bin = list[0]
		for _, s := range list[1:] {
			args = append(args, s)
		}

	case cue.ListKind:
		list, _ := v.List()
		if !list.Next() {
			return cue.Value{}, errors.New("empty command list")
		}
		bin, err = list.Value().String()
		if err != nil {
			return cue.Value{}, err
		}
		for list.Next() {
			str, err := list.Value().String()
			if err != nil {
				return cue.Value{}, err
			}
			args = append(args, str)
		}
	}

	cmd := exec.CommandContext(ctx, bin, args...)

	if v := v.Lookup("stdin"); v.IsValid() {
		if cmd.Stdin, err = v.Reader(); err != nil {
			return nil, fmt.Errorf("cue: %v", err)
		}
	}
	captureOut := !v.Lookup("stdout").IsNull()
	if !captureOut {
		cmd.Stdout = os.Stdout
	}
	captureErr := !v.Lookup("stderr").IsNull()
	if captureErr {
		cmd.Stderr = os.Stderr
	}

	update := map[string]interface{}{}
	var stdout, stderr []byte
	if captureOut {
		stdout, err = cmd.Output()
		update["stdout"] = string(stdout)
	} else {
		err = cmd.Run()
	}
	update["success"] = err == nil
	if err != nil {
		if exit, ok := err.(*exec.ExitError); ok && captureErr {
			stderr = exit.Stderr
		} else {
			return nil, fmt.Errorf("cue: %v", err)
		}
	}
	if captureErr {
		update["stderr"] = string(stderr)
	}
	return update, nil
}

type httpCmd struct{}

func newHTTPCmd(v cue.Value) (Runner, error) {
	return &httpCmd{}, nil
}

func (c *httpCmd) Run(ctx context.Context, v cue.Value) (res interface{}, err error) {
	// v.Validate()
	var header, trailer http.Header
	method := lookupString(v, "method")
	u := lookupString(v, "url")
	var r io.Reader
	if obj := v.Lookup("request"); v.Exists() {
		if v := obj.Lookup("body"); v.Exists() {
			r, err = v.Reader()
			if err != nil {
				return nil, err
			}
		}
		if header, err = parseHeaders(obj, "header"); err != nil {
			return nil, err
		}
		if trailer, err = parseHeaders(obj, "trailer"); err != nil {
			return nil, err
		}
	}
	req, err := http.NewRequest(method, u, r)
	if err != nil {
		return nil, err
	}
	req.Header = header
	req.Trailer = trailer

	// TODO:
	//  - retry logic
	//  - TLS certs
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, err := ioutil.ReadAll(resp.Body)
	// parse response body and headers
	return map[string]interface{}{
		"response": map[string]interface{}{
			"body":    string(b),
			"header":  resp.Header,
			"trailer": resp.Trailer,
		},
	}, err
}

func parseHeaders(obj cue.Value, label string) (http.Header, error) {
	m := obj.Lookup(label)
	if !m.Exists() {
		return nil, nil
	}
	iter, err := m.Fields()
	if err != nil {
		return nil, err
	}
	var h http.Header
	for iter.Next() {
		str, err := iter.Value().String()
		if err != nil {
			return nil, err
		}
		h.Add(iter.Label(), str)
	}
	return h, nil
}

func isValid(v cue.Value) bool {
	return v.Kind() == cue.BottomKind
}

var testOnce sync.Once

func newTestServerCmd(v cue.Value) (Runner, error) {
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

func (s testServerCmd) Run(ctx context.Context, v cue.Value) (x interface{}, err error) {
	return map[string]interface{}{"url": string(s)}, nil
}
