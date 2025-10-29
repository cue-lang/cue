// Copyright 2018 The CUE Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package load

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/build"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/parser"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/internal"
	"cuelang.org/go/mod/modconfig"
	"cuelang.org/go/mod/modfile"
	"cuelang.org/go/mod/module"
)

const (
	cueSuffix  = ".cue"
	modDir     = "cue.mod"
	moduleFile = "module.cue"
)

// FromArgsUsage is a partial usage message that applications calling
// FromArgs may wish to include in their -help output.
//
// Some of the aspects of this documentation, like flags and handling '--' need
// to be implemented by the tools.
const FromArgsUsage = `
<args> is a list of arguments denoting a set of instances of the form:

   <package>* <file_args>*

1. A list of source files

   CUE files are parsed, loaded and unified into a single instance. All files
   must have the same package name.

   Data files, like YAML or JSON, are handled in one of two ways:

   a. Explicitly mapped into a single CUE namespace, using the --path, --files
      and --list flags. In this case these are unified into a single instance
      along with any other CUE files.

   b. Treated as a stream of data elements that each is optionally unified with
      a single instance, which either consists of the other CUE files specified
       on the command line or a single package.

   By default, the format of files is derived from the file extension.
   This behavior may be modified with file arguments of the form <qualifiers>:
   For instance,

      cue eval foo.cue json: bar.data

   indicates that the bar.data file should be interpreted as a JSON file.
   A qualifier applies to all files following it until the next qualifier.

   The following qualifiers are available:

      encodings
      cue           CUE definitions and data
      json          JSON data, one value only
      jsonl         newline-separated JSON values
      yaml          a YAML file, may contain a stream
      proto         Protobuf definitions

      interpretations
      jsonschema   data encoding describes JSON Schema
      openapi      data encoding describes Open API

      formats
      data         output as -- or only accept -- data
      graph        data allowing references or anchors
      schema       output as schema; defaults JSON files to JSON Schema
      def          full definitions, including documentation

2. A list of relative directories to denote a package instance.

   Each directory matching the pattern is loaded as a separate instance.
   The instance contains all files in this directory and ancestor directories,
   up to the module root, with the same package name. The package name must
   be either uniquely determined by the files in the given directory, or
   explicitly defined using a package name qualifier. For instance, ./...:foo
   selects all packages named foo in the any subdirectory of the current
   working directory.

3. An import path referring to a directory within the current module

   All CUE files in that directory, and all the ancestor directories up to the
   module root (if applicable), with a package name corresponding to the base
   name of the directory or the optional explicit package name are loaded into
   a single instance.

   Examples, assume a module name of acme.org/root:
      mod.test/foo   package in cue.mod
      ./foo             package corresponding to foo directory
      .:bar             package in current directory with package name bar
`

// GenPath reports the directory in which to store generated
// files.
func GenPath(root string) string {
	return internal.GenPath(root)
}

// A Config configures load behavior.
type Config struct {
	// TODO: allow passing a cuecontext to be able to lookup and verify builtin
	// packages at loading time.

	// Context specifies the context for the load operation.
	Context *build.Context

	// ModuleRoot is the directory that contains the cue.mod directory
	// as well as all the packages which form part of the module being loaded.
	//
	// If left as the empty string, a module root is found by walking parent directories
	// starting from [Config.Dir] until one is found containing a cue.mod directory.
	// If it is a relative path, it will be interpreted relative to [Config.Dir].
	ModuleRoot string

	// Module specifies the module prefix. If not empty, this value must match
	// the module field of an existing cue.mod file.
	Module string

	// AcceptLegacyModules causes the module resolution code
	// to accept module files that lack a language.version field.
	AcceptLegacyModules bool

	// modFile holds the contents of the module file, or nil
	// if no module file was present. If non-nil, then
	// after calling Config.complete, modFile.Module will be
	// equal to Module.
	modFile *modfile.File

	// parserConfig holds the configuration that will be passed
	// when parsing CUE files. It includes the version from
	// the module file.
	parserConfig parser.Config

	// Package defines the name of the package to be loaded. If this is not set,
	// the package must be uniquely defined from its context. Special values:
	//    _    load files without a package
	//    *    load all packages. Files without packages are loaded
	//         in the _ package.
	Package string

	// Dir is the base directory for import path resolution.
	// For example, it is used to determine the main module,
	// and rooted import paths starting with "./" are relative to it.
	// If Dir is empty, the current directory is used.
	//
	// When using an Overlay with file entries such as "/foo/bar/baz.cue",
	// you can use an absolute path that is a parent of one of the overlaid files,
	// such as in this case "/foo" or "/foo/bar", even if these directories
	// do not exist in the host filesystem.
	Dir string

	// Tags defines boolean tags or key-value pairs to select files to build
	// or be injected as values in fields.
	//
	// Each string is of the form
	//
	//     key [ "=" value ]
	//
	// where key is a valid CUE identifier and value valid CUE scalar.
	//
	// The Tags values are used to both select which files get included in a
	// build and to inject values into the AST.
	//
	//
	// File selection
	//
	// Files with an attribute of the form @if(expr) before a package clause
	// are conditionally included if expr resolves to true, where expr refers to
	// boolean values in Tags.
	//
	// It is an error for a file to have more than one @if attribute or to
	// have a @if attribute without or after a package clause.
	//
	//
	// Value injection
	//
	// The Tags values are also used to inject values into fields with a
	// @tag attribute.
	//
	// For any field of the form
	//
	//    field: x @tag(key)
	//
	// and Tags value for which the name matches key, the field will be
	// modified to
	//
	//   field: x & "value"
	//
	// By default, the injected value is treated as a string. Alternatively, a
	// "type" option of the @tag attribute allows a value to be interpreted as
	// an int, number, or bool. For instance, for a field
	//
	//    field: x @tag(key,type=int)
	//
	// an entry "key=2" modifies the field to
	//
	//    field: x & 2
	//
	// Valid values for type are "int", "number", "bool", and "string".
	//
	// A @tag attribute can also define shorthand values, which can be injected
	// into the fields without having to specify the key. For instance, for
	//
	//    environment: string @tag(env,short=prod|staging)
	//
	// the Tags entry "prod" sets the environment field to the value "prod".
	// This is equivalent to a Tags entry of "env=prod".
	//
	// The use of @tag does not preclude using any of the usual CUE constraints
	// to limit the possible values of a field. For instance
	//
	//    environment: "prod" | "staging" @tag(env,short=prod|staging)
	//
	// ensures the user may only specify "prod" or "staging".
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

	// SkipImports causes the loading to ignore all imports and dependencies.
	// The registry will never be consulted. Any external package paths
	// mentioned on the command line will result in an error.
	// The [cue/build.Instance.Imports] field will be empty.
	SkipImports bool

	// If DataFiles is set, the loader includes entries for directories that
	// have no CUE files, but have recognized data files that could be converted
	// to CUE.
	DataFiles bool

	// ParseFile is called to read and parse each file when preparing a
	// package's syntax tree. It must be safe to call ParseFile simultaneously
	// from multiple goroutines. If ParseFile is nil, the loader will uses
	// parser.ParseFile.
	//
	// ParseFile should parse the source from src and use filename only for
	// recording position information.
	//
	// An application may supply a custom implementation of ParseFile to change
	// the effective file contents or the behavior of the parser, or to modify
	// the syntax tree.
	ParseFile func(name string, src interface{}, cfg parser.Config) (*ast.File, error)

	// Overlay provides a mapping of absolute file paths to file contents,
	// which are overlaid on top of the host operating system when loading files.
	//
	// If an overlaid file already exists in the host filesystem,
	// the overlaid file contents will be used in its place.
	// If an overlaid file does not exist in the host filesystem,
	// the loader behaves as if the overlaid file exists with its contents,
	// and that that all of its parent directories exist too.
	Overlay map[string]Source

	// Stdin defines an alternative for os.Stdin for the file "-". When used,
	// the corresponding build.File will be associated with the full buffer.
	Stdin io.Reader

	// Registry is used to fetch CUE module dependencies.
	//
	// When nil, [modconfig.NewRegistry] will be used to create a
	// registry instance using the variables set in [Config.Env]
	// as documented in `[cue help registryconfig]`.
	//
	// THIS IS EXPERIMENTAL. API MIGHT CHANGE.
	//
	// [cue help registryconfig]: https://cuelang.org/docs/reference/command/cue-help-registryconfig/
	Registry modconfig.Registry

	// Env provides environment variables for use in the configuration.
	// Currently this is only used in the construction of the Registry
	// value (see above). If this is nil, the current process's environment
	// will be used.
	Env []string

	fileSystem *fileSystem
}

func (c *Config) stdin() io.Reader {
	if c.Stdin == nil {
		return os.Stdin
	}
	return c.Stdin
}

type importPath string

func addImportQualifier(pkg importPath, name string) (importPath, error) {
	if name == "" {
		return pkg, nil
	}
	ip := ast.ParseImportPath(string(pkg))
	if ip.Qualifier == "_" {
		return "", fmt.Errorf("invalid import qualifier _ in %q", pkg)
	}
	if ip.ExplicitQualifier && ip.Qualifier != name {
		return "", fmt.Errorf("non-matching package names (%s != %s)", ip.Qualifier, name)
	}
	ip.Qualifier = name
	return importPath(ip.String()), nil
}

// Complete updates the configuration information. After calling complete,
// the following invariants hold:
//   - c.Dir is an absolute path.
//   - c.ModuleRoot is an absolute path
//   - c.Module is set to the module import prefix if there is a cue.mod file
//     with the module property.
//   - c.loader != nil
//   - c.cache != ""
//
// It does not initialize c.Context, because that requires the
// loader in order to use for build.Loader.
func (c Config) complete() (cfg *Config, err error) {
	// Ensure [Config.Dir] is a clean and absolute path,
	// necessary for matching directory prefixes later.
	if c.Dir == "" {
		c.Dir, err = os.Getwd()
		if err != nil {
			return nil, err
		}
	} else if c.Dir, err = filepath.Abs(c.Dir); err != nil {
		return nil, err
	}

	// TODO: we could populate this already with absolute file paths,
	// but relative paths cannot be added. Consider what is reasonable.
	fsys, err := newFileSystem(&c)
	if err != nil {
		return nil, err
	}
	c.fileSystem = fsys

	// Ensure [Config.ModuleRoot] is a clean and absolute path,
	// necessary for matching directory prefixes later.
	//
	// TODO: determine root on a package basis. Maybe we even need a
	// pkgname.cue.mod
	// Look to see if there is a cue.mod.
	//
	// TODO(mvdan): note that setting Config.ModuleRoot to a directory
	// without a cue.mod file does not result in any error, which is confusing
	// or can lead to not using the right CUE module silently.
	if c.ModuleRoot == "" {
		// Only consider the current directory by default
		c.ModuleRoot = c.Dir
		if root := c.findModRoot(c.Dir); root != "" {
			c.ModuleRoot = root
		}
	} else if !filepath.IsAbs(c.ModuleRoot) {
		c.ModuleRoot = filepath.Join(c.Dir, c.ModuleRoot)
	} else {
		c.ModuleRoot = filepath.Clean(c.ModuleRoot)
	}
	if c.SkipImports {
		// We should never use the registry in SkipImports mode
		// but make it always return an error just to be sure.
		c.Registry = errorRegistry{errors.New("unexpected use of registry in SkipImports mode")}
	} else if c.Registry == nil {
		registry, err := modconfig.NewRegistry(&modconfig.Config{
			Env: c.Env,
		})
		if err != nil {
			// If there's an error in the registry configuration,
			// don't error immediately, but only when we actually
			// need to resolve modules.
			registry = errorRegistry{err}
		}
		c.Registry = registry
	}
	c.parserConfig = parser.NewConfig(parser.ParseComments)
	if err := c.loadModule(); err != nil {
		return nil, err
	}
	return &c, nil
}

func (c *Config) languageVersion() string {
	if c.modFile == nil || c.modFile.Language == nil {
		return ""
	}
	return c.modFile.Language.Version
}

// loadModule loads the module file, resolves and downloads module
// dependencies. It sets c.Module if it's empty or checks it for
// consistency with the module file otherwise.
//
// Note that this function is a no-op if a module file does not exist,
// as it is still possible to load CUE without a module.
func (c *Config) loadModule() error {
	// TODO: also make this work if run from outside the module?
	modDir := filepath.Join(c.ModuleRoot, modDir)
	modFile := filepath.Join(modDir, moduleFile)
	f, cerr := c.fileSystem.openFile(modFile)
	if cerr != nil {
		// If we could not load cue.mod/module.cue, check whether the reason was
		// a legacy cue.mod file and give the user a clear error message.
		//
		// Common case: the file does not exist. Avoid an extra stat
		// syscall using the error code when we can.
		//
		// TODO(mvdan): we can remove this in mid 2026, once we can safely assume that
		// practically all cue.mod files have vanished.
		if errors.Is(cerr, fs.ErrNotExist) && runtime.GOOS != "windows" {
			// The file definitely does not exist. On Windows unfortunately due
			// to https://github.com/golang/go/issues/46734
			// we can't tell the difference between "does not exist"
			// and "is not a directory", hence the special casing.
			return nil
		}
		info, cerr2 := c.fileSystem.stat(modDir)
		if cerr2 == nil && !info.IsDir() {
			return fmt.Errorf("cue.mod files are no longer supported; use cue.mod/module.cue")
		}
		return nil
	}
	defer f.Close()
	data, err := io.ReadAll(f)
	if err != nil {
		return err
	}
	parseModFile := modfile.ParseNonStrict
	if c.Registry == nil {
		parseModFile = modfile.ParseLegacy
	} else if c.AcceptLegacyModules {
		// Note: technically this does not support all legacy module
		// files because some old module files might contain non-concrete
		// data, but that seems like an OK restriction for now at least,
		// given that no actual instances of non-concrete data in
		// module files have been discovered in the wild.
		parseModFile = modfile.FixLegacy
	}
	mf, err := parseModFile(data, modFile)
	if err != nil {
		return err
	}
	c.modFile = mf
	if mf.QualifiedModule() == "" {
		// Backward compatibility: allow empty module.cue file.
		// TODO maybe check that the rest of the fields are empty too?
		return nil
	}
	if c.Module != "" && c.Module != mf.Module {
		return errors.Newf(token.NoPos, "inconsistent modules: got %q, want %q", mf.Module, c.Module)
	}
	c.Module = mf.QualifiedModule()
	// Set the default version for CUE files without a module.
	c.parserConfig = c.parserConfig.Apply(parser.Version(c.modFile.Language.Version))
	return nil
}

func (c Config) isModRoot(dir string) bool {
	// Note: cue.mod used to be a file. We still allow both to match.
	_, err := c.fileSystem.stat(filepath.Join(dir, modDir))
	return err == nil
}

// findModRoot returns the module root that's ancestor
// of the given absolute directory path, or "" if none was found.
func (c Config) findModRoot(absDir string) string {
	abs := absDir
	for {
		if c.isModRoot(abs) {
			return abs
		}
		d := filepath.Dir(abs)
		if filepath.Base(filepath.Dir(abs)) == modDir {
			// The package was located within a "cue.mod" dir and there was
			// not cue.mod found until now. So there is no root.
			return ""
		}
		if len(d) >= len(abs) {
			return "" // reached top of file system, no cue.mod
		}
		abs = d
	}
}

func (c *Config) newErrInstance(err error) *build.Instance {
	i := c.Context.NewInstance("", nil)
	i.Root = c.ModuleRoot
	i.Module = c.Module
	i.ModuleFile = c.modFile
	i.Err = errors.Promote(err, "")
	return i
}

// errorRegistry implements [modconfig.Registry] by returning err from all methods.
type errorRegistry struct {
	err error
}

func (r errorRegistry) Requirements(ctx context.Context, m module.Version) ([]module.Version, error) {
	return nil, r.err
}

func (r errorRegistry) Fetch(ctx context.Context, m module.Version) (module.SourceLoc, error) {
	return module.SourceLoc{}, r.err
}

func (r errorRegistry) ModuleVersions(ctx context.Context, mpath string) ([]string, error) {
	return nil, r.err
}
