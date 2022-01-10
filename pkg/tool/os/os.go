// Copyright 2022 CUE Authors
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

package os

import (
	"os"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/internal/task"
)

func init() {
	task.Register("tool/os.Mkdir", newMkdirCmd)
}

type mkdirCmd struct{}

func newMkdirCmd(v cue.Value) (task.Runner, error) {
	return &mkdirCmd{}, nil
}

func (c *mkdirCmd) Run(ctx *task.Context) (res interface{}, err error) {
	path := ctx.String("path")

	if ctx.Err != nil {
		return nil, ctx.Err
	}

	var cParents bool
	createParents := ctx.Lookup("createParents")
	if createParents.Exists() {
		createParents, _ = createParents.Default()
		cParents, _ = createParents.Bool()
	}

	if cParents {
		if err := os.MkdirAll(path, 0755); err != nil {
			return nil, errors.Wrapf(err, ctx.Obj.Pos(), "failed to create dir")
		}
	} else {
		dir, err := os.Stat(path)
		if err == nil {
			if dir.IsDir() {
				return nil, nil
			}
		}
		if err := os.Mkdir(path, 0755); err != nil {
			return nil, errors.Wrapf(err, ctx.Obj.Pos(), "failed to create dir")
		}
	}

	return nil, nil
}
