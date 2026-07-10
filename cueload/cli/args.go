// Copyright 2026 The CUE Authors
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

package cli

// This file implements the command-line argument grammar: the
// classification of arguments into package patterns and files, and the
// file-qualifier syntax ("json:", "cue+schema:"). The grammar is ported
// from internal/filetypes; the qualifier vocabulary is reduced to what
// the public cuecodec surface can express: codec names and form tags.

import (
	"fmt"
	"path/filepath"
	"strings"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cuecodec"
	cuepath "cuelang.org/go/pkg/path"
)

// defaultCodecs is the codec set used to resolve file qualifiers and
// extensions.
//
// TODO(cueload): this should be the loader's codec set, but neither
// ParseArgs nor Command carries a loader, and cueload.Loader does not
// expose its configured *cuecodec.Set. Until it does, loaders extended
// beyond the default set (for example with cuecodec/toml) cannot name
// the extra codecs in qualifiers.
var defaultCodecs = cuecodec.Default()

// A fileArg is one file argument together with its resolved qualifier.
type fileArg struct {
	name string
	spec fileSpec
}

// A fileSpec is a parsed file qualifier.
type fileSpec struct {
	// codec names the file's format. It is always non-empty in a
	// resolved fileArg.
	codec string

	// form is the form tag, if any: "schema", "data", "graph" or
	// "dag". A data file marked "schema" acts as a schema for the
	// other data files rather than as a value.
	form string
}

// formTags is the set of form tags accepted in qualifiers, from the
// internal/filetypes tag vocabulary.
var formTags = map[string]bool{
	"schema": true,
	"data":   true,
	"graph":  true,
	"dag":    true,
}

// parseFileArgs parses the file arguments per the
//
//	file* (spec: file+)*
//
// grammar, resolving each file's type from the active qualifier or its
// extension.
func parseFileArgs(args []string) ([]fileArg, error) {
	var files []fileArg
	spec := fileSpec{}
	qualifier := ""
	hasFiles := false
	for i, s := range args {
		scope, file, found := cutScope(s)
		switch {
		case !found: // just a file name, like "foo.yaml"
			if file == "" {
				return nil, fmt.Errorf("empty file name")
			}
			fa, err := newFileArg(spec, file)
			if err != nil {
				return nil, err
			}
			files = append(files, fa)
			hasFiles = true

		case scope == "":
			return nil, fmt.Errorf("empty filetype prefix in %q", s)
		case file != "":
			return nil, fmt.Errorf("cannot combine file type and file name; did you mean %q?", scope+": "+file)

		default: // just a scope, like "json:"
			switch {
			case i == len(args)-1:
				qualifier = scope
				fallthrough
			case qualifier != "" && !hasFiles:
				return nil, fmt.Errorf("scoped qualifier %q without file", qualifier+":")
			}
			var err error
			spec, err = parseQualifier(scope)
			if err != nil {
				return nil, err
			}
			qualifier = scope
			hasFiles = false
		}
	}
	return files, nil
}

// newFileArg resolves the type of a single file argument, applying the
// input-mode defaults: standard input defaults to CUE, other files to
// the codec claiming their extension.
func newFileArg(spec fileSpec, name string) (fileArg, error) {
	if spec.codec == "" {
		if name == "-" {
			spec.codec = "cue"
		} else {
			c, ok := defaultCodecs.ByExtension(fileExt(name))
			if !ok {
				return fileArg{}, fmt.Errorf("unknown file extension for %q", name)
			}
			spec.codec = c.Name()
		}
	}
	return fileArg{name: name, spec: spec}, nil
}

// parseQualifier parses a file qualifier of the form tag('+'tag)*,
// where each tag is a codec name ("json") or a form tag ("schema").
//
// The internal/filetypes grammar also allows tag values
// (yaml+indentSequences=false); those map to codec-specific options,
// which the built-in cuecodec codecs do not define yet, so they are
// rejected here.
func parseQualifier(scope string) (fileSpec, error) {
	var spec fileSpec
	for tag := range strings.SplitSeq(scope, "+") {
		name, _, hasValue := strings.Cut(tag, "=")
		if hasValue {
			return fileSpec{}, fmt.Errorf("cannot specify a value for tag %q: filetype tag options are not supported", name)
		}
		switch {
		case formTags[name]:
			if spec.form != "" && spec.form != name {
				return fileSpec{}, fmt.Errorf("conflicting form tags %q and %q", spec.form, name)
			}
			spec.form = name
		default:
			if _, ok := defaultCodecs.Lookup(name); !ok {
				return fileSpec{}, fmt.Errorf("unknown filetype tag %q", name)
			}
			if spec.codec != "" && spec.codec != name {
				return fileSpec{}, fmt.Errorf("conflicting filetype tags %q and %q", spec.codec, name)
			}
			spec.codec = name
		}
	}
	return spec, nil
}

// checkPattern rejects malformed package patterns that would otherwise
// surface only when the plan is loaded.
func checkPattern(pattern string) error {
	ip := ast.ParseImportPath(pattern)
	if i := strings.Index(ip.Path, "..."); i >= 0 && ip.Path[i+3:] != "" {
		return fmt.Errorf("pattern %q: text after ... is not supported", pattern)
	}
	return nil
}

// isPackage reports whether a command-line argument is a package based
// on its lexical representation alone. Ported from internal/filetypes.
func isPackage(s string) bool {
	switch s {
	case ".", "..":
		return true
	case "", "-":
		return false
	}

	ip := ast.ParseImportPath(s)
	if ip.ExplicitQualifier {
		if strings.Contains(ip.Path, ":") || ip.Path == "-" {
			return false
		}
		if ast.IsValidIdent(ip.Qualifier) {
			// If it's got an explicit qualifier, the path has a colon
			// which isn't generally allowed in CUE file names.
			return true
		}
		// Even with an invalid qualifier (e.g. "pkg@v1" where the
		// version was placed after the qualifier), a path with a slash
		// indicates a package path, not a filetype scope like "json:".
		return strings.Contains(ip.Path, "/")
	}
	if ip.Version != "" {
		if strings.Contains(ip.Version, "/") {
			// We'll definitely not allow slashes in the version string
			// so treat it as a file name.
			return false
		}
		// Looks like an explicit version suffix. Deliberately leave
		// the syntax fairly open so that we get reasonable error
		// messages when invalid version queries are specified.
		return true
	}

	// No version and no qualifier. Any file without an extension
	// (except maybe '-') is invalid as a file, so we assume it is a
	// package. The section may still contain a dot, for instance
	// ./foo/., ./.foo/, or ./foo/...
	return strings.TrimLeft(fileExt(s), ".") == ""
}

// isScopeQualifier reports whether a command-line argument is a bare
// file type scope qualifier, such as "json:" or "cue+schema:", which
// carries no file name of its own but applies to the files after it.
func isScopeQualifier(s string) bool {
	scope, file, found := cutScope(s)
	return found && scope != "" && file == ""
}

// cutScope splits an argument into its scope qualifier and file name,
// guarding against absolute paths (including Windows drive letters)
// that contain a ":" without denoting a qualifier.
func cutScope(s string) (scope, file string, found bool) {
	if cuepath.IsAbs(s, cuepath.Windows) || cuepath.IsAbs(s, cuepath.Unix) {
		// Absolute paths on Windows can begin with a volume name, like
		// `C:\foo\bar`; do not confuse that for a scope prefix. The
		// Unix check keeps `/foo:colons.json` an absolute file name
		// rather than a `/foo` scope prefix on `colons.json`.
	} else if before, after, ok := strings.Cut(s, ":"); ok {
		return before, after, true
	}
	return "", s, false // just a file name
}

// fileExt is like filepath.Ext except that a name starting with "."
// has no extension unless it contains another ".", and "-" is treated
// as a special case so that stdin/stdout resolve like regular files.
func fileExt(f string) string {
	if f == "-" {
		return "-"
	}
	e := filepath.Ext(f)
	if e == "" || e == filepath.Base(f) {
		return ""
	}
	return e
}
