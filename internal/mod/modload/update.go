package modload

import (
	"context"
	"fmt"
	"io/fs"
	"runtime"
	"strings"
	"sync/atomic"

	"cuelang.org/go/internal/mod/modrequirements"
	"cuelang.org/go/internal/mod/semver"
	"cuelang.org/go/internal/par"
	"cuelang.org/go/mod/modfile"
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
	for _, v := range mversionsMap {
		newVersions = append(newVersions, v)
	}
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
		i, v := i, v
		if mv, err := module.ParseVersion(v); err == nil {
			// It's already canonical: nothing more to do.
			mversions[i] = mv
			continue
		}
		mpath, vers, ok := strings.Cut(v, "@")
		if !ok {
			if major, status := rs.DefaultMajorVersion(mpath); status == modrequirements.ExplicitDefault {
				// TODO allow a non-explicit default too?
				vers = major
			} else {
				vers = "latest"
			}
		}
		if err := module.CheckPathWithoutVersion(mpath); err != nil {
			return nil, fmt.Errorf("invalid module path in %q", v)
		}
		versionPrefix := ""
		if vers != "latest" {
			if !semver.IsValid(vers) {
				return nil, fmt.Errorf("%q does not specify a valid semantic version", v)
			}
			if semver.Build(vers) != "" {
				return nil, fmt.Errorf("build version suffixes not supported (%v)", v)
			}
			// It's a valid version but has no build suffix and it's not canonical,
			// which means it must be either a major-only or major-minor, so
			// the conforming canonical versions must have it as a prefix, with
			// a dot separating the last component and the next.
			versionPrefix = vers + "."
		}
		work.Add(func() {
			allVersions, err := reg.ModuleVersions(ctx, mpath)
			if err != nil {
				setError(err)
				return
			}
			possibleVersions := make([]string, 0, len(allVersions))
			for _, v := range allVersions {
				if strings.HasPrefix(v, versionPrefix) {
					possibleVersions = append(possibleVersions, v)
				}
			}
			chosen := latestVersion(possibleVersions)
			mv, err := module.NewVersion(mpath, chosen)
			if err != nil {
				// Should never happen, because we've checked that
				// mpath is valid and ModuleVersions
				// should always return valid semver versions.
				setError(err)
				return
			}
			mversions[i] = mv
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
