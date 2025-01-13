// Copyright 2020 CUE Authors
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

// Package jsonschema implements the JSON schema standard.
//
// # Mapping and Linking
//
// JSON Schema are often defined in a single file. CUE, on the other hand
// idiomatically defines schema as a definition.
//
// CUE:
//
//	$schema: which schema is used for validation.
//	$id: which validation does this schema provide.
//
//	Foo: _ @jsonschema(sc)
//	@source(https://...) // What schema is used to validate.
//
// NOTE: JSON Schema is a draft standard and may undergo backwards incompatible
// changes.
package jsonschema

import (
	"fmt"
	"net/url"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/ast/astutil"
	"cuelang.org/go/cue/token"
)

// Extract converts JSON Schema data into an equivalent CUE representation.
//
// The generated CUE schema is guaranteed to deem valid any value that is
// a valid instance of the source JSON schema.
func Extract(data cue.InstanceOrValue, cfg *Config) (*ast.File, error) {
	cfg = ref(*cfg)
	if cfg.MapURL == nil {
		cfg.MapURL = DefaultMapURL
	}
	if cfg.Map == nil {
		cfg.Map = defaultMap
	}
	if cfg.MapRef == nil {
		cfg.MapRef = func(loc SchemaLoc) (string, cue.Path, error) {
			return defaultMapRef(loc, cfg.Map, cfg.MapURL)
		}
	}
	if cfg.DefaultVersion == VersionUnknown {
		cfg.DefaultVersion = DefaultVersion
	}
	if cfg.Strict {
		cfg.StrictKeywords = true
		cfg.StrictFeatures = true
	}
	if cfg.ID == "" {
		// Always choose a fully-qualified ID for the schema, even
		// if it doesn't declare one.
		//
		// From https://json-schema.org/draft-07/draft-handrews-json-schema-01#rfc.section.8.1
		// > Informatively, the initial base URI of a schema is the URI at which it was found, or a suitable substitute URI if none is known.
		cfg.ID = DefaultRootID
	}
	rootIDURI, err := url.Parse(cfg.ID)
	if err != nil {
		return nil, fmt.Errorf("invalid Config.ID value %q: %v", cfg.ID, err)
	}
	if !rootIDURI.IsAbs() {
		return nil, fmt.Errorf("Config.ID %q is not absolute URI", cfg.ID)
	}
	d := &decoder{
		cfg:          cfg,
		mapURLErrors: make(map[string]bool),
		root:         data.Value(),
		rootID:       rootIDURI,
		defs:         make(map[string]*definedSchema),
		defForValue:  newValueMap[*definedSchema](),
	}

	f := d.decode(d.root)
	if d.errs != nil {
		return nil, d.errs
	}
	if err := astutil.Sanitize(f); err != nil {
		return nil, fmt.Errorf("cannot sanitize jsonschema resulting syntax: %v", err)
	}
	return f, nil
}

// DefaultVersion defines the default schema version used when
// there is no $schema field and no explicit [Config.DefaultVersion].
const DefaultVersion = VersionDraft2020_12

// A Config configures a JSON Schema encoding or decoding.
type Config struct {
	PkgName string

	// ID sets the URL of the original source, corresponding to the $id field.
	ID string

	// JSON reference of location containing schemas. The empty string indicates
	// that there is a single schema at the root. If this is non-empty,
	// the referred-to location should be an object, and each member
	// is taken to be a schema.
	//
	// Examples:
	//  "#/" or "#"                    top-level fields are schemas.
	//  "#/components/schemas"   the canonical OpenAPI location.
	//
	// Note: #/ should technically _not_ refer to the root of the
	// schema: this behavior is preserved for backwards compatibility
	// only. Just `#` is preferred.
	Root string

	// AllowNonExistentRoot prevents an error when there is no value at
	// the above Root path. Such an error can be useful to signal that
	// the data may not be a JSON Schema, but is not always a good idea.
	AllowNonExistentRoot bool

	// Map maps the locations of schemas and definitions to a new location.
	// References are updated accordingly. A returned label must be
	// an identifier or string literal.
	//
	// The default mapping is
	//    {}                     {}
	//    {"definitions", foo}   {#foo} or {#, foo}
	//    {"$defs", foo}         {#foo} or {#, foo}
	//
	// Deprecated: use [Config.MapRef].
	Map func(pos token.Pos, path []string) ([]ast.Label, error)

	// MapURL maps a URL reference as found in $ref to
	// an import path for a CUE package and a path within that package.
	// If this is nil, [DefaultMapURL] will be used.
	//
	// Deprecated: use [Config.MapRef].
	MapURL func(u *url.URL) (importPath string, path cue.Path, err error)

	// NOTE: this method is currently experimental. Its usage and type
	// signature may change.
	//
	// MapRef is used to determine how a JSON schema location maps to
	// CUE. It is used for both explicit references and for named
	// schemas inside $defs and definitions.
	//
	// For example, given this schema:
	//
	// 	{
	// 	    "$schema": "https://json-schema.org/draft/2020-12/schema",
	// 	    "$id": "https://my.schema.org/hello",
	// 	    "$defs": {
	// 	        "foo": {
	// 	            "$id": "https://other.org",
	// 	            "type": "object",
	// 	            "properties": {
	// 	                "a": {
	// 	                    "type": "string"
	// 	                },
	// 	                "b": {
	// 	                    "$ref": "#/properties/a"
	// 	                }
	// 	            }
	// 	        }
	// 	    },
	// 	    "allOf": [{
	// 	        "$ref": "#/$defs/foo"
	// 	    }, {
	// 	        "$ref": "https://my.schema.org/hello#/$defs/foo"
	// 	    }, {
	// 	        "$ref": "https://other.org"
	// 	    }, {
	// 	        "$ref": "https://external.ref"
	//	    }]
	// 	}
	//
	// ... MapRef will be called with the following locations for the
	// $ref keywords in order of appearance (no guarantees are made
	// about the actual order or number of calls to MapRef):
	//
	//	ID                                      RootRel
	//	https://other.org/properties/a          https://my.schema.org/hello#/$defs/foo/properties/a
	//	https://my.schema.org/hello#/$defs/foo  https://my.schema.org/hello#/$defs/foo
	//	https://other.org                       https://my.schema.org/hello#/$defs/foo
	//	https://external.ref                    <nil>
	//
	// It will also be called for the named schema in #/$defs/foo with these arguments:
	//
	//	https://other.org                       https://my.schema.org/hello#/$defs/foo
	//
	// MapRef should return the desired CUE location for the schema with
	// the provided IDs, consisting of the import path of the package
	// containing the schema, and a path within that package. If the
	// returned import path is empty, the path will be interpreted
	// relative to the root of the generated JSON schema.
	//
	// Note that MapRef is general enough to subsume use of [Config.Map] and
	// [Config.MapURL], which are both now deprecated. If all three fields are
	// nil, [DefaultMapRef] will be used.
	MapRef func(loc SchemaLoc) (importPath string, relPath cue.Path, err error)

	// NOTE: this method is currently experimental. Its usage and type
	// signature may change.
	//
	// DefineSchema is called, if not nil, for any schema that is defined
	// within the json schema being converted but is mapped somewhere
	// external via [Config.MapRef]. The invoker of [Extract] is
	// responsible for defining the schema e in the correct place as described
	// by the import path and its relative CUE path.
	//
	// The importPath and path are exactly as returned by [Config.MapRef].
	// If this or [Config.MapRef] is nil this function will never be called.
	// Note that importPath will never be empty, because if MapRef
	// returns an empty importPath, it's specifying an internal schema
	// which will be defined accordingly.
	DefineSchema func(importPath string, path cue.Path, e ast.Expr)

	// TODO: configurability to make it compatible with OpenAPI, such as
	// - locations of definitions: #/components/schemas, for instance.
	// - selection and definition of formats
	// - documentation hooks.

	// Strict reports an error for unsupported features and keywords,
	// rather than ignoring them. When true, this is equivalent to
	// setting both StrictFeatures and StrictKeywords to true.
	Strict bool

	// StrictFeatures reports an error for features that are known
	// to be unsupported.
	StrictFeatures bool

	// StrictKeywords reports an error when unknown keywords
	// are encountered.
	StrictKeywords bool

	// DefaultVersion holds the default schema version to use
	// when no $schema field is present. If it is zero, [DefaultVersion]
	// will be used.
	DefaultVersion Version

	_ struct{} // prohibit casting from different type.
}

// SchemaLoc defines the location of schema, both in absolute
// terms as its canonical ID and, optionally, relative to the
// root of the value passed to [Extract].
type SchemaLoc struct {
	// ID holds the canonical URI of the schema, as declared
	// by the schema or one of its parents.
	ID *url.URL

	// IsLocal holds whether the schema has been defined locally.
	// If true, then [SchemaLoc.Path] holds the path from the root
	// value, as passed to [Extract], to the schema definition.
	IsLocal bool
	Path    cue.Path
}

func (loc SchemaLoc) String() string {
	if loc.IsLocal {
		return fmt.Sprintf("id=%v localPath=%v", loc.ID, loc.Path)
	}
	return fmt.Sprintf("id=%v", loc.ID)
}

func ref[T any](x T) *T {
	return &x
}
