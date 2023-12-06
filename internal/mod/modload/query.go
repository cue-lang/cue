package modload

import (
	"context"
	"fmt"
	"path"
	"runtime"
	"sync"

	"cuelang.org/go/internal/mod/internal/par"
	"cuelang.org/go/internal/mod/modpkgload"
	"cuelang.org/go/internal/mod/modrequirements"
	"cuelang.org/go/internal/mod/module"
	"cuelang.org/go/internal/mod/semver"
)

// queryImport attempts to locate a module that can be added to the
// current build list to provide the package with the given import path.
//
// It avoids results that are already in the given requirements.
func (ld *loader) queryImport(ctx context.Context, pkgPath string, rs *modrequirements.Requirements) ([]module.Version, error) {
	if modpkgload.IsStdlibPackage(pkgPath) {
		// This package isn't in the standard library and isn't in any module already
		// in the build list.
		//
		// Moreover, the import path is reserved for the standard library, so
		// QueryPattern cannot possibly find a module containing this package.
		//
		// Instead of trying QueryPattern, report an ImportMissingError immediately.
		return nil, &modpkgload.ImportMissingError{Path: pkgPath}
	}

	// Look up module containing the package, for addition to the build list.
	// Goal is to determine the module, download it to dir,
	// and return m, dir, ImportMissingError.

	// TODO logging support
	logf("cue: finding module for package %s", pkgPath)

	candidates, err := ld.queryLatestModules(ctx, pkgPath, rs)
	if err != nil {
		return nil, err
	}
	if len(candidates) == 0 {
		return nil, fmt.Errorf("%v", &modpkgload.ImportMissingError{Path: pkgPath})
	}
	return candidates, nil
}

// queryLatestModules looks for potential modules that might contain the given
// package by looking for the latest module version of all viable prefixes of pkgPath.
// It does not return modules that are already present in the given requirements.
func (ld *loader) queryLatestModules(ctx context.Context, pkgPath string, rs *modrequirements.Requirements) ([]module.Version, error) {
	mp, mv, ok := module.SplitPathVersion(pkgPath)
	if !ok {
		return nil, fmt.Errorf("package %q does not include major version (TODO support latest major version query)", pkgPath)
	}
	latestModuleForPrefix := func(prefix string) (module.Version, error) {
		mpath := prefix + "@" + mv
		if _, ok := rs.RootSelected(mpath); ok {
			// Already present in current requirements.
			return module.Version{}, nil
		}

		versions, err := ld.registry.ModuleVersions(ctx, mpath)
		logf("getting module versions for %v -> %q, %v", mpath, versions, err)
		if err != nil {
			return module.Version{}, err
		}
		logf("-> %q", versions)
		if v := latestVersion(versions); v != "" {
			return module.NewVersion(prefix, v)
		}
		return module.Version{}, nil
	}
	work := par.NewQueue(runtime.GOMAXPROCS(0))
	var (
		mu         sync.Mutex
		candidates []module.Version
		queryErr   error
	)
	for prefix := mp; prefix != "."; prefix = path.Dir(prefix) {
		prefix := prefix
		work.Add(func() {
			v, err := latestModuleForPrefix(prefix)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				if queryErr == nil {
					queryErr = err
				}
				return
			}
			if v.IsValid() {
				candidates = append(candidates, v)
			}
		})
	}
	<-work.Idle()
	return candidates, queryErr
}

// latestVersion returns the latest of any of the given versions,
// ignoring prerelease versions if there is any stable version.
func latestVersion(versions []string) string {
	maxStable := ""
	maxAny := ""
	for _, v := range versions {
		if semver.Prerelease(v) == "" && (maxStable == "" || semver.Compare(v, maxStable) > 0) {
			maxStable = v
		}
		if maxAny == "" || semver.Compare(v, maxAny) > 0 {
			maxAny = v
		}
	}
	if maxStable != "" {
		return maxStable
	}
	return maxAny
}
