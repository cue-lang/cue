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

import (
	"unicode/utf8"

	"cuelang.org/go/cue/ast"
)

// defaultTabWidth is the assumed visual column width of a `"\t"`
// indent string when IndentWidth is not set explicitly. Four matches
// the convention `gofmt` and most editors use for displaying tabs.
const defaultTabWidth = 4

// Config controls the behavior of the pretty printer.
type Config struct {
	// Indent is the string emitted for one level of indentation. The
	// empty string disables indentation entirely. Common choices are
	// "\t" for tabs and a fixed run of spaces (e.g. "  ") for space-
	// based indentation.
	Indent string

	// IndentWidth is the visual column width of one indent level used
	// by the line-breaking heuristics. When greater than zero it is
	// used unchanged. When zero or negative, IndentWidth is inferred
	// from Indent: "" gives 0, "\t" gives defaultTabWidth, and any
	// other string gives its rune count.
	IndentWidth int

	// Width is the target line width for line-breaking decisions. If
	// zero, defaults to 120.
	Width int
}

func (cfg *Config) indentWidth() int {
	if cfg.IndentWidth > 0 {
		return cfg.IndentWidth
	}
	switch cfg.Indent {
	case "":
		return 0
	case "\t":
		return defaultTabWidth
	}
	return utf8.RuneCountInString(cfg.Indent)
}

func (cfg *Config) width() int {
	if cfg.Width == 0 {
		return 120
	}
	return cfg.Width
}

// Node formats an AST node as pretty-printed bytes.
func (cfg *Config) Node(n ast.Node) []byte {
	var c converter
	return cfg.Render(c.node(n))
}
