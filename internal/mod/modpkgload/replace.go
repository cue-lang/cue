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

package modpkgload

import (
	"strings"
	"unicode"

	"cuelang.org/go/mod/module"
)

// Replacement describes a single replacement directive.
type Replacement struct {
	// Module holds the replacement module version.
	// It is non-zero for module-version replacements.
	Module module.Version

	// Dir holds the replacement directory path.
	// It is non-empty for directory replacements.
	Dir string
}

// Replacements holds replacement directives and provides both forward
// lookups (original module path → replacement) and reverse lookups
// (replacement import path → canonical import path under the original module).
type Replacements struct {
	// forward maps from original module base path to its replacement.
	forward map[string]Replacement

	// reverse maps from replacement module base path to original module base path.
	// Only populated for module-version replacements (not directory replacements).
	reverse map[string]string
}

// NewReplacements creates a Replacements from a map keyed by original module
// base path (without major version suffix).
func NewReplacements(repls map[string]Replacement) *Replacements {
	if len(repls) == 0 {
		return nil
	}
	r := &Replacements{
		forward: make(map[string]Replacement, len(repls)),
		reverse: make(map[string]string),
	}
	for origBase, repl := range repls {
		r.forward[origBase] = repl
		if repl.Module.IsValid() {
			r.reverse[repl.Module.BasePath()] = origBase
		}
	}
	return r
}

// Lookup returns the replacement for the given module base path (without
// major version suffix), or ok=false if there is no replacement.
func (r *Replacements) Lookup(moduleBasePath string) (Replacement, bool) {
	if r == nil {
		return Replacement{}, false
	}
	repl, ok := r.forward[moduleBasePath]
	return repl, ok
}

// CanonicalImportPath rewrites importPath if it falls under a replacement
// module's namespace. For example, if original module "a.com/foo" is replaced
// by "b.com/bar", then "b.com/bar/subpkg" is rewritten to "a.com/foo/subpkg".
//
// If no rewriting is needed, importPath is returned unchanged.
func (r *Replacements) CanonicalImportPath(importPath string) string {
	if r == nil {
		return importPath
	}
	// Try each prefix of the import path to see if it matches
	// a replacement module's base path.
	for p := importPath; ; {
		if origBase, ok := r.reverse[p]; ok {
			if len(importPath) > len(p) {
				return origBase + importPath[len(p):]
			}
			return origBase
		}
		i := strings.LastIndex(p, "/")
		if i < 0 {
			break
		}
		p = p[:i]
	}
	return importPath
}

// IsReplaceDirectoryPath reports whether the given string looks like a
// filesystem path (as opposed to a module path with version).
// A value is a directory path if it starts with ".", "/" or matches a
// Windows absolute path (drive letter followed by colon).
func IsReplaceDirectoryPath(s string) bool {
	if strings.HasPrefix(s, ".") || strings.HasPrefix(s, "/") {
		return true
	}
	// Check for Windows absolute path: letter followed by ':'
	if len(s) >= 2 && s[1] == ':' && unicode.IsLetter(rune(s[0])) {
		return true
	}
	return false
}

// ParseReplaceValue parses a replace directive value string into a Replacement.
// The value is either a directory path or a module path with version
// (e.g. "example.com/bar@v0.1.0").
func ParseReplaceValue(s string) (Replacement, error) {
	if IsReplaceDirectoryPath(s) {
		return Replacement{Dir: s}, nil
	}
	mv, err := module.ParseVersion(s)
	if err != nil {
		return Replacement{}, err
	}
	return Replacement{Module: mv}, nil
}
