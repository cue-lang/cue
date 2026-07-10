// Copyright 2026 The CUE Authors
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

package cueload

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"io/fs"
	"iter"
	"os"
	"path"
	"path/filepath"
	"sync"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/parser"
	"cuelang.org/go/cue/stats"
	"cuelang.org/go/cue/token"
	cue "cuelang.org/go/cue/v2"
	"cuelang.org/go/cuecodec"
	"cuelang.org/go/internal"
	"cuelang.org/go/internal/core/adt"
	"cuelang.org/go/internal/core/compile"
	"cuelang.org/go/internal/core/debug"
	"cuelang.org/go/internal/core/runtime"
	"cuelang.org/go/internal/cuedebug"
	"cuelang.org/go/internal/envflag"
	"cuelang.org/go/internal/mod/modrequirements"
	"cuelang.org/go/internal/v2bridge"
	"cuelang.org/go/mod/modconfig"
	"cuelang.org/go/mod/modfile"
	"cuelang.org/go/mod/module"

	// Register the standard library builtins so that stdlib imports
	// resolve, mirroring cue/cuecontext.
	_ "cuelang.org/go/pkg"
)

// A Loader loads packages and files and converts them to CUE values.
// It is safe for concurrent use, and caches parses, module resolution,
// and built packages across calls. It owns the evaluation runtime its
// values belong to: values from different Loaders may not be combined.
type Loader struct {
	cfg      Config
	rootFS   fs.FS  // the filesystem "/"-rooted paths refer to
	dir      string // absolute (slash-separated, "/"-rooted) base directory
	codecs   *cuecodec.Set
	rt       *runtime.Runtime
	registry modconfig.Registry
	recorder *stats.Recorder

	buildTagSet map[string]bool
	parserCfg   parser.Config

	moduleRoot string // absolute path of the module root (or dir if no module)
	modFile    *modfile.File
	module     *Module // main module, or nil when loading outside a module
	reqs       *modrequirements.Requirements

	empty cue.Value // the top value, used by At and Go

	// buildMu serializes package building. It guards the build-state
	// fields of every Package created by this loader (see Package).
	// It must not be held while calling user-provided hooks such as
	// Config.Resolve.
	buildMu sync.Mutex

	// mu guards the fields below it.
	mu             sync.Mutex
	pkgs           map[string]*Package // canonical import path -> package
	modules        map[module.Version]*Module
	pkgByVertex    map[*adt.Vertex]*Package
	originByValue  map[cue.Value]Origin
	originByVertex map[*adt.Vertex]Origin
	tagVarValues   map[string]ast.Expr
	spellings      map[string]string // registered import spelling -> canonical path
}

// New returns a new Loader for the given configuration. A nil cfg is
// equivalent to a zero one. The configuration must not be modified
// after this call.
func New(cfg *Config) (*Loader, error) {
	var c Config
	if cfg != nil {
		c = *cfg
	}
	l := &Loader{
		cfg:            c,
		codecs:         c.Codecs,
		registry:       c.Registry,
		buildTagSet:    make(map[string]bool),
		pkgs:           make(map[string]*Package),
		modules:        make(map[module.Version]*Module),
		pkgByVertex:    make(map[*adt.Vertex]*Package),
		originByValue:  make(map[cue.Value]Origin),
		originByVertex: make(map[*adt.Vertex]Origin),
		tagVarValues:   make(map[string]ast.Expr),
		spellings:      make(map[string]string),
	}
	if l.codecs == nil {
		l.codecs = cuecodec.Default()
	}
	if l.registry == nil {
		l.registry = errRegistry{}
	}
	for _, t := range c.BuildTags {
		l.buildTagSet[t] = true
	}

	// The evaluation runtime. We use explicit settings so that the
	// runtime configuration does not depend on environment variables.
	var ec EvaluatorConfig
	if c.Evaluator != nil {
		ec = *c.Evaluator
	}
	if len(ec.Injections) > 0 {
		// TODO(cueload): support compile-time injections (@embed, wasm).
		return nil, fmt.Errorf("cueload: compile-time injections are not supported yet")
	}
	version := internal.DefaultVersion
	switch ec.Version {
	case EvalDefault:
	case EvalV2:
		version = internal.EvalV2
	case EvalV3:
		version = internal.EvalV3
	default:
		return nil, fmt.Errorf("cueload: unknown evaluator version %d", ec.Version)
	}
	var debugFlags cuedebug.Config
	if err := envflag.Parse(&debugFlags, ec.Debug); err != nil {
		return nil, fmt.Errorf("cueload: invalid debug options: %v", err)
	}
	l.rt = runtime.NewWithSettings(version, debugFlags)
	l.recorder = ec.Recorder

	// The filesystem and base directory. This is the one place where a
	// zero Config touches ambient state: with a nil FS the host
	// filesystem is used and Dir defaults to the working directory.
	if c.FS != nil {
		l.rootFS = c.FS
		if c.Dir == "" {
			l.dir = "/"
		} else {
			l.dir = absJoin("/", c.Dir)
		}
	} else {
		// TODO(cueload): this adapter assumes slash-separated absolute
		// paths rooted at "/" and does not support Windows drive
		// letters yet.
		l.rootFS = os.DirFS("/")
		if c.Dir == "" {
			wd, err := os.Getwd()
			if err != nil {
				return nil, err
			}
			l.dir = filepath.ToSlash(wd)
		} else if path.IsAbs(c.Dir) {
			l.dir = path.Clean(c.Dir)
		} else {
			wd, err := os.Getwd()
			if err != nil {
				return nil, err
			}
			l.dir = absJoin(filepath.ToSlash(wd), c.Dir)
		}
	}

	l.parserCfg = parser.NewConfig(parser.ParseComments)
	if err := l.initModule(); err != nil {
		return nil, err
	}

	// The top value, used to construct At and Go results.
	emptyFile, err := parser.ParseFile("-", "_", parser.NewConfig())
	if err != nil {
		return nil, err
	}
	ev, cerr := compile.Files(nil, l.rt, "_", emptyFile)
	if cerr != nil {
		return nil, cerr
	}
	l.empty = l.newValue(ev)
	return l, nil
}

// initModule discovers and loads the main module: it determines the
// module root (walking up from the base directory unless pinned by
// Config.ModuleRoot) and parses cue.mod/module.cue if present. Loading
// without a module is not an error; import resolution is simply
// unavailable.
func (l *Loader) initModule() error {
	root := ""
	if l.cfg.ModuleRoot != "" {
		root = absJoin(l.dir, l.cfg.ModuleRoot)
	} else {
		for d := l.dir; ; {
			if info, err := fs.Stat(l.rootFS, fsPath(path.Join(d, "cue.mod"))); err == nil && info.IsDir() {
				root = d
				break
			}
			parent := path.Dir(d)
			if parent == d {
				break
			}
			d = parent
		}
	}
	if root == "" {
		l.moduleRoot = l.dir
		return nil
	}
	l.moduleRoot = root

	modFilePath := path.Join(root, "cue.mod", "module.cue")
	data, err := fs.ReadFile(l.rootFS, fsPath(modFilePath))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return err
	}
	mf, err := modfile.ParseNonStrict(data, modFilePath)
	if err != nil {
		return err
	}
	if mf.QualifiedModule() == "" {
		// An empty module.cue file; treat as no module.
		return nil
	}
	l.modFile = mf
	if mf.Language != nil && mf.Language.Version != "" {
		l.parserCfg = l.parserCfg.Apply(parser.Version(mf.Language.Version))
	}
	rootFS, err := l.subFS(root)
	if err != nil {
		return err
	}
	l.module = &Module{
		loader:  l,
		path:    mf.QualifiedModule(),
		file:    mf,
		loc:     module.SourceLoc{FS: rootFS, Dir: "."},
		rootDir: root,
	}
	l.reqs = modrequirements.NewRequirements(
		mf.QualifiedModule(),
		l.registry,
		mf.DepVersions(),
		mf.DefaultMajorVersions(),
	)
	return nil
}

// Load interprets src, yielding its values in order. Work happens as the
// iterator advances: files are decoded and packages built on demand.
// Per-value errors flow through the stream without terminating it;
// structural errors (an unmatched pattern, an unknown codec) end it.
func (l *Loader) Load(ctx context.Context, src Source) iter.Seq2[cue.Value, error] {
	return func(yield func(cue.Value, error) bool) {
		if src.op == nil {
			yield(cue.Value{}, fmt.Errorf("cueload: invalid (zero) Source"))
			return
		}
		for item, fatal := range l.run(ctx, src.op) {
			if fatal != nil {
				yield(cue.Value{}, fatal)
				return
			}
			if item.hasOrigin {
				l.recordOrigin(item.v, item.origin)
			}
			if !yield(item.v, item.err) {
				return
			}
		}
	}
}

// LoadValue is like Load but requires src to denote exactly one value,
// returning the first error otherwise.
func (l *Loader) LoadValue(ctx context.Context, src Source) (cue.Value, error) {
	var vals []cue.Value
	for v, err := range l.Load(ctx, src) {
		if err != nil {
			return cue.Value{}, err
		}
		vals = append(vals, v)
	}
	if len(vals) != 1 {
		return cue.Value{}, fmt.Errorf("cueload: expected exactly one value, got %d", len(vals))
	}
	return vals[0], nil
}

// Packages loads the packages matched by the given patterns: import
// paths, relative directories, or "./..." wildcards. It returns one
// canonical *Package per matched import path. If no patterns are given,
// "." is assumed.
func (l *Loader) Packages(ctx context.Context, patterns ...string) ([]*Package, error) {
	if len(patterns) == 0 {
		patterns = []string{"."}
	}
	var all []string
	for _, pat := range patterns {
		paths, err := l.expandPattern(pat)
		if err != nil {
			return nil, err
		}
		all = append(all, paths...)
	}
	byPath, err := l.ensurePackages(ctx, all)
	if err != nil {
		return nil, err
	}
	var result []*Package
	seen := make(map[*Package]bool)
	for _, p := range all {
		pkg := byPath[p]
		if pkg == nil || seen[pkg] {
			continue
		}
		seen[pkg] = true
		result = append(result, pkg)
	}
	return result, nil
}

// Package is like Packages but requires exactly one match.
func (l *Loader) Package(ctx context.Context, pattern string) (*Package, error) {
	pkgs, err := l.Packages(ctx, pattern)
	if err != nil {
		return nil, err
	}
	if len(pkgs) != 1 {
		return nil, fmt.Errorf("cueload: pattern %q matched %d packages, want 1", pattern, len(pkgs))
	}
	return pkgs[0], nil
}

// Decode opens f and yields its documents in order: one Doc for a
// single-document format such as JSON, several for multi-document YAML
// or JSON Lines.
func (l *Loader) Decode(ctx context.Context, f File) iter.Seq2[Doc, error] {
	return func(yield func(Doc, error) bool) {
		if err := ctx.Err(); err != nil {
			yield(Doc{File: f}, err)
			return
		}
		dec, err := l.decoderFor(f)
		if err != nil {
			yield(Doc{File: f}, err)
			return
		}
		rc, err := l.openFile(f)
		if err != nil {
			yield(Doc{File: f}, err)
			return
		}
		defer rc.Close()
		opts := &cuecodec.DecodeOptions{
			Filename: f.Name,
			Options:  f.Type.Options,
		}
		i := 0
		for syntax, err := range dec.NewDecoder(rc, opts) {
			if cerr := ctx.Err(); cerr != nil {
				yield(Doc{File: f, Index: i}, cerr)
				return
			}
			if err != nil {
				yield(Doc{File: f, Index: i}, err)
				return
			}
			if !yield(Doc{File: f, Index: i, Syntax: syntax, loader: l}, nil) {
				return
			}
			i++
		}
	}
}

// Build converts parsed CUE syntax to a value in the loader's runtime.
// Imports resolve against the enclosing module as usual. The returned
// value is lazy; evaluation happens when it is forced.
func (l *Loader) Build(ctx context.Context, f *ast.File) (cue.Value, error) {
	w, err := l.buildFiles(ctx, "_", []*ast.File{f})
	if err != nil {
		return cue.Value{}, err
	}
	return l.newValue(w), nil
}

// PackageOf reports the package that v was built from: the nearest
// enclosing package root of v. It replaces v1's Value.BuildInstance.
// Values derived from package values (for example by unification with
// values from elsewhere) may not belong to any package.
//
// PackageOf answers from the realized state of v: for a value that has
// not been evaluated or realized yet it reports false.
func (l *Loader) PackageOf(v cue.Value) (*Package, bool) {
	rt, w := v2bridge.VertexOf(v)
	if w == nil || (rt != nil && rt != l.rt) {
		return nil, false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	for ; w != nil; w = w.Parent {
		if p, ok := l.pkgByVertex[w]; ok {
			return p, true
		}
	}
	return nil, false
}

// OriginOf reports the input that produced v, for values as yielded by
// [Loader.Load] or built by [Doc.Value].
func (l *Loader) OriginOf(v cue.Value) (Origin, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if o, ok := l.originByValue[v]; ok {
		return o, true
	}
	if _, w := v2bridge.VertexOf(v); w != nil {
		if o, ok := l.originByVertex[w]; ok {
			return o, true
		}
	}
	return Origin{}, false
}

// NewPackage creates a package not backed by the module system, for use
// by [PackageResolver] implementations. The package's value is the
// unification of the given files, which must be CUE files. Imports in
// the files resolve through the loader as usual.
func (l *Loader) NewPackage(ctx context.Context, path ast.ImportPath, files ...File) (*Package, error) {
	if len(files) == 0 {
		return nil, fmt.Errorf("cueload: NewPackage %v: no files given", path)
	}
	syntax, _, err := l.parseCUEFiles(files)
	if err != nil {
		return nil, err
	}
	sfs := make([]*SourceFile, len(syntax))
	for i, f := range syntax {
		sfs[i] = &SourceFile{Name: files[i].Name, Syntax: f}
	}
	p := &Package{
		loader: l,
		ip:     path.Canonical(),
		files:  sfs,
	}
	if err := l.resolveFileImports(ctx, p); err != nil {
		return nil, err
	}
	return l.internPackage(p.ip.String(), p), nil
}

// NewValuePackage creates a package whose value is v, for
// host-implemented packages (typically structs of [cue.Func] values).
// The value is forced when the package is first built.
func (l *Loader) NewValuePackage(path ast.ImportPath, v cue.Value) (*Package, error) {
	if v == (cue.Value{}) {
		return nil, fmt.Errorf("cueload: NewValuePackage %v: invalid (zero) value", path)
	}
	p := &Package{
		loader: l,
		ip:     path.Canonical(),
		fixed:  &v,
	}
	return l.internPackage(p.ip.String(), p), nil
}

// newValue wraps a vertex as a lazy cue/v2 value owned by the loader's
// runtime.
func (l *Loader) newValue(w *adt.Vertex) cue.Value {
	return v2bridge.NewVertexValue(l.rt, w).(cue.Value)
}

// recordOrigin remembers the origin of a value yielded by Load or built
// by Doc.Value, for later retrieval via OriginOf.
func (l *Loader) recordOrigin(v cue.Value, o Origin) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.originByValue[v] = o
	if _, w := v2bridge.VertexOf(v); w != nil {
		l.originByVertex[w] = o
	}
}

// forceVertex forces v and reports its realized vertex. The valueErr
// result holds the value's own error, if any; the fatal result reports
// failure of the forcing operation itself (cancellation, or a value
// from a foreign loader).
func (l *Loader) forceVertex(ctx context.Context, v cue.Value) (w *adt.Vertex, valueErr, fatal error) {
	if v == (cue.Value{}) {
		return nil, nil, fmt.Errorf("cueload: invalid (zero) value")
	}
	verr := v.Err(ctx)
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	rt, w := v2bridge.VertexOf(v)
	if rt != nil && rt != l.rt {
		return nil, nil, fmt.Errorf("cueload: value belongs to a different loader")
	}
	if w == nil {
		if verr != nil {
			return nil, nil, verr
		}
		return nil, nil, fmt.Errorf("cueload: cannot realize value")
	}
	return w, verr, nil
}

// newOpContext returns an evaluation context for one operation run by
// the loader itself. A stats recorder carried by ctx overrides the
// loader-level recorder.
func (l *Loader) newOpContext(ctx context.Context, v *adt.Vertex) *adt.OpContext {
	opCtx := adt.New(v, &adt.Config{
		Runtime: l.rt,
		Format:  nodeFormat,
		Context: ctx,
	})
	if ctx != nil {
		if r, ok := stats.RecorderFromContext(ctx); ok {
			opCtx.StatsRecorder = r
			return opCtx
		}
	}
	if l.recorder != nil {
		opCtx.StatsRecorder = l.recorder
	}
	return opCtx
}

var printConfig = &debug.Config{Compact: true, CompactBuiltins: true}

func nodeFormat(r adt.Runtime, n adt.Node) string {
	return debug.NodeString(r, n, printConfig)
}

// decoderFor resolves the codec for f and requires it to support
// decoding.
func (l *Loader) decoderFor(f File) (cuecodec.Decoder, error) {
	c, err := l.codecFor(f)
	if err != nil {
		return nil, err
	}
	dec, ok := c.(cuecodec.Decoder)
	if !ok {
		return nil, fmt.Errorf("cueload: file type %q does not support decoding", c.Name())
	}
	return dec, nil
}

// codecFor resolves the codec for f, from its explicit type or its file
// extension.
func (l *Loader) codecFor(f File) (cuecodec.Codec, error) {
	if name := f.Type.Codec; name != "" {
		e, ok := l.codecs.Lookup(name)
		if !ok {
			return nil, fmt.Errorf("cueload: unknown codec %q", name)
		}
		c, ok := e.(cuecodec.Codec)
		if !ok {
			return nil, fmt.Errorf("cueload: %q is not a codec", name)
		}
		return c, nil
	}
	ext := path.Ext(f.Name)
	c, ok := l.codecs.ByExtension(ext)
	if !ok {
		return nil, fmt.Errorf("cueload: cannot determine file type for %q", f.Name)
	}
	return c, nil
}

// openFile returns the content of f, per the File contract: literal
// Data, the Open hook, or the loader's filesystem.
func (l *Loader) openFile(f File) (io.ReadCloser, error) {
	switch {
	case f.Data != nil && f.Open != nil:
		return nil, fmt.Errorf("cueload: file %q sets both Data and Open", f.Name)
	case f.Data != nil:
		return io.NopCloser(bytes.NewReader(f.Data)), nil
	case f.Open != nil:
		return f.Open()
	}
	rc, err := l.rootFS.Open(fsPath(absJoin(l.dir, f.Name)))
	if err != nil {
		return nil, err
	}
	return rc, nil
}

// readFile returns the full content of f.
func (l *Loader) readFile(f File) ([]byte, error) {
	rc, err := l.openFile(f)
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return io.ReadAll(rc)
}

// parseCUEFiles reads and parses the given files, which must all be CUE
// files, and reports their common package name.
func (l *Loader) parseCUEFiles(files []File) ([]*ast.File, string, error) {
	var syntax []*ast.File
	pkgName := ""
	for i, f := range files {
		c, err := l.codecFor(f)
		if err != nil {
			return nil, "", err
		}
		if c.Name() != "cue" {
			return nil, "", fmt.Errorf("cueload: %q is not a CUE file (codec %q)", f.Name, c.Name())
		}
		data, err := l.readFile(f)
		if err != nil {
			return nil, "", err
		}
		af, err := l.parseFile(f.Name, data, l.parserCfg)
		if err != nil {
			return nil, "", err
		}
		if name := af.PackageName(); name != "" {
			if pkgName == "" || i == 0 {
				pkgName = name
			} else if name != pkgName {
				return nil, "", fmt.Errorf("cueload: found packages %q (%s) and %q (%s)",
					pkgName, files[0].Name, name, f.Name)
			}
		}
		syntax = append(syntax, af)
	}
	return syntax, pkgName, nil
}

// parseFile parses a CUE file, honoring Config.ParseFile.
func (l *Loader) parseFile(name string, data []byte, cfg parser.Config) (*ast.File, error) {
	if l.cfg.ParseFile != nil {
		return l.cfg.ParseFile(name, data, cfg)
	}
	return parser.ParseFile(name, data, cfg)
}

// subFS returns the filesystem rooted at the given absolute directory.
func (l *Loader) subFS(dir string) (fs.FS, error) {
	p := fsPath(dir)
	if p == "." {
		return l.rootFS, nil
	}
	return fs.Sub(l.rootFS, p)
}

// errRegistry is the registry used by a hermetic zero Config: any
// attempt to resolve a module through it fails.
type errRegistry struct{}

var errNoRegistry = fmt.Errorf("no module registry configured (a zero cueload.Config resolves only packages in the main module and the standard library)")

func (errRegistry) Fetch(ctx context.Context, m module.Version) (module.SourceLoc, error) {
	return module.SourceLoc{}, errNoRegistry
}

func (errRegistry) ModFile(ctx context.Context, m module.Version) (*modfile.File, error) {
	return nil, errNoRegistry
}

func (errRegistry) ModuleVersions(ctx context.Context, mpath string) ([]string, error) {
	return nil, errNoRegistry
}

// errVertexf returns a finalized vertex holding an error with the given
// message.
func errVertexf(format string, args ...any) *adt.Vertex {
	return errVertex(errors.Newf(token.NoPos, format, args...))
}

// errVertex returns a finalized vertex holding the given error.
func errVertex(err error) *adt.Vertex {
	b := &adt.Bottom{Err: errors.Promote(err, "")}
	n := &adt.Vertex{BaseValue: b}
	n.ForceDone()
	n.AddConjunct(adt.MakeRootConjunct(nil, b))
	return n
}

// absJoin resolves p relative to the absolute slash-separated directory
// dir, returning a cleaned "/"-rooted path.
func absJoin(dir, p string) string {
	if path.IsAbs(p) {
		return path.Clean(p)
	}
	return path.Join(dir, p)
}

// fsPath converts a "/"-rooted path to an [io/fs]-style path within the
// loader's root filesystem.
func fsPath(abs string) string {
	p := path.Clean(abs)
	for len(p) > 0 && p[0] == '/' {
		p = p[1:]
	}
	if p == "" {
		return "."
	}
	return p
}
