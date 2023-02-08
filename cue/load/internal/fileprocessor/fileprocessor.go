package fileprocessor

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/build"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/parser"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/internal"
)

const (
	cueSuffix = ".cue"
)

// An ImportMode controls the behavior of the Import method.
type ImportMode uint

const (
	// If findOnly is set, Import stops after locating the directory
	// that should contain the sources for a package. It does not
	// read any files in the directory.
	FindOnly ImportMode = 1 << iota

	// If importComment is set, parse import comments on package statements.
	// Import returns an error if it finds a comment it cannot understand
	// or finds conflicting comments in multiple source files.
	// See golang.org/s/go14customimport for more information.
	ImportComment

	AllowAnonymous
)

type Processor struct {
	firstFile        string
	firstCommentFile string
	imported         map[string][]token.Pos
	allTags          map[string]bool
	allFiles         bool
	IgnoreOther      bool // ignore files from other packages
	AllPackages      bool

	c      *Config
	tagger *Tagger
	Pkgs   map[string]*build.Instance
	pkg    *build.Instance

	err errors.Error
}

type Config struct {
	// Dir is the base directory for import path resolution.
	// For example, it is used to determine the main module,
	// and rooted import paths starting with "./" are relative to it.
	// If Dir is empty, the current directory is used.
	Dir string

	// Tags defines boolean tags or key-value pairs to select files to build
	// or be injected as values in fields.
	Tags []string

	// TagVars defines a set of key value pair the values of which may be
	// referenced by tags.
	//
	// Use DefaultTagVars to get a pre-loaded map with supported values.
	TagVars map[string]TagVar

	// Include all files, regardless of tags.
	AllCUEFiles bool

	// If Tests is set, the loader includes not just the packages
	// matching a particular pattern but also any related test packages.
	Tests bool

	// If Tools is set, the loader includes tool files associated with
	// a package.
	Tools bool

	// FilesMode indicates that files are specified
	// explicitly on the command line.
	FilesMode bool

	// If DataFiles is set, the loader includes entries for directories that
	// have no CUE files, but have recognized data files that could be converted
	// to CUE.
	DataFiles bool

	// Overlay provides a mapping of absolute file paths to file contents.  If
	// the file with the given path already exists, the parser will use the
	// alternative file contents provided by the map.
	Overlay map[string]Source

	// Stdin defines an alternative for os.Stdin for the file "-". When used,
	// the corresponding build.File will be associated with the full buffer.
	Stdin io.Reader

	FS *FileSystem
}

func (c *Config) stdin() io.Reader {
	if c.Stdin == nil {
		return os.Stdin
	}
	return c.Stdin
}

func New(c *Config, p *build.Instance, tg *Tagger) *Processor {
	return &Processor{
		imported: make(map[string][]token.Pos),
		allTags:  make(map[string]bool),
		c:        c,
		Pkgs:     map[string]*build.Instance{"_": p},
		pkg:      p,
		tagger:   tg,
	}
}

// Finalize finalizes the given instance, with respect to the file processor.
func (fp *Processor) Finalize(p *build.Instance) errors.Error {
	if fp.err != nil {
		return fp.err
	}
	if countCUEFiles(fp.c, p) == 0 &&
		!fp.c.DataFiles &&
		(p.PkgName != "_" || !fp.AllPackages) {
		fp.err = errors.Append(fp.err, &NoFilesError{Package: p, ignored: len(p.IgnoredFiles) > 0})
		return fp.err
	}

	for tag := range fp.allTags {
		p.AllTags = append(p.AllTags, tag)
	}
	sort.Strings(p.AllTags)

	p.ImportPaths, _ = cleanImports(fp.imported)

	return nil
}

// Add adds a file to be processed, with pos being the position of the package path
// in the import statement (or NoPos if not known), root is the module root,
// file represents the actual file and mode specifies how to add the file.
//
// After calling this, fp.Pkgs will be updated to reflect the currently known set
// of packages.
func (fp *Processor) Add(pos token.Pos, root string, file *build.File, mode ImportMode) (added bool) {
	fullPath := file.Filename
	if fullPath != "-" {
		if !filepath.IsAbs(fullPath) {
			fullPath = filepath.Join(root, fullPath)
		}
	}
	file.Filename = fullPath

	base := filepath.Base(fullPath)

	// special * and _
	p := fp.pkg // default package

	// badFile := func(p *build.Instance, err errors.Error) bool {
	badFile := func(err errors.Error) bool {
		fp.err = errors.Append(fp.err, err)
		file.ExcludeReason = fp.err
		p.InvalidFiles = append(p.InvalidFiles, file)
		return true
	}

	match, data, err := matchFile(fp.c, file, true, fp.allFiles, fp.allTags)
	switch {
	case match:

	case err == nil:
		// Not a CUE file.
		p.OrphanedFiles = append(p.OrphanedFiles, file)
		return false

	case !errors.Is(err, errExclude):
		return badFile(err)

	default:
		file.ExcludeReason = err
		if file.Interpretation == "" {
			p.IgnoredFiles = append(p.IgnoredFiles, file)
		} else {
			p.OrphanedFiles = append(p.OrphanedFiles, file)
		}
		return false
	}

	pf, perr := parser.ParseFile(fullPath, data, parser.ImportsOnly, parser.ParseComments)
	if perr != nil {
		badFile(errors.Promote(perr, "add failed"))
		return true
	}

	_, pkg, pos := internal.PackageInfo(pf)
	if pkg == "" {
		pkg = "_"
	}

	switch {
	case pkg == p.PkgName, mode&AllowAnonymous != 0:
	case fp.AllPackages && pkg != "_":
		q := fp.Pkgs[pkg]
		if q == nil {
			q = &build.Instance{
				PkgName: pkg,

				Dir:         p.Dir,
				DisplayPath: p.DisplayPath,
				ImportPath:  p.ImportPath + ":" + pkg,
				Root:        p.Root,
				Module:      p.Module,
			}
			fp.Pkgs[pkg] = q
		}
		p = q

	case pkg != "_":

	default:
		file.ExcludeReason = excludeError{errors.Newf(pos, "no package name")}
		p.IgnoredFiles = append(p.IgnoredFiles, file)
		return false // don't mark as added
	}

	if !fp.c.AllCUEFiles {
		if err := shouldBuildFile(pf, fp); err != nil {
			if !errors.Is(err, errExclude) {
				fp.err = errors.Append(fp.err, err)
			}
			file.ExcludeReason = err
			p.IgnoredFiles = append(p.IgnoredFiles, file)
			return false
		}
	}

	if pkg != "" && pkg != "_" {
		if p.PkgName == "" {
			p.PkgName = pkg
			fp.firstFile = base
		} else if pkg != p.PkgName {
			if fp.IgnoreOther {
				file.ExcludeReason = excludeError{errors.Newf(pos,
					"package is %s, want %s", pkg, p.PkgName)}
				p.IgnoredFiles = append(p.IgnoredFiles, file)
				return false
			}
			return badFile(&MultiplePackageError{
				Dir:      p.Dir,
				Packages: []string{p.PkgName, pkg},
				Files:    []string{fp.firstFile, base},
			})
		}
	}

	isTest := strings.HasSuffix(base, "_test"+cueSuffix)
	isTool := strings.HasSuffix(base, "_tool"+cueSuffix)

	if mode&ImportComment != 0 {
		qcom, line := findimportComment(data)
		if line != 0 {
			com, err := strconv.Unquote(qcom)
			if err != nil {
				badFile(errors.Newf(pos, "%s:%d: cannot parse import comment", fullPath, line))
			} else if p.ImportComment == "" {
				p.ImportComment = com
				fp.firstCommentFile = base
			} else if p.ImportComment != com {
				badFile(errors.Newf(pos, "found import comments %q (%s) and %q (%s) in %s", p.ImportComment, fp.firstCommentFile, com, base, p.Dir))
			}
		}
	}

	for _, decl := range pf.Decls {
		d, ok := decl.(*ast.ImportDecl)
		if !ok {
			continue
		}
		for _, spec := range d.Specs {
			quoted := spec.Path.Value
			path, err := strconv.Unquote(quoted)
			if err != nil {
				badFile(errors.Newf(
					spec.Path.Pos(),
					"%s: parser returned invalid quoted string: <%s>", fullPath, quoted,
				))
			}
			if !isTest || fp.c.Tests {
				fp.imported[path] = append(fp.imported[path], spec.Pos())
			}
		}
	}
	switch {
	case isTest:
		if fp.c.Tests {
			p.BuildFiles = append(p.BuildFiles, file)
		} else {
			file.ExcludeReason = excludeError{errors.Newf(pos,
				"_test.cue files excluded in non-test mode")}
			p.IgnoredFiles = append(p.IgnoredFiles, file)
		}
	case isTool:
		if fp.c.Tools {
			p.BuildFiles = append(p.BuildFiles, file)
		} else {
			file.ExcludeReason = excludeError{errors.Newf(pos,
				"_tool.cue files excluded in non-cmd mode")}
			p.IgnoredFiles = append(p.IgnoredFiles, file)
		}
	default:
		p.BuildFiles = append(p.BuildFiles, file)
	}
	return true
}

func countCUEFiles(c *Config, p *build.Instance) int {
	count := len(p.BuildFiles)
	for _, f := range p.IgnoredFiles {
		if c.Tools && strings.HasSuffix(f.Filename, "_tool.cue") {
			count++
		}
		if c.Tests && strings.HasSuffix(f.Filename, "_test.cue") {
			count++
		}
	}
	return count
}

func findimportComment(data []byte) (s string, line int) {
	// expect keyword package
	word, data := parseWord(data)
	if string(word) != "package" {
		return "", 0
	}

	// expect package name
	_, data = parseWord(data)

	// now ready for import comment, a // comment
	// beginning and ending on the current line.
	for len(data) > 0 && (data[0] == ' ' || data[0] == '\t' || data[0] == '\r') {
		data = data[1:]
	}

	var comment []byte
	switch {
	case bytes.HasPrefix(data, slashSlash):
		i := bytes.Index(data, newline)
		if i < 0 {
			i = len(data)
		}
		comment = data[2:i]
	}
	comment = bytes.TrimSpace(comment)

	// split comment into `import`, `"pkg"`
	word, arg := parseWord(comment)
	if string(word) != "import" {
		return "", 0
	}

	line = 1 + bytes.Count(data[:cap(data)-cap(arg)], newline)
	return strings.TrimSpace(string(arg)), line
}

func cleanImports(m map[string][]token.Pos) ([]string, map[string][]token.Pos) {
	all := make([]string, 0, len(m))
	for path := range m {
		all = append(all, path)
	}
	sort.Strings(all)
	return all, m
}

var (
	slashSlash = []byte("//")
	newline    = []byte("\n")
)

// skipSpaceOrComment returns data with any leading spaces or comments removed.
func skipSpaceOrComment(data []byte) []byte {
	for len(data) > 0 {
		switch data[0] {
		case ' ', '\t', '\r', '\n':
			data = data[1:]
			continue
		case '/':
			if bytes.HasPrefix(data, slashSlash) {
				i := bytes.Index(data, newline)
				if i < 0 {
					return nil
				}
				data = data[i+1:]
				continue
			}
		}
		break
	}
	return data
}

// parseWord skips any leading spaces or comments in data
// and then parses the beginning of data as an identifier or keyword,
// returning that word and what remains after the word.
func parseWord(data []byte) (word, rest []byte) {
	data = skipSpaceOrComment(data)

	// Parse past leading word characters.
	rest = data
	for {
		r, size := utf8.DecodeRune(rest)
		if unicode.IsLetter(r) || '0' <= r && r <= '9' || r == '_' {
			rest = rest[size:]
			continue
		}
		break
	}

	word = data[:len(data)-len(rest)]
	if len(word) == 0 {
		return nil, nil
	}

	return word, rest
}

// hasPathPrefix reports whether the path s begins with the
// elements in prefix.
func hasPathPrefix(s, prefix string) bool {
	switch {
	default:
		return false
	case len(s) == len(prefix):
		return s == prefix
	case len(s) > len(prefix):
		if prefix != "" && prefix[len(prefix)-1] == '/' {
			return strings.HasPrefix(s, prefix)
		}
		return s[len(prefix)] == '/' && s[:len(prefix)] == prefix
	}
}
