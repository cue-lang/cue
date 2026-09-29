// Copyright 2026 CUE Authors
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

package task_test

import (
	"testing"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
	"cuelang.org/go/internal/task"
	"cuelang.org/go/tools/flow"
)

type testRunner struct {
	service bool
	run     func()
}

func (r testRunner) Run(t *flow.Task, err error) error {
	r.run()
	return nil
}

func (r testRunner) IsService() bool { return r.service }

func TestStartBackgroundWhenReady(t *testing.T) {
	v := cuecontext.New().CompileString(`
		serve: $id: "serve"
		slowServe: $id: "slowServe"
		client: $id: "client"
		waitingServe: {
			$id:    "waitingServe"
			$after: client
		}
	`)
	started := make(chan struct{})
	serveDone := make(chan struct{})
	releaseSlowServe := make(chan struct{})
	runners := map[string]testRunner{
		"serve": {service: true, run: func() {
			(&task.Context{}).BackgroundTask(func() { close(started) })
		}},
		"slowServe":    {service: true, run: func() { <-releaseSlowServe }},
		"client":       {run: func() { <-t.Context().Done() }},
		"waitingServe": {service: true, run: func() {}},
	}
	cfg := &flow.Config{
		UpdateFunc: func(c *flow.Controller, ft *flow.Task) error {
			err := task.StartBackgroundWhenReady(c, ft)
			if ft != nil && ft.Path().String() == "serve" {
				close(serveDone)
			}
			return err
		},
	}
	c := flow.New(cfg, v, func(v cue.Value) (flow.Runner, error) {
		id, err := v.LookupPath(cue.ParsePath("$id")).String()
		if err != nil {
			return nil, nil
		}
		return runners[id], nil
	})
	go c.Run(t.Context())

	// A service which is still running holds back background work.
	<-serveDone
	select {
	case <-started:
		t.Fatal("background work started while a service was still running")
	default:
	}

	// A service waiting on another task, which might be a client
	// of the services which already ran, does not hold it back.
	close(releaseSlowServe)
	<-started
}
