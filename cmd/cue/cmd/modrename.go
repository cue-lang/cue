// Copyright 2025 The CUE Authors
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
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/build"
	"cuelang.org/go/cue/format"
	"cuelang.org/go/cue/literal"
	"cuelang.org/go/cue/load"
	"cuelang.org/go/cue/parser"
	"cuelang.org/go/internal/mod/semver"
	"cuelang.org/go/mod/modfile"
	"cuelang.org/go/mod/module"
	"github.com/spf13/cobra"
)

func newModRenameCmd(c *Command) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "rename <newModulePath>",
		Short: "rename the current module",
		Long: `Rename changes the name of the current module,
updating import statements in source files as required.

Note that this command is not yet stable and may be changed.
`,
		RunE: mkRunE(c, runModRename),
		Args: cobra.ExactArgs(1),
	}

	return cmd
}

type modRenamer struct {
	oldModulePath      string
	oldModuleMajor     string
	oldModuleQualifier string
	newModulePath      string
	newModuleMajor     string
	newModuleQualifier string
}

func runModRename(cmd *Command, args []string) error {
	modFilePath, mf, _, err := readModuleFile()
	if err != nil {
		return err
	}
	if mf.Module == args[0] {
		// Nothing to do
		return nil
	}
	var mr modRenamer
	mr.oldModulePath, mr.oldModuleMajor, err = splitModulePath(mf.Module)
	if err != nil {
		return err
	}
	mr.oldModuleQualifier = ast.ParseImportPath(mr.oldModulePath).Qualifier
	mf.Module = args[0]
	mr.newModulePath, mr.newModuleMajor, err = splitModulePath(mf.Module)
	if err != nil {
		return err
	}
	mr.newModuleQualifier = ast.ParseImportPath(mr.newModulePath).Qualifier

	// TODO if we're renaming to a module that we currently depend on,
	// perhaps we should detect that and give an error.
	newModFileData, err := modfile.Format(mf)
	if err != nil {
		return fmt.Errorf("invalid resulting module.cue file after edits: %v", err)
	}
	if err := os.WriteFile(modFilePath, newModFileData, 0o666); err != nil {
		return err
	}

	modRoot, err := findModuleRoot()
	if err != nil {
		return err
	}
	binst := load.Instances([]string{"./..."}, &load.Config{
		Dir:         modRoot,
		ModuleRoot:  modRoot,
		Tests:       true,
		Tools:       true,
		AllCUEFiles: true,
		Package:     "*",
		// Note: the mod renaming can work even when
		// some external imports don't.
		SkipImports: true,
	})
	if len(binst) == 0 {
		// No packages to rename.
		return nil
	}
	if binst[0].Module == "" {
		return fmt.Errorf("no current module to rename")
	}
	for _, inst := range binst {
		if err := inst.Err; err != nil {
			return err
		}
		for _, file := range inst.BuildFiles {
			if filepath.Dir(file.Filename) != inst.Dir {
				// Avoid processing files which are inherited from parent directories.
				continue
			}
			if err := mr.renameFile(file); err != nil {
				return err
			}
		}
	}
	return nil
}

func (mr *modRenamer) renameFile(file *build.File) error {
	syntax, err := parser.ParseFile(file.Filename, file.Source, parser.ParseComments)
	if err != nil {
		return err
	}

	changed := false
	for spec := range syntax.ImportSpecs() {
		ch, err := mr.rewriteImport(spec)
		if err != nil {
			return err
		}
		changed = changed || ch
	}
	if !changed {
		return nil
	}
	data, err := format.Node(syntax)
	if err != nil {
		return err
	}
	if err := os.WriteFile(file.Filename, data, 0o666); err != nil {
		return err
	}
	return nil
}

func (mr *modRenamer) rewriteImport(spec *ast.ImportSpec) (changed bool, err error) {
	importPath, err := literal.Unquote(spec.Path.Value)
	if err != nil {
		return false, fmt.Errorf("malformed import path in AST: %v", err)
	}
	ip := ast.ParseImportPath(importPath)
	if !pkgIsUnderneath(ip.Path, mr.oldModulePath) {
		return false, nil
	}

	// TODO it's possible that we've got a import of a package in a nested module
	// rather than a reference to a package in this module. We can only
	// tell that by actually importing the dependencies and looking up the
	// package in those dependencies, which seems like overkill for now at least.
	if ip.Qualifier == "" {
		return false, fmt.Errorf("import path %q has no implied package qualifier", importPath)
	}
	if ip.Version != "" && ip.Version != mr.oldModuleMajor {
		// Same module, different major version. Don't change it.
		return false, nil
	}
	ip.Path = mr.newModulePath + strings.TrimPrefix(ip.Path, mr.oldModulePath)
	if ip.Version != "" {
		// Keep the major version if it was there already; the main
		// module is always the default.
		ip.Version = mr.newModuleMajor
	}
	// Note: ip.Qualifier remains the same as before, which means
	// that regardless of the new import path, the implied identifier
	// will remain the same, so no change is needed to spec.Ident.
	ip.ExplicitQualifier = false // Only include if needed.
	spec.Path.Value = literal.String.Quote(ip.String())
	return true, nil
}

// pkgIsUnderneath reports whether pkg2 is a package
// underneath (or the same as) pkg1.
func pkgIsUnderneath(pkg1, pkg2 string) bool {
	if len(pkg1) < len(pkg2) {
		return false
	}
	if !strings.HasPrefix(pkg1, pkg2) {
		return false
	}
	return len(pkg1) == len(pkg2) || pkg1[len(pkg2)] == '/'
}

func splitModulePath(path string) (mpath string, mvers string, err error) {
	mpath, mvers, ok := ast.SplitPackageVersion(path)
	if ok {
		if semver.Major(mvers) != mvers {
			return "", "", fmt.Errorf("module path %q should contain the major version only", path)
		}
		if err := module.CheckPath(mpath); err != nil {
			return "", "", fmt.Errorf("invalid module path %q: %v", path, err)
		}
		return mpath, mvers, nil
	}
	if err := module.CheckPathWithoutVersion(mpath); err != nil {
		return "", "", fmt.Errorf("invalid module path %q: %v", path, err)
	}
	return mpath, "v0", nil
}
