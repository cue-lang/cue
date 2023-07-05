package load

import (
	_ "embed"
	"io"
	"path/filepath"
	"strings"

	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/load/internal/mvs"
	"cuelang.org/go/cue/token"
	"golang.org/x/mod/module"
	"golang.org/x/mod/semver"
)

//go:embed moduleschema.cue
var moduleSchema []byte

type modFile struct {
	Module string `json:"module"`
	Deps   map[string]*modDep
}

// versions returns all the modules that are dependended on by
// the module file.
func (mf *modFile) versions() []module.Version {
	if len(mf.Deps) == 0 {
		// It's important to return nil here because otherwise the
		// "mistake: chose versions" panic in mvs will trigger
		// on an empty version list.
		return nil
	}
	vs := make([]module.Version, 0, len(mf.Deps))
	for m, dep := range mf.Deps {
		vs = append(vs, module.Version{
			Path:    m,
			Version: dep.Version,
		})
	}
	return vs
}

type modDep struct {
	Version string `json:"v"`
}

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
	mf, err := parseModuleFile(data, mod)
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
	versions []module.Version
}

// lookup returns the module corresponding to the given import path, and the relative path
// of the package beneath that.
//
// It assumes that modules are not nested.
func (deps *dependencies) lookup(pkgPath importPath) (v module.Version, subPath string, ok bool) {
	for _, dep := range deps.versions {
		if subPath, ok := isParent(importPath(dep.Path), pkgPath); ok {
			return dep, subPath, true
		}
	}
	return module.Version{}, "", false
}

// resolveDependencies resolves all the versions of all the modules in the given module file,
// using regClient to fetch dependency information.
func resolveDependencies(mainModFile *modFile, regClient *registryClient) (*dependencies, error) {
	vs, err := mvs.BuildList(mainModFile.versions(), &mvsReqs{
		mainModule: mainModFile,
		regClient:  regClient,
	})
	if err != nil {
		return nil, err
	}
	return &dependencies{
		versions: vs,
	}, nil
}

// mvsReqs implements mvs.Reqs by fetching information using
// regClient.
type mvsReqs struct {
	mainModule *modFile
	regClient  *registryClient
}

// Required implements mvs.Reqs.Required.
func (reqs *mvsReqs) Required(m module.Version) (vs []module.Version, err error) {
	if m.Path == reqs.mainModule.Module {
		return reqs.mainModule.versions(), nil
	}
	mf, err := reqs.regClient.fetchModFile(m)
	if err != nil {
		return nil, err
	}
	return mf.versions(), nil
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

// isParent reports whether the module modPath contains the package with the given
// path, and if so, returns its relative path within that module.
func isParent(modPath, pkgPath importPath) (subPath string, ok bool) {
	if !strings.HasPrefix(string(pkgPath), string(modPath)) {
		return "", false
	}
	if len(pkgPath) == len(modPath) {
		return ".", true
	}
	if pkgPath[len(modPath)] != '/' {
		return "", false
	}
	return string(pkgPath[len(modPath)+1:]), true
}
