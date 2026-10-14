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

// This file holds the dynamic side of the file-type registry: the
// process-wide set of encodings registered at runtime through
// cuelang.org/go/unstable/encodingregistry, and the codec registry
// that internal/encoding and cmd/cue consult for decoder/encoder
// constructors.
//
// This package deliberately does not link the CUE evaluator: dynamic
// registrations arrive here pre-validated against the types.cue
// template by the registration package, as plain data.
//
// Concurrency: registrations are serialized by a mutex and publish a
// new immutable snapshot through an atomic pointer (copy on write).
// The first resolution or codec lookup freezes the registry, after
// which Register fails with *LateRegistrationError and readers proceed
// without locks; registration is add-only.

import (
	"fmt"
	"io"
	"slices"
	"sync"
	"sync/atomic"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/build"
)

// A Decoder decodes documents of a dynamically registered encoding
// into CUE syntax. Decode returns one expression per document and
// io.EOF when no documents remain.
type Decoder interface {
	Decode() (ast.Expr, error)
}

// An Encoder encodes concrete CUE syntax into a dynamically registered
// encoding. Close flushes any buffered output; it does not close the
// underlying writer.
type Encoder interface {
	Encode(n ast.Node) error
	Close() error
}

// A Codec holds the decoder and encoder constructors of a dynamically
// registered encoding. NewDecoder is non-nil unless Value supplies a
// value-plane decoder; NewEncoder is nil for decode-only encodings,
// mirroring built-in encodings such as ini and proto that decode but
// do not encode. Registrations arriving through the public
// registration package always carry a decoder; in-package tests may
// register a fully zero Codec for resolution-only encodings (see
// checkCodec).
type Codec struct {
	NewDecoder func(*build.File, io.Reader) (Decoder, error)
	NewEncoder func(*build.File, io.Writer) (Encoder, error)

	// Binary reports that files of this encoding are read as raw bytes,
	// bypassing the UTF-8/BOM normalization internal/encoding applies to
	// text encodings.
	Binary bool

	// Value optionally holds a value-plane codec (a
	// *cuelang.org/go/internal/encoding.ValueCodec) as an opaque payload.
	// It is stored as any so this package keeps no typed dependency on
	// the CUE evaluator; the internal/encoding dispatch site type-asserts
	// it. When non-nil it supersedes NewDecoder/NewEncoder for that
	// direction.
	Value any
}

// DynamicFileInfo holds the template-conforming file-type properties
// of a dynamic registration, either for all modes (Info) or as a
// per-mode overlay (PerMode). In an overlay, a set field replaces the
// base value for that mode — Interpretation and Form wholesale,
// Aspects key by key — and absent fields inherit the base; see
// mergeDynInfo.
type DynamicFileInfo struct {
	// Interpretation optionally names a built-in interpretation.
	Interpretation string

	// Form names the built-in form ("data", "graph", "dag", "schema"
	// or "final") whose aspect values and refinement domain the encoding
	// inherits, exactly as built-in encodings inherit from the forms table.
	Form string

	// Aspects holds concrete overrides on top of the form's values,
	// keyed by aspect name ("data", "references", ..., "attributes").
	// Absent keys inherit the form and mode values.
	Aspects map[string]bool
}

// DynamicEncoding describes one runtime-registered encoding as plain
// data. It is the internal form of encodingregistry.Encoding: the
// public registration package validates the declarative part against
// the types.cue #FileInfo template and then hands it to
// [RegisterEncoding], so this package never needs the evaluator.
type DynamicEncoding struct {
	Name       string
	Extensions []string

	// Info holds the encoding's file-type properties, applied in every
	// mode. PerMode optionally overrides them for specific modes with
	// replacement semantics (see DynamicFileInfo); each mode's composed
	// result is validated independently.
	Info    DynamicFileInfo
	PerMode map[Mode]DynamicFileInfo

	// Subsidiary tags, permitted in scopes such as "name+tag=value:".
	// A non-nil map value declares a constant default (D-004:
	// cascading defaults remain built-in-only); nil leaves the tag
	// unset unless given explicitly.
	Tags     map[string]*string
	BoolTags map[string]*bool

	Codec Codec
}

// A ConflictError reports a refused registration: registration is
// add-only (D-002), so any collision on name, qualifier, or extension
// with a built-in or a prior registration is an error.
type ConflictError struct {
	Encoding string // the attempted registration's name
	Kind     string // "name" | "extension"
	Key      string // the colliding name or extension

	// Owner names the encoding that owns the colliding name or
	// extension. For a collision with a tag (Tag == true) it names the
	// encoding that declared the tag; it is empty for built-in tags,
	// which are shared across the built-in file types rather than owned
	// by a single encoding.
	Owner string

	// Tag reports that the colliding name is a tag — a top-level
	// qualifier such as "schema" or a subsidiary tag such as "strict" —
	// rather than an encoding name.
	Tag bool

	BuiltIn bool // whether the colliding name or extension is built into CUE
}

func (e *ConflictError) Error() string {
	owner := "encoding"
	if e.BuiltIn {
		owner = "built-in encoding"
	}
	switch {
	case e.Kind == "extension":
		return fmt.Sprintf("cannot register encoding %q: extension %q already registered by %s %q",
			e.Encoding, e.Key, owner, e.Owner)
	case e.Tag && e.BuiltIn:
		return fmt.Sprintf("cannot register encoding %q: name already registered as a built-in tag",
			e.Encoding)
	case e.Tag:
		return fmt.Sprintf("cannot register encoding %q: name already registered as a tag of encoding %q",
			e.Encoding, e.Owner)
	default:
		return fmt.Sprintf("cannot register encoding %q: name already registered by %s %q",
			e.Encoding, owner, e.Owner)
	}
}

// A LateRegistrationError reports a registration that arrived after
// the registry was frozen by its first use for resolution or codec
// dispatch. Registrations must happen before any file-type resolution
// in the process, typically from an init function.
type LateRegistrationError struct {
	Encoding string
}

func (e *LateRegistrationError) Error() string {
	return fmt.Sprintf("cannot register encoding %q: file types already in use; register before the first use of file-type resolution", e.Encoding)
}

// dynRegistry is one immutable snapshot of all dynamic registrations,
// in the same open representation the built-in data uses so that
// registered encodings resolve through exactly the same driver.
type dynRegistry struct {
	tagInfo    [NumModes]map[string]*entry
	extensions [NumModes]map[string]*entry
	encodings  [NumModes]map[string]*entry

	// tagKinds classifies the registered qualifier and subsidiary tag
	// names for scope parsing, extending the generated tagTypes map.
	tagKinds map[string]TagType

	// extOwners maps a registered extension to its owning encoding
	// name, for deterministic conflict reporting.
	extOwners map[string]string

	// tagOwners maps a registered subsidiary tag name to the encoding
	// that first declared it: the analog of extOwners for the tag name
	// space, again for deterministic conflict reporting.
	tagOwners map[string]string

	codecs map[string]Codec
}

var emptyDynRegistry = &dynRegistry{}

var (
	dynMu     sync.Mutex // serializes Register
	dynState  atomic.Pointer[dynRegistry]
	dynFrozen atomic.Bool
)

func init() {
	dynState.Store(emptyDynRegistry)
}

// dynSnapshot returns the current dynamic registrations for a
// resolution or dispatch operation, freezing the registry: later
// registrations fail with *LateRegistrationError.
//
// The first freeze takes dynMu so that it serializes with
// RegisterEncoding, which checks dynFrozen and publishes its new state
// under the same lock. This makes the freeze atomic with respect to
// registration: a registration either completes (and is visible to this
// and all later snapshots) before the freeze, or observes the freeze
// and fails — a registration can never publish into a snapshot that an
// already-frozen resolution missed. Once frozen the fast path needs no
// lock, so steady-state resolution stays lock-free.
func dynSnapshot() *dynRegistry {
	if dynFrozen.Load() {
		return dynState.Load()
	}
	dynMu.Lock()
	dynFrozen.Store(true)
	s := dynState.Load()
	dynMu.Unlock()
	return s
}

// ValidateEncoding reports whether a DynamicEncoding's declared
// properties build valid resolution entries in every mode — a known
// interpretation and form, valid aspect names, a usable codec, and
// consistent per-mode overlays — and whether its name, extensions, and
// tags are free of conflicts with the built-in file types. It neither
// registers the encoding nor consults prior registrations: it is
// state-independent and side-effect-free, so conflicts with other
// runtime registrations are reported by [RegisterEncoding] alone. It
// performs no CUE evaluation, so a caller can validate a declaration at
// build time (see encodingregistry.Validate) as cheaply as at
// registration.
func ValidateEncoding(e DynamicEncoding) error {
	if err := e.checkCodec(); err != nil {
		return err
	}
	if err := e.checkBuiltinConflicts(); err != nil {
		return err
	}
	_, _, err := e.modeEntries()
	return err
}

// checkCodec rejects a codec that declares codec machinery without any
// decoder: files of such an encoding could be selected but never read.
// A decoder is either the syntax-plane NewDecoder or a value-plane
// codec carried in Value. A fully zero Codec is permitted so that
// in-package tests can register resolution-only encodings; the public
// registration package requires a decoder before it constructs a
// DynamicEncoding.
func (e DynamicEncoding) checkCodec() error {
	c := e.Codec
	if c.NewDecoder == nil && c.Value == nil && (c.NewEncoder != nil || c.Binary) {
		return fmt.Errorf("cannot register encoding %q: a decoder is required (Codec.NewDecoder or a value-plane Codec.Value)", e.Name)
	}
	return nil
}

// checkBuiltinConflicts reports a collision between the registration
// and the built-in file types: its name against built-in tags and
// encodings, its extensions against built-in extensions, and its
// subsidiary tags against built-in tags of another kind. The built-in
// data is a compile-time constant, so this check is state-independent
// and runs for ValidateEncoding as well as RegisterEncoding.
//
// The name space is the qualifier space: any built-in tag, top-level
// or subsidiary, blocks the name. A built-in encoding also blocks the
// name even when it is not itself a top-level tag — "binarypb" is the
// one such name — otherwise the registration would be half-absorbed
// (its declared file-info shadowed by the built-in's) while its codec
// went live.
func (e DynamicEncoding) checkBuiltinConflicts() error {
	builtins := builtinRegistry()
	builtinTags := tagTypes()
	_, isTag := builtinTags[e.Name]
	switch {
	case isBuiltinEncoding(builtins, e.Name):
		return &ConflictError{
			Encoding: e.Name, Kind: "name", Key: e.Name,
			Owner: e.Name, BuiltIn: true,
		}
	case isTag:
		return &ConflictError{
			Encoding: e.Name, Kind: "name", Key: e.Name,
			Tag: true, BuiltIn: true,
		}
	}
	for _, ext := range e.Extensions {
		for m := Input; m < NumModes; m++ {
			if ee, ok := builtins.extensions[m][ext]; ok {
				return &ConflictError{
					Encoding: e.Name, Kind: "extension", Key: ext,
					Owner: ee.encoding.resolve(), BuiltIn: true,
				}
			}
		}
	}
	// Subsidiary tag declarations may reuse existing subsidiary tag
	// names of the same kind, but not names of another kind.
	for name := range e.Tags {
		if t, ok := builtinTags[name]; ok && t != TagSubsidiaryString {
			return fmt.Errorf("cannot register encoding %q: tag %q conflicts with existing %v tag", e.Name, name, t)
		}
	}
	for name := range e.BoolTags {
		if t, ok := builtinTags[name]; ok && t != TagSubsidiaryBool {
			return fmt.Errorf("cannot register encoding %q: tag %q conflicts with existing %v tag", e.Name, name, t)
		}
	}
	return nil
}

// modeEntries builds and validates the per-mode resolution entries of
// a registration: for each mode, the qualifier/extension entry (the
// tagInfo analog) and the encodings entry (the "encodings: name:
// forms.X & {aspect overrides}" analog), composed from Info and any
// PerMode overlay. It is shared by ValidateEncoding and
// RegisterEncoding so the two cannot drift — the RegisterWithoutFullValidation
// contract relies on registration performing exactly these checks. It
// performs no CUE evaluation or template compilation.
func (e DynamicEncoding) modeEntries() (tagEntries, encEntries [NumModes]*entry, _ error) {
	for m := Input; m < NumModes; m++ {
		info := e.Info
		if overlay, ok := e.PerMode[m]; ok {
			info = mergeDynInfo(info, overlay)
		}
		tagEntry, err := e.newTagEntry(info)
		if err != nil {
			return tagEntries, encEntries, err
		}
		encEntry, err := e.newEncodingEntry(info)
		if err != nil {
			return tagEntries, encEntries, err
		}
		// Validate the complete entry composition the resolution path
		// will perform for this mode.
		if err := e.validateModeEntry(m, info, tagEntry, encEntry); err != nil {
			return tagEntries, encEntries, err
		}
		tagEntries[m], encEntries[m] = tagEntry, encEntry
	}
	return tagEntries, encEntries, nil
}

// validateModeEntry verifies the same composition the resolution path will
// perform for a file selected by the encoding's qualifier or extension. In
// particular, an interpretation and a form can each be valid on their own
// while being mutually exclusive (for example jsonschema and data), and an
// aspect override can conflict with a concrete value from an interpretation.
//
// Keep this evaluator-free: RegisterWithoutFullValidation relies on ValidateEncoding
// using only the generated, pure-Go entry model.
func (e DynamicEncoding) validateModeEntry(m Mode, info DynamicFileInfo, tagEntry, encEntry *entry) error {
	builtins := builtinRegistry()
	resolved := builtins.base[m].clone()
	if !resolved.unifyOne(tagEntry) {
		return fmt.Errorf("cannot register encoding %q: declared properties conflict in %s mode", e.Name, m)
	}

	// Model fromFileUncached after toFile has selected tagEntry. The tag
	// entry deliberately carries no form, so an interpretation is applied
	// first and the dynamic encoding entry subsequently supplies the form
	// and aspect refinements.
	interpretation := resolved.interpretation.resolve()
	form := resolved.form.resolve()
	if form != "" {
		fe := builtins.forms[form]
		if fe == nil || !resolved.unifyOne(fe) {
			return fmt.Errorf("cannot register encoding %q: form %q conflicts in %s mode", e.Name, form, m)
		}
	} else if interpretation != "" {
		ie := builtins.interps[interpretation]
		if ie == nil || !resolved.unifyOne(ie) {
			return fmt.Errorf("cannot register encoding %q: interpretation %q conflicts in %s mode", e.Name, interpretation, m)
		}
	}
	if !resolved.unifyOne(encEntry) {
		if interpretation != "" {
			return fmt.Errorf("cannot register encoding %q: interpretation %q conflicts with declared form %q or aspects in %s mode", e.Name, interpretation, info.Form, m)
		}
		return fmt.Errorf("cannot register encoding %q: declared form %q or aspects conflict in %s mode", e.Name, info.Form, m)
	}
	return nil
}

// isBuiltinEncoding reports whether name is a built-in encoding in any
// mode. Built-in encoding names are reserved even when they are not
// top-level tags, so a registration cannot shadow them.
func isBuiltinEncoding(builtins *registryData, name string) bool {
	for m := Input; m < NumModes; m++ {
		if _, ok := builtins.encodings[m][name]; ok {
			return true
		}
	}
	return false
}

// RegisterEncoding adds a dynamically registered encoding to the
// process-wide registry. The caller (the public registration package)
// has already validated the declarative record against the types.cue
// template; this function checks add-only conflicts, structural references,
// and the composed per-mode semantics represented by the generated built-in
// data, then inserts the encoding atomically. See encodingregistry.Register
// for the public entry point and semantics.
func RegisterEncoding(e DynamicEncoding) error {
	dynMu.Lock()
	defer dynMu.Unlock()

	if dynFrozen.Load() {
		return &LateRegistrationError{Encoding: e.Name}
	}
	old := dynState.Load()

	if err := e.checkCodec(); err != nil {
		return err
	}
	// Add-only conflict checks (D-002), first against the built-in
	// file types, then against prior registrations.
	if err := e.checkBuiltinConflicts(); err != nil {
		return err
	}
	if t, ok := old.tagKinds[e.Name]; ok {
		conflict := &ConflictError{Encoding: e.Name, Kind: "name", Key: e.Name}
		if t == TagTopLevel {
			// The colliding name is a prior registration's own name.
			conflict.Owner = e.Name
		} else {
			// The colliding name is a subsidiary tag of a prior
			// registration; name the encoding that declared it.
			conflict.Tag = true
			conflict.Owner = old.tagOwners[e.Name]
		}
		return conflict
	}
	for _, ext := range e.Extensions {
		if owner, ok := old.extOwners[ext]; ok {
			return &ConflictError{
				Encoding: e.Name, Kind: "extension", Key: ext,
				Owner: owner, BuiltIn: false,
			}
		}
	}
	// Subsidiary tag declarations may reuse existing subsidiary tag
	// names of the same kind, but not names of another kind (the
	// built-in half of this check lives in checkBuiltinConflicts).
	checkTag := func(name string, typ TagType) error {
		if t, ok := old.tagKinds[name]; ok && t != typ {
			return fmt.Errorf("cannot register encoding %q: tag %q conflicts with existing %v tag", e.Name, name, t)
		}
		return nil
	}
	for name := range e.Tags {
		if err := checkTag(name, TagSubsidiaryString); err != nil {
			return err
		}
	}
	for name := range e.BoolTags {
		if err := checkTag(name, TagSubsidiaryBool); err != nil {
			return err
		}
	}

	// Build and validate the open entries per mode before publishing
	// anything. The qualifier (tagInfo) and extension entries mirror a
	// built-in's tagInfo value: encoding name, declared
	// interpretation/form, and the subsidiary tag declarations. The
	// per-mode encodings entry mirrors a built-in's "encodings: name:
	// forms.X & {aspect overrides}" value and is what FromFile resolves
	// aspects from. All are built from the per-mode info, so a PerMode
	// overlay applies on the explicit-qualifier path (tagInfo) as well
	// as the extension path. Public RegisterWithoutFullValidation reaches this
	// path too; modeEntries uses no CUE evaluation or template
	// compilation, and the entries it returns are reused for
	// publication so prevalidated registration does not pay for a
	// duplicate construction pass.
	tagEntries, encEntries, err := e.modeEntries()
	if err != nil {
		return err
	}

	next := &dynRegistry{
		tagKinds:  cloneMap(old.tagKinds),
		tagOwners: cloneMap(old.tagOwners),
		extOwners: cloneMap(old.extOwners),
		codecs:    cloneMapWith(old.codecs, e.Name, e.Codec),
	}
	next.tagKinds[e.Name] = TagTopLevel
	for name := range e.Tags {
		next.tagKinds[name] = TagSubsidiaryString
		if _, ok := next.tagOwners[name]; !ok {
			next.tagOwners[name] = e.Name
		}
	}
	for name := range e.BoolTags {
		next.tagKinds[name] = TagSubsidiaryBool
		if _, ok := next.tagOwners[name]; !ok {
			next.tagOwners[name] = e.Name
		}
	}
	for m := Input; m < NumModes; m++ {
		next.tagInfo[m] = cloneMapWith(old.tagInfo[m], e.Name, tagEntries[m])
		next.extensions[m] = cloneMap(old.extensions[m])
		for _, ext := range e.Extensions {
			next.extensions[m][ext] = tagEntries[m]
		}
		next.encodings[m] = cloneMapWith(old.encodings[m], e.Name, encEntries[m])
	}
	for _, ext := range e.Extensions {
		next.extOwners[ext] = e.Name
	}

	dynState.Store(next)
	return nil
}

// newTagEntry builds the qualifier/extension entry for a registration:
// the analog of a built-in tagInfo value.
func (e DynamicEncoding) newTagEntry(info DynamicFileInfo) (*entry, error) {
	if err := e.checkInfo(info); err != nil {
		return nil, err
	}
	// Like a built-in tagInfo value, the qualifier/extension entry
	// carries the encoding name, an interpretation if declared, and
	// the subsidiary tag declarations — but not the form: forms and
	// aspects live in the per-mode encodings entry resolved by
	// FromFile, mirroring the built-in tagInfo/encodings split.
	ent := &entry{encoding: cstr(e.Name)}
	if info.Interpretation != "" {
		ent.interpretation = cstr(info.Interpretation)
	}
	if len(e.Tags) > 0 {
		ent.tags = make(map[string]sval, len(e.Tags))
		for name, dfltVal := range e.Tags {
			if dfltVal != nil {
				ent.tags[name] = dstr(*dfltVal)
			} else {
				ent.tags[name] = sval{}
			}
		}
	}
	if len(e.BoolTags) > 0 {
		ent.boolTags = make(map[string]bval, len(e.BoolTags))
		for name, dfltVal := range e.BoolTags {
			if dfltVal != nil {
				ent.boolTags[name] = dbool(*dfltVal)
			} else {
				ent.boolTags[name] = bval{}
			}
		}
	}
	return ent, nil
}

// newEncodingEntry builds the per-mode encodings entry for a
// registration: the analog of a built-in "encodings: name: forms.X &
// {aspect overrides}" value, resolved by FromFile.
func (e DynamicEncoding) newEncodingEntry(info DynamicFileInfo) (*entry, error) {
	ent := &entry{}
	if info.Form != "" {
		fe := builtinRegistry().forms[info.Form]
		if fe == nil {
			return nil, fmt.Errorf("cannot register encoding %q: unknown form %q", e.Name, info.Form)
		}
		ent = fe.clone()
		ent.boolTags, ent.tags = nil, nil
		// Preserve the built-in form's refinement domain. For example,
		// schema defaults to schema but callers may explicitly request
		// final or graph, and graph similarly admits dag or data. The dag
		// entry is the exception: types.cue expresses it as forms.graph &
		// form!="graph", whose defaultless result cannot be represented by
		// the generated entry. Supply the intended default and remaining
		// domain explicitly.
		if info.Form == "dag" {
			ent.form = dstrDom("dag", "dag", "data")
		}
	}
	for name, v := range info.Aspects {
		i := slices.Index(aspectNames[:], name)
		if i < 0 {
			return nil, fmt.Errorf("cannot register encoding %q: unknown aspect %q", e.Name, name)
		}
		var ok bool
		if ent.aspects[i], ok = ent.aspects[i].unify(cbool(v)); !ok {
			return nil, fmt.Errorf("cannot register encoding %q: aspect %q conflicts with form %q", e.Name, name, info.Form)
		}
	}
	return ent, nil
}

func (e DynamicEncoding) checkInfo(info DynamicFileInfo) error {
	builtins := builtinRegistry()
	if info.Interpretation != "" && builtins.interps[info.Interpretation] == nil {
		return fmt.Errorf("cannot register encoding %q: unknown interpretation %q", e.Name, info.Interpretation)
	}
	if info.Form != "" && builtins.forms[info.Form] == nil {
		return fmt.Errorf("cannot register encoding %q: unknown form %q", e.Name, info.Form)
	}
	return nil
}

// mergeDynInfo composes the effective file-type properties for one
// mode. A field set in the per-mode overlay replaces the base value —
// Interpretation and Form wholesale, Aspects key by key — and absent
// fields inherit the base. Replacement rather than unification is
// deliberate: a mode may select a form unrelated to the base (say, a
// schema encoding that exports as data), just as built-in encodings
// freely vary form and aspects per mode in types.cue. Each mode's
// composed result is still validated by validateModeEntry.
func mergeDynInfo(base, overlay DynamicFileInfo) DynamicFileInfo {
	out := base
	if overlay.Interpretation != "" {
		out.Interpretation = overlay.Interpretation
	}
	if overlay.Form != "" {
		out.Form = overlay.Form
	}
	if len(overlay.Aspects) > 0 {
		merged := make(map[string]bool, len(base.Aspects)+len(overlay.Aspects))
		for k, v := range base.Aspects {
			merged[k] = v
		}
		for k, v := range overlay.Aspects {
			merged[k] = v
		}
		out.Aspects = merged
	}
	return out
}

func cloneMap[V any](m map[string]V) map[string]V {
	out := make(map[string]V, len(m)+1)
	for k, v := range m {
		out[k] = v
	}
	return out
}

func cloneMapWith[V any](m map[string]V, k string, v V) map[string]V {
	out := cloneMap(m)
	out[k] = v
	return out
}

// LookupCodec returns the codec registered for the named encoding, or
// false if the encoding is not dynamically registered. Like
// resolution, it freezes the registry.
func LookupCodec(name string) (Codec, bool) {
	c, ok := dynSnapshot().codecs[name]
	return c, ok
}

// TypesCUESource returns the types.cue source that defines the
// file-type template and the built-in registrations. The public
// registration package validates dynamic registrations against its
// #FileInfo template.
func TypesCUESource() []byte {
	return []byte(typesCUE)
}

// ResetDynamicRegistryForTesting clears all dynamic registrations and
// unfreezes the registry. It exists only so tests can isolate
// registration scenarios within one process and must not be used
// outside tests.
//
// The caller must have exclusive control of the process-wide registry:
// the reset excludes concurrent registrations but not lock-free
// readers, so a resolution still in flight from before the reset could
// admit a result computed against the old registry into the just
// cleared caches. Do not call it from tests that run in parallel with
// other users of this package.
func ResetDynamicRegistryForTesting() {
	dynMu.Lock()
	defer dynMu.Unlock()
	dynState.Store(emptyDynRegistry)
	dynFrozen.Store(false)
	// The resolution caches memoize results over the frozen registry;
	// unfreezing invalidates them.
	clearParsedScopeCache()
	clearToFileCache()
	fromFileCache.Clear()
}
