package token

import "io/fs"

// FSLoc holds location information for a file within an [fs.FS].
// It carries the FS, the path within that FS, and a function
// to map from FS paths to display paths.
//
// Path holds a valid [fs.FS] path — forward-slash separated and
// not starting with a leading slash. It is suitable for passing
// directly to [fs.FS.Open] and related functions.
//
// FromFSPath maps [FSLoc.Path] to the display path used
// in error messages and position information. It is always
// set by the loader.
type FSLoc struct {
	FS         fs.FS
	Path       string
	FromFSPath func(string) string
}

// String returns the display path for the location.
// If FromFSPath is nil, it returns the path directly.
func (loc FSLoc) String() string {
	if loc.FromFSPath == nil {
		return loc.Path
	}
	return loc.FromFSPath(loc.Path)
}

// IsSet reports whether the location has been set.
func (loc FSLoc) IsSet() bool {
	return loc.FS != nil || loc.Path != ""
}
