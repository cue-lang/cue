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

// Files in package are to a large extent based on Go files from the following
// Go packages:
//    - cmd/go/internal/load
//    - go/build

import (
	"context"
	"fmt"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/build"
	"cuelang.org/go/internal/cueexperiment"
	"cuelang.org/go/internal/filetypes"
	"cuelang.org/go/internal/mod/modimports"
	"cuelang.org/go/internal/mod/modpkgload"
	"cuelang.org/go/internal/mod/modrequirements"
	"cuelang.org/go/mod/module"

	// Trigger the unconditional loading of all core builtin packages if load
	// is used. This was deemed the simplest way to avoid having to import
	// this line explicitly, and thus breaking existing code, for the majority
	// of cases, while not introducing an import cycle.
	_ "cuelang.org/go/pkg"
)

// Instances returns the instances named by the command line arguments 'args'.
// If errors occur trying to load an instance it is returned with Incomplete
// set. Errors directly related to loading the instance are recorded in this
// instance, but errors that occur loading dependencies are recorded in these
// dependencies.
func Instances(args []string, c *Config) []*build.Instance {
	ctx := context.TODO()
	if c == nil {
		c = &Config{}
	}
	// We want to consult the CUE_EXPERIMENT flag to see whether
	// consult external registries by default.
	if err := cueexperiment.Init(); err != nil {
		return []*build.Instance{c.newErrInstance(err)}
	}
	newC, err := c.complete()
	if err != nil {
		return []*build.Instance{c.newErrInstance(err)}
	}
	c = newC

	// TODO: This requires packages to be placed before files. At some point this
	// could be relaxed.
	i := 0
	for ; i < len(args) && filetypes.IsPackage(args[i]); i++ {
	}
	pkgArgs := args[:i]
	otherArgs := args[i:]

	// Pass all arguments that look like packages to loadPackages
	// so that they'll be available when looking up the packages
	// that are specified on the command line.
	// Relative import paths create a package with an associated
	// error but it turns out that's actually OK because the cue/load
	// logic resolves such paths without consulting pkgs.
	pkgs, err := loadPackages(ctx, c, pkgArgs)
	if err != nil {
		return []*build.Instance{c.newErrInstance(err)}
	}
	tg := newTagger(c)
	l := newLoader(c, tg, pkgs)

	if c.Context == nil {
		c.Context = build.NewContext(
			build.Loader(l.buildLoadFunc()),
			build.ParseFile(c.ParseFile),
		)
	}

	a := []*build.Instance{}
	if len(args) == 0 || i > 0 {
		for _, m := range l.importPaths(pkgArgs) {
			if m.Err != nil {
				inst := c.newErrInstance(m.Err)
				a = append(a, inst)
				continue
			}
			a = append(a, m.Pkgs...)
		}
	}

	if len(otherArgs) > 0 {
		files, err := filetypes.ParseArgs(otherArgs)
		if err != nil {
			return []*build.Instance{c.newErrInstance(err)}
		}
		a = append(a, l.cueFilesPackage(files))
	}

	for _, p := range a {
		tags, err := findTags(p)
		if err != nil {
			p.ReportError(err)
		}
		tg.tags = append(tg.tags, tags...)
	}

	// TODO(api): have API call that returns an error which is the aggregate
	// of all build errors. Certain errors, like these, hold across builds.
	if err := tg.injectTags(c.Tags); err != nil {
		for _, p := range a {
			p.ReportError(err)
		}
		return a
	}

	if tg.replacements == nil {
		return a
	}

	for _, p := range a {
		for _, f := range p.Files {
			ast.Walk(f, nil, func(n ast.Node) {
				if ident, ok := n.(*ast.Ident); ok {
					if v, ok := tg.replacements[ident.Node]; ok {
						ident.Node = v
					}
				}
			})
		}
	}

	return a
}

func loadPackages(ctx context.Context, cfg *Config, extraPkgs []string) (*modpkgload.Packages, error) {
	if cfg.Registry == nil || cfg.modFile == nil || cfg.modFile.Module == "" {
		return nil, nil
	}
	reqs := modrequirements.NewRequirements(
		cfg.modFile.Module,
		cfg.Registry,
		cfg.modFile.DepVersions(),
		cfg.modFile.DefaultMajorVersions(),
	)
	mainModLoc := module.SourceLoc{
		FS:  cfg.fileSystem.ioFS(cfg.ModuleRoot),
		Dir: ".",
	}
	allImports, err := modimports.AllImports(modimports.AllModuleFiles(mainModLoc.FS, mainModLoc.Dir))
	if err != nil {
		return nil, fmt.Errorf("cannot enumerate all module imports: %v", err)
	}
	// Add any packages specified on the command line so they're always
	// available.
	allImports = append(allImports, extraPkgs...)
	return modpkgload.LoadPackages(
		ctx,
		cfg.Module,
		mainModLoc,
		reqs,
		cfg.Registry,
		allImports,
	), nil
}
