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

package cache

import (
	"errors"
	"fmt"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/parser"
	"cuelang.org/go/internal/golangorgx/gopls/protocol"
	"cuelang.org/go/internal/lsp/definitions"
)

// Standalone models cue files which cannot be placed within a
// [Package] within a [Module]. This could be because:
//
//   - The cue file has no valid package declaration;
//   - No cue.mod/module.cue file could be found in any of the cue
//     file's ancestor directories;
//   - The cue.mod/module.cue file is invalid.
//
// It should be impossible for a file to simultaneously exist within a
// Package and within Standalone.
type Standalone struct {
	workspace *Workspace
	files     map[protocol.DocumentURI]*standaloneFile
}

func NewStandalone(workspace *Workspace) *Standalone {
	return &Standalone{
		workspace: workspace,
		files:     make(map[protocol.DocumentURI]*standaloneFile),
	}
}

// reloadFile ensures that a standalone file exists for uri and
// reloads it.
//
// If the file cannot be loaded then it is deleted.
func (s *Standalone) reloadFile(uri protocol.DocumentURI) error {
	file, found := s.files[uri]
	if !found {
		file = &standaloneFile{
			standalone: s,
			uri:        uri,
		}
		s.files[uri] = file
		s.workspace.invalidateActiveFilesAndDirs()
		s.workspace.debugLogf("%v Created", file)
	}
	file.isDirty = true
	return file.reload()
}

func (s *Standalone) activeFilesAndDirs(files map[protocol.DocumentURI][]packageOrModule, dirs map[protocol.DocumentURI]struct{}) {
	for _, file := range s.files {
		file.activeFilesAndDirs(files, dirs)
	}
}

// reloadFile reloads all standalone files that are dirty. If any such
// file cannot be reloaded, it is deleted.
//
// This method does not attempt to detect if any file has a valid
// package declaration, or exists within a valid module. It attempts
// to reload the existing standalone files only.
func (s *Standalone) reloadFiles() {
	for _, file := range s.files {
		_ = file.reload()
	}
}

// deleteFile ensures that a standalone file does not exist for uri.
func (s *Standalone) deleteFile(uri protocol.DocumentURI) {
	file, found := s.files[uri]
	if found {
		file.delete()
	}
}

// subtractModulesAndPackages attempts to transition standalone files
// to packages and modules. If a file has a package name, and if an
// existing valid module can be found, and a suitable package within
// that module exists or can be created, then that package is marked
// dirty and the file is removed from standalone. If any of those
// requirements are not met, then the file remains as a standalone
// file.
func (s *Standalone) subtractModulesAndPackages() error {
	for uri, file := range s.files {
		if pkgName := file.syntax.PackageName(); pkgName == "" {
			continue
		}

		m, err := s.workspace.FindModuleForFile(uri)
		if err != nil && err != errModuleNotFound {
			return err
		} else if m != nil {
			ip, dirUris, err := m.FindImportPathForFile(uri)
			if err != nil || ip == nil || len(dirUris) == 0 {
				continue
			}
			file.delete()
			pkg := m.EnsurePackage(*ip, dirUris)
			pkg.markFileDirty(uri)
			if len(dirUris) == 1 {
				for _, pkg := range m.DescendantPackages(*ip) {
					if pkg.importPath == *ip {
						continue
					}
					pkg.markFileDirty(uri)
				}
			}
		}
	}
	return nil
}

type standaloneFile struct {
	standalone *Standalone

	// uri is the URI for the file. Immutable.
	uri protocol.DocumentURI

	// isDirty means the standalone file should be reloaded.
	isDirty bool

	// syntax is the result of parsing the file as CUE. This is updated
	// whenever the file is reloaded.
	syntax *ast.File

	// definitions for this standalone file only. This is updated
	// whenever the file is reloaded.
	definitions *definitions.Definitions
}

func (f *standaloneFile) String() string {
	return fmt.Sprintf("StandaloneFile %v", f.uri)
}

var _ packageOrModule = (*standaloneFile)(nil)

// markFileDirty implements [packageOrModule]
func (f *standaloneFile) markFileDirty(file protocol.DocumentURI) {
	if file != f.uri {
		panic(fmt.Sprintf("%v being told about file %v", f, file))
	}
	f.isDirty = true
}

// encloses implements [packageOrModule]
func (f *standaloneFile) encloses(file protocol.DocumentURI) bool {
	return f.uri == file
}

// activeFilesAndDirs implements [packageOrModule]
func (f *standaloneFile) activeFilesAndDirs(files map[protocol.DocumentURI][]packageOrModule, dirs map[protocol.DocumentURI]struct{}) {
	files[f.uri] = append(files[f.uri], f)
	dirs[f.uri.Dir()] = struct{}{}
}

var standaloneParserConfig = parser.NewConfig(parser.ParseComments)

// delete removes the standalone file from the workspace.
func (f *standaloneFile) delete() {
	delete(f.standalone.files, f.uri)
	w := f.standalone.workspace
	if oldAst := f.syntax; oldAst != nil {
		delete(w.mappers, oldAst.Pos().File())
	}
	w.invalidateActiveFilesAndDirs()
	w.debugLogf("%v Deleted", f)
}

// reload reloads the file, reparses it and updates its state,
// updating the syntax and definitions fields, along with the
// workspace's mappers.
//
// If the file cannot be read at all then the file is deleted and
// [ErrBadFile] is returned.
//
// This method does not attempt to detect if the file has a valid
// package declaration, or exists within a valid module. It attempts
// to reload the existing standalone file only.
func (f *standaloneFile) reload() error {
	if !f.isDirty {
		return nil
	}
	f.isDirty = false

	w := f.standalone.workspace
	fh, err := w.overlayFS.ReadFile(f.uri)
	if err != nil {
		w.debugLogf("%v Error when reloading: %v", f, err)
		f.delete()
		return ErrBadFile
	}
	ast, _, err := fh.ReadCUE(standaloneParserConfig)
	if ast == nil {
		w.debugLogf("%v Error when reloading: %v", f, err)
		f.delete()
		return ErrBadFile
	}

	if oldAst := f.syntax; oldAst != nil {
		delete(w.mappers, oldAst.Pos().File())
	}

	f.syntax = ast
	f.definitions = definitions.Analyse(nil, ast)
	w.mappers[ast.Pos().File()] = protocol.NewMapper(f.uri, ast.Pos().File().Content())
	w.debugLogf("%v Reloaded %v", f, err)
	return nil
}

var ErrBadFile = errors.New("bad file")
