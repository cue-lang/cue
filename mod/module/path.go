package module

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/internal/mod/semver"
)

// The following regular expressions come from https://github.com/opencontainers/distribution-spec/blob/main/spec.md#pulling-manifests
// and ensure that we can store modules inside OCI registries.
var (
	basePathPat = sync.OnceValue(func() *regexp.Regexp {
		return regexp.MustCompile(`^[a-z0-9]+((\.|_|__|-+)[a-z0-9]+)*(/[a-z0-9]+((\.|_|__|-+)[a-z0-9]+)*)*$`)
	})
	tagPat = sync.OnceValue(func() *regexp.Regexp {
		return regexp.MustCompile(`^[a-zA-Z0-9_][a-zA-Z0-9._-]{0,127}$`)
	})
)

// Check checks that a given module path, version pair is valid.
// In addition to the path being a valid module path
// and the version being a valid semantic version,
// the two must correspond.
// For example, the path "foo.com/bar@v2" only corresponds to
// semantic versions beginning with "v2.".
func Check(path, version string) error {
	if err := CheckPath(path); err != nil {
		return err
	}
	if !semver.IsValid(version) {
		return &ModuleError{
			Path: path,
			Err:  &InvalidVersionError{Version: version, Err: errors.New("not a semantic version")},
		}
	}
	_, pathMajor, _ := ast.SplitPackageVersion(path)
	if err := CheckPathMajor(version, pathMajor); err != nil {
		return &ModuleError{Path: path, Err: err}
	}
	return nil
}

// firstPathOK reports whether r can appear in the first element of a module path.
// The first element of the path must be an LDH domain name, at least for now.
// To avoid case ambiguity, the domain name must be entirely lower case.
func firstPathOK(r rune) bool {
	return r == '-' || r == '.' ||
		'0' <= r && r <= '9' ||
		'a' <= r && r <= 'z'
}

// modPathOK reports whether r can appear in a module path element.
// Paths can be ASCII letters, ASCII digits, and limited ASCII punctuation: - . _ and ~.
func modPathOK(r rune) bool {
	if r < utf8.RuneSelf {
		return r == '-' || r == '.' || r == '_' ||
			'0' <= r && r <= '9' ||
			'a' <= r && r <= 'z'
	}
	return false
}

// importPathOK reports whether r can appear in a package import path element.
//
// Import paths are intermediate between module paths and file paths: we allow
// disallow characters that would be confusing or ambiguous as arguments to
// 'go get' (such as '@' and ' ' ), but allow certain characters that are
// otherwise-unambiguous on the command line and historically used for some
// binary names (such as '++' as a suffix for compiler binaries and wrappers).
func importPathOK(r rune) bool {
	return modPathOK(r) ||
		r == '+' ||
		r == '~' ||
		'A' <= r && r <= 'Z'
}

// fileNameOK reports whether r can appear in a file name.
// For now we allow all Unicode letters but otherwise limit to pathOK plus a few more punctuation characters.
// If we expand the set of allowed characters here, we have to
// work harder at detecting potential case-folding and normalization collisions.
// See note about "escaped paths" above.
func fileNameOK(r rune) bool {
	if r < utf8.RuneSelf {
		// Entire set of ASCII punctuation, from which we remove characters:
		//     ! " # $ % & ' ( ) * + , - . / : ; < = > ? [ \ ] ^ _ ` { | } ~
		// We disallow some shell special characters: " ' * < > ? ` |
		// (Note that some of those are disallowed by the Windows file system as well.)
		// We also disallow path separators / : and \ (fileNameOK is only called on path element characters).
		// We allow spaces (U+0020) in file names.
		const allowed = "!#$%&()+,-.=@[]^_{}~ "
		if '0' <= r && r <= '9' || 'A' <= r && r <= 'Z' || 'a' <= r && r <= 'z' {
			return true
		}
		return strings.ContainsRune(allowed, r)
	}
	// It may be OK to add more ASCII punctuation here, but only carefully.
	// For example Windows disallows < > \, and macOS disallows :, so we must not allow those.
	return unicode.IsLetter(r)
}

// CheckPathWithoutVersion is like [CheckPath] except that
// it expects a module path without a major version.
func CheckPathWithoutVersion(basePath string) (err error) {
	if _, _, ok := ast.SplitPackageVersion(basePath); ok {
		return fmt.Errorf("module path inappropriately contains version")
	}
	if err := checkPath(basePath, modulePath); err != nil {
		return err
	}
	firstPath, _, _ := strings.Cut(basePath, "/")
	if firstPath == "" {
		return fmt.Errorf("leading slash")
	}
	if !strings.Contains(firstPath, ".") {
		return fmt.Errorf("missing dot in first path element")
	}
	if basePath[0] == '-' {
		return fmt.Errorf("leading dash in first path element")
	}
	for _, r := range firstPath {
		if !firstPathOK(r) {
			return fmt.Errorf("invalid char %q in first path element", r)
		}
	}
	// Sanity check agreement with OCI specs.
	if !basePathPat().MatchString(basePath) {
		return fmt.Errorf("path does not conform to OCI repository name restrictions; see https://github.com/opencontainers/distribution-spec/blob/HEAD/spec.md#pulling-manifests")
	}
	return nil
}

// CheckPath checks that a module path is valid.
// A valid module path is a valid import path, as checked by CheckImportPath,
// with three additional constraints.
//
// First, the leading path element (up to the first slash, if any),
// by convention a domain name, must contain only lower-case ASCII letters,
// ASCII digits, dots (U+002E), and dashes (U+002D);
// it must contain at least one dot and cannot start with a dash.
//
// Second, there may be a final major version of the form
// @vN where N looks numeric
// (ASCII digits) and must not begin with a leading zero.
// Without such a major version, the major version is assumed
// to be v0.
//
// Third, no path element may begin with a dot.
func CheckPath(mpath string) (err error) {
	if mpath == "local" {
		return nil
	}
	defer func() {
		if err != nil {
			err = &InvalidPathError{Kind: "module", Path: mpath, Err: err}
		}
	}()

	basePath, vers, ok := ast.SplitPackageVersion(mpath)
	if ok {
		if semver.Major(vers) != vers {
			return fmt.Errorf("path can contain major version only")
		}
		if !tagPat().MatchString(vers) {
			return fmt.Errorf("non-conforming version %q", vers)
		}
	} else {
		basePath = mpath
	}
	if err := CheckPathWithoutVersion(basePath); err != nil {
		return err
	}
	return nil
}

// CheckImportPath checks that an import path is valid.
//
// A valid import path consists of one or more valid path elements
// separated by slashes (U+002F), optionally followed by
// an @vN (major version) qualifier.
// The path part must not begin with nor end in a slash.
//
// A valid path element is a non-empty string made up of
// lower case ASCII letters, ASCII digits, and limited ASCII punctuation: - . and _
// Punctuation characters may not be adjacent and must be between non-punctuation
// characters.
//
// The element prefix up to the first dot must not be a reserved file name
// on Windows, regardless of case (CON, com1, NuL, and so on).
func CheckImportPath(path string) error {
	parts := ast.ParseImportPath(path)
	if semver.Major(parts.Version) != parts.Version {
		return &InvalidPathError{
			Kind: "import",
			Path: path,
			Err:  fmt.Errorf("import paths can only contain a major version specifier"),
		}
	}
	if err := checkPath(parts.Path, importPath); err != nil {
		return &InvalidPathError{Kind: "import", Path: path, Err: err}
	}
	return nil
}

// pathKind indicates what kind of path we're checking. Module paths,
// import paths, and file paths have different restrictions.
type pathKind int

const (
	modulePath pathKind = iota
	importPath
	filePath
)

// checkPath checks that a general path is valid. kind indicates what
// specific constraints should be applied.
//
// checkPath returns an error describing why the path is not valid.
// Because these checks apply to module, import, and file paths,
// and because other checks may be applied, the caller is expected to wrap
// this error with InvalidPathError.
func checkPath(path string, kind pathKind) error {
	if !utf8.ValidString(path) {
		return fmt.Errorf("invalid UTF-8")
	}
	if path == "" {
		return fmt.Errorf("empty string")
	}
	if path[0] == '-' && kind != filePath {
		return fmt.Errorf("leading dash")
	}
	if strings.Contains(path, "//") {
		return fmt.Errorf("double slash")
	}
	if path[len(path)-1] == '/' {
		return fmt.Errorf("trailing slash")
	}
	elemStart := 0
	for i, r := range path {
		if r == '/' {
			if err := checkElem(path[elemStart:i], kind); err != nil {
				return err
			}
			elemStart = i + 1
		}
	}
	if err := checkElem(path[elemStart:], kind); err != nil {
		return err
	}
	return nil
}

// checkElem checks whether an individual path element is valid.
func checkElem(elem string, kind pathKind) error {
	if elem == "" {
		return fmt.Errorf("empty path element")
	}
	if strings.Count(elem, ".") == len(elem) {
		return fmt.Errorf("invalid path element %q", elem)
	}

	if kind == modulePath {

		if r := rune(elem[0]); r == '.' || r == '_' || r == '-' {
			return fmt.Errorf("leading %q in path element", r)
		}
		if r := rune(elem[len(elem)-1]); r == '.' || r == '_' || r == '-' {
			return fmt.Errorf("trailing %q in path element", r)
		}
	} else if elem[len(elem)-1] == '.' {
		return fmt.Errorf("trailing dot in path element")
	}
	for _, r := range elem {
		ok := false
		switch kind {
		case modulePath:
			ok = modPathOK(r)
		case importPath:
			ok = importPathOK(r)
		case filePath:
			ok = fileNameOK(r)
		default:
			panic(fmt.Sprintf("internal error: invalid kind %v", kind))
		}
		if !ok {
			return fmt.Errorf("invalid char %q", r)
		}
	}
	// Windows disallows a bunch of path elements, sadly.
	// See https://docs.microsoft.com/en-us/windows/desktop/fileio/naming-a-file
	short, _, _ := strings.Cut(elem, ".")
	for _, bad := range badWindowsNames {
		if strings.EqualFold(bad, short) {
			return fmt.Errorf("%q disallowed as path element component on Windows", short)
		}
	}

	if kind == filePath {
		// don't check for Windows short-names in file names. They're
		// only an issue for import paths.
		return nil
	}

	// Reject path components that look like Windows short-names.
	// Those usually end in a tilde followed by one or more ASCII digits.
	if tilde := strings.LastIndexByte(short, '~'); tilde >= 0 && tilde < len(short)-1 {
		suffix := short[tilde+1:]
		suffixIsDigits := true
		for _, r := range suffix {
			if r < '0' || r > '9' {
				suffixIsDigits = false
				break
			}
		}
		if suffixIsDigits {
			return fmt.Errorf("trailing tilde and digits in path element")
		}
	}

	return nil
}

// CheckFilePath checks that a slash-separated file path is valid.
// The definition of a valid file path is the same as the definition
// of a valid import path except that the set of allowed characters is larger:
// all Unicode letters, ASCII digits, the ASCII space character (U+0020),
// and the ASCII punctuation characters
// “!#$%&()+,-.=@[]^_{}~”.
// (The excluded punctuation characters, " * < > ? ` ' | / \ and :,
// have special meanings in certain shells or operating systems.)
//
// CheckFilePath may be less restrictive in the future, but see the
// top-level package documentation for additional information about
// subtleties of Unicode.
func CheckFilePath(path string) error {
	if err := checkPath(path, filePath); err != nil {
		return &InvalidPathError{Kind: "file", Path: path, Err: err}
	}
	return nil
}

// badWindowsNames are the reserved file path elements on Windows.
// See https://docs.microsoft.com/en-us/windows/desktop/fileio/naming-a-file
var badWindowsNames = []string{
	"CON",
	"PRN",
	"AUX",
	"NUL",
	"COM1",
	"COM2",
	"COM3",
	"COM4",
	"COM5",
	"COM6",
	"COM7",
	"COM8",
	"COM9",
	"LPT1",
	"LPT2",
	"LPT3",
	"LPT4",
	"LPT5",
	"LPT6",
	"LPT7",
	"LPT8",
	"LPT9",
}

// SplitPathVersion returns a prefix and version suffix such that
// prefix+"@"+version == path.
//
// SplitPathVersion returns (path, "", false) when there is no `@`
// character splitting the path or if the version is empty.
//
// It does not check that the version is valid in any way other than
// checking that it is not empty.
//
// For example:
//
// SplitPathVersion("foo.com/bar@v0.1") returns ("foo.com/bar", "v0.1", true).
// SplitPathVersion("foo.com/bar@badvers") returns ("foo.com/bar", "badvers", true).
// SplitPathVersion("foo.com/bar") returns ("foo.com/bar", "", false).
// SplitPathVersion("foo.com/bar@") returns ("foo.com/bar@", "", false).
//
// Deprecated: use [ast.SplitPackageVersion] instead.
//
//go:fix inline
func SplitPathVersion(path string) (prefix, version string, ok bool) {
	return ast.SplitPackageVersion(path)
}

// ImportPath holds the various components of an import path.
//
// Deprecated: use [ast.ImportPath] instead.
//
//go:fix inline
type ImportPath = ast.ImportPath

// ParseImportPath returns the various components of an import path.
// It does not check the result for validity.
//
// Deprecated: use [ast.ParseImportPath] instead.
//
//go:fix inline
func ParseImportPath(p string) ast.ImportPath {
	return ast.ParseImportPath(p)
}

// CheckPathMajor returns a non-nil error if the semantic version v
// does not match the path major version pathMajor.
func CheckPathMajor(v, pathMajor string) error {
	if m := semver.Major(v); m != pathMajor {
		return &InvalidVersionError{
			Version: v,
			Err:     fmt.Errorf("should be %s, not %s", pathMajor, m),
		}
	}
	return nil
}
