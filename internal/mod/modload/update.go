package modload

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"path"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync/atomic"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/internal/mod/modpkgload"
	"cuelang.org/go/internal/mod/modrequirements"
	"cuelang.org/go/internal/mod/semver"
	"cuelang.org/go/internal/par"
	"cuelang.org/go/mod/modfile"
	"cuelang.org/go/mod/modregistry"
	"cuelang.org/go/mod/module"
)

// UpdateVersions returns the main module's module file with the specified module versions
// updated if possible and added if not already present. It returns an error if asked
// to downgrade a module below a version already required by an external dependency.
//
// A module in the versions slice can be specified as one of the following:
//   - $module@$fullVersion: a specific exact version
//   - $module@$partialVersion: a non-canonical version
//     specifies the latest version that has the same major/minor numbers.
//   - $module@latest: the latest non-prerelease version, or latest prerelease version if
//     there is no non-prerelease version
//   - $module: equivalent to $module@latest if $module doesn't have a default major
//     version or $module@$majorVersion if it does, where $majorVersion is the
//     default major version for $module.
func UpdateVersions(ctx context.Context, fsys fs.FS, modRoot string, reg Registry, versions []string) (*modfile.File, error) {
	mainModuleVersion, mf, err := readModuleFile(fsys, modRoot)
	if err != nil {
		return nil, err
	}
	rs := modrequirements.NewRequirements(mf.QualifiedModule(), reg, mf.DepVersions(), mf.DefaultMajorVersions())
	mversions, err := resolveUpdateVersions(ctx, reg, rs, mainModuleVersion, versions)
	if err != nil {
		return nil, err
	}
	// Now we know what versions we want to update to, make a new set of
	// requirements with these versions in place.

	mversionsMap := make(map[string]module.Version)
	for _, v := range mversions {
		// Check existing membership of the map: if the same module has been specified
		// twice, then choose t
		if v1, ok := mversionsMap[v.Path()]; ok && v1.Version() != v.Version() {
			// The same module has been specified twice with different requirements.
			// Treat it as an error (an alternative approach might be to choose the greater
			// version, but making it an error seems more appropriate to the "choose exact
			// version" semantics of UpdateVersions.
			return nil, fmt.Errorf("conflicting version update requirements %v vs %v", v1, v)
		}
		mversionsMap[v.Path()] = v
	}
	g, err := rs.Graph(ctx)
	if err != nil {
		return nil, fmt.Errorf("cannot determine module graph: %v", err)
	}
	var newVersions []module.Version
	for _, v := range g.BuildList() {
		if v.Path() == mainModuleVersion.Path() {
			continue
		}
		if newv, ok := mversionsMap[v.Path()]; ok {
			newVersions = append(newVersions, newv)
			delete(mversionsMap, v.Path())
		} else {
			newVersions = append(newVersions, v)
		}
	}
	newVersions = slices.AppendSeq(newVersions, maps.Values(mversionsMap))
	slices.SortFunc(newVersions, module.Version.Compare)
	rs = modrequirements.NewRequirements(mf.QualifiedModule(), reg, newVersions, mf.DefaultMajorVersions())
	g, err = rs.Graph(ctx)
	if err != nil {
		return nil, fmt.Errorf("cannot determine new module graph: %v", err)
	}
	// Now check that the resulting versions are the ones we wanted.
	for _, v := range mversions {
		actualVers := g.Selected(v.Path())
		if actualVers != v.Version() {
			return nil, fmt.Errorf("other requirements prevent changing module %v to version %v (actual selected version: %v)", v.Path(), v.Version(), actualVers)
		}
	}
	// Make a new requirements with the selected versions of the above as roots.
	var finalVersions []module.Version
	for _, v := range g.BuildList() {
		if v.Path() != mainModuleVersion.Path() {
			finalVersions = append(finalVersions, v)
		}
	}
	rs = modrequirements.NewRequirements(mf.QualifiedModule(), reg, finalVersions, mf.DefaultMajorVersions())
	return modfileFromRequirements(mf, rs), nil
}

// ResolveAbsolutePackage resolves a package in a standalone fashion, irrespective
// of a module file. It returns the module containing that package and the location of the package.
//
// It tries to avoid hitting the network unless necessary by using cached results where available.
func ResolveAbsolutePackage(ctx context.Context, reg Registry, p string) (module.Version, module.SourceLoc, error) {
	fail := func(err error) (module.Version, module.SourceLoc, error) {
		return module.Version{}, module.SourceLoc{}, err
	}
	failf := func(format string, args ...interface{}) (module.Version, module.SourceLoc, error) {
		return fail(fmt.Errorf(format, args...))
	}
	if filepath.IsAbs(p) || path.IsAbs(p) {
		return failf("%q is not a package path", p)
	}
	ip := ast.ParseImportPath(p)
	// Before any further lookup, check that the path without the version specifier is valid;
	// for example foo.com/bar/@latest would be an example of an invalid path.
	ip1 := ip
	ip1.Version = ""
	if err := module.CheckImportPath(ip1.String()); err != nil {
		return fail(err)
	}

	tryResolve := func(fetch func(m module.Version) (module.SourceLoc, error)) (module.Version, module.SourceLoc, error) {
		locs, err := modpkgload.FindPackageLocations(ctx, p, func(ctx context.Context, prefixPath string) (module.Version, error) {
			mv, err := resolveModuleVersion(ctx, reg, nil, prefixPath+"@"+ip.Version)
			if errors.Is(err, errNoVersionsFound) {
				return module.Version{}, nil
			}
			return mv, err
		}, func(ctx context.Context, m module.Version) (loc module.SourceLoc, isLocal bool, err error) {
			loc, err = fetch(m)
			if errors.Is(err, modregistry.ErrNotFound) {
				err = nil
			}
			return loc, false, err
		})
		if err != nil {
			return fail(err)
		}
		if len(locs) == 1 {
			// We've got exactly one cache hit. Use it.
			return locs[0].Module, locs[0].Locs[0], nil
		}
		if len(locs) > 1 {
			return fail(&modpkgload.AmbiguousImportError{ImportPath: p, Locations: locs})
		}
		return fail(&modpkgload.ImportMissingError{Path: p})
	}

	if reg, ok := reg.(modpkgload.CachedRegistry); ok && ip.Version != "" && semver.Canonical(ip.Version) == ip.Version {
		// It's a canonical version and we're using a caching registry implementation.
		// We might be able to avoid hitting the network.
		mv, loc, err := tryResolve(reg.FetchFromCache)
		if err == nil || !errors.As(err, new(*modpkgload.ImportMissingError)) {
			return mv, loc, err
		}
		// Not found in cache. Try again with the non-cached version.
	}
	return tryResolve(func(m module.Version) (module.SourceLoc, error) {
		return reg.Fetch(ctx, m)
	})
}

var errNoVersionsFound = fmt.Errorf("no versions found")

// resolveModuleVersion resolves a module/version query to a canonical module version.
//
// The version may take any of the following forms:
//
//	$module@v1.2.3	- absolute version.
//	$module			- latest version
//	$module@v1		- latest version at v1
//	$module@v1.2	- latest version within v1.1
//	$module@latest	- same as $module
//	$module@v1.latest	- same as @v1
//
// If rs is non-nil, it will be used to choose a default major version when no
// major version is specified.
//
// It returns an errNoVersionsFound error if there are no versions for the query but
// all else is OK.
//
// TODO could support queries like <=v1.2.3 etc
func resolveModuleVersion(ctx context.Context, reg Registry, rs *modrequirements.Requirements, v string) (module.Version, error) {
	if mv, err := module.ParseVersion(v); err == nil {
		// It's already a canonical version; nothing to do.
		return mv, nil
	}
	mpath, vers, ok := strings.Cut(v, "@")
	if !ok {
		if rs != nil {
			if major, status := rs.DefaultMajorVersion(mpath); status == modrequirements.ExplicitDefault {
				// TODO allow a non-explicit default too?
				vers = major
			}
		}
		if vers == "" {
			vers = "latest"
		}
	}

	if err := module.CheckPathWithoutVersion(mpath); err != nil {
		return module.Version{}, fmt.Errorf("%w: invalid module path in %q", errNoVersionsFound, v)
	}
	versionPrefix := ""
	switch {
	case vers == "latest":
	case strings.HasSuffix(vers, ".latest"):
		versionPrefix = strings.TrimSuffix(vers, ".latest")
		if !semver.IsValid(versionPrefix) {
			return module.Version{}, fmt.Errorf("invalid version specified %q", vers)
		}
		if semver.Canonical(versionPrefix) == versionPrefix {
			// TODO maybe relax this a bit to allow v1.2.3.latest ?
			return module.Version{}, fmt.Errorf("cannot use .latest on canonical version %q", vers)
		}
	default:
		if !semver.IsValid(vers) {
			return module.Version{}, fmt.Errorf("%q does not specify a valid semantic version", v)
		}
		if semver.Build(vers) != "" {
			return module.Version{}, fmt.Errorf("build version suffixes not supported (%v)", v)
		}
		// It's a valid version but has no build suffix and it's not canonical,
		// which means it must be either a major-only or major-minor, so
		// the conforming canonical versions must have it as a prefix, with
		// a dot separating the last component and the next.
		versionPrefix = vers + "."
	}
	allVersions, err := reg.ModuleVersions(ctx, mpath)
	if err != nil {
		return module.Version{}, err
	}
	possibleVersions := make([]string, 0, len(allVersions))
	for _, v := range allVersions {
		if strings.HasPrefix(v, versionPrefix) {
			possibleVersions = append(possibleVersions, v)
		}
	}
	if len(possibleVersions) == 0 {
		return module.Version{}, fmt.Errorf("%w for module %v", errNoVersionsFound, v)
	}
	chosen := LatestVersion(possibleVersions)
	mv, err := module.NewVersion(mpath, chosen)
	if err != nil {
		// Should never happen, because we've checked that
		// mpath is valid and ModuleVersions
		// should always return valid semver versions.
		return module.Version{}, err
	}
	return mv, nil
}

// resolveUpdateVersions resolves a set of version strings as accepted by [UpdateVersions]
// into the actual module versions they represent.
func resolveUpdateVersions(ctx context.Context, reg Registry, rs *modrequirements.Requirements, mainModuleVersion module.Version, versions []string) ([]module.Version, error) {
	work := par.NewQueue(runtime.GOMAXPROCS(0))
	mversions := make([]module.Version, len(versions))
	var queryErr atomic.Pointer[error]
	setError := func(err error) {
		queryErr.CompareAndSwap(nil, &err)
	}
	for i, v := range versions {
		if mv, err := module.ParseVersion(v); err == nil {
			// It's already canonical: nothing more to do.
			mversions[i] = mv
			continue
		}
		work.Add(func() {
			mv, err := resolveModuleVersion(ctx, reg, rs, v)
			if err != nil {
				setError(err)
			} else {
				mversions[i] = mv
			}
		})
	}
	<-work.Idle()
	if errPtr := queryErr.Load(); errPtr != nil {
		return nil, *errPtr
	}
	for _, v := range mversions {
		if v.Path() == mainModuleVersion.Path() {
			return nil, fmt.Errorf("cannot update version of main module")
		}
	}
	return mversions, nil
}
