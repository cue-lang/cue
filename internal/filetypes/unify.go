// Copyright 2026 CUE Authors
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

package filetypes

// This file holds the fixed, generic unification driver that resolves
// file types over open per-encoding data structures derived from
// types.cue. The built-in data is generated into types_gen.go at build
// time; dynamic registrations (registry.go) insert additional entries
// at runtime. This driver never evaluates CUE.

import (
	"cmp"
	"maps"
	"slices"
	"sync"

	"cuelang.org/go/cue/build"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/internal/filetypes/internal"
)

// kind describes how much is known about a scalar property value,
// mirroring the CUE distinction between an absent field, a disjunction
// with a default, and a concrete value.
type kind uint8

const (
	unset      kind = iota // no value (absent or an unconstrained scalar)
	constraint             // non-default string domain or exclusion
	dflt                   // disjunction with a default; can be overridden
	concrete               // fixed value; a conflicting value is an error
	ref                    // default referencing another tag's resolved value
	// refDflt is a bval-only kind produced by unifying a ref default
	// with a constant default (*strict | bool with *true | bool, say):
	// per CUE, the result's default is the unification of both
	// defaults, so it survives only if the referenced tag resolves to
	// the recorded value. reference and value hold the two operands.
	refDflt
)

// sval is a string property or tag: encoding, interpretation, form, or a
// string tag declaration. Its unify method preserves concreteness, defaults,
// and the supported defaultless constraints from types.cue.
type sval struct {
	kind  kind
	value string
	// domain is a sorted closed set admitted by a defaulted or defaultless
	// disjunction. It is nil for an open domain. A concrete value unified
	// against a non-nil domain must be a member;
	// this is what stops, e.g., form:"data" from overriding the
	// schema-form default *"schema"|"final"|"graph".
	domain []string
	// excluded holds values excluded from an otherwise open domain. It is
	// used for negative constraints in types.cue (#Encoding's non-empty
	// requirement and tagInfo.pb's non-binarypb branch).
	excluded []string
}

// bval is a tri-state boolean property: one of the twelve aspects or a
// boolean tag declaration. A tag default may reference another tag of
// the same entry (e.g. strictKeywords: *strict | bool), recorded as kind == ref
// with the target name in reference.
type bval struct {
	kind      kind
	value     bool
	reference string
}

// Constructors used by the dynamic registry. The generated data in
// types_gen.go spells out its literals instead, so that it can be laid
// out as static data. The zero value is the unset state.

// cstr returns a concrete string value.
func cstr(v string) sval { return sval{kind: concrete, value: v} }

// dstr returns a defaulted string value with an open domain.
func dstr(v string) sval { return sval{kind: dflt, value: v} }

// dstrDom is dstr for a closed disjunction: v is the default and dom is
// the sorted set of all admissible values (including v).
func dstrDom(v string, dom ...string) sval { return sval{kind: dflt, value: v, domain: dom} }

// cbool returns a concrete Boolean value.
func cbool(v bool) bval { return bval{kind: concrete, value: v} }

// dbool returns a defaulted Boolean value.
func dbool(v bool) bval { return bval{kind: dflt, value: v} }

// The bvals the generated aspect arrays are built from. These spell out
// their literals rather than calling cbool and dbool so that the arrays
// in types_gen.go, and hence the arrays copied from them, can be laid
// out as static data.
var (
	cfalse = bval{kind: concrete, value: false}
	ctrue  = bval{kind: concrete, value: true}
	dfalse = bval{kind: dflt, value: false}
	dtrue  = bval{kind: dflt, value: true}
)

// unify merges two tri-state values: unset yields to anything, a
// concrete value beats a default but only if it is a member of the
// default's domain, and two concrete values must agree. Two defaults
// intersect their domains. The Boolean result reports compatibility: true
// means that the values unified without a conflict.
func (a sval) unify(b sval) (sval, bool) {
	switch {
	case a.kind == unset:
		return b, true
	case b.kind == unset:
		return a, true
	case a.kind == concrete && b.kind == concrete:
		return a, a.value == b.value
	case a.kind == concrete:
		return a, b.allows(a.value)
	case b.kind == concrete:
		return b, a.allows(b.value)
	default: // constraints and defaults
		return a.unifyDomains(b)
	}
}

// allows reports whether v is admitted by s's domain and exclusions.
func (s sval) allows(v string) bool {
	return (s.domain == nil || slices.Contains(s.domain, v)) && !slices.Contains(s.excluded, v)
}

// intersectDom intersects two disjunction domains. A nil operand is the
// open (universal) domain. The result is nil only when both operands
// are open; a closed-but-empty intersection is a non-nil empty slice so
// callers can distinguish "no admissible value" from "unconstrained".
func intersectDom(a, b []string) []string {
	switch {
	case a == nil:
		return b
	case b == nil:
		return a
	}
	out := []string{}
	for _, x := range a {
		if slices.Contains(b, x) {
			out = append(out, x)
		}
	}
	return out
}

// unifyDomains intersects two non-concrete string values. Defaults
// follow CUE semantics: the default of the result is the unification
// of the operands' defaults, so two equal defaults survive, two
// differing defaults cancel (the result is present but defaultless,
// as in (*"a" | string) & (*"b" | string)), and a one-sided default
// survives only when the other operand's domain admits it.
func (a sval) unifyDomains(b sval) (sval, bool) {
	dom := intersectDom(a.domain, b.domain)
	deny := slices.Sorted(maps.Keys(func() map[string]bool {
		m := make(map[string]bool, len(a.excluded)+len(b.excluded))
		for _, v := range a.excluded {
			m[v] = true
		}
		for _, v := range b.excluded {
			m[v] = true
		}
		return m
	}()))
	if dom != nil && !slices.ContainsFunc(dom, func(v string) bool { return !slices.Contains(deny, v) }) {
		return sval{}, false
	}
	result := sval{kind: constraint, domain: dom, excluded: deny}
	if a.kind == dflt && b.kind == dflt {
		if a.value == b.value && result.allows(a.value) {
			result.kind = dflt
			result.value = a.value
		}
		return result, true
	}
	for _, candidate := range []sval{a, b} {
		if candidate.kind == dflt && result.allows(candidate.value) {
			result.kind = dflt
			result.value = candidate.value
			break
		}
	}
	return result, true
}

// unify merges two Boolean tri-state values following CUE default
// semantics: bool values as such never conflict (bool & bool is bool),
// only concrete values can, and the default of the result is the
// unification of the operands' defaults — two differing defaults
// cancel to a defaultless-but-present bool (*false | bool unified with
// *true | bool is just bool), represented as kind constraint, which no
// later default can revive (a lost default mark does not come back in
// CUE). Reference defaults resolve at emission time (resolveBoolTag),
// so a ref meeting a constant default is recorded as refDflt and
// decided there.
func (a bval) unify(b bval) (bval, bool) {
	switch {
	case a.kind == unset:
		return b, true
	case b.kind == unset:
		return a, true
	case a.kind == concrete && b.kind == concrete:
		return a, a.value == b.value
	case a.kind == concrete:
		return a, true
	case b.kind == concrete:
		return b, true
	case a == b:
		return a, true
	case a.kind == constraint || b.kind == constraint:
		// A conflicted (defaultless) result absorbs any further default.
		return bval{kind: constraint}, true
	case a.kind == dflt && b.kind == dflt:
		// Differing constant defaults cancel; the tag stays present
		// without a default. (Equal operands were handled above.)
		return bval{kind: constraint}, true
	case a.kind == ref && b.kind == dflt:
		return bval{kind: refDflt, reference: a.reference, value: b.value}, true
	case a.kind == dflt && b.kind == ref:
		return bval{kind: refDflt, reference: b.reference, value: a.value}, true
	case a.kind == refDflt && b.kind == dflt, a.kind == refDflt && b.kind == ref:
		if (b.kind == dflt && b.value == a.value) || (b.kind == ref && b.reference == a.reference) {
			return a, true
		}
		return bval{kind: constraint}, true
	case a.kind == dflt && b.kind == refDflt, a.kind == ref && b.kind == refDflt:
		if (a.kind == dflt && a.value == b.value) || (a.kind == ref && a.reference == b.reference) {
			return b, true
		}
		return bval{kind: constraint}, true
	default:
		// Remaining pairs (distinct refs, distinct refDflts) have
		// defaults that cannot be proven equal; they cancel likewise.
		return bval{kind: constraint}, true
	}
}

func (a sval) resolve() string {
	if a.kind == unset || a.kind == constraint {
		return ""
	}
	return a.value
}

// Aspect indices are lexical so additions and generated output remain easy to
// audit against types.cue.
const (
	aAttributes = iota
	aConstraints
	aCycles
	aData
	aDefinitions
	aDocs
	aIncomplete
	aImports
	aKeepDefaults
	aOptional
	aReferences
	aStream
	numAspects
)

var aspectNames = [numAspects]string{
	aAttributes:   "attributes",
	aConstraints:  "constraints",
	aCycles:       "cycles",
	aData:         "data",
	aDefinitions:  "definitions",
	aDocs:         "docs",
	aIncomplete:   "incomplete",
	aImports:      "imports",
	aKeepDefaults: "keepDefaults",
	aOptional:     "optional",
	aReferences:   "references",
	aStream:       "stream",
}

// aspectBits maps the driver's aspect indices to the internal.Aspects
// bit flags used by FileInfo.SetAspects.
var aspectBits = [numAspects]internal.Aspects{
	aData:         internal.Data,
	aReferences:   internal.References,
	aCycles:       internal.Cycles,
	aDefinitions:  internal.Definitions,
	aOptional:     internal.Optional,
	aConstraints:  internal.Constraints,
	aKeepDefaults: internal.KeepDefaults,
	aIncomplete:   internal.Incomplete,
	aImports:      internal.Imports,
	aStream:       internal.Stream,
	aDocs:         internal.Docs,
	aAttributes:   internal.Attributes,
}

// entry is the open representation of one types.cue #FileInfo value: a
// mode base, a top-level tag, an extension, an encoding, a form, or an
// interpretation. Dynamic registrations insert values of this same
// type, so registered encodings resolve through exactly the same logic
// as built-ins. Entries are immutable once published; resolution works
// on a cloned accumulator.
type entry struct {
	encoding       sval
	interpretation sval
	form           sval

	aspects [numAspects]bval

	// boolTags and tags double as the allow-list for subsidiary tags:
	// a subsidiary tag is permitted iff its key is present.
	boolTags map[string]bval
	tags     map[string]sval

	// alts holds the disjuncts of a struct-level disjunction (only
	// tagInfo.pb and the per-mode encodings.cue value in today's
	// types.cue). The entry's own scalar fields are unused when alts
	// is non-empty; the first disjunct is the default one.
	alts []*entry
}

func (e *entry) clone() *entry {
	c := *e
	c.boolTags = maps.Clone(e.boolTags)
	c.tags = maps.Clone(e.tags)
	return &c
}

// unify1 merges b (which must not have alternatives) into e. It
// reports whether the merge succeeded; on failure e is left in an
// undefined state and must be restored by the caller.
func (e *entry) unify1(b *entry) bool {
	var ok bool
	if e.encoding, ok = e.encoding.unify(b.encoding); !ok {
		return false
	}
	if e.interpretation, ok = e.interpretation.unify(b.interpretation); !ok {
		return false
	}
	if e.form, ok = e.form.unify(b.form); !ok {
		return false
	}
	for i := range e.aspects {
		if e.aspects[i], ok = e.aspects[i].unify(b.aspects[i]); !ok {
			return false
		}
	}
	for k, v := range b.boolTags {
		if e.boolTags == nil {
			e.boolTags = make(map[string]bval, len(b.boolTags))
		}
		if old, exists := e.boolTags[k]; exists {
			nv, ok := old.unify(v)
			if !ok {
				return false
			}
			e.boolTags[k] = nv
		} else {
			e.boolTags[k] = v
		}
	}
	for k, v := range b.tags {
		if e.tags == nil {
			e.tags = make(map[string]sval, len(b.tags))
		}
		if old, exists := e.tags[k]; exists {
			nv, ok := old.unify(v)
			if !ok {
				return false
			}
			e.tags[k] = nv
		} else {
			e.tags[k] = v
		}
	}
	return true
}

// unifyOne unifies a single entry into e, trying disjuncts in order
// when b has alternatives. On failure e is left in an undefined state.
func (e *entry) unifyOne(b *entry) bool {
	if len(b.alts) == 0 {
		return e.unify1(b)
	}
	for _, alt := range b.alts {
		save := e.clone()
		if e.unify1(alt) {
			return true
		}
		*e = *save
	}
	return false
}

// unifyAll unifies every entry of bs into e with backtracking over
// struct-level disjunctions: like CUE, the outcome must not depend on
// the order the entries are applied in, so a later conflict may force
// an earlier entry onto one of its alternative disjuncts. Defaults are
// tried before alternatives, mirroring CUE default semantics for this
// shape. On failure, e is left in an undefined state.
func (e *entry) unifyAll(bs []*entry) bool {
	if len(bs) == 0 {
		return true
	}
	b := bs[0]
	if len(b.alts) == 0 {
		return e.unify1(b) && e.unifyAll(bs[1:])
	}
	for _, alt := range b.alts {
		save := e.clone()
		if e.unify1(alt) && e.unifyAll(bs[1:]) {
			return true
		}
		*e = *save
	}
	return false
}

// registryData holds one complete set of file-type data in open form.
// The built-in instance is generated from types.cue into types_gen.go.
type registryData struct {
	base       [NumModes]*entry            // modes[m].FileInfo
	extensions [NumModes]map[string]*entry // modes[m].extensions
	encodings  [NumModes]map[string]*entry // modes[m].encodings
	tagInfo    map[string]*entry           // top-level qualifier tags
	forms      map[string]*entry
	interps    map[string]*entry
}

// builtinRegistry lazily materializes the immutable built-in file-type data
// generated in types_gen.go. Keeping it out of package initialization avoids
// paying for the large maps in processes that never resolve file types.
var builtinRegistry = sync.OnceValue(newBuiltinRegistry)

// Resolution results are pure functions of their keys over the frozen
// registry — the registry freezes at its first use (dynSnapshot), and
// both caches only fill from resolution calls, i.e. after the freeze —
// so the memoized results never need invalidation. Successful results
// only: error messages embed the filename, and error paths are not
// performance-critical. ResetDynamicRegistryForTesting clears both
// caches together with the freeze flag.
//
// The old table-based implementation was in effect a build-time
// precomputation of these same caches; memoizing restores its
// per-operation cost (DYNFT-CMP-002 budget) while the first resolution
// of each combination runs the generic driver.

type toFileKey struct {
	mode  Mode
	scope string // canonical parsed top-level and boolean tag state
	ext   string
}

type fromFileKey struct {
	mode           Mode
	encoding       build.Encoding
	interpretation build.Interpretation
	form           build.Form
}

var (
	toFileCache   = newBoundedCache[toFileKey, *build.File](maxToFileCacheEntries)
	fromFileCache = newBoundedCache[fromFileKey, FileInfo](maxFromFileCacheEntries) // Filename empty
)

// maxToFileCacheEntries bounds retained results even when a dynamic
// encoding declares enough boolean tags to create an exponential number
// of distinct, bounded-valued semantic scopes. Hits remain lock-free; at the
// cap, FIFO replacement lets new workloads displace stale entries.
const maxToFileCacheEntries = 4096

// maxFromFileCacheEntries bounds retained FromFile results. The
// built-in key space (mode, encoding, interpretation, form) is small,
// but dynamic registrations extend the encoding axis without limit, so
// the cache is FIFO-bounded like its siblings.
const maxFromFileCacheEntries = 4096

func cacheToFileResult(key toFileKey, f *build.File) {
	if _, ok := toFileCache.Load(key); ok {
		return
	}
	toFileCache.LoadOrStore(key, copyFile(f, ""))
}

// clearToFileCache is only used while tests or benchmarks have exclusive
// control of the process-wide registry.
func clearToFileCache() {
	toFileCache.Clear()
}

// copyFile returns a mutable copy of an immutable cached template with
// the filename filled in. Tag maps are cloned so callers may mutate
// the result freely, as they could with uncached results.
func copyFile(tmpl *build.File, filename string) *build.File {
	f := *tmpl
	f.Filename = filename
	f.Tags = maps.Clone(tmpl.Tags)
	f.BoolTags = maps.Clone(tmpl.BoolTags)
	return &f
}

// toFile is the fixed resolution driver: mode ⊓ tags ⊓ extension ⊓
// subsidiary tags over the open data, with the same error texts at the
// same decision points as the earlier table-based implementation.
// Successful resolutions are memoized per (mode, canonical parsed scope,
// extension).
func toFile(mode Mode, sc *scope, filename string) (*build.File, error) {
	// Only cacheable keys are memoized. Subsidiary string tag values
	// (e.g. lang=<anything>) are unbounded and user-controllable, so a
	// scope carrying them would let the cache grow without limit; such
	// resolutions run the driver each time instead. Everything else — mode,
	// top-level tags, Boolean tags, and extension — has a bounded value space
	// after registry freeze, and the FIFO places a hard bound on retained results.
	cacheable := len(sc.subsidiaryString) == 0
	key := toFileKey{mode: mode, scope: canonicalScopeKey(sc), ext: fileExt(filename)}
	if cacheable {
		if tmpl, ok := toFileCache.Load(key); ok {
			return copyFile(tmpl, filename), nil
		}
	}
	f, err := toFileUncached(mode, sc, filename)
	if err != nil {
		return nil, err
	}
	if cacheable {
		cacheToFileResult(key, f)
	}
	return f, nil
}

func toFileUncached(mode Mode, sc *scope, filename string) (*build.File, error) {
	if err := checkMode(mode); err != nil {
		return nil, err
	}
	dyn := dynSnapshot()
	builtins := builtinRegistry()

	var bs []*entry
	if len(sc.topLevel) > 0 {
		for _, tag := range slices.Sorted(maps.Keys(sc.topLevel)) {
			ti := builtins.tagInfo[tag]
			if ti == nil {
				ti = dyn.tagInfo[mode][tag]
			}
			if ti == nil {
				return nil, errors.Newf(token.NoPos, "unknown filetype %s", tag)
			}
			bs = append(bs, ti)
		}
	}
	e := builtins.base[mode].clone()
	if !e.unifyAll(bs) {
		return nil, errors.Newf(token.NoPos, "invalid tag combination")
	}

	if !encodingPinned(bs) {
		ext := fileExt(filename)
		if ext == "" {
			return nil, errors.Newf(token.NoPos, "no encoding specified for file %q", filename)
		}
		ee := builtins.extensions[mode][ext]
		if ee == nil {
			ee = dyn.extensions[mode][ext]
		}
		if ee == nil {
			return nil, errors.Newf(token.NoPos, "unknown file extension %s", ext)
		}
		// The extension entry participates in disjunct selection
		// together with the tag entries: re-resolve from the mode base
		// so a conflict with the extension can force a tag's
		// struct-level disjunction (tagInfo.pb) onto a non-default
		// alternative, exactly as unifying the CUE values would.
		// Unifying ee into the already-resolved e instead would commit
		// pb to its binarypb default before the extension is seen.
		e = builtins.base[mode].clone()
		if !e.unifyAll(append(bs, ee)) {
			return nil, errors.Newf(token.NoPos, "could not determine file type for file %q", filename)
		}
	}

	// Subsidiary tags. As before, a tag class the resolved file type
	// permits no tags of at all yields "tag %s is not allowed in this
	// context", while a non-member of a non-empty class yields the
	// "field %q not allowed" text of the earlier generated functions.
	if len(sc.subsidiaryString) > 0 {
		if len(e.tags) == 0 {
			return nil, errors.Newf(token.NoPos, "tag %s is not allowed in this context", someKey(sc.subsidiaryString))
		}
		for _, k := range slices.Sorted(maps.Keys(sc.subsidiaryString)) {
			v := sc.subsidiaryString[k]
			decl, ok := e.tags[k]
			if !ok {
				return nil, errors.Newf(token.NoPos, "field %q not allowed", k)
			}
			if decl.kind == concrete && decl.value != v {
				return nil, errors.Newf(token.NoPos, "conflict on %s; %#v provided but need %#v", k, v, decl.value)
			}
			e.tags[k] = cstr(v)
		}
	}
	if len(sc.subsidiaryBool) > 0 {
		if len(e.boolTags) == 0 {
			return nil, errors.Newf(token.NoPos, "tag %s is not allowed in this context", someKey(sc.subsidiaryBool))
		}
		for _, k := range slices.Sorted(maps.Keys(sc.subsidiaryBool)) {
			v := sc.subsidiaryBool[k]
			decl, ok := e.boolTags[k]
			if !ok {
				return nil, errors.Newf(token.NoPos, "field %q not allowed", k)
			}
			if decl.kind == concrete && decl.value != v {
				return nil, errors.Newf(token.NoPos, "conflict on %s; %#v provided but need %#v", k, v, decl.value)
			}
			e.boolTags[k] = cbool(v)
		}
	}

	f := &build.File{
		Filename:       filename,
		Encoding:       build.Encoding(e.encoding.resolve()),
		Interpretation: build.Interpretation(e.interpretation.resolve()),
		Form:           build.Form(e.form.resolve()),
	}
	for k := range e.tags {
		if v, ok := resolveStrTag(e.tags, k); ok {
			if f.Tags == nil {
				f.Tags = make(map[string]string)
			}
			f.Tags[k] = v
		}
	}
	for k := range e.boolTags {
		if v, ok := resolveBoolTag(e.boolTags, k); ok {
			if f.BoolTags == nil {
				f.BoolTags = make(map[string]bool)
			}
			f.BoolTags[k] = v
		}
	}
	return f, nil
}

// encodingPinned reports whether any tag entry declares the encoding
// directly, mirroring the earlier CUE-evaluated algorithm's
// lookup(fileVal, "encoding").Exists() test: a tag with a concrete
// (json) or defaulted (jsonschema's *"json" | _) encoding field pins
// the encoding and the file extension is ignored, while an entry whose
// encoding lives only inside struct-level disjuncts (tagInfo.pb) does
// not — the alts fields of such an entry are deliberately not
// consulted, just as a CUE field lookup does not descend into an
// unresolved disjunction. The mode base never declares an encoding, so
// with no pinning tag the extension entry participates in resolution.
func encodingPinned(bs []*entry) bool {
	for _, b := range bs {
		switch b.encoding.kind {
		case concrete, dflt:
			return true
		}
	}
	return false
}

// resolveBoolTag resolves the final value of a boolean tag, following
// reference defaults such as strictKeywords: *strict | bool to the
// referenced tag's resolved value. Reference chains in types.cue are
// short and acyclic; the recursion bottoms out on any non-ref kind.
func resolveBoolTag(m map[string]bval, k string) (value, present bool) {
	v, ok := m[k]
	if !ok || v.kind == unset {
		return false, false
	}
	switch v.kind {
	case ref:
		return resolveBoolTag(m, v.reference)
	case constraint:
		// Present but defaultless (two differing defaults canceled);
		// there is no concrete value to emit unless one is set
		// explicitly, which replaces the entry with a concrete kind.
		return false, false
	case refDflt:
		// The default survives only if the referenced tag resolves to
		// the recorded constant, per the CUE rule that the result's
		// default is the unification of both operands' defaults.
		if rv, ok := resolveBoolTag(m, v.reference); ok && rv == v.value {
			return v.value, true
		}
		return false, false
	}
	return v.value, true
}

func resolveStrTag(m map[string]sval, k string) (value string, present bool) {
	v, ok := m[k]
	if !ok || v.kind == unset || v.kind == constraint {
		return "", false
	}
	return v.value, true
}

// fromFile is the fixed driver behind FromFile: base[mode] unified
// with the concrete fields of b, then with the form entry (form given)
// or the interpretation entry (interpretation given without form), and
// with the per-mode encoding entry when no interpretation applies —
// the same order as the CUE logic the earlier tables were generated
// from. Unlike those tables, which reported "no encoding specified"
// for every miss, each failure names its actual cause: an unknown
// encoding, interpretation, or form, or a combination the mode does
// not support.
func fromFile(b *build.File, mode Mode) (*FileInfo, error) {
	key := fromFileKey{mode, b.Encoding, b.Interpretation, b.Form}
	if fi, ok := fromFileCache.Load(key); ok {
		fi.Filename = b.Filename
		return &fi, nil
	}
	fi, err := fromFileUncached(b, mode)
	if err != nil {
		return nil, err
	}
	cached := *fi
	cached.Filename = ""
	fromFileCache.LoadOrStore(key, cached)
	return fi, nil
}

func fromFileUncached(b *build.File, mode Mode) (*FileInfo, error) {
	if err := checkMode(mode); err != nil {
		return nil, err
	}
	dyn := dynSnapshot()
	builtins := builtinRegistry()

	if b.Encoding == "" {
		return nil, errors.Newf(token.NoPos, "no encoding specified")
	}
	// Validate the names unconditionally, so a bogus value is reported
	// even when the field it would refine is not consulted below (a
	// given form suppresses the interpretation entry, an interpretation
	// suppresses the built-in encoding entry).
	ee := builtins.encodings[mode][string(b.Encoding)]
	if ee == nil {
		ee = dyn.encodings[mode][string(b.Encoding)]
	}
	if ee == nil {
		return nil, errors.Newf(token.NoPos, "unknown encoding %q", b.Encoding)
	}
	if b.Interpretation != "" && builtins.interps[string(b.Interpretation)] == nil {
		return nil, errors.Newf(token.NoPos, "unknown interpretation %q", b.Interpretation)
	}
	if b.Form != "" && builtins.forms[string(b.Form)] == nil {
		return nil, errors.Newf(token.NoPos, "unknown form %q", b.Form)
	}

	failForm := func() (*FileInfo, error) {
		return nil, errors.Newf(token.NoPos,
			"form %q is not supported for encoding %q in %s mode", b.Form, b.Encoding, mode)
	}
	failInterpretation := func() (*FileInfo, error) {
		return nil, errors.Newf(token.NoPos,
			"interpretation %q is not supported for encoding %q in %s mode", b.Interpretation, b.Encoding, mode)
	}
	e := builtins.base[mode].clone()
	var ok bool
	if e.encoding, ok = e.encoding.unify(cstr(string(b.Encoding))); !ok {
		return nil, errors.Newf(token.NoPos, "unknown encoding %q", b.Encoding)
	}
	if b.Interpretation != "" {
		if e.interpretation, ok = e.interpretation.unify(cstr(string(b.Interpretation))); !ok {
			return failInterpretation()
		}
	}
	if b.Form != "" {
		if e.form, ok = e.form.unify(cstr(string(b.Form))); !ok {
			return failForm()
		}
		if !e.unifyAll([]*entry{builtins.forms[string(b.Form)]}) {
			return failForm()
		}
	} else if b.Interpretation != "" {
		if !e.unifyAll([]*entry{builtins.interps[string(b.Interpretation)]}) {
			return failInterpretation()
		}
	}
	if b.Interpretation == "" {
		if !e.unifyAll([]*entry{ee}) {
			if b.Form != "" {
				return failForm()
			}
			return nil, errors.Newf(token.NoPos,
				"encoding %q is not supported in %s mode", b.Encoding, mode)
		}
	} else if de := dyn.encodings[mode][string(b.Encoding)]; de != nil {
		// A built-in encoding's form and aspects come from the
		// interpretation when one applies, so its encoding entry is not
		// unified here. A dynamic encoding, however, may declare its own
		// form and aspect overrides alongside an interpretation; those
		// must still apply (and conflict with the interpretation if
		// incompatible), so its entry participates regardless.
		if !e.unifyAll([]*entry{de}) {
			return failInterpretation()
		}
	}
	fi := &FileInfo{
		Filename:       b.Filename,
		Encoding:       build.Encoding(e.encoding.resolve()),
		Interpretation: build.Interpretation(e.interpretation.resolve()),
		Form:           build.Form(e.form.resolve()),
	}
	var aspects internal.Aspects
	for i := range e.aspects {
		if e.aspects[i].kind != unset && e.aspects[i].value {
			aspects |= aspectBits[i]
		}
	}
	fi.SetAspects(aspects)
	return fi, nil
}

func someKey[K cmp.Ordered, V any](m map[K]V) K {
	return slices.Sorted(maps.Keys(m))[0]
}
