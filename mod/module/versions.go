package module

import (
	"golang.org/x/mod/semver"
)

// Versions implements mvs.Versions[Version]
type Versions struct{}

func (Versions) Version(v Version) string {
	return v.Version()
}

func (Versions) Path(v Version) string {
	return v.Path()
}

func (Versions) New(p, v string) (Version, error) {
	return NewVersion(p, v)
}

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
