package token

import "io/fs"

// FSLoc holds location information for a file within an [fs.FS].
// It carries the FS, the path within that FS, and a function
// to map from FS paths to display paths.
type FSLoc struct {
	// FS holds the FS within which the file resides.
	FS fs.FS

	// Path holds a valid [fs.FS] path — forward-slash separated and
	// not starting with a leading slash. It is suitable for passing
	// directly to [fs.FS.Open] and related functions.
	Path string

	// FromFSPath maps [FSLoc.Path] to the display path used
	// in error messages and position information. It is always
	// set by cue/load, but if nil for some reason, it's treated
	// as if it were the identity function.
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

// IsZero reports whether the location is the zero value.
func (loc FSLoc) IsZero() bool {
	return loc.FS == nil && loc.Path == "" && loc.FromFSPath == nil
}
