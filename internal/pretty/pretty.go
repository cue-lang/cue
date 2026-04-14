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

package pretty

import "cuelang.org/go/cue/ast"

// Config controls the behavior of the pretty printer.
type Config struct {
	// Indent is the string used for one level of indentation. Use "\t"
	// for tabs or a string of spaces (e.g. "  ") for space-based
	// indentation. If empty, defaults to "\t".
	Indent string

	// Width is the target line width for line-breaking decisions. If
	// zero, defaults to 80.
	Width int
}

func (cfg *Config) indent() string {
	if cfg.Indent == "" {
		return "\t"
	}
	return cfg.Indent
}

func (cfg *Config) width() int {
	if cfg.Width == 0 {
		return 80
	}
	return cfg.Width
}

// Node formats an AST node as pretty-printed bytes.
func (cfg *Config) Node(n ast.Node) []byte {
	var c converter
	return cfg.Render(c.node(n))
}
