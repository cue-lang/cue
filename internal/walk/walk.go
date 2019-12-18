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

// Package walk allows walking over CUE values.
//
// This package replicates Value.Walk in the cue package.
// There are several different internal uses of walk. This
// package is intended as a single implementation that on
// which all these implementations should converge. Once a
// satisfactory API has been established, it can be made public.
package walk

import "cuelang.org/go/cue"

// TODO:
// - allow overriding options for descendants.
//   Perhaps func(f cue.Value) *Config?
// - Get field information.

// Alternatives:
// type Visitor struct {}
// func (v *Visitor) Do(cue.Value)
// - less typing
// - two elements are grouped together in UI.

type Config struct {
	Before func(f cue.Value) bool
	After  func(f cue.Value)
	Opts   []cue.Option
}

func Value(v cue.Value, c *Config) {
	switch v.Kind() {
	case cue.StructKind:
		if c.Before != nil && !c.Before(v) {
			return
		}
		iter, _ := v.Fields(c.Opts...)
		for iter.Next() {
			Value(iter.Value(), c)
		}
	case cue.ListKind:
		if c.Before != nil && !c.Before(v) {
			return
		}
		list, _ := v.List()
		for list.Next() {
			Value(list.Value(), c)
		}
	default:
		if c.Before != nil {
			c.Before(v)
		}
	}
	if c.After != nil {
		c.After(v)
	}
}
