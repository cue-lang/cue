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
	"slices"
	"strconv"
	"strings"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/interpreter/embed"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/internal/golangorgx/gopls/protocol"
	"cuelang.org/go/internal/lsp/fscache"
	"cuelang.org/go/internal/lsp/rangeset"
)

// EvaluatorForPackageFunc is a callback function used to resolve
// package evals by their import path. It returns the Evals for the
// given import path, or nil if the package cannot be resolved.
type EvaluatorForPackageFunc = func(importPath ast.ImportPath) *Evaluator
type EvaluatorsForEmbedAttrFunc = func(attr *ast.Attribute) []*Evaluator

// ImportersFunc is a callback function to fetch the package
// evaluator for packages that directly import the current package.
type ImportersFunc = func() []*Evaluator
type EmbeddersFunc = func() map[*Evaluator][]*embed.Embed

type Evaluator struct {
	config Config
	// pkgFrame is the top level (or root) lexical scope
	pkgFrame *frame
	// pkgDecls contains every package declaration with the files
	// passed to [New]. This is not the same as the
	// [pkgFrame.navigable] (which is the entire package scope).
	pkgDecls *navigable
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
	PkgImporters       ImportersFunc
	ForEmbedAttribute  EvaluatorsForEmbedAttrFunc
	PkgEmbedders       EmbeddersFunc
	SupportsReferences bool
}

func (c *Config) init() {
	if c.ForPackage == nil {
		c.ForPackage = func(importPath ast.ImportPath) *Evaluator { return nil }
	}
	if c.PkgImporters == nil {
		c.PkgImporters = func() []*Evaluator { return nil }
	}

	if c.ForEmbedAttribute == nil {
		c.ForEmbedAttribute = func(attr *ast.Attribute) []*Evaluator { return nil }
	}
	if c.PkgEmbedders == nil {
		c.PkgEmbedders = func() map[*Evaluator][]*embed.Embed { return nil }
	}

	c.SupportsReferences = true
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

	for _, file := range files {
		fe := &FileEvaluator{
			evaluator: evaluator,
			File:      file,
		}
		evaluator.byFilename[file.Filename] = fe
		fileFr := fe.newFrame(pkgFrame, file, fileFramesNavigable)
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

	for _, fe := range e.byFilename {
		clear(fe.likelyReferenceOffsets)
		clear(fe.importSpecNavigables)
		fileFr := fe.newFrame(pkgFrame, fe.File, fileFramesNavigable)
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

// initialNavsForImport locates navigable that are likely to
// contain uses of the import path.
func (e *Evaluator) initialNavsForImport(ip ast.ImportPath) []*navigable {
	// When calculating usages of a field, we can discover that the
	// field is exported by the package. We then look for all the
	// packages that import us. To try to avoid brute force evaluation
	// of the whole of these "downstream" packages, we use
	// [FileEvaluator.findIdentUsageOffsets] to quickly search for
	// file offsets that appear to be of idents that match the imported
	// package's name (i.e. our original now-"upstream" package). We
	// evaluate up to those offsets only, and return the
	// navigable now associated with those offsets.
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

			for _, offsets := range fe.findIdentUsageOffsets(name) {
				for _, fr := range fe.evalForOffset(offsets) {
					result = append(result, fr.navigable)
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

type FileEvaluator struct {
	evaluator *Evaluator
	// File is the original [ast.File] that was passed to [New].
	File *ast.File
	// likelyReferenceOffsets contains file offsets for idents that are
	// likely to be references (as opposed to declarations) and likely
	// to reference imported packages. It is lazily populated by
	// [FileEvaluator.findIdentUsageOffsets].
	likelyReferenceOffsets map[string][]int
	// importSpecNavigables contains entries for every import within this
	// file. Within this file, all import specs that are of the
	// same (canonical) import path, share the same
	// navigable. This is made possible by this map.
	importSpecNavigables map[ast.ImportPath]*navigable
}

// DefinitionsForOffset reports the definitions that the file offset
// (number of bytes from the start of the file) resolves to.
func (fe *FileEvaluator) DefinitionsForOffset(offset int) []ast.Node {
	if !fe.evaluator.config.SupportsReferences {
		return nil
	}
	var nodes []ast.Node

	for nav := range fe.definitionsForOffset(offset) {
		for _, fr := range nav.frames {
			if fr.key != nil {
				nodes = append(nodes, fr.key)
			}
		}
	}

	return nodes
}

// DocCommentsForOffset is very similar to DefinitionsForOffset. It
// reports the doc comments associated with the definitions that the
// file offset (number of bytes from the start of the file) resolves
// to.
func (fe *FileEvaluator) DocCommentsForOffset(offset int) map[ast.Node][]*ast.CommentGroup {
	commentsMap := make(map[ast.Node][]*ast.CommentGroup)

	for nav := range fe.definitionsForOffset(offset) {
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
// (number of bytes from the start of the file).
func (fe *FileEvaluator) CompletionsForOffset(offset int) map[Completion]map[string]struct{} {
	var completions map[Completion]map[string]struct{}
	addCompletions := func(completion Completion, nameSet map[string]struct{}) {
		if len(nameSet) == 0 {
			return
		}
		if completions == nil {
			completions = make(map[Completion]map[string]struct{})
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
nextFrame:
	for _, fr := range leafFrames {
		if fr.navigable == fe.evaluator.pkgDecls {
			// do not make any suggestions if we're inside a package decl
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
		inFieldDecl := ok && withinInclusive(offset, fieldDecl.start, fieldDecl.end)
		if inFieldDecl {
			// Only make suggestions if we're really within the field key ident.
			if keyIdent := fieldDecl.keyIdent; keyIdent != nil {
				start := keyIdent.Pos()
				end := keyIdent.End()
				if withinInclusive(offset, start, end) {
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
			// We're going to make suggestions that replace the whole of
			// the existing path element.
			embedCompletions := Completion{Start: pc.start.Offset(), End: pc.end.Offset()}
			if compIdx < len(p.components)-1 {
				// We're before the last component in the path, so
				// back off the end so it doesn't include the .
				embedCompletions.End--
			}
			if compIdx == 0 && !p.startsInline {
				// We're in the first component of a path which starts
				// with an ident. The first component must be resolvable
				// lexically, the same as embeds.
				suggestEmbedsFrom[fr] = embedCompletions
				if len(p.components) == 2 {
					// This path only has 1 component, so we offer to
					// switch it to a field.
					fieldCompletions := embedCompletions
					fieldCompletions.Suffix = ": "
					suggestFieldsFrom[fr.navigable] = fieldCompletions
				}
			} else {
				// We're in some component of a path, but not at the start.
				nameSet := make(map[string]struct{})
				for nav := range expandNavigables(pc.unexpanded) {
					for name := range nav.bindings {
						if !strings.HasPrefix(name, "__") {
							nameSet[name] = struct{}{}
						}
					}
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

		if unknown := fr.unknownRanges; unknown != nil && unknown.Contains(offset) {
			// We're within some AST node that this evaluator does not
			// process (eg a BasicLit). Do not offer any completions *at
			// all*.
			return nil
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
			// sensible.
			suggestFieldsFrom[fr.navigable] = Completion{
				Start:  offset,
				End:    offset,
				Suffix: ": ",
			}
			suggestEmbedsFrom[fr] = Completion{
				Start: offset,
				End:   offset,
			}
			continue nextFrame
		}

		node := fr.node
		s, isStruct := node.(*ast.StructLit)
		if isStruct && s.Lbrace.IsValid() && s.Rbrace.IsValid() && !withinInclusive(offset, s.Lbrace, s.Rbrace) {
			continue
		}

		suggestEmbedsFrom[fr] = Completion{Start: offset, End: offset}

		_, isFile := node.(*ast.File)
		if isFile || isStruct {
			// We must be between fields.
			suggestFieldsFrom[fr.navigable] = Completion{Start: offset, End: offset}
		}
	}

	processedEmbedFrames := make(map[*frame]struct{})
	for fr, embedCompletions := range suggestEmbedsFrom {
		nameSet := make(map[string]struct{})
		for childFr, parentFr := fr, fr.parent; parentFr != nil; childFr, parentFr = parentFr, parentFr.parent {
			if _, seen := processedEmbedFrames[childFr]; seen {
				break
			}
			processedEmbedFrames[childFr] = struct{}{}
			for name := range parentFr.bindings {
				if !strings.HasPrefix(name, "__") {
					nameSet[name] = struct{}{}
				}
			}
		}
		if len(nameSet) == 0 {
			continue
		}
		embedCompletions.Kind = protocol.VariableCompletion
		addCompletions(embedCompletions, nameSet)
	}

	for nav, fieldCompletions := range suggestFieldsFrom {
		nameSet := make(map[string]struct{})
		for nav := range expandNavigables([]*navigable{nav}) {
			for name := range nav.bindings {
				if !strings.HasPrefix(name, "__") {
					nameSet[name] = struct{}{}
				}
			}
		}
		if len(nameSet) == 0 {
			continue
		}
		fieldCompletions.Kind = protocol.FieldCompletion
		addCompletions(fieldCompletions, nameSet)
	}

	return completions
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
				offsets := fe.findIdentUsageOffsets(name)
				result := slices.Collect(fe.definitionsForOffset(offsets...))
				if len(result) > 0 {
					navs = result
					break
				}
			}
		}
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

// usages attempts to discover all uses of the given navs whilst doing
// as little work as possible.
func usages(navsWorklist []*navigable) {
	navsWorklist = slices.Clip(navsWorklist)

	traversedNavs := make(map[*navigable]struct{})
	evaluatedNavs := make(map[*navigable]struct{})

	for len(navsWorklist) > 0 {
		nav := navsWorklist[0]
		navsWorklist = navsWorklist[1:]

		if _, seen := traversedNavs[nav]; seen {
			continue
		}
		traversedNavs[nav] = struct{}{}

		// We're going to have to evaluate everything in nav.frames. But
		// we might need to evaluate frames from the ancestors of nav
		// too.
		isExported := false
		var targetNavs []*navigable
		var evalWorklist []*navigable
		{
			pkgNav := nav.evaluator.pkgFrame.navigable
			var child *navigable
			for parent := nav; parent != nil; child, parent = parent, parent.parent {
				if parent == pkgNav {
					isExported = true
					if child != nil {
						for _, fr := range nav.frames {
							if _, ok := fr.node.(*ast.ImportSpec); ok {
								// We're looking for usages of an imported pkg
								// within the current pkg. Imports do not
								// automatically get re-exported by a package,
								// so although we've reached the pkgNav, this
								// import spec is not exported.
								isExported = false
								break
							}
						}
					}
				}
				evalWorklist = []*navigable{parent}
				if child == nil {
					continue
				}
				// Because each nav has a role to play as a parent and
				// then a child, we only add navs to the traversedNavs
				// set once they've played both roles.
				traversedNavs[child] = struct{}{}
				targetNavs = append(targetNavs, child)
				if parent.name != "" && child.name == "" {
					// The transition from an unnamed child nav to a named
					// parent nav is the place to stop.
					//
					//	x: (({y: 6} & {y: int}).y + 1)
					//
					// If the user is asking for usages of the first y, we
					// must walk up until we find the x navigable binding
					// and evaluate all its frames.
					break
				}
			}
		}

		for len(evalWorklist) > 0 {
			nav := evalWorklist[0]
			evalWorklist = evalWorklist[1:]

			if _, seen := evaluatedNavs[nav]; seen {
				continue
			}
			evaluatedNavs[nav] = struct{}{}

			// If this nav's evaluator does not support references
			// (e.g. json) then there's no point evaluating it, because
			// doing so cannot possibly find any uses of the targetNavs.
			if !nav.evaluator.config.SupportsReferences {
				continue
			}
			nav.eval()
			for _, fr := range nav.frames {
				for _, childFr := range fr.childFrames {
					evalWorklist = append(evalWorklist, childFr.navigable)
				}
			}
		}

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
		// navsWorklist. I.e. we have established that one of j,h,i has
		// escaped from the in-line struct and so at least some of x's
		// ancestors must now be evaluated.
		for _, target := range targetNavs {
			for _, use := range target.usedBy {
				// Only if the use of target is basically an embedding
				// (i.e. appears in resolvesTo) do we need to go
				// further. So for example, when inspecting the uses of y,
				// if we found x: y + 1 we would not then need to inspect
				// the uses of x.
				if _, found := use.navigable.resolvesTo[target]; found {
					navsWorklist = append(navsWorklist, use.navigable)
				}
			}
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
				navsWorklist = append(navsWorklist, remotePkg.initialNavsForImport(ip)...)
			}
			// Embeds however are different: they are embedded into a
			// specific part of a CUE AST, and it absolutely makes sense
			// to start by evaluating that section of the AST.
			for remotePkg, embeds := range nav.evaluator.config.PkgEmbedders() {
				remotePkg.bootFiles()
				for _, embed := range embeds {
					pos := embed.Field.Value.Pos()
					fe := remotePkg.byFilename[pos.Filename()]
					if fe == nil {
						continue
					}
					for _, fr := range fe.evalForOffset(pos.Offset()) {
						navsWorklist = append(navsWorklist, fr.navigable)
					}
				}
			}
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

// findIdentUsageOffsets does a rough-and-ready walk through the ast,
// searching for idents with the given name. It returns their offsets.
//
// It cuts out some subtrees: where a field definition label matches
// the given name, or where a let- or for-clause creates a binding for
// the given name. Thus the intention is to return offsets that are
// for idents which match the given name, and are unlikely to resolve
// to other fields. This is approximate and false positives will
// definitely be returned, but it's a single fast pass over the AST.
func (fe *FileEvaluator) findIdentUsageOffsets(name string) []int {
	likelyReferenceOffsets := fe.likelyReferenceOffsets
	if likelyReferenceOffsets == nil {
		likelyReferenceOffsets = make(map[string][]int)
		fe.likelyReferenceOffsets = likelyReferenceOffsets
	}

	offsets, found := likelyReferenceOffsets[name]
	if !found {
		ast.Walk(fe.File, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.ImportDecl:
				// We want to find potential uses of name. If name exists
				// within an import decl, it can only be as the import's
				// local package name, which we definitely want to exclude
				// - that's a definition of name, not a use.
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
			case *ast.Ident:
				if n.Name == name {
					offsets = append(offsets, n.Pos().Offset())
				}
			}
			return true
		}, nil)
		likelyReferenceOffsets[name] = offsets
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
	seen := make(map[*navigable]struct{})
	worklist := []*navigable{fe.evaluator.pkgFrame.navigable}
	for len(worklist) > 0 {
		nav := worklist[0]
		worklist = worklist[1:]

		if _, found := seen[nav]; found {
			continue
		}
		seen[nav] = struct{}{}

		nav.eval()

		for _, fr := range nav.frames {
			// The very top level pkgFrame.navigable will not "contain"
			// the file+offset, but it will have a child that does. This
			// is the only case where a child frame would contain a
			// file+offset and its parent frame does not.

			isLeaf := fr.contains(fe, offset)
			for _, child := range fr.childFrames {
				if child.contains(fe, offset) {
					worklist = append(worklist, child.navigable)
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
	// evaluated tracks whether this navigable has been evaluated,
	// ensuring it is only evaluated once. Note that it is possible for
	// a navigable's frames to be evaluated without the navigable
	// itself being evaluated. Therefore it is not sufficient to rely
	// on finding unevaluated frames within a navigable.
	evaluated bool
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
func (n *navigable) eval() {
	if n.evaluated {
		return
	}
	n.evaluated = true
	// Calling fr.eval() can append new frames to
	// fr.navigable.frames (for example, the evaluation of
	// BinaryExpr or comprehension clauses, can append new frames to
	// the current navigable). Thus we don't use range.
	for i := 0; i < len(n.frames); i++ {
		fr := n.frames[i]
		fr.eval()
	}

	if len(n.bindings) == 0 {
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
	worklist := navs
	navsSet := make(map[*navigable]struct{})
	for len(worklist) > 0 {
		nav := worklist[0]
		worklist = worklist[1:]
		if _, seen := navsSet[nav]; seen {
			continue
		}
		navsSet[nav] = struct{}{}

		nav.eval()

		worklist = slices.AppendSeq(worklist, maps.Keys(nav.resolvesTo))
	}
	return navsSet
}

func expandNavigablesWithUsages(navs []*navigable) map[*navigable]struct{} {
	fmt.Printf("expandNavigablesWithUsages %v\n", navs)

	result := make(map[*navigable]struct{})
	for _, nav := range navs {
		var names []string
		for ; nav != nil && nav.name != ""; nav = nav.parent {
			names = append(names, nav.name)
		}
		fmt.Printf("  %v n:%p np:%p pk:%p\n", names, nav, nav.parent, nav.evaluator.pkgFrame.navigable)
		if nav.parent == nav.evaluator.pkgFrame.navigable {
			nav = nav.evaluator.pkgDecls
		}
		usages([]*navigable{nav})
		navs := []*navigable{nav}
		var importSpecNavs []*navigable
		for _, useFr := range nav.usedBy {
			if _, isSpec := useFr.node.(*ast.ImportSpec); isSpec {
				importSpecNavs = append(importSpecNavs, useFr.navigable)
			}
		}
		if len(importSpecNavs) > 0 {
			usages(importSpecNavs)
			navs = navs[:0]
			for _, nav := range importSpecNavs {
				navs = append(navs, nav)
			}
		}

		walkFromMap := make(map[*navigable]struct{})
		for _, nav := range navs {
			fmt.Println("->", nav, nav.usedBy)
			for _, fr := range nav.usedBy {
				if _, found := fr.navigable.resolvesTo[nav]; !found {
					continue
				}
				walkFromMap[fr.navigable] = struct{}{}
			}
		}

		navs = slices.Collect(maps.Keys(walkFromMap))
		for _, name := range slices.Backward(names) {
			navs = navigateByName(expandNavigables(navs), name)
		}
		maps.Copy(result, expandNavigables(navs))
	}
	for nav := range result {
		fmt.Println("result", nav)
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
	// node is the initial node that this frame is solely responsible
	// for evaluating.
	node ast.Node
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
		return withinInclusive(offset, start, end)
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

// newFrame creates a new [frame] which is a child of the current
// frame f. This is a light wrapper around
// [fileEvaluator.newFrame]. See those docs for more details on the
// arguments to this function.
func (f *frame) newFrame(node ast.Node, nav *navigable) *frame {
	child := f.fileEvaluator.newFrame(f, node, nav)
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

	var embeddedResolvable, resolvable []ast.Expr
	var comprehensionsStash map[ast.Node]ast.Node

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

			childFr := f.newFrame(nil, f.fileEvaluator.evaluator.pkgDecls)
			if fscache.IsPhantomPackage(node) {
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
				childFr := f.newFrame(node, nav)
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

				// DefinitionsForOffset always traverses a path, so here
				// we add a path so that DefinitionsForOffset on this
				// import spec reports the package declarations of the
				// remote pkg.
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
			for _, elt := range node.Elts {
				unprocessed = append(unprocessed, elt)
			}

		case *ast.EmbedDecl:
			f.newFrame(node.Expr, f.navigable)

		case *ast.PostfixExpr:
			if node.Op == token.ELLIPSIS {
				unprocessed = append(unprocessed, node.X)
			} else {
				// Currently should never happen as Postfix is only used
				// with ellipses. Just in case that changes, behave the
				// same as Unary.
				f.newFrame(node.X, nil)
			}

		case *ast.ParenExpr:
			unprocessed = append(unprocessed, node.X)

		case *ast.UnaryExpr:
			f.newFrame(node.X, nil)

		case *ast.BinaryExpr:
			switch node.Op {
			case token.AND:
				f.newFrame(node.X, f.navigable).addRange(node)
				f.newFrame(node.Y, f.navigable).addRange(node)
			case token.OR:
				lhsNav := f.newFrame(node.X, nil).navigable
				rhsNav := f.newFrame(node.Y, nil).navigable
				f.navigable.ensureResolvesTo([]*navigable{lhsNav, rhsNav})
				lhsNav.recordUsage(node, f)
				rhsNav.recordUsage(node, f)
			default:
				f.newFrame(node.X, nil)
				f.newFrame(node.Y, nil)
			}

		case *ast.Alias:
			// X=e (the old deprecated alias syntax)
			f.newBinding(node.Ident, node.Expr)

		case *ast.Ellipsis:
			childFr := f.newFrame(node.Type, nil)
			childFr.key = node
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
			// b and c are not unified. So we do not use
			// ensureNavigableBinding, as that would merge the ellipses.
			childFr.navigable.name = "__..."
			f.ellipses = append(f.ellipses, childFr.navigable)

		case *ast.CallExpr:
			resolvable = append(resolvable, node.Fun)
			for _, arg := range node.Args {
				f.newFrame(arg, nil)
			}

		case *ast.Ident, *ast.SelectorExpr, *ast.IndexExpr:
			embeddedResolvable = append(embeddedResolvable, node.(ast.Expr))

		case *fieldDeclExpr:
			aliasIdent := node.aliasIdent
			key := node.key
			if key != nil && !strings.HasPrefix(node.keyName, "__") && aliasIdent != key {
				f.newPathFromAncestralNames(key, node.keyName)
			}
			if aliasIdent != nil {
				f.newPathFromAncestralNames(aliasIdent, aliasIdent.Name)
			}
			unprocessed = append(unprocessed, node.exprs...)

		case *ast.Comprehension:
			clause := node.Clauses[0]
			unprocessed = append(unprocessed, clause)
			// We don't know how many child frames we'll need to
			// process clause. So we stash whatever remains of this
			// comprehension and can later find it once we've finished
			// processing our clause.
			if comprehensionsStash == nil {
				comprehensionsStash = make(map[ast.Node]ast.Node)
			}
			if len(node.Clauses) == 1 {
				// Base-case: we're dealing with the last clause. So that
				// clause gets processed in this frame, and we make sure we
				// can later use that last clause to find the body (value)
				// of this comprehension.
				comprehensionsStash[clause] = node.Value
			} else {
				// Non-base-case: we're processing the first clause in
				// this frame, and all that remain go into a copy of the
				// comprehension, which we find later and pass to an
				// appropriate child/descendant.
				nodeCopy := *node
				nodeCopy.Clauses = node.Clauses[1:]
				comprehensionsStash[clause] = &nodeCopy
			}

		case *ast.IfClause:
			f.newFrame(node.Condition, nil)

			comprehensionTail := comprehensionsStash[node]
			f.newFrame(comprehensionTail, f.navigable)

		case *ast.ForClause:
			f.newFrame(node.Source, nil)

			stack := frameStack{f.newFrame(nil, nil)}

			if key := node.Key; key != nil {
				stack.push(key, stack.peek().newBinding(key, nil))
			}
			if val := node.Value; val != nil {
				stack.push(val, stack.peek().newBinding(val, nil))
			}

			comprehensionTail := comprehensionsStash[node]
			stack.push(comprehensionTail, stack.peek().newFrame(comprehensionTail, f.navigable))

		case *ast.LetClause:
			ident := node.Ident
			// A let clause might or might not be within a comprehension.
			if comprehensionTail, found := comprehensionsStash[node]; found {
				// We're within a wider comprehension.
				f.newFrame(node.Expr, nil)

				stack := frameStack{f.newFrame(nil, nil)}
				stack.push(ident, stack.peek().newBinding(ident, nil))
				stack.push(comprehensionTail, stack.peek().newFrame(comprehensionTail, f.navigable))

			} else {
				// We're not within a wider comprehension: the binding
				// must be added to the current frame f because we need to
				// be able to find it from the first element of a path.
				f.newBinding(ident, node.Expr)
			}

		case *ast.Field:
			fieldDecl := fieldNames(node)
			keyName := fieldDecl.keyName

			var childNav *navigable
			if keyName != "" {
				childNav = f.navigable.ensureNavigable(keyName)
			}

			var childFr *frame

			aliasIdent := fieldDecl.aliasIdent
			if aliasIdent == nil {
				childFr = f.newFrame(node.Value, childNav)

			} else if fieldDecl.aliasInParentScope {
				childFr = f.newFrame(node.Value, childNav)
				f.appendBinding(aliasIdent.Name, childFr)

			} else {
				wrapper := f.newFrame(nil, nil)
				wrapper.addRange(node)
				wrapper.navigable.ensureResolvesTo([]*navigable{f.navigable})
				childFr = wrapper.newFrame(node.Value, childNav)

				if expr := fieldDecl.aliasValue; expr != nil {
					wrapper.newBinding(aliasIdent, expr)
					// newBinding will have made a frame with a fresh
					// fieldDeclExpr in it for aliasIdent, so we should
					// nil it out in fieldDecl so that our frame for
					// fieldDecl (below) doesn't also find the alias.
					fieldDecl.aliasIdent = nil
				} else {
					wrapper.appendBinding(aliasIdent.Name, childFr)
				}
			}

			childFr.key = fieldDecl.key
			childFr.docsNode = node
			fieldDecl.valueFrame = childFr

			if keyName != "" {
				f.appendBinding(keyName, childFr)
			}

			if node.TokenPos.IsValid() {
				childFr.start = node.TokenPos.Add(1)
			}

			if (fieldDecl.key != nil && !strings.HasPrefix(keyName, "__")) || len(fieldDecl.exprs) > 0 {
				// The reason for guarding against __ here (which is
				// possibly surprising given we also detect it in the
				// fieldDeclExpr case above) is because the new frame will
				// become authoritative for the offsets in fieldDecl,
				// which would clobber the real value given that __ tends
				// to mean no real key exists.
				//
				// We rely on the invariant that if the key *does* start
				// with __ then we've created it and in these cases the
				// field label is a simple ident with no aliases and no
				// expressions in the field label. Therefore,
				// len(fieldDecl.exprs) > 0 implies !strings.HasPrefix(keyName, "__")
				childFr.parent.newFrame(fieldDecl, nil)
			}

			var remotePkgNavs []*navigable
			for _, attr := range node.Attrs {
				childFr.addRange(attr)
				for _, remotePkgEvaluator := range f.fileEvaluator.evaluator.config.ForEmbedAttribute(attr) {
					// The attribute is an embed of 1 or more files, which
					// we have converted to pkgs with evaluators. Booting
					// them means their pkgDecls have frames.
					//
					// The behaviour here is very similar to processing an
					// ImportSpec.
					remotePkgEvaluator.bootFiles()
					// We add a path that records that this field resolves
					// to the remote package. DefinitionsForOffset always
					// passes through a path, so this path ensures
					// DefinitionsForOffset on the attr itself will take
					// you to the embedded files.
					p := &path{
						frame: childFr,
						components: []pathComponent{{
							unexpanded: []*navigable{remotePkgEvaluator.pkgDecls},
							node:       attr,
						}},
					}
					childFr.childPaths = append(childFr.childPaths, p)

					// Ensure that the child frame resolves to the file
					// navs from the remote package. This allows paths that
					// travels through the embed and into the remote pkg to
					// resolve correctly.
					for _, remoteFileFr := range remotePkgEvaluator.pkgFrame.childFrames {
						remotePkgNavs = append(remotePkgNavs, remoteFileFr.navigable)
					}

					// We also record that the attr uses the remote package.
					remotePkgEvaluator.pkgDecls.recordUsage(attr, childFr)
				}
			}
			childFr.navigable.ensureResolvesTo(remotePkgNavs)

		default:
			f.addUnknownRange(node.Pos().Offset(), node.End().Offset())
		}
	}

	for _, expr := range embeddedResolvable {
		f.createPath(expr, f.navigable)
	}
	for _, expr := range resolvable {
		f.createPath(expr, nil)
	}
	for _, p := range f.childPaths {
		p.resolvesToChanged(0)
	}
}

// fieldNames analysis the field's label, returning a populated
// [fieldDeclExpr].
func fieldNames(field *ast.Field) *fieldDeclExpr {
	// NB it is known that this doesn't cope well yet with fields which
	// contain multiple aliases. Apparently this is legal:
	//
	//	s: x=[y=string]: {name: y, nextage: x.age+1}
	//	out: s
	//	out: john: {age: 13}
	//
	// TODO: rework this code to cope with multiple aliases per field

	label := field.Label

	var unprocessed []ast.Node
	var aliasIdent *ast.Ident
	aliasInParentScope := false
	alias, isAlias := label.(*ast.Alias)
	if isAlias {
		aliasIdent = alias.Ident
		aliasInParentScope = true
		unprocessed = append(unprocessed, alias.Expr)

		switch expr := alias.Expr.(type) {
		case *ast.ListLit:
			// X=[e]: field
			// X is only visible within field
			aliasInParentScope = false

		case ast.Label:
			// X=ident: field
			// X="basic": field
			// X="\(e)": field
			// X=(e): field
			// X is visible within s
			label = expr
			unprocessed = nil
		}
	}

	var key ast.Node
	var keyIdent *ast.Ident
	keyName := ""
	var aliasValue ast.Expr
	switch label := label.(type) {
	case *ast.Ident:
		key = label
		keyName = label.Name
		keyIdent = label

	case *ast.BasicLit:
		name, _, err := ast.LabelName(label)
		if err == nil {
			key = label
			keyName = name
		}

	case *ast.Interpolation:
		unprocessed = append(unprocessed, label)

	case *ast.ParenExpr:
		if alias, ok := label.X.(*ast.Alias); ok {
			// (X=e): field
			// X is only visible within field.
			// Although the spec supports this, the parser doesn't seem to.
			aliasIdent = alias.Ident
			aliasValue = alias.Expr
		} else {
			unprocessed = append(unprocessed, label.X)
		}

	case *ast.ListLit:
		for _, elt := range label.Elts {
			if alias, ok := elt.(*ast.Alias); ok {
				// [X=e]: field
				// X is only visible within field.
				aliasIdent = alias.Ident
				aliasValue = alias.Expr
			} else {
				unprocessed = append(unprocessed, elt)
			}
		}
	}

	if key == nil && aliasIdent != nil {
		key = aliasIdent
	}

	start, end := field.Label.Pos(), field.Label.End()
	if strings.HasPrefix(keyName, "__") {
		end = start
	} else if field.TokenPos.IsValid() {
		end = field.TokenPos
	}

	return &fieldDeclExpr{
		keyName:            keyName,
		key:                key,
		keyIdent:           keyIdent,
		aliasIdent:         aliasIdent,
		aliasInParentScope: aliasInParentScope,
		aliasValue:         aliasValue,
		exprs:              unprocessed,
		start:              start,
		end:                end,
	}
}

// newBinding creates and returns a new [frame], and stores it under
// the given name in the current frame only.
func (f *frame) newBinding(key *ast.Ident, unprocessed ast.Node) *frame {
	childFr := f.newFrame(unprocessed, nil)
	name := key.Name
	f.appendBinding(name, childFr)
	if !strings.HasPrefix(name, "__") {
		// Same logic as in [frame.ensureNavigableBinding] above;
		expr := &fieldDeclExpr{
			key:        key,
			keyIdent:   key,
			keyName:    name,
			start:      key.Pos(),
			end:        key.End(),
			valueFrame: childFr,
		}
		f.newFrame(expr, nil)
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
func (f *frame) createPath(expr ast.Expr, receiver *navigable) {
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

		default:
			component = nil
			childFr := f.newFrame(curExpr, nil)
			if nextEnd.IsValid() {
				childFr.end = nextEnd
			}
			rootNav = childFr.navigable
			startsInline = true
		}
	}

	if len(components) == 0 {
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
					return f.navigable, nav.name
				}
			}

			if !requireIdent {
				return f.navigable, name
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
			return f.navigable, name
		}

		if f.isFileFrame() {
			// If we've got this far, we're allowed to inspect the
			// (shared) navigable bindings directly without having to go
			// via our bindings.
			if _, found := f.navigable.bindings[name]; found {
				return f.navigable, name
			}
			// Support for the Self experiment:
			parentNav := frameOrig.navigable.parent
			if name == "self" && frameOrig.fileEvaluator.File.Pos().Experiment().AliasV2 {
				return parentNav, ""
			}
			return nil, ""
		}
	}
	return nil, ""
}

// docComments extracts the comments from the current frame.
func (n *frame) docComments() []*ast.CommentGroup {
	if n.docsNode == nil {
		return nil
	}
	var comments []*ast.CommentGroup
	for _, group := range ast.Comments(n.docsNode) {
		if group.Doc && len(group.List) > 0 && group.List[0].Pos().Compare(n.docsNode.Pos()) < 0 {
			comments = append(comments, group)
		}
	}
	return comments
}

// fieldDeclExpr models all the different possible parts of a field
// declaration label in a more readily accessible form than the AST
// itself.
type fieldDeclExpr struct {
	// Always nil: make the struct implement [ast.Node]
	ast.Node
	// keyName is the main name of the field, whether that's from an
	// Ident or a BasicLit. It is not the name of any alias: if there
	// is an alias but no main name, then keyName will be blank
	// (e.g. X="\(y)")
	keyName string
	// keyIdent is the main Ident of the field if it exists.
	keyIdent *ast.Ident
	// key is either the main key of the field (whether it's an Ident
	// or a BasicLit), or if neither of those exists, then it's the
	// alias Ident.
	key ast.Node
	// aliasIdent is the alias Ident of the field if it exists.
	aliasIdent *ast.Ident
	// aliasInParentScope tracks whether the alias is visible to the
	// rest of the parent scope or just the field value's scope. Most
	// aliases are visible in the parent scope, but some are not,
	// e.g. [x=e]: y
	aliasInParentScope bool
	// aliasValue contains the expression associated with the
	// alias. For (x=e]: y and [x=e]: y aliases, the alias x should
	// resolve to the expression e, and not the field value y.
	aliasValue ast.Expr
	// start and end track the bounds of the fieldDeclExpr.
	start token.Pos
	end   token.Pos
	// exprs contains expressions from within the field declaration
	// that need to be evaluated. E.g. "\(w)-\(x.y)": _
	// Note this is always disjoint from aliasValue.
	exprs []ast.Node
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
	// receiver is only used when a path is embedded. When the final
	// component of a path is resolved, if receiver is non-nil, then
	// its ensureResolvesTo method is called with the final set of
	// navigables.
	receiver *navigable
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
}

// pathComponent models part of a path.
type pathComponent struct {
	// node is the part of the path modelled by this pathComponent. For
	// example in the path x.y, we have two components, the first with
	// node which is the ident x, and the second with node which is the
	// ident y.
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
	p.receiver.ensureResolvesTo(components[i].unexpanded)
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
		if withinInclusive(offset, start, end) {
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
	return i, components[i+1].unexpanded
}

// withinInclusive reports whether offset lies within the range start
// to end, inclusive on both ends. It is up to the caller to ensure
// that start and end are from the same file, and start is before end,
// and that offset is appropriate for the file.
func withinInclusive(offset int, start, end token.Pos) bool {
	return start.Offset() <= offset && offset <= end.Offset()
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
