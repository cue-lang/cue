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

package flow_test

import (
	"context"
	"fmt"
	"os"
	"path"
	"strings"
	"sync"
	"testing"
	"time"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/format"
	"cuelang.org/go/cue/stats"
	"cuelang.org/go/internal/cuetxtar"
	"cuelang.org/go/tools/flow"
)

// TestTasks tests the logic that determines which nodes are tasks and what are
// their dependencies.
func TestFlow(t *testing.T) {
	test := cuetxtar.TxTarTest{
		Root: "./testdata",
		Name: "run",
	}

	test.Run(t, func(t *cuetxtar.Test) {
		v := cuecontext.New().BuildInstance(t.Instance())
		if err := v.Err(); err != nil {
			t.Fatal(err)
		}

		seqNum = 0

		var tasksTotal stats.Counts

		updateFunc := func(c *flow.Controller, task *flow.Task) error {
			str := flow.MermaidGraph(c)
			step := fmt.Sprintf("t%d", seqNum)
			fmt.Fprintln(t.Writer(step), str)

			if task != nil {
				n := task.Value().Syntax(cue.Final())
				b, err := format.Node(n)
				if err != nil {
					t.Fatal(err)
				}
				fmt.Fprintln(t.Writer(path.Join(step, "value")), string(b))

				stats := task.Stats()
				tasksTotal.Add(stats)
				fmt.Fprintln(t.Writer(path.Join(step, "stats")), &stats)
			}

			incSeqNum()

			return nil
		}

		cfg := &flow.Config{
			Root:            cue.ParsePath("root"),
			InferTasks:      t.Bool("InferTasks"),
			IgnoreConcrete:  t.Bool("IgnoreConcrete"),
			FindHiddenTasks: t.Bool("FindHiddenTasks"),
			UpdateFunc:      updateFunc,
		}

		c := flow.New(cfg, v, taskFunc)

		w := t.Writer("errors")
		if err := c.Run(context.Background()); err != nil {
			cwd, _ := os.Getwd()
			fmt.Fprint(w, "error: ")
			errors.Print(w, err, &errors.Config{
				Cwd:     cwd,
				ToSlash: true,
			})
		}

		totals := c.Stats()
		if tasksTotal != zeroStats && totals != tasksTotal {
			t.Errorf(diffMsg, tasksTotal, totals, tasksTotal.Since(totals))
		}
		fmt.Fprintln(t.Writer("stats/totals"), totals)
	})
}

var zeroStats stats.Counts

const diffMsg = `
stats: task totals differens from controller:
task totals:
%v

controller totals:
%v

task totals - controller totals:
%v`

func TestFlowValuePanic(t *testing.T) {
	f := `
    root: {
        a: {
            $id: "slow"
            out: string
        }
        b: {
            $id:    "slow"
            $after: a
            out:    string
        }
    }
    `
	ctx := cuecontext.New()
	v := ctx.CompileString(f)

	ch := make(chan bool, 1)

	cfg := &flow.Config{
		Root: cue.ParsePath("root"),
		UpdateFunc: func(c *flow.Controller, t *flow.Task) error {
			ch <- true
			return nil
		},
	}

	c := flow.New(cfg, v, taskFunc)

	defer func() { recover() }()

	go c.Run(context.TODO())

	// Call Value amidst two task runs. This should trigger a panic as the flow
	// is not terminated.
	<-ch
	c.Value()
	<-ch

	t.Errorf("Value() did not panic")
}

func taskFunc(v cue.Value) (flow.Runner, error) {
	switch name, err := v.Lookup("$id").String(); name {
	default:
		if err == nil {
			return flow.RunnerFunc(func(t *flow.Task) error {
				t.Fill(map[string]string{"stdout": "foo"})
				return nil
			}), nil
		}
		if err != nil && v.Lookup("$id").Exists() {
			return nil, err
		}

	case "valToOut":
		return flow.RunnerFunc(func(t *flow.Task) error {
			if str, err := t.Value().Lookup("val").String(); err == nil {
				t.Fill(map[string]string{"out": str})
			}
			return nil
		}), nil

	case "failure":
		return flow.RunnerFunc(func(t *flow.Task) error {
			return errors.New("failure")
		}), nil

	case "abort":
		return flow.RunnerFunc(func(t *flow.Task) error {
			return flow.ErrAbort
		}), nil

	case "list":
		return flow.RunnerFunc(func(t *flow.Task) error {
			t.Fill(map[string][]int{"out": {1, 2}})
			return nil
		}), nil

	case "slow":
		return flow.RunnerFunc(func(t *flow.Task) error {
			time.Sleep(10 * time.Millisecond)
			t.Fill(map[string]string{"out": "finished"})
			return nil
		}), nil

	case "sequenced":
		// This task is used to serialize different runners in case
		// non-deterministic scheduling is possible.
		return flow.RunnerFunc(func(t *flow.Task) error {
			seq, err := t.Value().Lookup("seq").Int64()
			if err != nil {
				return err
			}

			waitSeqNum(seq)

			if str, err := t.Value().Lookup("val").String(); err == nil {
				t.Fill(map[string]string{"out": str})
			}

			return nil
		}), nil
	}
	return nil, nil
}

// These vars are used to serialize tasks that are run in parallel. This allows
// for testing running tasks in parallel, while obtaining deterministic output.
var (
	seqNum  int64
	seqLock sync.Mutex
	seqCond = sync.NewCond(&seqLock)
)

func incSeqNum() {
	seqCond.L.Lock()
	seqNum++
	seqCond.Broadcast()
	seqCond.L.Unlock()
}

func waitSeqNum(seq int64) {
	seqCond.L.Lock()
	for seq != seqNum {
		seqCond.Wait()
	}
	seqCond.L.Unlock()
}

// DO NOT REMOVE: for testing purposes.
func TestX(t *testing.T) {
	in := `
	`

	if strings.TrimSpace(in) == "" {
		t.Skip()
	}

	rt := cuecontext.New()
	v := rt.CompileString(in)
	if err := v.Err(); err != nil {
		t.Fatal(err)
	}

	c := flow.New(&flow.Config{
		Root: cue.ParsePath("root"),
		UpdateFunc: func(c *flow.Controller, ft *flow.Task) error {
			if ft != nil {
				t.Errorf("\nTASK:\n%s", ft.Stats())
			}
			return nil
		},
	}, v, taskFunc)

	t.Error(flow.MermaidGraph(c))

	if err := c.Run(context.Background()); err != nil {
		t.Fatal(errors.Details(err, nil))
	}

	t.Errorf("\nCONTROLLER:\n%s", c.Stats())
}
