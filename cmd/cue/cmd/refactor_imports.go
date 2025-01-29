// Copyright 2025 CUE Authors
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

package cmd

import (
	"os"
	"path/filepath"
	"strings"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/ast/astutil"
	"cuelang.org/go/cue/format"
	"cuelang.org/go/cue/literal"
	"cuelang.org/go/cue/load"
	"cuelang.org/go/cue/parser"
	"cuelang.org/go/internal/mod/semver"

	"github.com/spf13/cobra"
)

func newRefactorImportsCmd(c *Command) *cobra.Command {
	cmd := &cobra.Command{
		// Experimental so far.
		Hidden: true,

		Use:   "imports [<oldImportPath] <newImportPath>",
		Short: "rewrite import paths",
		Long: `WARNING: THIS COMMAND IS EXPERIMENTAL.

This command alters import directives in the current module. By
default it rewrites any imports in the current module that have a path
prefix matching oldImportPath to replace that prefix by newImportPath.
It does not attempt to adjust the contents of the cue.mod/module.cue file:
use "cue mod get" or "cue mod tidy" for that.

If the major version suffix is omitted from oldImportPath, then the
major version will match the default major version specified in the
module.cue file for that import path unless --all-major is specified,
in which case all major versions will match.

If the --exact flag is specified, then oldImportPath is only
considered to match when the entire path matches, rather than matching
any path prefix. The --exact flag is implied if either oldImportPath
or newImportPath contain an explicit package qualifier or when the
--ident flag is specified.

If oldImportPath is omitted and --exact is not specified,
oldImportPath is taken to be the same as newImportPath but with the
major version suffix omitted (see above for details). If oldImportPath
is omitted and --exact *is* specified, oldImportPath is taken to be
the same as newImportPath (this is useful in conjunction with
--ident).

By default the identifier that the package is imported as will be kept
the same (this is to minimize code churn). However, if --update-ident
is specified, the identifier that the package is imported as will be
updated according to the new import path's default identifier. If
--ident is specified, the identifier that the package is imported as
will be updated to that identifier; this also implies --exact. The
resulting CUE code is sanitized: that is, other than importing a
different package, identifiers within the file will always refer to
the same import directive.

For example:

	# Change from k8s "cue get go" imports to new curated namespace
	cue refactor imports k8s.io cuelabs.dev/x/k8s

	# Update to use a new major version of the foo.com/bar module.
	cue refactor imports foo.com/bar@v0 foo.com/bar@v1

	# A shorter form of the above, assuming v0 is the default major
	# version for foo.com/bar.
	cue refactor imports foo.com/bar@v1

	# Use a different package from the pubsub package directory
	cue refactor imports github.com/cue-unity/services/pubsub github.com/cue-unity/services/pubsub:otherpkg

	# Use a different identifier for the import of the pubsub package.
	cue refactor imports --ident otherPubSub github.com/cue-unity/services/pubsub

	# Update only foo.com/bar, not (say) foo.com/baz/somethingelse
	cue refactor imports --exact foo.com/bar foo.com/baz

	# Refactor, for example github.com/foo/bar/something.v2/pkg to foo.example/something/v2/pkg
	cue refactor imports 'github.com/foo/bar/(.*)\.(v[0-9]+)(.*)' 'foo.example/$1/$2$3'
`[1:],
		RunE: mkRunE(c, runRefactorImports),
		Args: cobra.RangeArgs(1, 2),
	}
	cmd.Flags().Bool(string(flagExact), false, "exact match for package path instead of prefix match")
	cmd.Flags().Bool(string(flagUpdateIdent), false, "update imported identifier name too")
	cmd.Flags().Bool(string(flagAllMajor), false, "match all versions when major version omitted")
	cmd.Flags().String(string(flagIdent), "", "specify imported identifier (implies --exact)")

	return cmd
}

func runRefactorImports(cmd *Command, args []string) error {
	exactMatch := flagExact.Bool(cmd)
	updateIdent := flagUpdateIdent.Bool(cmd)
	allMajor := flagAllMajor.Bool(cmd)
	newIdent := flagIdent.String(cmd)
	if newIdent != "" {
		exactMatch = true
	}

	newImportPath := ast.ParseImportPath(args[len(args)-1])
	oldImportPath := newImportPath
	if len(args) > 1 {
		oldImportPath = ast.ParseImportPath(args[0])
	}
	// "The `--exact` flag is implied if either oldImportPath or
	// newImportPath contain an explicit package qualifier or when the
	// `--ident` flag is specified"
	exactMatch = exactMatch || newImportPath.ExplicitQualifier || oldImportPath.ExplicitQualifier

	// When matching, ignore whether the qualifier is explicit or not.
	oldImportPath.ExplicitQualifier = false

	// "If oldImportPath is omitted and `--exact` is not specified,
	// oldImportPath is taken to be the same as newImportPath but with
	// the major version suffix omitted"
	if len(args) == 1 && !exactMatch {
		oldImportPath.Version = ""
	}
	if allMajor {
		oldImportPath.Version = ""
	}
	modRoot, err := findModuleRoot()
	if err != nil {
		return err
	}
	_, mf, _, err := readModuleFile()
	if err != nil {
		return nil
	}
	omittedMajor := oldImportPath.Version == ""
	if omittedMajor && !allMajor {
		// Find the default major version from the module.cue file if
		// there is one. If there isn't, it might be a dependency not
		// yet present or in cue.mod/pkg; in that case we leave it
		// empty and match only imports with a missing major version.
		if mv, ok := mf.ModuleForImportPath(oldImportPath.Path); ok {
			oldImportPath.Version = semver.Major(mv.Version())
		}
	}
	matchPkg := func(ip ast.ImportPath) (_m bool) {
		// Quick check: if the import path doesn't at least start with
		// the old import path then it can't possibly match, regardless
		// of anything else.
		if !pkgIsUnderneath(ip.Path, oldImportPath.Path) {
			return false
		}
		// Ignore whether the qualifier is explicit or not.
		ip.ExplicitQualifier = false
		if allMajor {
			ip.Version = ""
		} else if ip.Version == "" {
			mv, ok := mf.ModuleForImportPath(ip.String())
			if !ok && !omittedMajor {
				// Can't find the major version for the package
				// and it was specified in the pattern.
				return false
			}
			ip.Version = semver.Major(mv.Version())
		}
		if exactMatch {
			return ip == oldImportPath
		}
		// We've already checked the path prefix, and we know
		// that if the qualifier is explicit, exactMatch will be true,
		// so the only thing left to check is the major version.
		return allMajor || ip.Version == oldImportPath.Version
	}
	binst := load.Instances([]string{"./..."}, &load.Config{
		Dir:         modRoot,
		ModuleRoot:  modRoot,
		Tests:       true,
		Tools:       true,
		AllCUEFiles: true,
		Package:     "*",
		// Note: the import path refactoring can work even when some
		// external imports don't.
		SkipImports: true,
	})
	for _, inst := range binst {
		if err := inst.Err; err != nil {
			return err
		}
		for _, file := range inst.BuildFiles {
			if filepath.Dir(file.Filename) != inst.Dir {
				// Avoid processing files which are inherited from parent directories.
				continue
			}
			syntax, err := parser.ParseFile(file.Filename, file.Source, parser.ParseComments)
			if err != nil {
				return err
			}
			if !refactorImports(syntax, func(importPath, ident string) (_newIP, _newIdent string) {
				oldIP := ast.ParseImportPath(importPath)
				if !matchPkg(oldIP) {
					// No match: no change.
					return importPath, ident
				}
				newIP := newImportPath
				if !exactMatch {
					if suffix := strings.TrimPrefix(oldIP.Path, oldImportPath.Path); suffix != "" {
						newIP.Path += suffix
						// The qualifier on the replacement is no longer applicable
						// because the path has changed.
						newIP.Qualifier = ""
						newIP.ExplicitQualifier = false
					}
				}
				if newIdent != "" {
					return newIP.String(), newIdent
				}
				if oldIP.ExplicitQualifier {
					// The old import wants a specific package.
					newIP.Qualifier = oldIP.Qualifier
				}
				if !updateIdent || ident != "" {
					// Either we want to keep the identifier the same
					// or there's an explicit alias already.
					if ident == "" {
						ident = oldIP.Qualifier
					}
					return newIP.String(), ident
				}
				// We might need to update the identifier to use the new import path.
				if len(newIP.Path) == len(newImportPath.Path) || newIP.ExplicitQualifier {
					// The path is unchanged or the qualifier was explicltly
					// specified, meaning that the qualifier will also be unchanged.
					return newIP.String(), newIP.Qualifier
				}
				if !newIP.ExplicitQualifier {
					// We don't want the qualifier from the base replacement path.
					newIP.Qualifier = ""
				}
				// Round-trip the new import path to determine the new qualifier.
				newIP = ast.ParseImportPath(newIP.String())
				return newIP.String(), newIP.Qualifier
			}) {
				// Nothing changed: no need to write the file.
				continue
			}
			if err := astutil.Sanitize(syntax); err != nil {
				return err
			}
			data, err := format.Node(syntax)
			if err != nil {
				return err
			}
			if err := os.WriteFile(file.Filename, data, 0o666); err != nil {
				return err
			}
		}
	}
	return nil
}

// refactorImports walks the given file content, calling processImport
// for all imported packages, passing it the import path, and the
// identifier that it's imported as or the empty string when there's no
// explicit import identifier. The values returned by processImport will
// be used to replace the import path and identifier in the file's
// import block.
//
// It reports whether the file content has changed.
//
// TODO this is potentially useful in general and could be moved
// out into another package.
func refactorImports(f *ast.File, processImport func(importPath, ident string) (newImportPath, newIdent string)) bool {
	identChanges := make(map[*ast.ImportSpec]string)
	stopped := false
	changed := false
	astutil.Apply(f, func(c astutil.Cursor) bool {
		if stopped {
			return false
		}
		switch n := c.Node().(type) {
		case *ast.ImportDecl:
			return true
		case *ast.ImportSpec:
			importPath, err := literal.Unquote(n.Path.Value)
			if err != nil {
				// Should never happen, as the AST has just been parsed.
				panic(err)
			}
			ident := ""
			if n.Name != nil {
				ident = n.Name.Name
			}
			newImportPath, newIdent := processImport(importPath, ident)
			if newImportPath == importPath && newIdent == ident {
				return false
			}
			changed = true
			if newImportPath != importPath {
				n.Path = ast.NewString(newImportPath)
			}
			if ident == "" {
				ident = ast.ParseImportPath(importPath).Qualifier
			}
			newIP := ast.ParseImportPath(newImportPath)
			if newIdent == "" {
				newIdent = newIP.Qualifier
			}
			if newIdent == newIP.Qualifier {
				// Ensure the formatter puts the import on a new line
				// if the previous import was.
				if n.Name != nil {
					ast.SetRelPos(n.Path, n.Name.Pos().RelPos())
				}
				n.Name = nil // No need for an explicit alias
			} else {
				n.Name = ast.NewIdent(newIdent)
			}
			if newIdent != ident {
				// The identifier for the import path has changed:
				// we'll need to walk the rest of the file to change
				// identifiers that refer to this import.
				identChanges[n] = newIdent
			}
			// We're done fixing the import spec already: no need
			// recurse into it.
			return false
		case *ast.Ident:
			refNode, ok := n.Node.(*ast.ImportSpec)
			if !ok {
				return false
			}
			newIdent, ok := identChanges[refNode]
			if ok {
				// This identifier is referring to an import spec that's changed;
				// update it so that it's using the new name.
				n.Name = newIdent
			}
			return false
		case *ast.Package, *ast.Comment, *ast.CommentGroup, *ast.Attribute:
			// All of the above node types can occur in the preamble.
			return true
		case *ast.File:
			return true
		default:
			if len(identChanges) == 0 {
				// We've advanced beyond the preamble
				// and no identifiers have changed for import
				// paths, so there's no need to proceed any further.
				// We want to cause Apply to return immediately,
				// but we can't do that without returning true
				// from this function first.
				stopped = true
			}
			return true
		}
	}, func(c astutil.Cursor) bool {
		if stopped {
			// We've gone beyond the preamble and there's
			// nothing to do.
			return false
		}
		return true
	})
	return changed
}
