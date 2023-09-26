package load

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/internal/mod/modfile"
	"cuelang.org/go/internal/mod/module"
	"cuelang.org/go/internal/mod/mvs"
	"cuelang.org/go/internal/mod/semver"
)

// loadModule loads the module file, resolves and downloads module
// dependencies. It sets c.Module if it's empty or checks it for
// consistency with the module file otherwise.
func (c *Config) loadModule() error {
	// TODO: also make this work if run from outside the module?
	mod := filepath.Join(c.ModuleRoot, modDir)
	info, cerr := c.fileSystem.stat(mod)
	if cerr != nil {
		return nil
	}
	// TODO remove support for legacy non-directory module.cue file
	// by returning an error if info.IsDir is false.
	if info.IsDir() {
		mod = filepath.Join(mod, moduleFile)
	}
	f, cerr := c.fileSystem.openFile(mod)
	if cerr != nil {
		return nil
	}
	defer f.Close()
	data, err := io.ReadAll(f)
	if err != nil {
		return err
	}
	parseModFile := modfile.ParseNonStrict
	if c.Registry == nil {
		parseModFile = modfile.ParseLegacy
	}
	mf, err := parseModFile(data, mod)
	if err != nil {
		return err
	}
	c.modFile = mf
	if mf.Module == "" {
		// Backward compatibility: allow empty module.cue file.
		// TODO maybe check that the rest of the fields are empty too?
		return nil
	}
	if c.Module != "" && c.Module != mf.Module {
		return errors.Newf(token.NoPos, "inconsistent modules: got %q, want %q", mf.Module, c.Module)
	}
	c.Module = mf.Module
	return nil
}

type dependencies struct {
	mainModule *modfile.File
	versions   []module.Version
}

// lookup returns the module corresponding to the given import path, and the relative path
// of the package beneath that.
//
// It assumes that modules are not nested.
func (deps *dependencies) lookup(pkgPath importPath) (v module.Version, subPath string, err error) {
	type answer struct {
		v       module.Version
		subPath string
	}
	var possible []answer
	for _, dep := range deps.versions {
		if subPath, ok := isParent(dep, pkgPath); ok {
			possible = append(possible, answer{dep, subPath})
		}
	}
	switch len(possible) {
	case 0:
		return module.Version{}, "", fmt.Errorf("no dependency found for import path %q", pkgPath)
	case 1:
		return possible[0].v, possible[0].subPath, nil
	}
	var found *answer
	for i, a := range possible {
		dep, ok := deps.mainModule.Deps[a.v.Path()]
		if ok && dep.Default {
			if found != nil {
				// More than one default.
				// TODO this should be impossible and checked by modfile.
				return module.Version{}, "", fmt.Errorf("more than one default module for import path %q", pkgPath)
			}
			found = &possible[i]
		}
	}
	if found == nil {
		return module.Version{}, "", fmt.Errorf("no default module found for import path %q", pkgPath)
	}
	return found.v, found.subPath, nil
}

// resolveDependencies resolves all the versions of all the modules in the given module file,
// using regClient to fetch dependency information.
func resolveDependencies(mainModFile *modfile.File, regClient *registryClient) (*dependencies, error) {
	vs, err := mvs.BuildList[module.Version](mainModFile.DepVersions(), &mvsReqs{
		mainModule: mainModFile,
		regClient:  regClient,
	})
	if err != nil {
		return nil, err
	}
	return &dependencies{
		mainModule: mainModFile,
		versions:   vs,
	}, nil
}

// mvsReqs implements mvs.Reqs by fetching information using
// regClient.
type mvsReqs struct {
	module.Versions
	mainModule *modfile.File
	regClient  *registryClient
}

// Required implements mvs.Reqs.Required.
func (reqs *mvsReqs) Required(m module.Version) (vs []module.Version, err error) {
	if m.Path() == reqs.mainModule.Module {
		return reqs.mainModule.DepVersions(), nil
	}
	mf, err := reqs.regClient.fetchModFile(context.TODO(), m)
	if err != nil {
		return nil, err
	}
	return mf.DepVersions(), nil
}

// Required implements mvs.Reqs.Max.
func (reqs *mvsReqs) Max(v1, v2 string) string {
	if cmpVersion(v1, v2) < 0 {
		return v2
	}
	return v1
}

// cmpVersion implements the comparison for versions in the module loader.
//
// It is consistent with semver.Compare except that as a special case,
// the version "" is considered higher than all other versions.
// The main module (also known as the target) has no version and must be chosen
// over other versions of the same module in the module dependency graph.
func cmpVersion(v1, v2 string) int {
	if v2 == "" {
		if v1 == "" {
			return 0
		}
		return -1
	}
	if v1 == "" {
		return 1
	}
	return semver.Compare(v1, v2)
}

// isParent reports whether the module modv contains the package with the given
// path, and if so, returns its relative path within that module.
func isParent(modv module.Version, pkgPath importPath) (subPath string, ok bool) {
	modBase := modv.BasePath()
	pkgBase, pkgMajor, pkgHasVersion := module.SplitPathVersion(string(pkgPath))
	if !pkgHasVersion {
		pkgBase = string(pkgPath)
	}

	if !strings.HasPrefix(pkgBase, modBase) {
		return "", false
	}
	if len(pkgBase) == len(modBase) {
		subPath = "."
	} else if pkgBase[len(modBase)] != '/' {
		return "", false
	} else {
		subPath = pkgBase[len(modBase)+1:]
	}
	// It's potentially a match, but we need to check the major version too.
	if !pkgHasVersion || semver.Major(modv.Version()) == pkgMajor {
		return subPath, true
	}
	return "", false
}
