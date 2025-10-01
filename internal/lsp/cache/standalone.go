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

func (s *Standalone) refreshFile(uri protocol.DocumentURI) error {
	file, found := s.files[uri]
	if !found {
		file = &standaloneFile{
			standalone: s,
			uri:        uri,
		}
		s.files[uri] = file
		s.workspace.debugLog(fmt.Sprintf("%v Created", file))
	}
	return file.refresh()
}

func (s *Standalone) deleteFile(uri protocol.DocumentURI) {
	file, found := s.files[uri]
	if found {
		file.delete()
	}
}

func (s *Standalone) subtractModulesAndPackages() error {
	for uri, file := range s.files {
		if pkgName := file.syntax.PackageName(); pkgName == "" {
			continue
		}

		m, err := s.workspace.FindModuleForFile(uri)
		if err != nil {
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
	standalone  *Standalone
	uri         protocol.DocumentURI
	syntax      *ast.File
	definitions *definitions.Definitions
}

func (f *standaloneFile) String() string {
	return fmt.Sprintf("StandaloneFile %v", f.uri)
}

var standaloneParserConfig = parser.NewConfig(parser.ParseComments)

func (f *standaloneFile) delete() {
	delete(f.standalone.files, f.uri)
	w := f.standalone.workspace
	if oldAst := f.syntax; oldAst != nil {
		delete(w.mappers, oldAst.Pos().File())
	}
	w.debugLog(fmt.Sprintf("%v Deleted", f))
}

func (f *standaloneFile) refresh() error {
	w := f.standalone.workspace
	fh, err := w.overlayFS.ReadFile(f.uri)
	if err != nil {
		w.debugLog(fmt.Sprintf("%v Error when reloading: %v", f, err))
		f.delete()
		return ErrBadFile
	}
	ast, _, err := fh.ReadCUE(standaloneParserConfig)
	if ast == nil {
		w.debugLog(fmt.Sprintf("%v Error when reloading: %v", f, err))
		f.delete()
		return ErrBadFile
	}

	if oldAst := f.syntax; oldAst != nil {
		delete(w.mappers, oldAst.Pos().File())
	}

	f.syntax = ast
	f.definitions = definitions.Analyse(nil, ast)
	w.mappers[ast.Pos().File()] = protocol.NewMapper(f.uri, ast.Pos().File().Content())
	w.debugLog(fmt.Sprintf("%v Reloaded %v", f, err))
	return nil
}

var ErrBadFile = errors.New("bad file")
