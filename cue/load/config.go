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
	"os"
	"path/filepath"
	"runtime"

	"cuelang.org/go/cue/build"
	"cuelang.org/go/cue/errors"
)

const (
	cueSuffix  = ".cue"
	defaultDir = "cue"
	modFile    = "cue.mod"
	pkgDir     = "pkg" // TODO: vendor?
)

// FromArgsUsage is a partial usage message that applications calling
// FromArgs may wish to include in their -help output.
//
// Some of the aspects of this documentation, like flags and handling '--' need
// to be implemented by the tools.
const FromArgsUsage = `
<args> is a list of arguments denoting a set of instances.
It may take one of two forms:

1. A list of *.cue source files.

   All of the specified files are loaded, parsed and type-checked
   as a single instance.

2. A list of relative directories to denote a package instance.

   Each directory matching the pattern is loaded as a separate instance.
   The instance contains all files in this directory and ancestor directories,
   up to the module root, with the same package name. The package name must
   be either uniquely determined by the files in the given directory, or
   explicitly defined using the '-p' flag.

   Files without a package clause are ignored.

   Files ending in *_test.cue files are only loaded when testing.

3. A list of import paths, each denoting a package.

   The package's directory is loaded from the package cache. The version of the
   package is defined in the modules cue.mod file.

A '--' argument terminates the list of packages.
`

// A Config configures load behavior.
type Config struct {
	// Context specifies the context for the load operation.
	// If the context is cancelled, the loader may stop early
	// and return an ErrCancelled error.
	// If Context is nil, the load cannot be cancelled.
	Context *build.Context

	loader *loader

	modRoot string // module root for package paths ("" if unknown)

	// cache specifies the package cache in which to look for packages.
	cache string

	// Package defines the name of the package to be loaded. In this is not set,
	// the package must be uniquely defined from its context.
	Package string

	// Dir is the directory in which to run the build system's query tool
	// that provides information about the packages.
	// If Dir is empty, the tool is run in the current directory.
	Dir string

	// The build and release tags specify build constraints that should be
	// considered satisfied when processing +build lines. Clients creating a new
	// context may customize BuildTags, which defaults to empty, but it is
	// usually an error to customize ReleaseTags, which defaults to the list of
	// CUE releases the current release is compatible with.
	BuildTags   []string
	releaseTags []string

	// If Tests is set, the loader includes not just the packages
	// matching a particular pattern but also any related test packages.
	Tests bool

	// If Tools is set, the loader includes tool files associated with
	// a package.
	Tools bool

	// If DataFiles is set, the loader includes entries for directories that
	// have no CUE files, but have recognized data files that could be converted
	// to CUE.
	DataFiles bool

	// StdRoot specifies an alternative directory for standard libaries.
	// This is mostly used for bootstrapping.
	StdRoot string

	fileSystem
}

func (c Config) newInstance(path string) *build.Instance {
	i := c.Context.NewInstance(path, nil)
	i.DisplayPath = path
	return i
}

func (c Config) newErrInstance(m *match, path string, err errors.Error) *build.Instance {
	i := c.Context.NewInstance(path, nil)
	i.DisplayPath = path
	i.ReportError(err)
	return i
}

func (c Config) complete() (cfg *Config, err error) {
	// Each major CUE release should add a tag here.
	// Old tags should not be removed. That is, the cue1.x tag is present
	// in all releases >= CUE 1.x. Code that requires CUE 1.x or later should
	// say "+build cue1.x", and code that should only be built before CUE 1.x
	// (perhaps it is the stub to use in that case) should say "+build !cue1.x".
	c.releaseTags = []string{"cue0.1"}

	if c.Dir == "" {
		c.Dir, err = os.Getwd()
		if err != nil {
			return nil, err
		}
	}

	// TODO: determine root on a package basis. Maybe we even need a
	// pkgname.cue.mod
	// Look to see if there is a cue.mod.
	if c.modRoot == "" {
		abs, err := findRoot(c.Dir)
		if err != nil {
			// Not using modules: only consider the current directory.
			c.modRoot = c.Dir
		} else {
			c.modRoot = abs
		}
	}

	c.loader = &loader{cfg: &c}

	if c.Context == nil {
		c.Context = build.NewContext(build.Loader(c.loader.loadFunc(c.Dir)))
	}

	if c.cache == "" {
		c.cache = filepath.Join(home(), defaultDir)
		// os.MkdirAll(c.Cache, 0755) // TODO: tools task
	}

	return &c, nil
}

func findRoot(dir string) (string, error) {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	abs := absDir
	for {
		info, err := os.Stat(filepath.Join(abs, modFile))
		if err == nil && !info.IsDir() {
			return abs, nil
		}
		d := filepath.Dir(abs)
		if len(d) >= len(abs) {
			break // reached top of file system, no cue.mod
		}
		abs = d
	}
	abs = absDir
	for {
		info, err := os.Stat(filepath.Join(abs, pkgDir))
		if err == nil && info.IsDir() {
			return abs, nil
		}
		d := filepath.Dir(abs)
		if len(d) >= len(abs) {
			return "", err // reached top of file system, no pkg dir.
		}
		abs = d
	}
}

func home() string {
	env := "HOME"
	if runtime.GOOS == "windows" {
		env = "USERPROFILE"
	} else if runtime.GOOS == "plan9" {
		env = "home"
	}
	return os.Getenv(env)
}
