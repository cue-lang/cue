// Copyright 2025 CUE Authors
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

// WARNING: THIS PACKAGE IS EXPERIMENTAL.
// ITS API MAY CHANGE AT ANY TIME.
//
// Evaluator performs light-weight evaluation of CUE ASTs. It is used
// in the LSP for "jump-to-definition" functionality, amongst
// others. A path is either an [ast.Ident], or a CUE expression
// followed by zero or more idents, all chained together by dots.
//
// # Introduction
//
// In the text that follows, subscripts are used in order to make
// identifiers (idents) unique for the purpose of explanation, but
// they should not be considered part of the ident itself, from the
// point of view of CUE.
//
// For example, in the code:
//
//	x₁: 17
//	y: x₂
//
// If the user places their cursor on `x₂` and invokes
// "jump-to-definition", the cursor should move to `x₁`. In CUE, there
// can be several nodes that define a binding. For example:
//
//	x₁: 17
//	y: x₂
//	x₃: int
//
// Now, if the user places their cursor on `x₂` and invokes
// "jump-to-definition", they should see both `x₁` and `x₃` as targets
// to which they can jump.
//
// The implementation is a lazy, memoized, call-by-need evaluator. The
// purpose of this evaluator is to calculate what each element of each
// path resolves to; to track uses of fields; documentation comments;
// and other properties useful for the LSP. There is no subsumption,
// no unification of values other than structs. And the little that
// this evaluator does do is imprecise. For example, it does not test
// field names (even when known) against patterns. It does not compute
// the names of dynamic fields, even when it is trivial to do so. It
// is a MAY-analysis and not a MUST-analysis. This means that it may
// offer jump-to-definition targets that do not occur during full
// evaluation, but which we are unable to dismiss with only the simple
// evaluation offered here.  A good example of this is with
// disjunctions:
//
//	x₁: {a₁: 3} | {a₂: 4}
//	y₁: x₂
//	y₂: a₃: <n₁
//	n₂: 4
//	z₁: y₃.a₄
//
// Here, `a₄` will always resolve to both `a₁` and `a₂`, even though
// the constraint via `a₃` may (or may not) eliminate one (or both!)
// branches of the disjunction.
//
// # Algorithm 1: simplified CUE
//
// In CUE, a path such as `x.y.z`, where `x` is an ident, is only
// legal if `x` is defined in the same lexical scope as the path
// `x.y.z`, or any ancestor lexical scope. There is one exception to
// this which is the package scope, which arguably doesn't exist
// lexically. We return to the package scope much later on.
//
// This restriction on paths complicates the algorithm. For example:
//
//	x₁: y₁: x₂.a₁
//	x₃: {
//		x₄: a₂: 17
//		z₁: x₅.a₃
//	}
//	x₆: a₄: 18
//
// Here, `x₂` refers to `x₁`, `x₃`, and `x₆`, whilst `x₅` refers only
// to `x₄`. Similarly, `a₁` refers to `a₄`, but `a₃` refers to `a₂`.
//
// To explain this evaluator, we start with a simplified version of
// CUE which does not place this restriction on paths: i.e. the first
// (and possibly only) element of a path may resolve to a definition
// that does *not* exist in the same lexical scope (or ancestor of) as
// that path.
//
// In this evaluator, a "frame" is a collection of bindings,
// i.e. key-value pairs. The values are themselves frames.  A frame is
// created with one or more unprocessed [ast.Node] values, for
// example, a [ast.File], or a [ast.StructLit].
//
// When a frame is evaluated, its unprocessed values are unpacked. A
// [ast.StructLit] for example contains a number of [ast.Decl] nodes,
// which are themselves then processed. When a frame encounters a
// [ast.Field], the frame ensures a binding exists for the field's
// name, and adds the field's value to the binding's frame's
// unprocessed values. Thus if the same field definition is
// encountered multiple times, its values are accumulated into a
// single child frame. Note that evaluation of a frame is not
// recursive: its bindings are not automatically evaluated. Thus a
// frame is the unit of evaluation; by adding new frames you can
// create new points where evaluation can pause (and optionally resume
// later on).
//
// If, during evaluation, a frame encounters a path, the path might
// correspond to the value of a field (i.e. the frame is for something
// like `x: y`), or an embedding into a struct. The frame keeps track
// of these embedded paths and once processing of the frame's values
// is complete, it then resolves the embedded paths to further frames,
// and records that this frame itself resolves to these other frames
// (the resolvesTo field).
//
// The consequence is that the evaluation of a frame creates and fully
// populates (with their unprocessed values) all of its own bindings
// before any resolution of paths occurs. Thus evaluation can be
// driven by demand: if a path is encountered that accesses one of the
// frame's bindings (or any binding of an ancestor frame), then it is
// guaranteed that the binding (if it exists) contains its complete
// set of values before it is accessed, and so it is safe to evaluate.
//
// Consider this example:
//
//	x: y
//	y: {
//		a: 3
//		b: y.a
//	}
//
// Evaluating the outermost frame will create two bindings, one for
// `x` (with the path `y` as its value), and one for `y` (containing
// the [ast.StructLit] as its value). If the frame for `y` is
// evaluated, it will create its own bindings for `a` (a frame
// containing the [ast.BasicLit] `3`), and for `b` (a frame containing
// the path `y.a`).
//
// Imagine we want to resolve, in the outermost frame, the path
// `x.a`. We first evaluate the outermost frame, then inspect its
// bindings. We find an `x` in there, so we grab that frame. This
// completes resolving the `x` of `x.a`. We now wish to find an `a`
// within that frame, so we evaluate it. This frame contains only the
// path `y` and so we have to resolve `y` and record that result
// within our frame.
//
// Every frame knows its own parent frame. This frame containing the
// path `y` will inspect its own bindings for `y`, and find
// nothing. It asks its ancestors whether they know of a binding for
// `y`. Its parent does have a binding for `y`, so we grab that
// frame. This completes the resolution of `y`, and thus the
// evaluation of the frame that contains the path `y`. We now ask this
// same frame whether it contains a binding for `a`. It doesn't, but
// we also inspect all the frames that this frame resolves to. There
// is one resolved-to frame, and it does contain a binding for `a`, so
// we grab that. This completes the resolution of `x.a`.
//
// In summary: this algorithm traverses the AST breadth first and
// incrementally, to lazily merge together bindings that share the
// same path into frames.
//
// Unmentioned is that there are various [ast.Expr] types that can use
// paths but not declare their own bindings, for example an
// interpolated string. When these are encountered during evaluation,
// the frame accumulates and processes them in the same way as
// embedded paths. The only difference is they don't need to be
// recorded within the frame's resolves-to set.
//
// # Querying
//
// In the previous section, we walked through the example of
// attempting to resolve the path `x.a` in the outermost frame. But this
// isn't what an LSP client will ask. An LSP client doesn't know what
// path the cursor is on, nor anything about the current scope or how
// these may correspond to frames. The LSP client knows only the
// cursor's line and column number.
//
// To facilitate an API that allows querying by file-coordinates,
// frames are extended with a start and end [token.Pos] range. For
// each [ast.Node] that a frame processes, it extends its range to the
// node's start file-offset and end file-offset. Then, when asked to
// resolve whatever exists at some file-coordinate, we only need to
// evaluate the frames that contain the file-coordinate in question.
//
// # Algorithm 2: real CUE
//
// If we stuck to algorithm 1, it would mean that in:
//
//	a₁: b: c: a₂
//	a₃: b: a₄: 5
//
// `a₂` would resolve to `a₄`. It also means that you get scary
// collisions with aliases, for example:
//
//	a: l₁=b: c: l₂.x
//	a: x: l₃.c
//
// Here, `l₃` resolves to `l₁`, or `b`. So the rule that if the the first
// element of any path is an ident, then it can only be resolved
// lexically, must be implemented. This means that this evaluator must
// model "lexical bindings" which are candidates for resolving the
// first element of a path, separately from "navigable bindings" which
// are candidates for resolving the rest of the path (as you navigate
// the path...). The lexical bindings do not have the "merging"
// behaviour of algorithm 1, for example:
//
//	x₁: y₁: 6
//	x₂: y₂: 7
//
// Whereas before (in Algorithm 1) the evaluator would create one
// binding for `x`, now the evaluator creates two bindings for `x`,
// each having a distinct frame. Both of those frames share a
// "navigable bindings" struct and so any children that either of
// these frames have, can be grouped together appropriately via their
// shared "navigable bindings". Thus in this example, the evaluation
// of the outermost frame creates two bindings for `x`; these distinct
// child frames share a navigable, and also have one binding each for
// their respective `y` fields. These `y` fields are grouped together
// within their own shared navigable.
//
// This means that when resolving the first element of a path, we can
// walk up the lexical bindings only, and then once that's resolved,
// switch to the navigable bindings for the rest of the path.
//
// For aliases, comprehensions and one or two other things, a binding
// can be created in the current frame which is not added to the
// frame's navigable bindings. This means it can only ever be found
// and used as the first element of a path. A navigable binding is
// always also a lexical binding, but a lexical binding need not be a
// navigable binding.
//
// # File and Package scopes
//
// CUE states that fields declared at the top level of a file are not
// in the file's scope, but are in fact in the package's scope. At
// construction, the file frames all share a "navigable". Thus if two
// different files in the same package both declare the same field,
// they will be correctly grouped together within that navigable.
//
// When a file frame processes an [ast.File], lexical and navigable
// bindings will be created as normal. When resolving the first
// element of a path in some deeper frame, it can be the case that
// after walking up the chain of ancestor frames, no matching lexical
// binding is found even within the relevant file's frame's
// bindings. In this case, it is safe to directly inspect the file's
// navigable bindings, which amounts to the package's lexical
// bindings. In this way, a path's first element can be an ident that
// is only declared in some separate file within the same package, and
// yet it can still be resolved.
//
// # Field declaration keys
//
// We wish for jump-to-definition from a field declaration's key to
// resolve to other declarations of the same field. For example:
//
//	x₁: y₁: int
//	x₂: y₂: 7
//
// `x₁` and `x₂` should resolve to each other. Similarly `y₁` and
// `y₂`. To achieve this, when a field is encountered and a new
// binding added, we create a 2nd child frame, which contains a
// [fieldDeclExpr] as its unprocessed value. This value, when
// evaluated, walks up the navigable ancestors, gathering
// their names, and stopping when either the package root is reached,
// or the navigable has no name. From this oldest ancestor,
// the calculated path is then resolved using the normal mechanics for
// path resolution. This path will resolve to all the declarations of
// the field in question. Imagine that in the above example, both
// `int` and `7` are replaced with the path `x.y`.
//
// # Find Usages / References
//
// When a field usage is resolved, we record in the definition
// navigable the frame and [ast.Node] that uses the definition.
//
// For example:
//
//	x: y
//	y: 3
//
// The `y` on line 1 will resolve to the navigable that represents the
// `y` field of line 2. Inside that navigable we record it is "used
// by" the `y` node of line 1.
//
// So, when the user asks for Find Usages for a given file offset, we
// first find the definition of that offset as described
// previously. Field usages resolve to their declarations, and field
// declarations resolve to themselves. Having found the correct
// navigable, we now need to force the evaluation of every frame that
// could possibly make use of this navigable; evaluating a frame is
// enough to add entries to the relevant navigable's usedBy field.
//
// In general, a field can be accessed from anywhere, and so having
// found the definition of a field, the simple approach is to evaluate
// every frame that contributes to the definition's navigable, all of
// the descendants of those frames, all of the ancestors of those
// frames, and also those ancestors' descendants. In other words,
// everything. This could get expensive, so there are a couple of ways
// we can trim evaluation.
//
//	x: {h: 3, y: 4}.y
//
// If the user is asking for the usages of `h`, we evaluate everything
// within the in-line struct lit, but we can detect that `h` is not
// used by `x` and so we do not need to evaluate anything further up
// the tree.
//
//	x: {i: h: 3, y: 4}.i
//
// Here though, we don't care just about `h`, we have to also care
// about the path to `h` from within this in-line struct,
// i.e. `[i,h]`. We can detect that `i` is used by `x`, and so `h` has
// escaped from the in-line struct and so we need to evaluate further
// up the tree where there might be an `x.h`.
//
// Even if a field does escape an in-line struct, other expressions
// can block it from being accessed.
//
//	x: struct.MaxFields({i: h: 3}.i, 1)
//
// Although `h` escapes from the in-line struct, it does not escape
// the function call, so again we do not need to evaluate anything
// further up the tree.
package eval

import (
	"cmp"
	"fmt"
	"iter"
	"maps"
	stdpath "path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/inject/embed"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/internal/golangorgx/gopls/protocol"
	"cuelang.org/go/internal/lsp/fscache"
	"cuelang.org/go/internal/lsp/rangeset"
)

// EvaluatorForPackageFunc is a callback function used to resolve
// package evals by their import path. It returns the Evals for the
// given import path, or nil if the package cannot be resolved.
type EvaluatorForPackageFunc = func(importPath ast.ImportPath) *Evaluator

// EvaluatorsForEmbedAttrFunc is a callback function used to resolve
// embed attributes into [embed.Embed] values, and a list of
// [Evaluator]s for the embedded packages.
type EvaluatorsForEmbedAttrFunc = func(attrPos token.Pos) (*embed.Embed, []*Evaluator)

// ImportersFunc is a callback function to fetch the package
// evaluator for packages that directly import the current package.
type ImportersFunc = func() []*Evaluator

// EmbeddersFunc is a callback function to fetch the evaluators for
// all the packages that embed us. Each evaluator is associated with
// the [embed.Embed] attributes that expanded to this package.
type EmbeddersFunc = func() map[*Evaluator][]*embed.Embed

// paramsName is the name of the hidden child of a function's
// navigable through which the function's labeled parameters are
// reached by name. Like every name starting with "__", it is never
// reported by the graph API or offered as a completion.
const paramsName = "__params"

type Evaluator struct {
	config Config
	// pkgFrame is the top level (or root) lexical scope
	pkgFrame *frame
	// pkgDecls contains every package declaration with the files
	// passed to [New]. This is not the same as the
	// [pkgFrame.navigable] (which is the entire package scope).
	pkgDecls *navigable
	// fileFramesNav is the navigable shared by all of the package's
	// file frames. It aggregates the package's top-level bindings,
	// and is the root of the graph exposed by [Evaluator.Root].
	fileFramesNav *navigable
	// byFilename maps file names to [FileEvaluator]
	byFilename map[string]*FileEvaluator
}

type Config struct {
	// IP is the canonical (with major-version suffix) import path for
	// this package.
	IP ast.ImportPath
	// ImportCanonicalisation provides canonical (with major-version
	// suffix) import paths for every import spec within the current
	// package. The LSP always uses canonical import paths, but
	// individual cue files can have import statements that (1) elide
	// major version suffices; (2) have explicit but unnecessary
	// qualifiers. Being able to map such paths from import statements
	// to canonical ImportPaths is necessary when (1) grouping together
	// common imports (perhaps different files within this package
	// import the same package, but the path can differ); (2) searching
	// for import specs that have paths which refer to a particular
	// ImportPath.
	ImportCanonicalisation map[string]ast.ImportPath
	// ForPackage is a callback function to resolve imported packages.
	ForPackage EvaluatorForPackageFunc
	// PkgImporters is a callback function to fetch the evaluator for
	// all packages that directly import this package.
	PkgImporters ImportersFunc
	// ForEmbedAttribute is callback function to resolve embed
	// attributes.
	ForEmbedAttribute EvaluatorsForEmbedAttrFunc
	// PkgEmbedders is a callback function to fetch the evaluators for
	// the packages that embed us.
	PkgEmbedders EmbeddersFunc
	// PackageIsEmbedded indicates that this evaluator represents a
	// package which is embedded (e.g. made from JSON files), as
	// opposed to being constructed of normal ".cue" files.
	PackageIsEmbedded bool
}

func (c *Config) init() {
	if c.ForPackage == nil {
		c.ForPackage = func(importPath ast.ImportPath) *Evaluator { return nil }
	}
	if c.PkgImporters == nil {
		c.PkgImporters = func() []*Evaluator { return nil }
	}

	if c.ForEmbedAttribute == nil {
		c.ForEmbedAttribute = func(attrPos token.Pos) (*embed.Embed, []*Evaluator) { return nil, nil }
	}
	if c.PkgEmbedders == nil {
		c.PkgEmbedders = func() map[*Evaluator][]*embed.Embed { return nil }
	}
}

// New creates and performs initial configuration of a new [Evaluator]
// value. It does not perform any evaluation eagerly. All files
// provided will be treated as if they are part of the same package
// (without checking). The set of files cannot be modified after
// construction; instead, construction is cheap, so the intention is
// you replace the whole Evaluator value.
//
// For the ip, importCanonicalisation, forPackage, and pkgImporters
// parameters, see the corresponding fields in [Definitions].
func New(config Config, files ...*ast.File) *Evaluator {
	config.init()
	evaluator := &Evaluator{
		config:     config,
		byFilename: make(map[string]*FileEvaluator, len(files)),
	}
	evaluator.Reset()

	pkgFrame := evaluator.pkgFrame
	fileFramesNavigable := &navigable{
		evaluator: evaluator,
		parent:    pkgFrame.navigable,
	}
	evaluator.fileFramesNav = fileFramesNavigable

	for _, file := range files {
		fe := &FileEvaluator{
			evaluator: evaluator,
			File:      file,
		}
		evaluator.byFilename[file.Filename] = fe
		fileFr := fe.newFrame(pkgFrame, file, fileFramesNavigable)
		fileFr.kind = DeclFile
		pkgFrame.childFrames = append(pkgFrame.childFrames, fileFr)
		// The AST doesn't include blank space after the last token. But
		// the user's cursor can be in that space and we need to be able
		// to resolve such offsets to a frame. Also, the cursor can be
		// after the very last char, so we need a +1
		tokFile := file.Pos().File()
		if tokFile == nil || tokFile.Size() == 0 {
			continue
		}
		fileFr.end = tokFile.Pos(tokFile.Size()+1, token.NoRelPos)
	}

	return evaluator
}

// Reset clears all cached evaluation from Evaluator. The Evaluation
// is returned to the same state as after initial construction, before
// any evaluation has taken place. This is useful for when foreign
// packages (which are (transitively) imported by this package, or
// import this package) have been modified but this package itself has
// not been.
func (e *Evaluator) Reset() {
	// pkgFrame, and its navigable, are the roots. They have no parents.
	pkgFrame := &frame{}
	pkgFrame.navigable = &navigable{
		evaluator: e,
		frames:    []*frame{pkgFrame},
		bindings: map[string]*navigable{
			"close": {
				evaluator: e,
				name:      "__close",
			},
		},
	}
	e.pkgFrame = pkgFrame

	// pkgDecls is a child of pkgFrame.navigable. It will have every
	// package declaration contributed to it as they are encountered.
	e.pkgDecls = &navigable{
		evaluator: e,
		parent:    pkgFrame.navigable,
	}

	// fileFramesNavigable is also a child of pkgFrame.navigable. It has
	// every file frame contributed to it.
	fileFramesNavigable := &navigable{
		evaluator: e,
		parent:    pkgFrame.navigable,
	}
	e.fileFramesNav = fileFramesNavigable

	for _, fe := range e.byFilename {
		clear(fe.likelyUseOffsets)
		clear(fe.labelOffsets)
		clear(fe.importSpecNavigables)
		fileFr := fe.newFrame(pkgFrame, fe.File, fileFramesNavigable)
		fileFr.kind = DeclFile
		pkgFrame.childFrames = append(pkgFrame.childFrames, fileFr)
		tokFile := fe.File.Pos().File()
		if tokFile == nil {
			continue
		}
		fileFr.end = tokFile.Pos(tokFile.Size()+1, token.NoRelPos)
	}
}

// ForFile looks up the [FileEvaluator] for the given filename.
func (e *Evaluator) ForFile(filename string) *FileEvaluator {
	e.bootFiles()
	return e.byFilename[filename]
}

// bootFiles evaluates the top level (root) pkgFrame and its direct
// children only. This is useful to ensure package declarations and
// import specs have been processed.
func (e *Evaluator) bootFiles() {
	// Eval the pkgFrame and its immediate children only
	// (which will be fileFrames). This is enough to
	// ensure the package declarations have been found
	// and added to the pkgDecls.
	pkgFrame := e.pkgFrame
	pkgFrame.eval()
	for _, fileFr := range pkgFrame.childFrames {
		fileFr.eval()
	}
}

// parseImportSpec attempts to extract the path from the given import
// spec, and return its canonical form with major-version suffix if at
// all possible. It makes use of the
// [Evaluator.importCanonicalisation] field.
func (e *Evaluator) parseImportSpec(spec *ast.ImportSpec) *ast.ImportPath {
	str, err := strconv.Unquote(spec.Path.Value)
	if err != nil {
		return nil
	}
	if ip, found := e.config.ImportCanonicalisation[str]; found {
		return &ip
	}
	// modimports/modpkgload calls Canonical on import paths, so if
	// importCanonicalisation is built from the result of modimports
	// etc then unnecessary explicit qualifiers in import specs will
	// have been removed. So, parse the path, find the canonical form,
	// and then check again to see if we find anything:
	ip := ast.ParseImportPath(str).Canonical()
	if ip, found := e.config.ImportCanonicalisation[ip.String()]; found {
		return &ip
	}
	return &ip
}

// initialNavsForImport locates navigables that are likely to
// contain uses of the import path.
func (e *Evaluator) initialNavsForImport(ip ast.ImportPath) []*navigable {
	// When calculating usages of a field, we can discover that the
	// field is exported by the package. We then look for all the
	// packages that import us. To try to avoid brute force evaluation
	// of the whole of these "downstream" packages, we use
	// [FileEvaluator.findLikelyUseOffsets] to quickly search for file
	// offsets that appear to be of idents (or imports) that match the
	// imported package's name (i.e. our original now-"upstream"
	// package). We evaluate up to those offsets only, and return the
	// navigables that contain those offsets.
	e.bootFiles()
	var result []*navigable
	for _, fe := range e.byFilename {
		nav, found := fe.importSpecNavigables[ip]
		if !found {
			continue
		}
		for _, fr := range nav.frames {
			spec := fr.node.(*ast.ImportSpec)
			name := e.importSpecName(spec)
			if name == "" {
				continue
			}

			offsets := fe.findLikelyUseOffsets(name)
			for _, offsets := range [][]int{offsets.imports, offsets.idents} {
				for _, offset := range offsets {
					for _, fr := range fe.evalForOffset(offset) {
						result = append(result, fr.navigable)
					}
				}
			}
		}
	}
	return result
}

// importSpecName extracts the name of the import spec: the alias name
// if that's in use, or otherwise the package qualifier, whether
// implied or explicit.
func (e *Evaluator) importSpecName(spec *ast.ImportSpec) string {
	name := ""
	if alias := spec.Name; alias != nil && alias.Name != "" {
		name = alias.Name
	} else {
		ip := e.parseImportSpec(spec)
		if ip == nil {
			return ""
		}
		name = ip.Qualifier
	}
	return name
}

// embedders constructs the set of navigables in remote evaluators
// that correspond to where this package appears in those remote ASTs
// via embed attributes.
func (e *Evaluator) embedders() map[*navigable]struct{} {
	var result []*navigable
	for remotePkg, embeds := range e.config.PkgEmbedders() {
		remotePkg.bootFiles()
		for _, embed := range embeds {
			node := embed.Node
			if f, ok := node.(*ast.Field); ok {
				node = f.Value
			}
			remotePos := node.Pos()
			remoteFilename := remotePos.Filename()
			remoteFe := remotePkg.byFilename[remoteFilename]
			if remoteFe == nil {
				continue
			}
			remoteFrames := remoteFe.evalForOffset(remotePos.Offset())
			var unexpanded []*navigable
			for _, fr := range remoteFrames {
				unexpanded = append(unexpanded, fr.navigable)
			}

			if embed.IsGlob() {
				// The evalForOffset call above will have also discovered
				// the embed attribute is a glob, and will have
				// constructed a navigable with bindings for each file
				// name of the embedded files. Here we're doing the "other
				// side" of that: navigating by the names of the files
				// that make up this Evaluator.
				remoteDir := stdpath.Dir(filepath.ToSlash(remoteFilename)) + "/"
				for filename := range e.byFilename {
					filename = filepath.ToSlash(filename)
					fieldName := strings.TrimPrefix(filename, remoteDir)
					result = append(result, navigateByName(expandNavigables(unexpanded), fieldName)...)
				}

			} else {
				result = append(result, unexpanded...)
			}
		}
	}
	return expandNavigables(result)
}

type FileEvaluator struct {
	evaluator *Evaluator
	// File is the original [ast.File] that was passed to [New].
	File *ast.File
	// likelyUseOffsets contains file offsets for imports and idents
	// that are likely to be uses (as opposed to declarations) and
	// likely to reference imported packages. It is lazily populated by
	// [FileEvaluator.findLikelyUseOffsets].
	likelyUseOffsets map[string]useOffsets
	// labelOffsets contains the file offsets of the call labels and
	// parameter labels spelled like a given name. It is lazily
	// populated by [FileEvaluator.findLabelOffsets].
	labelOffsets map[string][]int
	// importSpecNavigables contains entries for every import within this
	// file. Within this file, all import specs that are of the
	// same (canonical) import path, share the same
	// navigable. This is made possible by this map.
	importSpecNavigables map[ast.ImportPath]*navigable
}

// useOffsets models likely file offsets for a given name. It
// separates out offsets of import specs which use the name from other
// idents because import specs can be considered either usages or
// definitions of a name, depending on context.
type useOffsets struct {
	imports []int
	idents  []int
}

// DefinitionNodesForOffset reports the [Node]s denoted by the
// definitions that the file offset (number of bytes from the start of
// the file) resolves to. The source-level declarations of the
// definitions, and their positions, are available through each node's
// [Node.Decls].
func (fe *FileEvaluator) DefinitionNodesForOffset(offset int) NodeSet {
	var nodes NodeSet

	for nav := range fe.definitionsForOffset(offset) {
		nodes = append(nodes, (*Node)(nav))
	}

	return nodes
}

// DocCommentsForOffset is very similar to DefinitionNodesForOffset. It
// reports the doc comments associated with the definitions that the
// file offset (number of bytes from the start of the file) resolves
// to.
func (fe *FileEvaluator) DocCommentsForOffset(offset int) map[ast.Node][]*ast.CommentGroup {
	commentsMap := make(map[ast.Node][]*ast.CommentGroup)

	navs := fe.definitionsForOffset(offset)
	if fe.evaluator.config.PackageIsEmbedded {
		navs = maps.Keys(expandNavigablesViaPath(slices.Collect(navs)))
	}

	for nav := range navs {
		for _, fr := range nav.frames {
			if fr.key != nil {
				if comments := fr.docComments(); len(comments) > 0 {
					commentsMap[fr.key] = comments
				}
			}
		}
	}

	return commentsMap
}

// DeclsForOffset returns the [Decl]s of the innermost declarations
// that contain the given file offset (number of bytes from the start
// of the file). It is the bridge from file offsets into the graph
// API: explore onwards with the returned Decls' methods ([Decl.Node],
// [Decl.Value], [Decl.Resolve] etc).
//
// An offset within a field's label is reported as that field's value
// declaration. For example, given:
//
//	x: 5
//
// every offset within this declaration, whether within the label x or
// within the value 5, yields the Decl whose Value is the 5. The same
// applies to the ident of an alias or let: an offset within the y of
// `let y = 5` yields the Decl of the bound value.
func (fe *FileEvaluator) DeclsForOffset(offset int) []*Decl {
	var decls []*Decl
	seen := make(map[*frame]struct{})
	for _, fr := range fe.evalForOffset(offset) {
		if fieldDecl, ok := fr.node.(*fieldDeclExpr); ok {
			// The frame models a field's label (or the ident of an
			// alias or similar binding): its node is synthetic and
			// carries no source-level syntax, so report the
			// declaration of the corresponding value instead.
			fr = fieldDecl.valueFrame
		}
		if fr.node == nil {
			// Scaffolding frames (e.g. for comprehension clauses) have
			// no syntax of their own.
			continue
		}
		if _, found := seen[fr]; found {
			continue
		}
		seen[fr] = struct{}{}
		decls = append(decls, (*Decl)(fr))
	}
	return decls
}

type Completion struct {
	// Start and End contain the range of the file that should be
	// replaced by this Completion. They are byte offsets.
	Start int
	End   int
	// Suffix should be appended to all string suggestions. It's
	// typically ": " for suggested fields.
	Suffix string
	Kind   protocol.CompletionItemKind
}

// CompletionsForOffset reports the set of strings that can form a new
// path element following the path element indicated by the offset
// (number of bytes from the start of the file). Each string maps to
// the [Node] it denotes, or nil if that is not known.
func (fe *FileEvaluator) CompletionsForOffset(offset int) map[Completion]map[string]*Node {
	var completions map[Completion]map[string]*Node
	addCompletions := func(completion Completion, nameSet map[string]*Node) {
		if len(nameSet) == 0 {
			return
		}
		if completions == nil {
			completions = make(map[Completion]map[string]*Node)
		}
		names, found := completions[completion]
		if !found {
			completions[completion] = nameSet
			return
		}
		maps.Copy(names, nameSet)
	}

	suggestEmbedsFrom := make(map[*frame]Completion)
	suggestFieldsFrom := make(map[*navigable]Completion)

	leafFrames := fe.evalForOffset(offset)

	// A frame's range can extend beyond its own node: the operands of
	// a binary & or | each cover the whole expression so that they
	// always evaluate together. nodeOwned reports whether the offset
	// lies within the node of at least one leaf frame. It gates only
	// the final fallback below: the earlier cases all impose their own
	// positional requirements, and their suggestions from sibling
	// operand frames coincide with the owner's (the operands share a
	// navigable and a scope). The fallback suggests unconditionally at
	// the cursor, where a frame whose node excludes the offset would
	// pollute the owning frame's suggestions with names from enclosing
	// scopes.
	nodeOwned := false
	for _, fr := range leafFrames {
		if node := fr.node; node != nil && node.Pos().IsValid() && token.WithinInclusive(offset, node.Pos(), node.End()) {
			nodeOwned = true
			break
		}
	}

nextFrame:
	for _, fr := range leafFrames {
		if fr.navigable == fe.evaluator.pkgDecls {
			// do not make any suggestions if we're inside a package decl
			continue
		}
		if _, ok := fr.node.(*ast.ImportSpec); ok {
			// do not make any suggestions if we're inside an import spec
			continue
		}

		// 1. Are we in a fieldDecl?
		//
		// We want to handle being in a path *within* a fieldDecl
		// carefully: that path could be from an expression
		// (e.g. dynamic field: (a.|b): foo), or it could just be the
		// fake ancestral path of a simple field key ident. So we test
		// paths *after* explicitly testing the key ident of a
		// fieldDecl.
		fieldDecl, ok := fr.node.(*fieldDeclExpr)
		inFieldDecl := ok && token.WithinInclusive(offset, fieldDecl.start, fieldDecl.end)
		if inFieldDecl {
			if fieldDecl.valueFrame.kind == DeclParam {
				// A parameter's label and alias name the parameter; no
				// field can take their place.
				continue nextFrame
			}
			// Only make suggestions if we're really within the field key ident.
			if keyIdent, ok := fieldDecl.key.(*ast.Ident); ok {
				start := keyIdent.Pos()
				end := keyIdent.End()
				if token.WithinInclusive(offset, start, end) {
					// It's an existing field key ident, we don't care
					// where the colon is; our suggestions replace the
					// whole ident.
					suggestFieldsFrom[fieldDecl.valueFrame.navigable.parent] = Completion{
						Start: start.Offset(),
						End:   end.Offset(),
					}
					continue nextFrame
				}
			}
			if fieldDecl.start.Offset() == offset {
				// The cursor is right at the start of the field decl, but
				// the field decl must either have no key ident, or
				// there's some alias or something else. E.g. |(a): f.
				// We're going to suggest new fields to insert, and they
				// need to have a trailing colon and space.
				suggestFieldsFrom[fr.navigable.parent] = Completion{
					Start:  offset,
					End:    offset,
					Suffix: ": ",
				}
				continue nextFrame
			}
		}

		if unknown := fr.unknownRanges; unknown != nil && unknown.Contains(offset) {
			// We're within some AST node that this evaluator does not
			// process (eg a BasicLit), or within attribute text. Do not
			// offer any completions *at all*. This is checked before
			// the paths below because an embed attribute installs a
			// path that covers the whole attribute: resolvable for
			// definitions, but nothing a completion could replace.
			return nil
		}

		// 2. Are we in a path?
		//
		// We could still be in a field decl here, but not within the
		// key ident, nor at the very start of the field. E.g. a string
		// interpolation: "a-\(b|.c)-d": e
		for _, p := range fr.childPaths {
			compIdx, _ := p.definitionsForOffset(offset)
			if compIdx < 0 {
				continue
			}
			pc := p.components[compIdx]
			if _, isAttr := pc.node.(*ast.Attribute); isAttr {
				// An attribute's path leads from the attribute to what
				// it embeds, for definitions; the attribute is not a
				// path element that a completion could replace.
				continue
			}
			// The suggestions replace the path element at the cursor in
			// its entirety. The component's start and end bound the
			// cursor positions it owns, which reach a following "." and
			// begin after a preceding one; the element's own text is
			// that of its node: an ident, a selector's name, a call's
			// label, or an index literal. After a "." ending a line the
			// parser synthesizes the selector on the next line, after
			// the cursor: the suggestions then insert at the cursor.
			embedCompletions := Completion{Start: pc.node.Pos().Offset(), End: pc.node.End().Offset()}
			if embedCompletions.Start > offset {
				embedCompletions = Completion{Start: offset, End: offset}
			}
			if compIdx == 0 && !p.startsInline {
				// We're in the first component of a path which starts
				// with an ident. The first component must be resolvable
				// lexically, the same as embeds.
				suggestEmbedsFrom[fr] = embedCompletions
				_, inStruct := fr.node.(*ast.StructLit)
				if len(p.components) == 2 && (inStruct || !fr.noFieldForm()) {
					// This path only has 1 component, so we offer to
					// switch it to a field: a path embedded in a struct
					// literal can always be one.
					fieldCompletions := embedCompletions
					fieldCompletions.Suffix = ": "
					suggestFieldsFrom[fr.navigable] = fieldCompletions
				}
			} else {
				// We're in some component of a path, but not at the start.
				nameSet := make(map[string]*Node)
				for nav := range expandNavigables(pc.unexpanded) {
					addBindings(nameSet, nav.bindings)
				}
				if len(nameSet) > 0 {
					embedCompletions.Kind = protocol.VariableCompletion
					addCompletions(embedCompletions, nameSet)
				}
			}
			continue nextFrame
		}

		if inFieldDecl {
			continue nextFrame
		}

		// 3. Are we at the very start of some value? E.g. x: |true
		for ancestor := fr; ancestor != nil; ancestor = ancestor.parent {
			node := ancestor.node
			if ancestor.key == nil || node == nil {
				continue
			}
			if node.Pos().Offset() < offset {
				break
			}
			// We're injecting embed *and* field suggestions (not
			// replacing). Field suggestions require a trailing colon and
			// space.
			//
			// The key thing here is the detection that we have a field
			// decl to our left, and so injecting additional fields is
			// sensible. A parameter's label to our left is not a field
			// decl.
			if !ancestor.noFieldForm() {
				suggestFieldsFrom[fr.navigable] = Completion{
					Start:  offset,
					End:    offset,
					Suffix: ": ",
				}
			}
			suggestEmbedsFrom[fr] = Completion{
				Start: offset,
				End:   offset,
			}
			continue nextFrame
		}

		node := fr.node
		s, isStruct := node.(*ast.StructLit)
		if isStruct && s.Lbrace.IsValid() && s.Rbrace.IsValid() && !token.WithinInclusive(offset, s.Lbrace, s.Rbrace) {
			continue
		}
		if nodeOwned && node != nil && node.Pos().IsValid() && !token.WithinInclusive(offset, node.Pos(), node.End()) {
			// The offset is within this frame's range only because the
			// range extends beyond the frame's own node; the offset
			// belongs to some other leaf frame's node. Avoid the general
			// fallback that follows.
			continue
		}

		suggestEmbedsFrom[fr] = Completion{Start: offset, End: offset}

		_, isFile := node.(*ast.File)
		if isFile || isStruct {
			// We must be between fields. The offset of the "{" itself
			// precedes the struct, where a field is valid only if the
			// struct's own value could start with one.
			if !(isStruct && offset == s.Lbrace.Offset() && fr.noFieldForm()) {
				suggestFieldsFrom[fr.navigable] = Completion{Start: offset, End: offset}
			}
		}
	}

	processedEmbedFrames := make(map[*frame]struct{})
	for fr, embedCompletions := range suggestEmbedsFrom {
		nameSet := make(map[string]*Node)
		for childFr, parentFr := fr, fr.parent; parentFr != nil; childFr, parentFr = parentFr, parentFr.parent {
			if _, seen := processedEmbedFrames[childFr]; seen {
				break
			}
			processedEmbedFrames[childFr] = struct{}{}
			for name, frames := range parentFr.bindings {
				if strings.HasPrefix(name, "__") {
					continue
				}
				if _, found := nameSet[name]; found {
					// The walk runs from inner to outer scopes, so the
					// binding already seen shadows this one.
					continue
				}
				nameSet[name] = (*Node)(frames[0].navigable)
			}
		}
		if len(nameSet) == 0 {
			continue
		}
		embedCompletions.Kind = protocol.VariableCompletion
		addCompletions(embedCompletions, nameSet)
	}

	expander := expandNavigables
	if fe.evaluator.config.PackageIsEmbedded {
		expander = expandNavigablesViaPath
	}

	for nav, fieldCompletions := range suggestFieldsFrom {
		nameSet := make(map[string]*Node)
		for nav := range expander([]*navigable{nav}) {
			addBindings(nameSet, nav.bindings)
		}
		if len(nameSet) == 0 {
			continue
		}
		fieldCompletions.Kind = protocol.FieldCompletion
		addCompletions(fieldCompletions, nameSet)
	}

	return completions
}

// addBindings adds the given bindings to nameSet, mapping each name
// to the [Node] it denotes. Names that are already present, and names
// starting with "__", are left alone.
func addBindings(nameSet map[string]*Node, bindings map[string]*navigable) {
	for name, binding := range bindings {
		if strings.HasPrefix(name, "__") {
			continue
		}
		if _, found := nameSet[name]; !found {
			nameSet[name] = (*Node)(binding)
		}
	}
}

// UsagesForOffset reports the nodes that make use of whatever the
// file offset (number of bytes from the start of the file) resolves
// to.
func (fe *FileEvaluator) UsagesForOffset(offset int, includeDefinitions bool) []ast.Node {
	navs := slices.Collect(fe.definitionsForOffset(offset))

	if len(navs) == 1 {
		nav := navs[0]
		pkgEval := fe.evaluator
		if nav == nav.evaluator.pkgDecls && nav.evaluator != pkgEval {
			// We have resolved the offset to package declarations of
			// some remote package. From this we can infer that the user
			// is actually asking for usages of an import, so we hunt
			// through the usedBy looking for a import spec that's from
			// this file.
			navs = navs[:0]
			for n, fr := range nav.usedBy {
				if fr.fileEvaluator != fe {
					continue
				}
				spec, ok := n.(*ast.ImportSpec)
				if !ok {
					continue
				}
				// If we set navs to fr.navigable then we would eval
				// the whole package. To avoid that, we extract the
				// name of the import and hunt through the AST of this
				// file, searching for uses of that name.
				name := pkgEval.importSpecName(spec)
				if name == "" {
					continue
				}
				offsets := fe.findLikelyUseOffsets(name)
				// In this case, the import spec is a definition not a
				// use, so we ignore offsets.imports.
				navs = slices.AppendSeq(navs, fe.definitionsForOffset(offsets.idents...))
			}
		}
	}

	for _, nav := range navs {
		nav.readLabels()
	}
	usages(navs)

	exprs := make(map[ast.Node]struct{})
	for _, nav := range navs {
		for node, fr := range nav.usedBy {
			if includeDefinitions || node != fr.key {
				exprs[node] = struct{}{}
			}
		}
	}

	if includeDefinitions {
		for _, nav := range navs {
			for _, fr := range nav.frames {
				if fr.key != nil {
					exprs[fr.key] = struct{}{}
				}
			}
		}
	}

	return slices.Collect(maps.Keys(exprs))
}

// readLabels finds and reads the call labels and parameter labels
// that may bind the parameter n names, so that each records itself as
// a use of the parameters it resolves to before the search for n's
// uses begins. For a navigable that is not a labeled parameter it
// does nothing.
//
// Labels are found by spelling, in the files of n's package and, when
// the field holding the function is exported (see
// [navigable.exported]), in the files of every package importing it,
// directly or through other packages. A label is read only where it
// is found, so nothing else in those files is evaluated.
func (n *navigable) readLabels() {
	if n.name == "" || !slices.ContainsFunc(n.frames, func(fr *frame) bool { return fr.kind == DeclParam }) {
		return
	}
	// The parameter's navigable is a child of the signature navigable,
	// which is a child of the function's. The function's navigable is
	// anonymous when the literal is written inline, as in a call of
	// it: the field holding the function is then the nearest named
	// ancestor.
	fieldNav := n.parent.parent
	for fieldNav.name == "" && fieldNav.parent != nil {
		fieldNav = fieldNav.parent
	}
	exported := fieldNav.exported()
	pkgs := newWorkqueue(n.evaluator)
	for pkg := range pkgs.items() {
		if exported {
			pkgs.enqueue(pkg.config.PkgImporters()...)
		}
		for _, fe := range pkg.byFilename {
			// Reading a label resolves its path, which records the use;
			// the definitions themselves are not needed here.
			for range fe.definitionsForOffset(fe.findLabelOffsets(n.name)...) {
			}
		}
	}
}

// usageSearchRoot reports where the search for uses of n evaluates:
// root is the navigable whose frames, and their descendants, the
// search evaluates, and targets are the navigables from n up to, but
// excluding, root, whose uses the search inspects. exported reports
// whether n is reachable from the package root by field names, so
// that packages importing this one may use it.
//
// The walk up from n stops at the transition from an unnamed navigable
// to a named parent:
//
//	x: (({y: 6} & {y: int}).y + 1)
//
// For uses of the first y, it walks up until it finds the x navigable
// binding, whose frames must all be evaluated.
func (n *navigable) usageSearchRoot() (root *navigable, targets []*navigable, exported bool) {
	pkgNav := n.evaluator.pkgFrame.navigable
	var child *navigable
	for parent := n; parent != nil; child, parent = parent, parent.parent {
		if parent == pkgNav {
			exported = true
			if child != nil {
				for _, fr := range n.frames {
					if _, ok := fr.node.(*ast.ImportSpec); ok {
						// We're looking for usages of an imported pkg
						// within the current pkg. Imports do not
						// automatically get re-exported by a package,
						// so although we've reached the pkgNav, this
						// import spec is not exported.
						exported = false
						break
					}
				}
			}
		}
		root = parent
		if child == nil {
			continue
		}
		targets = append(targets, child)
		if parent.name != "" && child.name == "" {
			break
		}
	}
	return root, targets, exported
}

// exported reports whether packages importing this one can use n:
// whether n is reachable from the package root by field names, or
// escapes, through a use that resolves to it or to a navigable on
// its climb, into a navigable that is. It climbs as [usages] does,
// from n and then from each navigable escaped into, and stops at the
// first climb that reaches the package root. A climb that stops
// short of the root evaluates the tree it stops at, to find the
// uses; the root's tree is never evaluated.
func (n *navigable) exported() bool {
	navsWorkqueue := newWorkqueue(n)

	traversedNavs := make(map[*navigable]struct{})
	evaluatedNavs := make(map[*navigable]struct{})

	for nav := range navsWorkqueue.items() {
		if _, seen := traversedNavs[nav]; seen {
			continue
		}
		traversedNavs[nav] = struct{}{}

		root, targetNavs, isExported := nav.usageSearchRoot()
		if isExported {
			return true
		}
		for _, target := range targetNavs {
			traversedNavs[target] = struct{}{}
		}
		root.evalTree(evaluatedNavs)
		for _, target := range targetNavs {
			navsWorkqueue.enqueueSeq(target.escapesInto())
		}
	}
	return false
}

// evalTree evaluates n's frames and their descendants, except those
// in embedded packages, skipping the navigables in evaluated and
// adding to it each one it evaluates.
//
// An embedded package, such as a json file, is assumed not to support
// references, so evaluating it cannot find a use of anything: skipping
// it avoids evaluating an enormous json file for nothing.
func (n *navigable) evalTree(evaluated map[*navigable]struct{}) {
	evalWorkqueue := newWorkqueue(n)
	for nav := range evalWorkqueue.items() {
		if _, seen := evaluated[nav]; seen {
			continue
		}
		evaluated[nav] = struct{}{}
		if nav.evaluator.config.PackageIsEmbedded {
			continue
		}
		nav.eval()
		for _, fr := range nav.frames {
			for _, childFr := range fr.childFrames {
				evalWorkqueue.enqueue(childFr.navigable)
			}
		}
	}
}

// escapesInto reports the navigables n escapes into: those of the
// uses of n that resolve to it, as an embedding does. A use through
// which no path reaches n, such as x in x: n + 1, does not count: the
// search for n's uses need not go on to the uses of x.
func (n *navigable) escapesInto() iter.Seq[*navigable] {
	return func(yield func(*navigable) bool) {
		for _, use := range n.usedBy {
			if _, found := use.navigable.resolvesTo[n]; found && !yield(use.navigable) {
				return
			}
		}
	}
}

// usages attempts to discover all uses of the given navs whilst doing
// as little work as possible.
func usages(navsWorklist []*navigable) {
	navsWorkqueue := newWorkqueue(navsWorklist...)

	traversedNavs := make(map[*navigable]struct{})
	evaluatedNavs := make(map[*navigable]struct{})

	for nav := range navsWorkqueue.items() {
		if _, seen := traversedNavs[nav]; seen {
			continue
		}
		traversedNavs[nav] = struct{}{}

		// We're going to have to evaluate everything in nav.frames. But
		// we might need to evaluate frames from the ancestors of nav
		// too.
		root, targetNavs, isExported := nav.usageSearchRoot()
		// Because each nav has a role to play as a parent and then a
		// child, we only add navs to the traversedNavs set once
		// they've played both roles.
		for _, target := range targetNavs {
			traversedNavs[target] = struct{}{}
		}
		root.evalTree(evaluatedNavs)

		// We must evaluate every frame (and consider their ancestors
		// for evaluation) that uses any navigable in targetNavs.
		//
		//	x: {i: h: j: 3, k: i}.k.h
		//
		// The user asked for usages of j, we established targetNavs is
		// [j,h,i]. We have evaluated everything within x, and now we
		// look at any discovered uses of j,h,i. We will find i is used
		// by k, and so we schedule k for evaluation. We find k is used
		// by x but x doesn't resolve to k. However, we also find h is
		// used by x, and x does resolve to h and so we add x to the
		// navsWorkqueue. I.e. we have established that one of j,h,i has
		// escaped from the in-line struct and so at least some of x's
		// ancestors must now be evaluated.
		for _, target := range targetNavs {
			navsWorkqueue.enqueueSeq(target.escapesInto())
		}

		if isExported {
			// For all the packages that import us, find where usages of
			// this package appears.
			//
			// Yes, after booting the remotePkg, our pkg decls, will be
			// "usedBy" the relevant import spec in remotePkg. And yes,
			// those import specs themselves would be usedBy paths that
			// make use of the import. But only after those paths have
			// been evaluated. So the whole point is to figure out which
			// parts of the remotePkg should be evaluated, in order to
			// populate the usedBy field of the navs we care about (which
			// is not necessarily the import spec anyway).
			ip := nav.evaluator.config.IP
			for _, remotePkg := range nav.evaluator.config.PkgImporters() {
				navsWorkqueue.enqueue(remotePkg.initialNavsForImport(ip)...)
			}
			// Embeds however are different: they are embedded into a
			// specific part of a CUE AST, and it absolutely makes sense
			// to start by evaluating that section of the AST.
			navsWorkqueue.enqueueSeq(maps.Keys(nav.evaluator.embedders()))
		}
	}
}

// definitionsForOffset gathers together the navigables which are the
// definitions reachable from each offset.
func (fe *FileEvaluator) definitionsForOffset(offsets ...int) iter.Seq[*navigable] {
	seen := make(map[*navigable]struct{})

	return func(yield func(*navigable) bool) {
		for _, offset := range offsets {
			leafFrames := fe.evalForOffset(offset)
			for _, fr := range leafFrames {
				for _, p := range fr.childPaths {
					_, navs := p.definitionsForOffset(offset)
					for _, nav := range navs {
						if _, found := seen[nav]; found {
							continue
						}
						seen[nav] = struct{}{}
						if !yield(nav) {
							return
						}
					}
				}
			}
		}
	}
}

// findLikelyUseOffsets does a rough-and-ready walk through the ast,
// searching for uses of the given name. It returns their offsets.
//
// It cuts out some subtrees: where a field definition label matches
// the given name, or where a let clause, a for clause or a function
// literal's parameter creates a binding for the given name. Call
// labels and parameter labels and aliases name parameters rather than
// use a name, and are left out. Thus the intention is to return
// offsets that are for idents which match the given name, and are
// unlikely to resolve to other fields. This is approximate and false
// positives will definitely be returned, but it's a single fast pass
// over the AST.
func (fe *FileEvaluator) findLikelyUseOffsets(name string) useOffsets {
	likelyUseOffsets := fe.likelyUseOffsets
	if likelyUseOffsets == nil {
		likelyUseOffsets = make(map[string]useOffsets)
		fe.likelyUseOffsets = likelyUseOffsets
	}

	offsets, found := likelyUseOffsets[name]
	if !found {
		var visit func(n ast.Node) bool
		visit = func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.ImportSpec:
				if nameIdent := n.Name; nameIdent != nil && nameIdent.Name == name {
					offsets.imports = append(offsets.imports, nameIdent.Pos().Offset())
				} else if ip := fe.evaluator.parseImportSpec(n); ip != nil && ip.Qualifier == name {
					offsets.imports = append(offsets.imports, n.Path.Pos().Offset())
				}
				return false
			case *ast.Field:
				if label, ok := n.Label.(*ast.Ident); ok && label.Name == name {
					return false
				}
				// NB we could get smarter and try and look inside an
				// alias. Also, although this cuts out the subtree of
				// Field, it completely ignores the fact that this field
				// is in-scope for all its sibling fields.
			case *ast.LetClause:
				if n.Ident.Name == name {
					return false
				}
			case *ast.ForClause:
				if n.Key != nil && n.Key.Name == name {
					return false
				}
				if n.Value != nil && n.Value.Name == name {
					return false
				}
			case *ast.Func:
				// A parameter binds its local name (its alias when
				// present and not blank, otherwise its label) throughout
				// the literal, as a let clause binds its name.
				for _, param := range n.Parameters() {
					local, _ := param.Label.(*ast.Ident)
					if alias := param.Alias; alias != nil && isNonBlank(alias.Field) {
						local = alias.Field
					}
					if isNonBlank(local) && local.Name == name {
						return false
					}
				}
			case *ast.FuncParam:
				// The label and alias name the parameter; only the
				// constraint and the default can use name.
				for _, expr := range []ast.Expr{n.Value, n.Default} {
					if expr != nil {
						ast.Walk(expr, visit, nil)
					}
				}
				return false
			case *ast.CallExpr:
				// A call's labels name the callee's parameters.
				ast.Walk(n.Fun, visit, nil)
				for _, arg := range n.Args {
					ast.Walk(arg, visit, nil)
				}
				return false
			case *ast.Ident:
				if n.Name == name {
					offsets.idents = append(offsets.idents, n.Pos().Offset())
				}
			}
			return true
		}
		ast.Walk(fe.File, visit, nil)
		likelyUseOffsets[name] = offsets
	}

	return offsets
}

// findLabelOffsets walks the file's syntax for the call labels and
// parameter labels spelled like the given name, and returns their
// offsets. Unlike [FileEvaluator.findLikelyUseOffsets], it prunes
// nothing: a label binds a parameter by spelling, whatever is in
// scope around it.
func (fe *FileEvaluator) findLabelOffsets(name string) []int {
	labelOffsets := fe.labelOffsets
	if labelOffsets == nil {
		labelOffsets = make(map[string][]int)
		fe.labelOffsets = labelOffsets
	}

	offsets, found := labelOffsets[name]
	if !found {
		ast.Walk(fe.File, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.CallExpr:
				for _, label := range n.ArgLabels {
					if ident, ok := label.(*ast.Ident); ok && ident.Name == name {
						offsets = append(offsets, ident.Pos().Offset())
					}
				}
			case *ast.FuncParam:
				if label, ok := n.Label.(*ast.Ident); ok && label.Name == name {
					offsets = append(offsets, label.Pos().Offset())
				}
			}
			return true
		}, nil)
		labelOffsets[name] = offsets
	}

	return offsets
}

// newFrame creates a new [frame]. All arguments other than parent may
// be nil. Node is the [ast.Node] which is to be processed by the new
// frame. The navigable is the (potentially shared) bindings which are
// used in the resolution of the non-first-components of a path; if
// navigable is nil, then a new navigable will be created and used.
func (fe *FileEvaluator) newFrame(parent *frame, node ast.Node, nav *navigable) *frame {
	if parent == nil {
		panic("parent must not be nil")
	}
	if nav == nil {
		nav = &navigable{
			evaluator: fe.evaluator,
			parent:    parent.navigable,
		}
	}
	child := &frame{
		fileEvaluator: fe,
		parent:        parent,
		node:          node,
		navigable:     nav,
		start:         token.NoPos,
		end:           token.NoPos,
	}
	nav.frames = append(nav.frames, child)
	if node != nil {
		child.start = node.Pos()
		child.end = node.End()
	}
	return child
}

// evalForOffset evaluates from the pkgFrame, evaluating only frames
// within navigables for which at least one frame contains the given
// file-byte-offset.
//
// Note that in some cases a single offset can result in several leaf
// frames. A good example of this is how explicit unification is
// evaluated: `x & y` will result in two frames - one for x, one for
// y, but both will be given the same file-offset-range to ensure that
// both are always evaluated at the same time, given their bindings
// should be combined (both frames will use the same navigable).
func (fe *FileEvaluator) evalForOffset(offset int) []*frame {
	if offset < 0 {
		return nil
	}

	var leafFrames []*frame
	workqueue := newWorkqueue(fe.evaluator.pkgFrame.navigable)
	for nav := range workqueue.items() {
		nav.eval()

		for _, fr := range nav.frames {
			// The very top level pkgFrame.navigable will not "contain"
			// the file+offset, but it will have a child that does. This
			// is the only case where a child frame would contain a
			// file+offset and its parent frame does not.

			isLeaf := fr.contains(fe, offset)
			for _, child := range fr.childFrames {
				if child.contains(fe, offset) {
					workqueue.enqueue(child.navigable)
					isLeaf = false
				}
			}
			if isLeaf {
				leafFrames = append(leafFrames, fr)
			}
		}
	}

	return leafFrames
}

// navigable groups together frames, and itself is a node in a graph
// (directed, acyclic) of navigables.
type navigable struct {
	evaluator *Evaluator
	// frameEvalCursor holds the index into frames of the next frame to
	// be evaluated. Frames can be added to a navigable after it has
	// been evaluated; frameEvalCursor tracks which frames have been
	// evaluated so far, so a later call to [navigable.eval] discovers
	// new frames. Note that it is possible for a navigable's frames to
	// be evaluated without the navigable itself being
	// evaluated. Therefore it is not sufficient to rely on finding
	// unevaluated frames within a navigable.
	frameEvalCursor int
	// parent is the parent navigable. The graph of navigables can be
	// different from the graph of frames, because two frames in a
	// parent-child relationship can reuse the same navigable. A good
	// example of this is:
	//
	//	x: y & z
	//
	// Here, the frame for the x field-value will create two child
	// frames, one for each of y and z, but all three will use the same
	// navigable.
	parent *navigable
	// frames are the frames that contribute to this navigable. It is
	// an invariant that every member of frames has its navigable field
	// set to this navigable. It is also an invariant that every frame
	// that has a particular navigable value in its navigable field
	// will appear in that navigable's frames field.
	frames []*frame
	// bindings contains all bindings for this navigable node. These
	// bindings are "merged"; for example:
	//
	//	x: a
	//	x: b
	//
	// There would only be one navigable that covers both x
	// field-values. This is in contrast to [frame], where bindings are
	// not merged: there would be two bindings (frame)s for x.
	bindings map[string]*navigable
	// name is the identifier name for this binding. This may be the
	// empty string if this navigable itself does not appear in
	// its parent's bindings. A good example of this is a let
	// expression:
	//
	//	let x = 3
	//
	// The frame containing this expression will have its own binding
	// for x to a child frame. That child frame will have a fresh
	// navigable, but that navigable will not appear in the parent
	// frame's own navigable's bindings. This is because navigables are
	// used for resolving non-first-elements of a path, and let
	// expressions (amongst others) introduce bindings that are not
	// visible to non-first-path-elements.
	name string
	// resolvesTo points to the navigables this navigable resolves to,
	// due to embedded paths. For example, in x: y.z, whatever frame
	// y.z resolves to, its navigable bindings will be stored in the
	// resolvesTo field of x's navigable.
	resolvesTo map[*navigable]struct{}
	// resolvesToObservers contains paths, each with an index, that
	// should be informed whenever the [resolvesTo] set for this
	// navigable changes.
	resolvesToObservers map[*path]int
	// usedBy maps [ast.Node]s to [frame]s when the ast.Node resolves
	// to this nav. The ast.Node will be some part of the frame's own
	// node. For example:
	//
	// x: 3
	// y: x
	//
	// the navigable with name x and frames `x: 3` (i.e. line 1) will
	// have its usedBy map contain the ast.Node for x (from the 2nd
	// line) with the `y: x` frame.
	usedBy map[ast.Node]*frame
}

// eval evaluates the navigable's frames lazily. Evaluation is not
// recursive: it does not evaluate children. Except in special cases
// (e.g. pkgFrames), [frame.eval] should not be directly
// called. Instead call this [navigable.eval].
//
// eval may be called any number of times: each call evaluates any
// frames that have been added to this navigable since the previous
// call. In particular, a re-entrant call made while this navigable is
// mid-evaluation completes the evaluation of its remaining
// frames. For example, in
//
//	p: p.x.s & {x: s: 3}
//
// resolving the first operand's path re-enters this eval (via
// [expandNavigables]) whilst the second operand's frame is still
// pending, and must evaluate it before the path can find x.
//
// This maintains the invariant that path resolution relies upon:
// every [navigateByName] is immediately preceded by an
// [expandNavigables] over the same navigables, so a navigable's
// bindings are only ever read once all its (current) frames have been
// evaluated. A navigable's bindings are mutated only by evals of its
// own frames, and its frames are only added by evals of its own or
// its parent's frames. So a path, having resolved, cannot be
// invalidated by later evaluation.
func (n *navigable) eval() {
	// Calling fr.eval() can append new frames to fr.navigable.frames
	// (for example, the evaluation of BinaryExpr or comprehension
	// clauses, can append new frames to the current navigable), and
	// re-entrant calls to this eval can consume frames from further
	// along the list. Thus we must loop on the cursor in the navigable
	// struct.
	evaluateFrames := n.frameEvalCursor < len(n.frames)
	for n.frameEvalCursor < len(n.frames) {
		fr := n.frames[n.frameEvalCursor]
		n.frameEvalCursor++
		fr.eval()
	}

	if !evaluateFrames || len(n.bindings) == 0 {
		return
	}
	//	a: b: c: _
	//	x: a
	//	x: b: c: _
	//
	// We want to make sure that x.b resolves to a.b (and then later if
	// we evaluate x.b, we'll make x.b.c resolve to a.b.c).
	navs := expandNavigables([]*navigable{n})
	for name, nav := range n.bindings {
		nav.ensureResolvesTo(navigateByName(navs, name))
	}
}

// ensureResolvesTo makes sure that this navigable's resolvesTo set
// contains every navigable within navs. If the resolvesTo set grows
// then the navigable's resolvesTo observers will be invoked.
func (n *navigable) ensureResolvesTo(navs []*navigable) {
	changed := false
	resolvesTo := n.resolvesTo
	for _, nav := range navs {
		if nav == n {
			continue
		} else if _, found := resolvesTo[nav]; found {
			continue
		}
		if resolvesTo == nil {
			resolvesTo = make(map[*navigable]struct{})
			n.resolvesTo = resolvesTo
		}
		resolvesTo[nav] = struct{}{}
		changed = true
	}
	if !changed {
		return
	}
	for path, i := range n.resolvesToObservers {
		path.resolvesToChanged(i)
	}
}

// ensureResolvesToObserver ensures that the path p with index i are
// registered in this navigable as a resolvesTo observer. Once
// registered, any subsequent change to the navigable's resolvesTo set
// will result in the path's [resolvesToChanged] method being invoked,
// with the index i.
func (n *navigable) ensureResolvesToObserver(p *path, i int) {
	resolvesToObservers := n.resolvesToObservers
	if resolvesToObservers == nil {
		resolvesToObservers = make(map[*path]int)
		n.resolvesToObservers = resolvesToObservers
	}
	resolvesToObservers[p] = i
}

// recordUsage records that the [ast.Node] node (which will occur
// somewhere within the [frame] fr.node) is considered to resolve to
// nav (i.e. be a use of nav).
func (n *navigable) recordUsage(node ast.Node, fr *frame) {
	if n.usedBy == nil {
		n.usedBy = make(map[ast.Node]*frame)
	}
	n.usedBy[node] = fr
}

// ensureNavigable returns a navigable to be used for bindings of the
// given name. It will create a fresh navigable if there is no
// existing navigable available for the given name.
func (n *navigable) ensureNavigable(name string) *navigable {
	bindings := n.bindings
	if bindings == nil {
		bindings = make(map[string]*navigable)
		n.bindings = bindings
	}
	childNav, found := bindings[name]
	if !found {
		childNav = &navigable{
			evaluator: n.evaluator,
			parent:    n,
			name:      name,
		}
		bindings[name] = childNav
	}
	return childNav
}

// navigateByName searches the bindings of every member of the navs
// set by the given name, and the accumulated results returned.
func navigateByName(navs map[*navigable]struct{}, name string) []*navigable {
	var results []*navigable
	for nav := range navs {
		childNav, found := nav.bindings[name]
		if found {
			results = append(results, childNav)
		}

		if name == paramsName {
			// An ellipsis stands in for the fields or elements a value
			// may have, never for a function's parameters.
			continue
		}
		for _, fr := range nav.frames {
			childFrs, found := fr.bindings[name]
			// If there is also a matching binding in the frame, but it
			// leads to a different navigable, then it must be for an
			// alias or similar. Thus the frame didn't match, so if the
			// frame has ellipses then we include those.
			addEllipses := !found || (len(childFrs) == 1 && childFrs[0].navigable != childNav)
			if addEllipses {
				results = append(results, fr.ellipses...)
			}
		}
	}
	return results
}

// expandNavigables maximally expands the given navigables:
// transitively inspecting all the frames that contribute to each
// navigable, evaluating them and including the resolvesTo
// navigables. This expands each given navigable to every navigable
// that can be reached (transitively) via embedding.
func expandNavigables(navs []*navigable) map[*navigable]struct{} {
	if len(navs) == 0 {
		return nil
	}
	workqueue := newWorkqueue(navs...)
	for nav := range workqueue.items() {
		// nav.resolvesTo is read only after nav is evaluated:
		// evaluation can grow it.
		nav.eval()

		workqueue.enqueueSeq(maps.Keys(nav.resolvesTo))
	}
	return workqueue.seen
}

// expandNavigablesViaPath is used by embedded packages only. From the
// given navs it walks up the tree, constructing a path as it goes. If
// it reaches the root of the navigable tree it looks for where this
// evaluator/package is embedded into remote evaluators, and from
// those locations it walks forward down the (reversed) path.
func expandNavigablesViaPath(navs []*navigable) map[*navigable]struct{} {
	result := expandNavigables(navs)
	// NB the loop body adds the navigables it discovers to result:
	// iterate over a snapshot of the keys so that only the original
	// navigables are path-walked.
	for _, nav := range slices.Collect(maps.Keys(result)) {
		var names []string
		for ; nav != nil && nav.name != ""; nav = nav.parent {
			names = append(names, nav.name)
		}

		var walkFrom []*navigable

		if nav.parent == nav.evaluator.pkgFrame.navigable {
			walkFrom = slices.Collect(maps.Keys(nav.evaluator.embedders()))
		} else {
			walkFrom = []*navigable{nav}
		}

		for _, name := range slices.Backward(names) {
			walkFrom = navigateByName(expandNavigables(walkFrom), name)
		}
		maps.Copy(result, expandNavigables(walkFrom))
	}
	return result
}

// frame corresponds to a node from the AST. A frame can be created at
// any time, and creates the opportunity for evaluation to be paused
// (and later resumed). Any binding reachable via
// frame.parent*.bindings is a candidate for resolving the first
// (ident) element of a path, and the navigable field's value (which
// can be shared between frames) offers candidates for resolving
// subsequent elements of a path. So creating a new frame creates a
// new namespace for lexical resolution, and may or may not create a
// new namespace for non-lexical resolution.
type frame struct {
	fileEvaluator *FileEvaluator
	// evaluated tracks whether this frame has been evaluated, ensuring
	// it is only evaluated once.
	evaluated bool
	// embedded tracks whether this frame is embedded within its parent
	// frame. This is useful to tell apart frames which share the same
	// navigable. For example:
	//
	//	x: y
	//	{y: _}
	//
	// The embedded frame uses the same navigable as the surrounding
	// frame, but must be treated separately so that the path `y` does
	// not resolve to the `y` field within the embedded frame.
	//
	// Note this embedded field is set for more than just normal cue
	// embeddings - it is also set for things the LSP treats like
	// embeddings, such as disjunctions and comprehensions.
	embedded bool
	// kind classifies the syntactic construct that this frame
	// models. It is surfaced by the graph API via [Decl.Kind]. The
	// zero value is [DeclExpression]: a frame that does not
	// correspond to any source-level declaration.
	kind DeclKind
	// constraint records the optionality marker of the field that this
	// frame declares: [ConstraintOptional] for optional fields (x?:
	// v), and [ConstraintRequired] for required fields (x!: v). It is
	// [ConstraintRegular] (the zero value) for regular fields, and for
	// frames that do not model a field declaration at all. Beyond
	// recording it, the evaluator makes no use of optionality: `x?: v`
	// declares `x` exactly as `x: v` does.
	constraint Constraint
	// signature marks the frame of a function literal's signature: the
	// scope binding the literal's parameters, whose child frames
	// evaluate the parameters' constraints and defaults, the result
	// type and the body.
	signature bool
	// parent is the parent frame.
	parent *frame
	// childFrames contains every frame that is a child of this
	// frame. When searching for a given file-offset, these frames are
	// tested for whether they contain the desired file-offset.
	childFrames []*frame
	// childPaths contains every path that is considered part of this
	// frame.
	childPaths []*path
	// key is the position that is considered to define this frame. For
	// example, if a frame represents `x: {}` then key is set to the
	// `x` ident. This can be nil, such as when a frame is an
	// expression. For example in the path {a: 3, b: a}.b, a frame with
	// no key will be created, containing the structlit {a: 3, b: a}.
	key ast.Node
	// label is the complete label of the field declaration that this
	// frame models, with any value alias unwrapped. Unlike key, it is
	// unaffected by aliasing: for v=[string]: {...} the label is the
	// bracketed pattern. It is nil for frames that do not model field
	// declarations, and for fields synthesized by the evaluator
	// (e.g. list elements). It is surfaced via [Decl.Label].
	label ast.Label
	// node is the initial node that this frame is solely responsible
	// for evaluating.
	node  ast.Node
	attrs []*ast.Attribute
	// docsNode is set to node if this frame is considered to be a
	// source of documentation comments. These are used for the "hover"
	// LSP functionality.
	docsNode ast.Node
	// bindings contains all bindings for this frame. Note the map's
	// values are slices because a single frame can have multiple
	// bindings for the same key. For example:
	//
	//	x: bool
	//	x: true
	//
	// Bindings are used for the resolution of the first element of a
	// path, if that element is an ident. Thus to some extent they (and
	// an frame itself) correspond to a lexical scope. Bindings are
	// more general than fields: they include aliases and
	// comprehensions as well as normal fields.
	bindings map[string][]*frame
	// ellipses contains navigables for ellipsis values.
	ellipses []*navigable
	// patterns contains navigables for the values of pattern
	// constraints (e.g. [string]: x: 3) declared within this
	// frame. The evaluator never matches field names against patterns:
	// these navigables are anonymous and take no part in navigation,
	// but their frames remain traversable by offset.
	patterns []*navigable
	// dynamics contains navigables for the values of dynamic fields
	// (e.g. (expr): x: 3, or "\(expr)": x: 3) declared within this
	// frame. The evaluator never computes the names of dynamic fields,
	// even when trivial: these navigables are anonymous and take no
	// part in navigation, but their frames remain traversable by
	// offset.
	dynamics []*navigable
	// keyFrame is the frame that records the paths of this field's
	// key expressions (dynamic labels, key interpolations). It is a
	// sibling of this frame, created on the parent, because a key
	// resolves in the scope enclosing the field; but it belongs to
	// this declaration for [Decl.Resolve].
	keyFrame *frame
	// navigable provides access to the "navigable bindings" that is
	// shared between multiple frames that should be considered
	// "merged together".
	navigable *navigable
	// start and end contain the bounds (inclusive) of this frame. They
	// can differ from the bounds of the frame's node; this can be
	// essential for evaluation to proceed correctly: for example with
	// x & y, we ensure that the frames for both sides have the same
	// bounds so that they will both claim to contain the same offsets,
	// and so both will always be evaluated together, which ensures
	// they are evaluated as if merged together.
	start token.Pos
	end   token.Pos
	// unknownRanges tracks ranges for which no completions should be
	// offered. This includes the ranges for all nodes which do not
	// have explicit cases within [frame.eval]. A good example is
	// BasicLit: within a BasicLit, no completions should be offered.
	unknownRanges *rangeset.RangeSet
}

// isFileFrame reports whether f is the top level package frame or a
// direct child of it.
func (f *frame) isFileFrame() bool {
	return f.fileEvaluator == nil || f.parent == f.fileEvaluator.evaluator.pkgFrame
}

// noFieldForm reports whether no field form (a completion such as
// "port: ") may be offered at the start of the frame's value or in
// place of it. A field form is valid only where the code unifies the
// value with the struct holding the field. The parts of a function
// literal are not: a parameter's constraint or default, the result
// type and the body are expressions that a field cannot start, and
// nor can an operand of a conjunction, disjunction or default marker
// within them.
func (f *frame) noFieldForm() bool {
	for f.parent != nil && f.embedded && (f.kind == DeclConjunct || f.kind == DeclDisjunct || f.kind == DeclDefault) {
		f = f.parent
	}
	return f.parent != nil && f.parent.signature
}

// addRange records that the frame covers the range from the node's
// start file-offset to its end file-offset. Because the AST is
// non-recursive in a few areas (e.g. comprehensions), it's sometimes
// necessary to explicitly extend the range of an frame so that
// navigation-by-offset evaluates the correct frames.
func (f *frame) addRange(node ast.Node) {
	start := node.Pos()
	end := node.End()
	if !start.IsValid() || !end.IsValid() {
		return
	}

	if !f.start.IsValid() {
		f.start = start
	} else if f.start.File() != start.File() {
		panic("Attempt to combine different files in node range start")
	} else if start.Offset() < f.start.Offset() {
		f.start = start
	}

	if !f.end.IsValid() {
		f.end = end
	} else if f.end.File() != end.File() {
		panic("Attempt to combine different files in node range end")
	} else if end.Offset() > f.end.Offset() {
		f.end = end
	}
}

// contains reports whether the frame contains the given file-offset.
func (f *frame) contains(fe *FileEvaluator, offset int) (r bool) {
	start, end := f.start, f.end
	if !start.IsValid() || !end.IsValid() {
		return false
	} else if f.fileEvaluator != fe {
		return false
	} else {
		return token.WithinInclusive(offset, start, end)
	}
}

// addUnknownRange ensures that no completions will be offered for
// cursor positions between start and end.
func (f *frame) addUnknownRange(start, end int) {
	unknownRanges := f.unknownRanges
	if unknownRanges == nil {
		unknownRanges = rangeset.NewRangeSet()
		f.unknownRanges = unknownRanges
	}
	unknownRanges.Add(start, end)
}

// addUnknownRangesBetweenChildren ensures that no completions will be
// offered at the offsets within the frame's range that none of its
// child frames covers: the syntax the frame models without a child
// frame of its own. The ranges added never overlap a child frame's.
// The frame's end offset, the position just after its syntax, is not
// covered: it ends the value the frame is part of, where completions
// are offered as after any value.
func (f *frame) addUnknownRangesBetweenChildren() {
	if !f.start.IsValid() || !f.end.IsValid() {
		return
	}
	covered := rangeset.NewRangeSet()
	for _, child := range f.childFrames {
		if child.start.IsValid() && child.end.IsValid() {
			covered.Add(child.start.Offset(), child.end.Offset()+1)
		}
	}
	next := f.start.Offset()
	for _, r := range covered.Ranges() {
		if r.Start > next {
			f.addUnknownRange(next, r.Start)
		}
		next = max(next, r.End)
	}
	if end := f.end.Offset(); next < end {
		f.addUnknownRange(next, end)
	}
}

// newFrame creates a new [frame] which is a child of the current
// frame f. This is a light wrapper around
// [fileEvaluator.newFrame]. See those docs for more details on the
// arguments to this function.
func (f *frame) newFrame(node ast.Node, nav *navigable, embedded bool) *frame {
	child := f.fileEvaluator.newFrame(f, node, nav)
	child.embedded = embedded
	f.childFrames = append(f.childFrames, child)
	return child
}

// eval evaluates the frame lazily. Evaluation is not recursive: it
// does not evaluate child bindings. eval must be called before a
// frame's bindings, childFrames, or childPaths are accessed. See also
// the package level documentation.
func (f *frame) eval() {
	if f.evaluated {
		return
	}
	f.evaluated = true
	if f.node == nil {
		return
	}

	//fmt.Printf("%p eval with key %v node %T; nav: %p\n", f, f.key, f.node, f.navigable)

	var embeddedResolvable []ast.Expr
	// A comprehensionTail is the remainder of a comprehension, stashed
	// under the clause being processed: the body for the last clause,
	// or a comprehension holding the remaining clauses.
	type comprehensionTail struct {
		node ast.Node
		// docsNode carries the comprehension's doc comments to its
		// body decl; it is set only alongside the body, so that the
		// intermediate decls of a multi-clause comprehension do not
		// repeat the docs.
		docsNode ast.Node
	}
	var comprehensionsStash map[ast.Node]comprehensionTail

	unprocessed := []ast.Node{f.node}
	for len(unprocessed) > 0 {
		node := unprocessed[0]
		unprocessed = unprocessed[1:]
		//fmt.Printf("%p eval processing %T %v\n", f, node, node)

		switch node := node.(type) {
		case *ast.File:
			for _, decl := range node.Decls {
				unprocessed = append(unprocessed, decl)
			}

		case *ast.Package:
			// Package declarations must be added to the pkgDecls
			// navigable, so that they can all be found when resolving
			// imports of this package, in some other package.

			childFr := f.newFrame(nil, f.fileEvaluator.evaluator.pkgDecls, false)
			childFr.kind = DeclPackage
			if fscache.IsPhantomPackage(node) {
				// For packages with invented names, be careful to avoid
				// returning file coordinates that use the length of the
				// invented name.
				childFr.key = &ast.Ident{
					Name:    "",
					NamePos: f.fileEvaluator.File.Pos().File().Pos(0, token.NoRelPos),
				}
			} else {
				childFr.key = node.Name
			}
			childFr.addRange(node)
			childFr.docsNode = node
			p := &path{
				frame: childFr,
				components: []pathComponent{{
					unexpanded: []*navigable{f.fileEvaluator.evaluator.pkgDecls},
					node:       node.Name,
				}},
			}
			childFr.childPaths = append(childFr.childPaths, p)

		case *ast.ImportDecl:
			for _, spec := range node.Specs {
				unprocessed = append(unprocessed, spec)
			}

		case *ast.ImportSpec:
			// We process import specs twice, for laziness reasons: we
			// avoid the possibility that evaluating a filenode would
			// lookup every imported package and evaluate its filenodes
			// (which themselves might do the same...).
			pkgEval := f.fileEvaluator.evaluator
			if f.isFileFrame() {
				// 1) At the file level, the first time we see the
				// ImportSpec, we create appropriate file-scope bindings,
				// but also pass the spec as the unprocessed value to a
				// fresh child frame;
				ip := pkgEval.parseImportSpec(node)
				if ip == nil {
					break
				}
				name := ""
				var key ast.Node
				if alias := node.Name; alias != nil && alias.Name != "" {
					name = alias.Name
					key = alias
				} else if ip.Qualifier != "" {
					name = ip.Qualifier
					key = node.Path
				} else {
					break
				}

				importSpecNavigables := f.fileEvaluator.importSpecNavigables
				if importSpecNavigables == nil {
					importSpecNavigables = make(map[ast.ImportPath]*navigable)
					f.fileEvaluator.importSpecNavigables = importSpecNavigables
				}
				nav, found := importSpecNavigables[*ip]
				childFr := f.newFrame(node, nav, false)
				childFr.kind = DeclImport
				if !found {
					importSpecNavigables[*ip] = childFr.navigable
				}
				childFr.key = key
				f.appendBinding(name, childFr)

			} else {
				// 2) In that child frame, the second time we see the
				// ImportSpec, we lookup the package imported and add a
				// resolution to them.
				ip := pkgEval.parseImportSpec(node)
				remotePkgEvaluator := pkgEval.config.ForPackage(*ip)
				if remotePkgEvaluator != nil {
					// The pkg exists. Booting it means that its
					// pkgDecls have frames which are the
					// package declarations from every file in that
					// package.
					remotePkgEvaluator.bootFiles()
				}
				if remotePkgEvaluator == nil {
					// Something went wrong. We create a fake evaluator to
					// handle this so that elsewhere we can treat all
					// imports the same, regardless of whether they were
					// successful or not. Essentially, unsuccessful imports
					// just appear as empty phantom packages.
					//
					// If we really need to, we can tell that the import
					// was successful or not from its evaluator:
					//
					// 1. a bad import will have an empty IP (importPath) field
					// 2. a bad import will have pkgDecls with no frames
					remotePkgEvaluator = New(Config{})
				}

				// DefinitionNodesForOffset always traverses a path, so
				// here we add a path so that DefinitionNodesForOffset on
				// this import spec reports the package declarations of
				// the remote pkg.
				p := &path{
					frame: f,
					components: []pathComponent{{
						unexpanded: []*navigable{remotePkgEvaluator.pkgDecls},
						node:       f.key,
					}},
				}
				f.childPaths = append(f.childPaths, p)

				// Any path that actually traverses into the remote pkg
				// can do so by following the resolvesTo of this frame's
				// navigable. Rather than resolving to the remove pkg
				// declarations, we must resolve to the files that make up
				// the remote pkg.
				remotePkgFileFrames := remotePkgEvaluator.pkgFrame.childFrames
				remotePkgNavs := make([]*navigable, len(remotePkgFileFrames))
				for i, remoteFileFr := range remotePkgFileFrames {
					remotePkgNavs[i] = remoteFileFr.navigable
				}
				f.navigable.ensureResolvesTo(remotePkgNavs)

				// We also record that we are using those package
				// decls. This means that from the result of resolving the
				// import spec, we can always get back to this frame f.
				remotePkgEvaluator.pkgDecls.recordUsage(node, f)
			}

		case *ast.StructLit:
			for _, elt := range node.Elts {
				unprocessed = append(unprocessed, elt)
			}

		case *ast.ListLit:
			for i, elt := range node.Elts {
				if _, ok := elt.(*ast.Ellipsis); ok {
					unprocessed = append(unprocessed, elt)
					continue
				}
				// Fake list elements as numbered fields. These will
				// immediately be converted into bindings via the
				// *ast.Field case below.
				unprocessed = append(unprocessed, &ast.Field{
					Label:    &ast.Ident{NamePos: elt.Pos(), Name: "__" + fmt.Sprint(i)},
					TokenPos: token.NoPos,
					Value:    elt,
				})
			}

		case *ast.Interpolation:
			// The expressions used within an interpolation must be
			// resolvable, but the interpolation itself is a string: it
			// does not resolve to the values of those expressions. So,
			// as with unary operators, each expression is evaluated in
			// a child frame with a fresh navigable.
			for _, elt := range node.Elts {
				if _, ok := elt.(*ast.BasicLit); ok {
					unprocessed = append(unprocessed, elt)
				} else {
					f.newFrame(elt, nil, false)
				}
			}

		case *ast.EmbedDecl:
			childFr := f.newFrame(node.Expr, f.navigable, true)
			childFr.kind = DeclEmbedding
			childFr.docsNode = node

		case *ast.PostfixExpr:
			switch node.Op {
			case token.ELLIPSIS:
				unprocessed = append(unprocessed, node.X)
			case token.OPTION:
				embeddedResolvable = append(embeddedResolvable, node)
			default:
				// Currently should never happen. Just in case though,
				// behave the same as Unary.
				f.newFrame(node.X, nil, false)
			}

		case *ast.ParenExpr:
			unprocessed = append(unprocessed, node.X)

		case *ast.UnaryExpr:
			if node.Op == token.MUL {
				childFr := f.newFrame(node.X, f.navigable, true)
				childFr.kind = DeclDefault
				childFr.addRange(node)
			} else {
				f.newFrame(node.X, nil, false)
			}

		case *ast.BinaryExpr:
			switch node.Op {
			case token.AND, token.OR:
				kind := DeclConjunct
				if node.Op == token.OR {
					kind = DeclDisjunct
				}
				for _, operand := range []ast.Expr{node.X, node.Y} {
					childFr := f.newFrame(operand, f.navigable, true)
					childFr.kind = kind
					childFr.addRange(node)
				}
			default:
				f.newFrame(node.X, nil, false)
				f.newFrame(node.Y, nil, false)
			}

		case *ast.Alias:
			// X=e (the old deprecated alias syntax)
			childFr := f.newBinding(node.Ident, nil)
			childFr.kind = DeclAlias
			childFr.navigable.ensureResolvesTo([]*navigable{f.navigable})
			unprocessed = append(unprocessed, node.Expr)

		case *ast.Ellipsis:
			childFr := f.newFrame(node.Type, nil, false)
			childFr.key = node
			// The parser attaches a comment heading a typed list
			// ellipsis to the type expression, not to the ellipsis
			// itself.
			if node.Type != nil && len(ast.DocComments(node)) == 0 {
				childFr.docsNode = node.Type
			} else {
				childFr.docsNode = node
			}
			childFr.addRange(node)
			// The navigable needs a name so that [UsagesForOffset] will
			// traverse up out of it and thus we'll evaluate frames
			// outside of the scope (f), which may lead to the recording
			// of uses of the ellipsis (or frames within it).
			//
			// However, ellipses are not unified together, e.g. in
			//
			//	[a, ...b] & [...c]
			//
			// b and c are not unified.
			childFr.navigable.name = "__..."
			childFr.kind = DeclEllipsis
			f.ellipses = append(f.ellipses, childFr.navigable)

		case *ast.CallExpr:
			var argFr *frame
			for _, arg := range node.Args {
				argFr = f.newFrame(arg, nil, false)
				argFr.docsNode = arg
			}
			f.createPath(node.Fun, f.callReceiver(node, argFr))

		case *ast.Func:
			// A function literal is one lexical scope, the signature
			// frame, binding its parameters under their local names. Its
			// constraints, defaults, result type and body are evaluated in
			// child frames of that scope, so a parameter name in any of
			// them reaches the parameter. The signature frame's navigable
			// is fresh and anonymous: a function has no fields, so
			// nothing in the literal contributes to f's navigable.
			sigFr := f.newFrame(nil, nil, false)
			sigFr.signature = true
			sigFr.addRange(node)
			for _, param := range node.Parameters() {
				sigFr.newParam(param)
			}
			if ret := node.Ret; ret != nil {
				retFr := sigFr.newFrame(ret, nil, false)
				if node.Arrow.IsValid() {
					retFr.start = node.Arrow.Add(len("->"))
				}
			}
			if body := node.Body; body != nil {
				bodyFr := sigFr.newFrame(body, nil, false)
				if node.Colon.IsValid() {
					bodyFr.start = node.Colon.Add(1)
				}
			}
			// Positions in the literal that none of its parts covers (the
			// func, -> and : tokens, parentheses, commas, the = of a
			// default, attributes) offer no completions.
			sigFr.addUnknownRangesBetweenChildren()
			// The function's navigable has a hidden __params child that
			// resolves to the signature navigable of each of its
			// literals, and, as [navigable.eval] links like-named
			// children, to the __params of whatever the function unifies
			// with. Call labels reach labeled parameters through it. The
			// signature navigable resolves back to it, so that a name in
			// the literal reaches the like-named parameter of every
			// literal the function's value unifies it with, as a field's
			// name reaches the field's other declarations; it stays
			// anonymous so that the search for a parameter's uses stops at
			// the function's navigable.
			paramsNav := f.navigable.ensureNavigable(paramsName)
			paramsNav.ensureResolvesTo([]*navigable{sigFr.navigable})
			sigFr.navigable.ensureResolvesTo([]*navigable{paramsNav})

		case *ast.Ident, *ast.SelectorExpr, *ast.IndexExpr:
			embeddedResolvable = append(embeddedResolvable, node.(ast.Expr))

		case *fieldDeclExpr:
			if node.valueFrame.kind == DeclParam {
				// A parameter's label and alias each name the parameter
				// by its label in the signature navigable, which reaches
				// the like-named parameters of the literals the function
				// unifies with. A lexical lookup of the label would miss
				// the parameter: for a~x only x is bound. A
				// positional-only parameter has no name at call sites, so
				// its alias names the parameter alone.
				nav, name := node.valueFrame.navigable.parent, node.keyName
				if name == "" {
					nav = node.valueFrame.navigable
				}
				if key := node.key; key != nil {
					f.newPathRootedAt(key, nav, name, node.valueFrame)
				}
				if alias := node.valueAliasIdent; alias != nil {
					f.newPathRootedAt(alias, nav, name, node.valueFrame)
				}
				break
			}
			if key := node.key; key != nil && !strings.HasPrefix(node.keyName, "__") {
				f.newPathFromAncestralNames(key, node.keyName)
			}
			if valueAlias := node.valueAliasIdent; valueAlias != nil {
				f.newPathFromAncestralNames(valueAlias, valueAlias.Name)
			}
			unprocessed = append(unprocessed, node.exprs...)

		case *ast.Comprehension:
			clause := node.Clauses[0]
			unprocessed = append(unprocessed, clause)
			if fallback := node.Fallback; fallback != nil {
				fallbackFr := f.newFrame(fallback.Body, f.navigable, true)
				fallbackFr.kind = DeclComprehension
			}
			// We don't know how many child frames we'll need to
			// process clause. So we stash whatever remains of this
			// comprehension and can later find it once we've finished
			// processing our clause.
			if comprehensionsStash == nil {
				comprehensionsStash = make(map[ast.Node]comprehensionTail)
			}
			if len(node.Clauses) == 1 {
				// Base-case: we're dealing with the last clause. So that
				// clause gets processed in this frame, and we make sure we
				// can later use that last clause to find the body (value)
				// of this comprehension. The body decl carries the
				// comprehension's doc comments (a copy made below
				// shares the original's comments).
				comprehensionsStash[clause] = comprehensionTail{
					node:     node.Value,
					docsNode: node,
				}
			} else {
				// Non-base-case: we're processing the first clause in
				// this frame, and all that remain go into a copy of the
				// comprehension, which we find later and pass to an
				// appropriate child/descendant.
				nodeCopy := *node
				nodeCopy.Clauses = node.Clauses[1:]
				nodeCopy.Fallback = nil
				comprehensionsStash[clause] = comprehensionTail{node: &nodeCopy}
			}

		case *ast.IfClause:
			f.newFrame(node.Condition, nil, false)

			tail := comprehensionsStash[node]
			tailFr := f.newFrame(tail.node, f.navigable, true)
			tailFr.kind = DeclComprehension
			tailFr.docsNode = tail.docsNode

		case *ast.ForClause:
			f.newFrame(node.Source, nil, false)

			stack := frameStack{f.newFrame(nil, nil, false)}

			if key := node.Key; isNonBlank(key) {
				keyFr := stack.peek().newBinding(key, nil)
				keyFr.kind = DeclAlias
				stack.push(key, keyFr)
			}
			if val := node.Value; isNonBlank(val) {
				valFr := stack.peek().newBinding(val, nil)
				valFr.kind = DeclAlias
				stack.push(val, valFr)
			}

			tail := comprehensionsStash[node]
			tailFr := stack.peek().newFrame(tail.node, f.navigable, true)
			tailFr.kind = DeclComprehension
			tailFr.docsNode = tail.docsNode
			stack.push(tail.node, tailFr)

		case *ast.LetClause:
			ident := node.Ident
			// A let clause might or might not be within a comprehension.
			if tail, found := comprehensionsStash[node]; found {
				// We're within a wider comprehension.
				f.newFrame(node.Expr, nil, false)

				stack := frameStack{f.newFrame(nil, nil, false)}
				identFr := stack.peek().newBinding(ident, nil)
				identFr.kind = DeclAlias
				stack.push(ident, identFr)
				tailFr := stack.peek().newFrame(tail.node, f.navigable, true)
				tailFr.kind = DeclComprehension
				tailFr.docsNode = tail.docsNode
				stack.push(tail.node, tailFr)

			} else {
				// We're not within a wider comprehension: the binding
				// must be added to the current frame f because we need to
				// be able to find it from the first element of a path.
				letFr := f.newBinding(ident, node.Expr)
				letFr.kind = DeclAlias
				letFr.docsNode = node
			}

		case *ast.TryClause:
			tail := comprehensionsStash[node]
			if ident := node.Ident; ident != nil {
				// Assignment form: try x = expr { ... }
				f.newFrame(node.Expr, nil, false)

				stack := frameStack{f.newFrame(nil, nil, false)}
				identFr := stack.peek().newBinding(ident, nil)
				identFr.kind = DeclAlias
				stack.push(ident, identFr)
				tailFr := stack.peek().newFrame(tail.node, f.navigable, true)
				tailFr.kind = DeclComprehension
				tailFr.docsNode = tail.docsNode
				stack.push(tail.node, tailFr)
			} else {
				// Struct form: try { ... }
				tailFr := f.newFrame(tail.node, f.navigable, true)
				tailFr.kind = DeclComprehension
				tailFr.docsNode = tail.docsNode
			}

		case *ast.Field:
			fieldDecl := fieldNames(node)
			keyName := fieldDecl.keyName

			var childNav *navigable
			if keyName != "" {
				childNav = f.navigable.ensureNavigable(keyName)
			}

			valueAlias := fieldDecl.valueAliasIdent
			keyAlias := fieldDecl.keyAliasIdent

			parentFr := f
			if fieldDecl.patternField && (valueAlias != nil || keyAlias != nil) {
				parentFr = f.newFrame(nil, nil, false)
				parentFr.addRange(node)
				parentFr.navigable.ensureResolvesTo([]*navigable{f.navigable})
			}

			childFr := parentFr.newFrame(node.Value, childNav, false)
			switch {
			case fieldDecl.patternField:
				childFr.kind = DeclPattern
				f.patterns = append(f.patterns, childFr.navigable)
			case fieldDecl.dynamicField:
				childFr.kind = DeclDynamic
				f.dynamics = append(f.dynamics, childFr.navigable)
			default:
				childFr.kind = DeclField
			}
			childFr.constraint = constraintFromToken(node.Constraint)
			// We rely on the invariant that if the key starts with __
			// then we've created it (e.g. list elements), and so there
			// is no source-level label.
			if !strings.HasPrefix(keyName, "__") {
				childFr.label = fieldDecl.label
			}
			if valueAlias != nil {
				childFr.key = valueAlias
				childFr.parent.appendBinding(valueAlias.Name, childFr)
			} else if fieldDecl.patternField || fieldDecl.dynamicField {
				// A pattern constraint or dynamic field has no name, so
				// its label ([expr], (expr), or an interpolation) is
				// the closest thing it has to a defining key. Nothing
				// resolves to this frame's navigable unless the field
				// has a value alias (in which case the alias is the
				// key, as usual), so this key is only observable via
				// the graph API.
				childFr.key = node.Label
			}

			if strings.HasPrefix(keyName, "__") {
				// A synthesized field (a list element) has no
				// source-level label: doc comments attach to the
				// element expression itself. A comprehension element's
				// docs are carried by its comprehension body decl,
				// like any comprehension's.
				if _, ok := node.Value.(*ast.Comprehension); !ok {
					childFr.docsNode = node.Value
				}
			} else {
				childFr.docsNode = node
			}
			fieldDecl.valueFrame = childFr

			if keyAlias != nil {
				keyAliasFr := parentFr.newBinding(keyAlias, fieldDecl.keyExpr)
				keyAliasFr.kind = DeclAlias
			}
			if fieldDecl.key != nil {
				childFr.key = fieldDecl.key
			}
			if keyName != "" {
				f.appendBinding(keyName, childFr)
			}
			if node.TokenPos.IsValid() {
				childFr.start = node.TokenPos.Add(1)
			}

			// The reason for guarding against __ here (which is possibly
			// surprising given we also detect it in the fieldDeclExpr
			// case above) is because the new frame will become
			// authoritative for the offsets in fieldDecl, which would
			// clobber the real value given that __ tends to mean no real
			// key exists.
			//
			// We rely on the invariant that if the key *does* start with
			// __ then we've created it and in these cases the field
			// label is a simple ident with no aliases and no expressions
			// in the field label. Therefore, len(fieldDecl.exprs) > 0
			// implies !strings.HasPrefix(keyName, "__")
			if !strings.HasPrefix(keyName, "__") {
				childFr.keyFrame = childFr.parent.newFrame(fieldDecl, nil, false)
			}
			childFr.attrs = append(childFr.attrs, node.Attrs...)
			for _, attr := range node.Attrs {
				childFr.addRange(attr)
				// Attribute text is not CUE expression syntax: offer no
				// completions within it.
				childFr.addUnknownRange(attr.Pos().Offset(), attr.End().Offset())
			}

		case *ast.Attribute:
			f.attrs = append(f.attrs, node)
			f.addUnknownRange(node.Pos().Offset(), node.End().Offset())

		default:
			f.addUnknownRange(node.Pos().Offset(), node.End().Offset())
		}
	}

	for _, expr := range embeddedResolvable {
		f.createPath(expr, f.navigable.ensureResolvesTo)
	}
	for _, p := range f.childPaths {
		if p.resolveOnRead {
			continue
		}
		p.resolvesToChanged(0)
	}

	for _, attr := range f.attrs {
		embed, remoteEvals := f.fileEvaluator.evaluator.config.ForEmbedAttribute(attr.Pos())
		if len(remoteEvals) == 0 {
			continue
		}
		isGlob := embed.IsGlob()
		for _, remotePkgEvaluator := range remoteEvals {
			// The attribute is an embed of 1 or more files, which
			// we have converted to pkgs with evaluators. Booting
			// them means their pkgDecls have frames.
			//
			// The behaviour here is very similar to processing an
			// ImportSpec.
			remotePkgEvaluator.bootFiles()

			frs := []*frame{f}
			if isGlob {
				frs = frs[:0]
				dir := stdpath.Dir(filepath.ToSlash(f.fileEvaluator.File.Filename)) + "/"
				for remoteFilename := range remotePkgEvaluator.byFilename {
					remoteFilename = filepath.ToSlash(remoteFilename)
					fieldName := strings.TrimPrefix(remoteFilename, dir)
					fieldNameIdent := ast.NewIdent(fieldName)
					grandChildFr := f.newFrame(fieldNameIdent, f.navigable.ensureNavigable(fieldName), false)
					grandChildFr.kind = DeclField
					grandChildFr.addRange(attr)
					grandChildFr.addUnknownRange(attr.Pos().Offset(), attr.End().Offset())
					f.appendBinding(fieldName, grandChildFr)
					frs = append(frs, grandChildFr)
				}
			}

			for _, fr := range frs {
				// We add a path that records that this frame resolves to
				// the remote package. DefinitionNodesForOffset always
				// passes through a path, so this path ensures
				// DefinitionNodesForOffset on the attr itself will take
				// you to the embedded files.
				p := &path{
					frame: fr,
					components: []pathComponent{{
						unexpanded: []*navigable{remotePkgEvaluator.pkgDecls},
						node:       attr,
					}},
				}
				fr.childPaths = append(fr.childPaths, p)

				// Ensure that the frame resolves to the file navs from
				// the remote package. This allows paths that travels
				// through the embed and into the remote pkg to resolve
				// correctly.
				remotePkgFileFrames := remotePkgEvaluator.pkgFrame.childFrames
				remotePkgNavs := make([]*navigable, len(remotePkgFileFrames))
				for i, remoteFileFr := range remotePkgFileFrames {
					remotePkgNavs[i] = remoteFileFr.navigable
				}
				fr.navigable.ensureResolvesTo(remotePkgNavs)

				// We also record that the attr uses the remote package.
				remotePkgEvaluator.pkgDecls.recordUsage(attr, fr)
			}
		}
	}
}

// fieldNames analysis the field's label, returning a populated
// [fieldDeclExpr].
func fieldNames(field *ast.Field) *fieldDeclExpr {
	var result fieldDeclExpr

	label := field.Label

	alias, isAlias := label.(*ast.Alias)
	if isAlias {
		result.valueAliasIdent = alias.Ident
		if expr, ok := alias.Expr.(ast.Label); ok {
			// X=ident: field
			// X="basic": field
			// X="\(e)": field
			// X=(e): field
			// X=[e]: field
			label = expr
		} else {
			result.exprs = append(result.exprs, alias.Expr)
		}
	}
	result.label = label

	switch label := label.(type) {
	case *ast.Ident:
		result.key = label
		result.keyName = label.Name

	case *ast.BasicLit:
		name, _, err := ast.LabelName(label)
		if err == nil {
			result.key = label
			result.keyName = name
		}

	case *ast.Interpolation:
		result.dynamicField = true
		result.exprs = append(result.exprs, label)

	case *ast.ParenExpr:
		result.dynamicField = true
		result.exprs = append(result.exprs, label.X)

	case *ast.ListLit:
		result.patternField = true
		if len(label.Elts) > 0 {
			// If the source is invalid (e.g. mid-edit, "[]: field"),
			// the parser can produce a pattern label with no elements.
			elt := label.Elts[0]
			if alias, ok := elt.(*ast.Alias); ok {
				// [X=e]: field
				// X is only visible within field.
				result.keyAliasIdent = alias.Ident
				result.keyExpr = alias.Expr
			} else {
				result.exprs = append(result.exprs, elt)
			}
		}
	}

	if alias := field.Alias; alias != nil {
		// The blank identifier binds nothing: ~(_, V) declares only
		// V, and ~(K, _) only K.
		if isNonBlank(alias.Field) {
			result.valueAliasIdent = alias.Field
		}
		if isNonBlank(alias.Label) {
			result.keyAliasIdent = alias.Label
		}
	}

	start, end := field.Label.Pos(), field.Label.End()
	if strings.HasPrefix(result.keyName, "__") {
		end = start
	} else if field.TokenPos.IsValid() {
		end = field.TokenPos
	}
	result.start = start
	result.end = end

	return &result
}

// isNonBlank reports whether ident is present and is not the blank
// identifier _, which binds nothing wherever it stands for a name.
func isNonBlank(ident *ast.Ident) bool {
	return ident != nil && ident.Name != "_"
}

// newParam creates the frames that model a function parameter under
// the signature frame f: the parameter's own frame, which evaluates
// its constraint, a key frame for its label and alias, and a frame for
// its default, which is not part of the parameter's value. A labeled
// parameter's navigable is the child of f's navigable named by the
// label, so that call sites can reach it by name; a positional-only
// or anonymous parameter gets a fresh anonymous navigable. The
// parameter's local name (its alias when present and not blank,
// otherwise its label when not blank) is bound in f.
func (f *frame) newParam(param *ast.FuncParam) {
	label, _ := param.Label.(*ast.Ident)
	var alias *ast.Ident
	if param.Alias != nil {
		alias = param.Alias.Field
	}

	var nav *navigable
	if isNonBlank(label) {
		nav = f.navigable.ensureNavigable(label.Name)
	}
	paramFr := f.newFrame(param.Value, nav, false)
	paramFr.kind = DeclParam
	paramFr.label = param.Label
	paramFr.constraint = constraintFromToken(param.Constraint)
	paramFr.docsNode = param
	if param.TokenPos.IsValid() {
		paramFr.start = param.TokenPos.Add(1)
	}
	paramFr.attrs = append(paramFr.attrs, param.Attrs...)
	for _, attr := range param.Attrs {
		paramFr.addRange(attr)
		// Attribute text is not CUE expression syntax: offer no
		// completions within it.
		paramFr.addUnknownRange(attr.Pos().Offset(), attr.End().Offset())
	}

	switch {
	case isNonBlank(alias):
		f.appendBinding(alias.Name, paramFr)
	case isNonBlank(label):
		f.appendBinding(label.Name, paramFr)
	}

	if def := param.Default; def != nil {
		defFr := f.newFrame(def, nil, false)
		if param.Equal.IsValid() {
			defFr.start = param.Equal.Add(1)
		}
	}

	decl := &fieldDeclExpr{
		label:      param.Label,
		valueFrame: paramFr,
	}
	if isNonBlank(label) {
		paramFr.key = label
		decl.key = label
		decl.keyName = label.Name
	}
	if isNonBlank(alias) {
		if paramFr.key == nil {
			paramFr.key = alias
		}
		decl.valueAliasIdent = alias
	}
	if param.Label == nil && param.Alias == nil {
		// An anonymous parameter has no key syntax.
		return
	}
	// The key frame spans the label and alias, up to the colon. A blank
	// label or alias names nothing, but is still the parameter's key
	// syntax.
	if param.Label != nil {
		decl.start = param.Label.Pos()
		decl.end = param.Label.End()
	} else {
		decl.start = param.Alias.Pos()
	}
	if param.Alias != nil {
		decl.end = param.Alias.End()
	}
	if param.TokenPos.IsValid() {
		decl.end = param.TokenPos
	}
	paramFr.keyFrame = f.newFrame(decl, nil, false)
}

// newPathRootedAt adds to the frame's childPaths a path for the key of
// a declaration, in place of the lexical lookup of
// [frame.newPathFromAncestralNames]: the path looks name up in nav,
// or, when name is empty, resolves to nav itself. The path is
// attributed to valueFrame, as a field's key path is.
func (f *frame) newPathRootedAt(key ast.Node, nav *navigable, name string, valueFrame *frame) {
	p := &path{
		frame: valueFrame,
		components: []pathComponent{{
			name:       name,
			node:       key,
			start:      key.Pos(),
			end:        key.End(),
			unexpanded: []*navigable{nav},
		}, {
			node: key,
		}},
	}
	f.childPaths = append(f.childPaths, p)
}

// callReceiver creates the paths that lead from the call's callee to
// the parameters of the functions it may denote, and returns the
// receiver for the callee's path, which roots them at the navigables
// the callee resolves to, adding to their roots whenever the callee's
// resolution grows. argFr evaluates the call's argument when it has
// exactly one.
//
// Each labeled argument whose label is an identifier gets a path
// through the __params child of the callee's navigables to the
// like-named parameter of every function literal the callee denotes.
// The __params component has no syntax of its own, and leads the path
// so that no lookup by position can land on it. Resolving a label
// path expands the callee, which only a query at the label needs, so
// the paths are resolved when read rather than with f's other paths.
//
// Given the navigables the callee resolves to, the receiver also makes
// the value of a call of close denote its argument, and the value of a
// partial application (a call whose arguments end in ...) denote the
// callee, as an embedding of it would: the result of a partial
// application is the function it was applied to, so labels in later
// calls of it reach the callee's parameters through that link, which
// records nothing but the link itself.
func (f *frame) callReceiver(call *ast.CallExpr, argFr *frame) func([]*navigable) {
	var labelPaths []*path
	for _, label := range call.ArgLabels {
		ident, ok := label.(*ast.Ident)
		if !ok {
			continue
		}
		p := &path{
			frame:         f,
			resolveOnRead: true,
			components: []pathComponent{{
				name: paramsName,
			}, {
				name:  ident.Name,
				node:  ident,
				start: ident.Pos(),
				end:   ident.End(),
			}, {
				node: ident,
			}},
		}
		labelPaths = append(labelPaths, p)
		f.childPaths = append(f.childPaths, p)
	}
	return func(navs []*navigable) {
		if len(call.Args) == 1 {
			for nav := range expandNavigables(navs) {
				if nav.name == "__close" {
					f.navigable.ensureResolvesTo([]*navigable{argFr.navigable})
					argFr.navigable.recordUsage(call, f)
					break
				}
			}
		}
		if call.Ellipsis.IsValid() {
			f.navigable.ensureResolvesTo(navs)
		}
		for _, p := range labelPaths {
			p.addRoots(navs)
		}
	}
}

// newBinding creates and returns a new [frame], and stores it under
// the given name in the current frame only.
func (f *frame) newBinding(key *ast.Ident, unprocessed ast.Node) *frame {
	childFr := f.newFrame(unprocessed, nil, false)
	name := key.Name
	f.appendBinding(name, childFr)
	if !strings.HasPrefix(name, "__") {
		expr := &fieldDeclExpr{
			key:        key,
			keyName:    name,
			start:      key.Pos(),
			end:        key.End(),
			valueFrame: childFr,
		}
		f.newFrame(expr, nil, false)
		childFr.key = key
	}
	return childFr

}

// appendBinding stores the binding under the given name in the
// current frame only.
func (f *frame) appendBinding(name string, binding *frame) {
	if f.bindings == nil {
		f.bindings = make(map[string][]*frame)
	}
	f.bindings[name] = append(f.bindings[name], binding)
}

// newPathFromAncestralNames creates a fake path and adds it to the
// frame's childPaths. This is used by field declarations: the fake
// paths created will resolve to other field declarations which are
// unified with the current frame.
//
// Note that this method can only be called if the current frame's
// node is a [fieldDeclExpr].
//
// Consider:
//
//	a: b: x: int
//	c: a & {b: x: 4}
//
// We're at the final x on line 2. We need to make sure this resolves
// to the x on line 1. Imagine instead of a field decl of x, it was a
// use of x:
//
//	a: b: x: int
//	c: a & {b: _anything: x}
//
// That wouldn't work - that's not a valid path to x. Instead, we want:
//
//	a: b: x: int
//	c: a & {b: _anything: c.b.x}
//
// With that in mind, we want to construct as long a path as
// possible and then treat it like a normal path (selector)
// expression.
func (f *frame) newPathFromAncestralNames(key ast.Node, keyName string) {
	fieldDecl := f.node.(*fieldDeclExpr)

	nav, name := fieldDecl.valueFrame.parent.resolvePathRoot(keyName, false)
	if nav == nil {
		return
	}
	components := []pathComponent{{
		name:  name,
		node:  key,
		start: key.Pos(),
		end:   key.End(),
	}}
	if name != "" {
		for ; nav != nil && nav.name != ""; nav = nav.parent {
			components = append(components, pathComponent{
				name: nav.name,
			})
		}
	}
	if nav == nil {
		return
	}

	slices.Reverse(components)
	components[0].unexpanded = []*navigable{nav}

	components = append(components, pathComponent{node: key})

	p := &path{
		frame:      fieldDecl.valueFrame,
		components: components,
	}

	f.childPaths = append(f.childPaths, p)
}

// createPath creates a path from the expression expr. If the
// expression is considered to be embeddded, then receiver should be
// the navigable of the frame into which the expression is embedded.
// The new path is added to the frame's childPaths.
func (f *frame) createPath(expr ast.Expr, receiver func([]*navigable)) {
	var components []pathComponent
	var rootNav *navigable
	nextEnd := token.NoPos
	startsInline := false
	for component := expr; component != nil; {
		switch curExpr := component.(type) {
		case *ast.Ident:
			component = nil
			end := nextEnd
			if !end.IsValid() {
				end = curExpr.End()
			}
			name := curExpr.Name
			rootNav, name = f.resolvePathRoot(name, true)
			components = append(components, pathComponent{
				name:  name,
				node:  curExpr,
				start: curExpr.Pos(),
				end:   end,
			})

		case *ast.SelectorExpr:
			component = curExpr.X
			end := nextEnd
			if !end.IsValid() {
				end = curExpr.End()
			}
			nextEnd = token.NoPos
			sel := curExpr.Sel
			start := sel.Pos()
			if curExpr.Period.IsValid() {
				nextEnd = curExpr.Period
				start = curExpr.Period.Add(1)
			}
			name, _, err := ast.LabelName(sel)
			if err != nil {
				// wipe out anything we've built before. We can still work
				// with whatever's to the left of this SelectorExpr, but
				// nothing from sel onwards.
				components = components[:0]
				receiver = nil
				continue
			}
			components = append(components, pathComponent{
				name:  name,
				node:  sel,
				start: start,
				end:   end,
			})

		case *ast.IndexExpr:
			component = curExpr.X
			end := nextEnd
			if !end.IsValid() {
				end = curExpr.End()
			}
			nextEnd = token.NoPos
			idx := curExpr.Index
			start := idx.Pos()
			if curExpr.Lbrack.IsValid() {
				nextEnd = curExpr.Lbrack
				start = curExpr.Lbrack
			}
			lit, ok := idx.(*ast.BasicLit)
			if !ok {
				// If it's a not a basic lit (i.e. it's path/ident etc),
				// we don't attempt to calculate the dynamic index.
				components = components[:0]
				receiver = nil
				// But we do need to attempt to resolve the (nested) path:
				f.createPath(idx, nil)
				continue
			}

			name := "__" + lit.Value
			if lit.Kind != token.INT { // maybe string index
				var err error
				name, _, err = ast.LabelName(lit)
				if err != nil {
					components = components[:0]
					receiver = nil
					continue
				}
			}

			components = append(components, pathComponent{
				name:  name,
				node:  idx,
				start: start,
				end:   end,
			})

		case *ast.PostfixExpr:
			component = curExpr.X
			nextEnd = curExpr.OpPos

		default:
			component = nil
			childFr := f.newFrame(curExpr, nil, false)
			if nextEnd.IsValid() {
				childFr.end = nextEnd
			}
			rootNav = childFr.navigable
			startsInline = true
		}
	}

	if len(components) == 0 {
		if rootNav != nil && receiver != nil {
			// The expression is inline in its entirety, with no name
			// to look up after it: it resolves to its own navigable,
			// which the receiver gets at once.
			receiver([]*navigable{rootNav})
		}
		return
	}
	slices.Reverse(components)
	if rootNav != nil {
		// Even if there's no rootNav, it's important we keep going
		// otherwise completions won't work - i.e. we need to be able to
		// detect when an offset is within a path, even if the path is
		// broken. So we don't return early when rootNav is nil, but we
		// need to make sure we don't start putting nils into lists of
		// unexpanded navigables.
		components[0].unexpanded = []*navigable{rootNav}
	}
	// components always needs to be one longer to hold the results of
	// the final path element.
	components = append(components, pathComponent{node: expr})

	p := &path{
		frame:        f,
		receiver:     receiver,
		components:   components,
		startsInline: startsInline,
	}

	f.childPaths = append(f.childPaths, p)
}

// resolvePathRoot resolves only the [ast.Ident] first element of a
// path. CUE restricts the first element of any path (if it's an
// ident) to be lexically defined. So here, we search for a match via
// the frame's own bindings (and its ancestry), whereas for subsequent
// path elements, we search the navigable bindings.
func (f *frame) resolvePathRoot(name string, requireIdent bool) (*navigable, string) {
	frameOrig := f
	for ; f != nil; f = f.parent {
		fNav := f.navigable
		if childFrs, found := f.bindings[name]; found {
			if len(childFrs) == 1 {
				nav := childFrs[0].navigable
				if nav.name == "" {
					// name has been resolved to an alias (or comprehension
					// binding, dynamic field, pattern etc). Because it
					// doesn't have a name, we can't subsequently use
					// navigateByName, so we return the nav and "", and
					// [path.resolvesToChanged] will know to not attempt to
					// reresolve this.
					return nav, ""
				} else if nav.name != name {
					// name has been resolved to an alias which had a
					// normal ident or basiclit field name. Switch to that
					// name.
					return fNav, nav.name
				}
			}

			if !requireIdent {
				return fNav, name
			}
			// If name lexically matches a non-alias, it must be matching
			// an ident and not a basiclit. But that ident can come from
			// any of the (potentially many) matching bindings!
			identFound := false
			for _, childFr := range childFrs {
				if _, ok := childFr.key.(*ast.Ident); ok {
					identFound = true
					break
				}
			}
			if !identFound {
				continue
			}
			return fNav, name
		}

		if f.isFileFrame() {
			// If we've got this far, we're allowed to inspect the
			// (shared) navigable bindings directly without having to go
			// via our bindings.
			if nav, found := fNav.bindings[name]; found {
				// We must only return fNav if the binding has been made
				// by a frame which is *not* embedded.
				for _, fr := range fNav.frames {
					if fr.embedded {
						continue
					}
					for _, childFr := range fr.bindings[name] {
						// check that the childFr has the right navigable
						// otherwise we could get tripped up by aliases etc.
						if childFr.navigable == nav {
							return fNav, name
						}
					}
				}
			}
			// Support for the Self experiment:
			if name == "self" && frameOrig.fileEvaluator.File.Pos().Experiment().AliasV2 {
				return frameOrig.selfNavigable(), ""
			}
			// Finally, inspect the globals
			if fNav.parent == nil {
				return nil, ""
			} else {
				return fNav.parent.bindings[name], ""
			}
		}
	}
	return nil, ""
}

// selfNavigable returns the navigable that self denotes for a
// reference within this frame: that of the innermost struct or list
// literal enclosing the reference, or the file's root when no literal
// does. Given:
//
//	x: {
//		a: 1
//		b: self.a + 1
//	}
//
// the reference is in an operand frame under b's frame, neither of
// which holds a literal struct or list; x's value frame holds the
// struct, so self denotes x.
func (f *frame) selfNavigable() *navigable {
	for ; f != nil; f = f.parent {
		switch f.node.(type) {
		case *ast.StructLit, *ast.ListLit, *ast.File:
			return f.navigable
		}
	}
	return nil
}

// docComments extracts the comments from the current frame.
func (n *frame) docComments() []*ast.CommentGroup {
	if n.docsNode == nil {
		return nil
	}
	return ast.DocComments(n.docsNode)
}

// fieldDeclExpr models all the different possible parts of a field
// declaration label, or of a function parameter's label and alias, in
// a more readily accessible form than the AST itself. The kind of the
// valueFrame tells the two apart.
type fieldDeclExpr struct {
	// Always nil: make the struct implement [ast.Node]
	ast.Node

	// keyName is the main name of the field, whether that's from an
	// Ident or a BasicLit. It is not the name of any alias: if there
	// is an alias but no main name, then keyName will be blank
	// (e.g. X="\(y)")
	keyName string
	// key is the main key of the field if it's an Ident or a BasicLit
	key ast.Label
	// label is the field's complete label with any value alias
	// unwrapped: for v=[string]: {...} it is the bracketed pattern.
	label ast.Label
	// keyAliasIdent is the alias that refers to the field's key's name
	keyAliasIdent *ast.Ident
	// valueAliasIdent is the alias that refers to the field's value
	valueAliasIdent *ast.Ident
	// patternField indicates that we have a field definition which is
	// a pattern. This limits visibility of all aliases to the field's
	// value scope.
	patternField bool
	// dynamicField indicates that the field's name is computed by an
	// expression ((expr): v) or an interpolation ("\(expr)": v). The
	// name is never computed by this evaluator, even when trivial.
	dynamicField bool
	// keyExpr contains the expression associated with the
	// key. For [x=e]: y aliases, the alias x should resolve to
	// the expression e, and not the field value y.
	keyExpr ast.Expr
	// exprs contains expressions from within the field declaration
	// that need to be evaluated. E.g. "\(w)-\(x.y)": _
	// Note this is always disjoint from keyExpr.
	exprs []ast.Node

	// start and end track the bounds of the fieldDeclExpr.
	start token.Pos
	end   token.Pos
	// valueFrame links the fieldDeclExpr to the frame in which the
	// value of this field is evaluated.
	valueFrame *frame
}

var _ ast.Node = (*fieldDeclExpr)(nil)

// Pos implements [ast.Node]
func (e *fieldDeclExpr) Pos() token.Pos {
	return e.start
}

// End implements [ast.Node]
func (e *fieldDeclExpr) End() token.Pos {
	return e.end
}

// path models CUE path expressions, and their resolution. Each
// component of a path captures to what it resolves.
type path struct {
	// frame is the frame that contains this path in some way. Once
	// each component of the path is resolved to a set of navigables,
	// this frame and the component's node are added to the usedBy
	// field of each of those navs.
	frame *frame
	// When the final component of a path is resolved, if receiver is
	// non-nil, then it is called with the final set of navigables.
	receiver func([]*navigable)
	// components is the list of components of this path.
	//
	// For simple paths which start with an ident, components is always
	// one longer than the number of parts of the path so that that the
	// last pathComponent captures the results of the final path part.
	//
	// For paths that start with an inline struct (or list), the
	// components will start immediately after the inline component,
	// and will be one longer than the number of subsequent parts.
	//
	// components of length 1 are possible though: these are used to
	// model resolutions which are computed via other
	// means. E.g. package declarations and import specs.
	components []pathComponent
	// startsInline records whether this path started with an inline
	// struct or list (which will not be part of the components).
	startsInline bool
	// resolveOnRead marks a path that the evaluation of its frame
	// leaves unresolved: it is resolved, and the mark cleared, the
	// first time [path.definitionsForOffset] or
	// [path.definitionsForNode] finds a component in it. It is set on a
	// call's label paths, whose resolution expands the callee.
	resolveOnRead bool
}

// pathComponent models part of a path.
//
// Consider this example:
//
//	a: x: b
//	b: y: 5
//	c: x: y: int
//	c: a
//	c: x: f(x.y)
//
// and we have the path `x.y` within `c`. We will have three
// components in total:
//
//  1. node: Ident x; name: "x";
//     unexpanded: the initial navs that lexically contain `x` (i.e. `c`'s nav);
//     expanded: the navs for `c` and `a` (as `a` is embedded in `c`).
//  2. node: Ident y; name: "y";
//     unexpanded: the resulting navs from (1) (i.e. the navs for `a.x` and `c.x`)
//     which we expand;
//     expanded: the navs for `c.x`, `a.x`, and `b`, in which we search for a binding y.
//  3. node: SelectorExpr x.y; name: "";
//     unexpanded: the resulting navs from (2) (i.e. the navs for `b.y` and `c.x.y`);
//     expanded: nil.
type pathComponent struct {
	// node is the part of the path modelled by this pathComponent.
	node ast.Node
	// unexpanded is the set of navigables that the previous path
	// component resolves to.
	unexpanded []*navigable
	// expanded is the set of navs that can be reached from the
	// expanding the unexpanded navs by following their resolvesTo
	// fields transitively. This set should never decrease in
	// size. Whenever it increases in size, we re-resolve the current
	// component and all subsequent components in this path.
	//
	// Both unexpanded and expanded can be thought of as the "inputs"
	// for the resolution of name. The results of that resolution are
	// stored in the next component.
	//
	// expanded is calculated by passing unexpanded to
	// [expandNavigables], which is why unexpanded is a slice and
	// expanded is a set.
	expanded map[*navigable]struct{}
	// name is the name of the binding which we search for in all of
	// the expanded navs.
	name string
	// start and end are the bounds of this pathComponent. Note that
	// for the path `x . y` the end of the x component is the position
	// of the dot, and the start of the y component is one character to
	// the right of the dot.
	start token.Pos
	end   token.Pos
}

// resolvesToChanged indicates that the i-th component of this path
// might need re-resolving, because the set of navigables that
// constitute the inputs to the i-th component has expanded.
//
// If this i-th component really does need re-resolving, then the
// i-th+1 component is then tested to see if its input set has grown,
// and so on.
func (p *path) resolvesToChanged(i int) {
	components := p.components
	compLen := len(components)

	for ; i < compLen-1; i++ {
		pc := &components[i]
		unexpanded := pc.unexpanded

		if pc.name != "" {
			expanded := expandNavigables(unexpanded)
			switch cmp.Compare(len(pc.expanded), len(expanded)) {
			case 0: // no change
				return
			case 1: // new expanded set is smaller than old. "impossible"
				panic(fmt.Sprintf("For path component %d, the expanded navs set shrank! Before: %d, after: %d",
					i, len(pc.expanded), len(expanded)))
			case -1:
				pc.expanded = expanded
				for nav := range expanded {
					nav.ensureResolvesToObserver(p, i)
				}
			}

			unexpanded = navigateByName(expanded, pc.name)
		}

		if len(unexpanded) == 0 {
			for i++; i < compLen; i++ {
				c := &components[i]
				c.unexpanded = nil
				c.expanded = nil
			}
			return
		}

		components[i+1].unexpanded = unexpanded
		if pc.node != nil {
			for _, nav := range unexpanded {
				// We eval import specs in two stages (see [frame.eval])
				// so that we delay booting remote pkg evals as long as
				// possible. Because of this, if we resolve an ident to an
				// import spec, we must eval it "early" otherwise use of
				// the remote pkg may not be recorded correct. E.g.
				//
				//	package a
				//	import "b"
				//	c: b
				//
				// When resolving the path b on line 3, if we don't eval
				// the import spec here, we would never record that pkg b
				// is used by pkg a.
				for _, fr := range nav.frames {
					if _, ok := fr.node.(*ast.ImportSpec); ok {
						nav.eval()
						break
					}
				}
				nav.recordUsage(pc.node, p.frame)
			}
		}
	}

	// If we get here, we have successfully resolved every component of
	// this path and have new results for the final path
	// component.

	if p.receiver == nil {
		return
	}
	p.receiver(components[i].unexpanded)
}

// addRoots adds navs to the set of navigables the path's first
// component resolves from, and resolves the path again unless its
// resolution still waits for its first read.
func (p *path) addRoots(navs []*navigable) {
	pc := &p.components[0]
	grown := false
	for _, nav := range navs {
		if slices.Contains(pc.unexpanded, nav) {
			continue
		}
		pc.unexpanded = append(pc.unexpanded, nav)
		grown = true
	}
	if grown && !p.resolveOnRead {
		p.resolvesToChanged(0)
	}
}

// read resolves the path if its resolution waits for a read, and
// clears the mark.
func (p *path) read() {
	if !p.resolveOnRead {
		return
	}
	p.resolveOnRead = false
	p.resolvesToChanged(0)
}

// definitionsForOffset searches the components of this path for a
// component that contains the given offset. If found, the component's
// index, and the results of its resolution (not its inputs) are
// returned. If not found, -1, nil are returned.
func (p *path) definitionsForOffset(offset int) (int, []*navigable) {
	components := p.components
	compLen := len(components)

	if compLen == 1 {
		pc := components[0]
		start := pc.node.Pos()
		end := pc.node.End()
		if token.WithinInclusive(offset, start, end) {
			return 0, pc.unexpanded
		}
		return -1, nil
	}

	i, found := slices.BinarySearchFunc(components[:compLen-1], nil, func(pc pathComponent, t any) int {
		if pc.node == nil {
			// this slice element (pc) precedes the target. I.e. hunt further right.
			return -1
		}
		// cmp must return a negative number if the slice element
		// precedes the target, or a positive number if the slice
		// element follows the target.
		if offset < pc.start.Offset() {
			return 1
		}
		if pc.end.Offset() < offset {
			return -1
		}
		return 0
	})
	if !found {
		return -1, nil
	}
	p.read()
	return i, components[i+1].unexpanded
}

// definitionsForNode is the twin of [path.definitionsForOffset]: it
// searches the components of this path for one whose node is el (by
// pointer), and returns the component's index and the results of its
// resolution. If not found, -1, nil are returned.
func (p *path) definitionsForNode(el ast.Node) (int, []*navigable) {
	components := p.components
	for i := range components {
		if components[i].node != el {
			continue
		}
		p.read()
		// The results of resolving a component are stored in the
		// component that follows it. The final component of a
		// multi-component path exists only to hold the results of
		// the whole path, so a match on it (the whole path
		// expression) - or on the single component of a length-1
		// path - finds its results in place.
		if i+1 < len(components) {
			return i, components[i+1].unexpanded
		}
		return i, components[i].unexpanded
	}
	return -1, nil
}

// frameStack is used when evaluating comprehensions. It allows a
// stack of frames to be built and ensures that the range of a frame
// is always a superset of the range of any frame above it in the
// stack (stacks grow upwards).
type frameStack []*frame

func (stack *frameStack) push(n ast.Node, node *frame) {
	nodes := *stack
	for _, node := range nodes {
		node.addRange(n)
	}
	*stack = append(nodes, node)
}

func (stack *frameStack) peek() *frame {
	nodes := *stack
	if len(nodes) == 0 {
		return nil
	}
	return nodes[len(nodes)-1]
}

// workqueue is a generic container that models a queue of items where
// every item must never be processed more than once.
type workqueue[T comparable] struct {
	// todo is the list of items. It is only ever appended to.
	todo []T
	// seen is a set that captures every item ever enqueued onto the
	// workqueue. Presence in seen does not imply that the item has
	// been "processed".
	seen map[T]struct{}
}

// newWorkqueue creates a new workqueue with the items provided in the
// todo list and seen set, deduplicated.
func newWorkqueue[T comparable](items ...T) *workqueue[T] {
	wq := &workqueue[T]{seen: make(map[T]struct{})}
	for _, item := range items {
		wq.enqueue(item)
	}
	return wq
}

// items provides an iterator over every item in the todo list. It is
// safe to enqueue items whilst iterating over them (within the same
// Go-routine): if the enqueued item has never been enqueued before
// then it will eventually emitted by the sequence.
func (wq *workqueue[T]) items() iter.Seq[T] {
	return func(yield func(T) bool) {
		for i := 0; i < len(wq.todo); i++ {
			if !yield(wq.todo[i]) {
				return
			}
		}
	}
}

// enqueue adds each item to the todo list if it has not already been
// enqueued.
func (wq *workqueue[T]) enqueue(items ...T) {
	for _, item := range items {
		if _, seen := wq.seen[item]; seen {
			continue
		}
		wq.seen[item] = struct{}{}
		wq.todo = append(wq.todo, item)
	}
}

// enqueue eagerly iterates through the sequence of items provided,
// adding each to the todo list if it has not already been enqueued.
func (wq *workqueue[T]) enqueueSeq(items iter.Seq[T]) {
	for item := range items {
		if _, seen := wq.seen[item]; seen {
			continue
		}
		wq.seen[item] = struct{}{}
		wq.todo = append(wq.todo, item)
	}
}
