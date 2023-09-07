package module

import (
	"cuelang.org/go/internal/mod/semver"
)

// Versions implements mvs.Versions[Version].
type Versions struct{}

// New implements mvs.Versions[Version].Version.
func (Versions) Version(v Version) string {
	return v.Version()
}

// New implements mvs.Versions[Version].Path.
func (Versions) Path(v Version) string {
	return v.Path()
}

// New implements mvs.Versions[Version].New.
func (Versions) New(p, v string) (Version, error) {
	return NewVersion(p, v)
}

// Max implements mvs.Reqs.Max.
//
// Note that the empty
// string is allowed because it's a potential version of
// a build target where there is no known version.
// See [mvs.Reqs] for more detail.
func (Versions) Max(v1, v2 string) string {
	if v1 == "none" || v2 == "" {
		return v2
	}
	if v2 == "none" || v1 == "" {
		return v1
	}
	if semver.Compare(v1, v2) > 0 {
		return v1
	}
	return v2
}
