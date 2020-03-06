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

package load

import (
	"bytes"
	"io/ioutil"
	"path/filepath"
	"strings"
	"unicode"

	"cuelang.org/go/cue/build"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/internal/filetypes"
)

// matchFileTest reports whether the file with the given name in the given directory
// matches the context and would be included in a Package created by ImportDir
// of that directory.
//
// matchFileTest considers the name of the file and may use cfg.Build.OpenFile to
// read some or all of the file's content.
func matchFileTest(cfg *Config, dir, name string) (match bool, err error) {
	file, err := filetypes.ParseFile(filepath.Join(dir, name), filetypes.Input)
	if err != nil {
		return false, nil
	}
	match, _, err = matchFile(cfg, file, false, false, nil)
	return
}

// matchFile determines whether the file with the given name in the given directory
// should be included in the package being constructed.
// It returns the data read from the file.
// If returnImports is true and name denotes a CUE file, matchFile reads
// until the end of the imports (and returns that data) even though it only
// considers text until the first non-comment.
// If allTags is non-nil, matchFile records any encountered build tag
// by setting allTags[tag] = true.
func matchFile(cfg *Config, file *build.File, returnImports, allFiles bool, allTags map[string]bool) (match bool, data []byte, err errors.Error) {
	if fi := cfg.fileSystem.getOverlay(file.Filename); fi != nil {
		if fi.file != nil {
			file.Source = fi.file
		} else {
			file.Source = fi.contents
		}
	}

	if file.Encoding != build.CUE {
		return
	}

	if file.Filename == "-" {
		b, err2 := ioutil.ReadAll(cfg.stdin())
		if err2 != nil {
			err = errors.Newf(token.NoPos, "read stdin: %v", err)
			return
		}
		file.Source = b
		data = b
		match = true // don't check shouldBuild for stdin
		return
	}

	name := filepath.Base(file.Filename)
	if !cfg.filesMode && strings.HasPrefix(name, ".") {
		return
	}

	if strings.HasPrefix(name, "_") {
		return
	}

	f, err := cfg.fileSystem.openFile(file.Filename)
	if err != nil {
		return
	}

	data, err = readImports(f, false, nil)
	f.Close()
	if err != nil {
		err = errors.Newf(token.NoPos, "read %s: %v", file.Filename, err)
		return
	}

	// Look for +build comments to accept or reject the file.
	if !shouldBuild(cfg, data, allTags) && !allFiles {
		return
	}

	match = true
	return
}

// shouldBuild reports whether it is okay to use this file,
// The rule is that in the file's leading run of // comments
// and blank lines, which must be followed by a blank line
// (to avoid including a Go package clause doc comment),
// lines beginning with '// +build' are taken as build directives.
//
// The file is accepted only if each such line lists something
// matching the file. For example:
//
//	// +build windows linux
//
// marks the file as applicable only on Windows and Linux.
//
// If shouldBuild finds a //go:binary-only-package comment in the file,
// it sets *binaryOnly to true. Otherwise it does not change *binaryOnly.
//
func shouldBuild(cfg *Config, content []byte, allTags map[string]bool) bool {
	// Pass 1. Identify leading run of // comments and blank lines,
	// which must be followed by a blank line.
	end := 0
	p := content
	for len(p) > 0 {
		line := p
		if i := bytes.IndexByte(line, '\n'); i >= 0 {
			line, p = line[:i], p[i+1:]
		} else {
			p = p[len(p):]
		}
		line = bytes.TrimSpace(line)
		if len(line) == 0 { // Blank line
			end = len(content) - len(p)
			continue
		}
		if !bytes.HasPrefix(line, slashslash) { // Not comment line
			break
		}
	}
	content = content[:end]

	// Pass 2.  Process each line in the run.
	p = content
	allok := true
	for len(p) > 0 {
		line := p
		if i := bytes.IndexByte(line, '\n'); i >= 0 {
			line, p = line[:i], p[i+1:]
		} else {
			p = p[len(p):]
		}
		line = bytes.TrimSpace(line)
		if bytes.HasPrefix(line, slashslash) {
			line = bytes.TrimSpace(line[len(slashslash):])
			if len(line) > 0 && line[0] == '+' {
				// Looks like a comment +line.
				f := strings.Fields(string(line))
				if f[0] == "+build" {
					ok := false
					for _, tok := range f[1:] {
						if doMatch(cfg, tok, allTags) {
							ok = true
						}
					}
					if !ok {
						allok = false
					}
				}
			}
		}
	}

	return allok
}

// doMatch reports whether the name is one of:
//
//	tag (if tag is listed in cfg.Build.BuildTags or cfg.Build.ReleaseTags)
//	!tag (if tag is not listed in cfg.Build.BuildTags or cfg.Build.ReleaseTags)
//	a comma-separated list of any of these
//
func doMatch(cfg *Config, name string, allTags map[string]bool) bool {
	if name == "" {
		if allTags != nil {
			allTags[name] = true
		}
		return false
	}
	if i := strings.Index(name, ","); i >= 0 {
		// comma-separated list
		ok1 := doMatch(cfg, name[:i], allTags)
		ok2 := doMatch(cfg, name[i+1:], allTags)
		return ok1 && ok2
	}
	if strings.HasPrefix(name, "!!") { // bad syntax, reject always
		return false
	}
	if strings.HasPrefix(name, "!") { // negation
		return len(name) > 1 && !doMatch(cfg, name[1:], allTags)
	}

	if allTags != nil {
		allTags[name] = true
	}

	// Tags must be letters, digits, underscores or dots.
	// Unlike in CUE identifiers, all digits are fine (e.g., "386").
	for _, c := range name {
		if !unicode.IsLetter(c) && !unicode.IsDigit(c) && c != '_' && c != '.' {
			return false
		}
	}

	// other tags
	for _, tag := range cfg.BuildTags {
		if tag == name {
			return true
		}
	}
	for _, tag := range cfg.releaseTags {
		if tag == name {
			return true
		}
	}

	return false
}
