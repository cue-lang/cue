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

// Package file provides file operations for cue tasks.
package file

import (
	"io/ioutil"
	"os"
	"path/filepath"

	"cuelang.org/go/cue"
	"cuelang.org/go/internal/task"
)

func init() {
	task.Register("tool/file.Read", newReadCmd)
	task.Register("tool/file.Append", newAppendCmd)
	task.Register("tool/file.Create", newCreateCmd)
	task.Register("tool/file.Glob", newGlobCmd)
}

func newReadCmd(v cue.Value) (task.Runner, error)   { return &cmdRead{}, nil }
func newAppendCmd(v cue.Value) (task.Runner, error) { return &cmdAppend{}, nil }
func newCreateCmd(v cue.Value) (task.Runner, error) { return &cmdCreate{}, nil }
func newGlobCmd(v cue.Value) (task.Runner, error)   { return &cmdGlob{}, nil }

type cmdRead struct{}
type cmdAppend struct{}
type cmdCreate struct{}
type cmdGlob struct{}

func lookupStr(v cue.Value, str string) string {
	str, _ = v.Lookup(str).String()
	return str
}

func (c *cmdRead) Run(ctx *task.Context, v cue.Value) (res interface{}, err error) {
	b, err := ioutil.ReadFile(lookupStr(v, "filename"))
	if err != nil {
		return nil, err
	}
	update := map[string]interface{}{"contents": b}

	switch v.Lookup("contents").IncompleteKind() &^ cue.BottomKind {
	case cue.BytesKind:
	case cue.StringKind:
		update["contents"] = string(b)
	}
	return update, nil
}

func (c *cmdAppend) Run(ctx *task.Context, v cue.Value) (res interface{}, err error) {
	filename := lookupStr(v, "filename")
	mode, err := v.Lookup("permissions").Int64()
	if err != nil {
		return nil, err
	}

	f, err := os.OpenFile(filename, os.O_APPEND|os.O_WRONLY, os.FileMode(mode))
	if err != nil {
		return nil, err
	}
	defer f.Close()

	b, _ := v.Lookup("contents").Bytes()
	if _, err := f.Write(b); err != nil {
		return nil, err
	}
	return nil, nil
}

func (c *cmdCreate) Run(ctx *task.Context, v cue.Value) (res interface{}, err error) {
	filename := lookupStr(v, "filename")
	mode, err := v.Lookup("permissions").Int64()
	if err != nil {
		return nil, err
	}

	b, _ := v.Lookup("contents").Bytes()
	return nil, ioutil.WriteFile(filename, b, os.FileMode(mode))
}

func (c *cmdGlob) Run(ctx *task.Context, v cue.Value) (res interface{}, err error) {
	m, err := filepath.Glob(lookupStr(v, "glob"))
	return m, err
}
