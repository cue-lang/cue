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

package encodingregistry_test

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/go-quicktest/qt"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/build"
	"cuelang.org/go/cue/cuecontext"
	"cuelang.org/go/internal/encoding"
	"cuelang.org/go/internal/filetypes"
	"cuelang.org/go/unstable/encodingregistry"
)

// TestEndToEnd registers a previously unknown encoding with a distinct
// extension and checks that file arguments using the extension and the
// explicit qualifier resolve to the registered properties, and that
// decoding and encoding work through internal/encoding, the same path
// CUE commands use.
func TestEndToEnd(t *testing.T) {
	reset(t)

	err := encodingregistry.Register(encodingregistry.Encoding{
		Name:       "kv",
		Extensions: []string{".kv"},
		Info:       encodingregistry.FileInfo{Form: "data"},
		NewDecoder: newKVDecoder,
		NewEncoder: newKVEncoder,
	})
	qt.Assert(t, qt.IsNil(err))

	// Resolution by extension. Like built-in data encodings (yaml,
	// jsonl, ...), the build.File carries the encoding; the declared
	// form surfaces through FromFile below, with the same resolution
	// semantics as built-ins.
	f, err := filetypes.ParseFile("creds.kv", filetypes.Input)
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.Equals(f.Encoding, build.Encoding("kv")))
	qt.Assert(t, qt.Equals(f.Form, build.Form("")))
	qt.Assert(t, qt.Equals(f.Filename, "creds.kv"))

	// Resolution by explicit qualifier on a foreign extension.
	f2, err := filetypes.ParseFile("kv:creds.conf", filetypes.Input)
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.Equals(f2.Encoding, build.Encoding("kv")))
	qt.Assert(t, qt.Equals(f2.Form, build.Form("")))

	// Resolution through ParseArgs, the cmd/cue argument path.
	files, err := filetypes.ParseArgs([]string{"kv:", "creds.conf", "more.kv"})
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.HasLen(files, 2))
	qt.Assert(t, qt.Equals(files[0].Encoding, build.Encoding("kv")))
	qt.Assert(t, qt.Equals(files[1].Encoding, build.Encoding("kv")))

	// FromFile reports the registered properties.
	fi, err := filetypes.FromFile(f, filetypes.Input)
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.Equals(fi.Encoding, build.Encoding("kv")))
	qt.Assert(t, qt.Equals(fi.Form, build.Data))
	qt.Assert(t, qt.IsTrue(fi.Data))
	qt.Assert(t, qt.IsFalse(fi.Incomplete))

	// Decode through internal/encoding, the path CUE commands use for
	// data files.
	ctx := cuecontext.New()
	f.Source = []byte("user=alice\nhost=example.com\n")
	dec := encoding.NewDecoder(ctx, f, nil)
	defer dec.Close()
	qt.Assert(t, qt.IsNil(dec.Err()))
	qt.Assert(t, qt.IsFalse(dec.Done()))
	v := ctx.BuildFile(dec.File())
	qt.Assert(t, qt.IsNil(v.Err()))
	user, err := v.LookupPath(cue.ParsePath("user")).String()
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.Equals(user, "alice"))
	dec.Next()
	qt.Assert(t, qt.IsTrue(dec.Done()))

	// Encode back through internal/encoding.
	var buf strings.Builder
	enc, err := encoding.NewEncoder(ctx, &build.File{
		Filename: "out.kv",
		Encoding: "kv",
		Form:     build.Data,
	}, &encoding.Config{Out: &buf})
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.IsNil(enc.Encode(v)))
	qt.Assert(t, qt.IsNil(enc.Close()))
	// CUE preserves field order, so the round trip reproduces the
	// original input byte for byte.
	qt.Assert(t, qt.Equals(buf.String(), "user=alice\nhost=example.com\n"))
}

// TestDecodeOnly checks the decode/encode asymmetry: NewEncoder == nil
// registers a decode-only encoding whose use as an output target
// produces the same error text an unencodable built-in produces.
func TestDecodeOnly(t *testing.T) {
	reset(t)

	err := encodingregistry.Register(encodingregistry.Encoding{
		Name:       "kvro",
		Extensions: []string{".kvro"},
		Info:       encodingregistry.FileInfo{Form: "data"},
		NewDecoder: newKVDecoder,
	})
	qt.Assert(t, qt.IsNil(err))

	ctx := cuecontext.New()
	var buf strings.Builder
	_, err = encoding.NewEncoder(ctx, &build.File{
		Filename: "out.kvro",
		Encoding: "kvro",
		Form:     build.Data,
	}, &encoding.Config{Out: &buf})
	qt.Assert(t, qt.ErrorMatches(err, `unsupported encoding "kvro"`))
}

// TestRegisterValidation checks registration-time validation of the
// declarative record.
func TestRegisterValidation(t *testing.T) {
	reset(t)

	dec := newKVDecoder

	// A decoder is required.
	err := encodingregistry.Register(encodingregistry.Encoding{
		Name: "kvx",
	})
	qt.Assert(t, qt.ErrorMatches(err, `cannot register encoding "kvx": a decoder is required \(NewDecoder or NewValueDecoder\)`))

	// An empty name is rejected.
	err = encodingregistry.Register(encodingregistry.Encoding{
		NewDecoder: dec,
	})
	qt.Assert(t, qt.ErrorMatches(err, `cannot register encoding: name must be non-empty`))

	// Template validation: an unknown form does not conform to the
	// file-type template.
	err = encodingregistry.Register(encodingregistry.Encoding{
		Name:       "kvx",
		NewDecoder: dec,
		Info:       encodingregistry.FileInfo{Form: "nosuchform"},
	})
	qt.Assert(t, qt.IsNotNil(err))
	qt.Assert(t, qt.StringContains(err.Error(), `cannot register encoding "kvx"`))

	// Aspect is a defined string type so future callers can still construct
	// unknown values. Template validation remains the fail-closed boundary.
	err = encodingregistry.Register(encodingregistry.Encoding{
		Name:       "kvx",
		NewDecoder: dec,
		Info: encodingregistry.FileInfo{Aspects: map[encodingregistry.Aspect]bool{
			encodingregistry.Aspect("nosuchaspect"): true,
		}},
	})
	qt.Assert(t, qt.IsNotNil(err))
	qt.Assert(t, qt.StringContains(err.Error(), `cannot register encoding "kvx"`))

	// Extensions must include the leading dot.
	err = encodingregistry.Register(encodingregistry.Encoding{
		Name:       "kvx",
		NewDecoder: dec,
		Extensions: []string{"kvx"},
	})
	qt.Assert(t, qt.ErrorMatches(err, `cannot register encoding "kvx": extension "kvx" must start with a dot and name a suffix`))
}

// TestConflicts checks the add-only conflict semantics of
// registration: collisions with a built-in name, a built-in extension, a
// prior registration's name, and a prior registration's extension are
// each refused with deterministic ConflictError text naming the
// conflicting owner.
func TestConflicts(t *testing.T) {
	reset(t)

	dec := newKVDecoder

	// Collision with a built-in name.
	err := encodingregistry.Register(encodingregistry.Encoding{
		Name:       "json",
		NewDecoder: dec,
	})
	qt.Assert(t, qt.ErrorMatches(err, `cannot register encoding "json": name already registered by built-in encoding "json"`))
	var conflict *encodingregistry.ConflictError
	qt.Assert(t, qt.IsTrue(errors.As(err, &conflict)))
	qt.Assert(t, qt.Equals(*conflict, encodingregistry.ConflictError{
		Encoding: "json",
		Kind:     "name",
		Key:      "json",
		Owner:    "json",
		BuiltIn:  true,
	}))

	// Collision with a built-in top-level tag that is not an encoding
	// (e.g. "schema") is also refused: the name space is the qualifier
	// space. The message identifies the owner as a built-in tag; there
	// is no built-in encoding "schema" to blame.
	err = encodingregistry.Register(encodingregistry.Encoding{
		Name:       "schema",
		NewDecoder: dec,
	})
	qt.Assert(t, qt.ErrorMatches(err, `cannot register encoding "schema": name already registered as a built-in tag`))
	qt.Assert(t, qt.IsTrue(errors.As(err, &conflict)))
	qt.Assert(t, qt.IsTrue(conflict.BuiltIn))
	qt.Assert(t, qt.Equals(conflict.Owner, ""))

	// Collision with a built-in subsidiary tag (e.g. "strict", a
	// boolean tag of the jsonschema and openapi interpretations) is
	// refused the same way, again without inventing an owning encoding.
	err = encodingregistry.Register(encodingregistry.Encoding{
		Name:       "strict",
		NewDecoder: dec,
	})
	qt.Assert(t, qt.ErrorMatches(err, `cannot register encoding "strict": name already registered as a built-in tag`))
	qt.Assert(t, qt.IsTrue(errors.As(err, &conflict)))
	qt.Assert(t, qt.IsTrue(conflict.BuiltIn))
	qt.Assert(t, qt.Equals(conflict.Owner, ""))

	// Collision with a built-in extension.
	err = encodingregistry.Register(encodingregistry.Encoding{
		Name:       "kvb",
		Extensions: []string{".yaml"},
		NewDecoder: dec,
	})
	qt.Assert(t, qt.ErrorMatches(err, `cannot register encoding "kvb": extension "\.yaml" already registered by built-in encoding "yaml"`))
	qt.Assert(t, qt.IsTrue(errors.As(err, &conflict)))
	qt.Assert(t, qt.Equals(*conflict, encodingregistry.ConflictError{
		Encoding: "kvb",
		Kind:     "extension",
		Key:      ".yaml",
		Owner:    "yaml",
		BuiltIn:  true,
	}))

	// A successful registration to conflict with.
	err = encodingregistry.Register(encodingregistry.Encoding{
		Name:       "kvc",
		Extensions: []string{".kvc"},
		NewDecoder: dec,
	})
	qt.Assert(t, qt.IsNil(err))

	// Collision with a prior registration's name.
	err = encodingregistry.Register(encodingregistry.Encoding{
		Name:       "kvc",
		NewDecoder: dec,
	})
	qt.Assert(t, qt.ErrorMatches(err, `cannot register encoding "kvc": name already registered by encoding "kvc"`))
	qt.Assert(t, qt.IsTrue(errors.As(err, &conflict)))
	qt.Assert(t, qt.IsFalse(conflict.BuiltIn))

	// Collision with a prior registration's extension.
	err = encodingregistry.Register(encodingregistry.Encoding{
		Name:       "kvd",
		Extensions: []string{".kvc"},
		NewDecoder: dec,
	})
	qt.Assert(t, qt.ErrorMatches(err, `cannot register encoding "kvd": extension "\.kvc" already registered by encoding "kvc"`))

	// Collision with a prior registration's subsidiary tag: the
	// reported owner is the encoding that declared the tag, not the
	// colliding tag name itself.
	err = encodingregistry.Register(encodingregistry.Encoding{
		Name:       "kvowner",
		NewDecoder: dec,
		BoolTags:   map[string]encodingregistry.BoolTagDecl{"kvflag": {}},
	})
	qt.Assert(t, qt.IsNil(err))
	err = encodingregistry.Register(encodingregistry.Encoding{
		Name:       "kvflag",
		NewDecoder: dec,
	})
	qt.Assert(t, qt.ErrorMatches(err, `cannot register encoding "kvflag": name already registered as a tag of encoding "kvowner"`))
	qt.Assert(t, qt.IsTrue(errors.As(err, &conflict)))
	qt.Assert(t, qt.Equals(conflict.Owner, "kvowner"))
	qt.Assert(t, qt.IsFalse(conflict.BuiltIn))

	// A failed registration must not have left partial state behind:
	// kvd's name is still free.
	err = encodingregistry.Register(encodingregistry.Encoding{
		Name:       "kvd",
		Extensions: []string{".kvd"},
		NewDecoder: dec,
	})
	qt.Assert(t, qt.IsNil(err))
}

// TestLateRegistration checks the registration-after-use semantics:
// the first resolution freezes the registry and later Register calls
// fail with LateRegistrationError.
func TestLateRegistration(t *testing.T) {
	reset(t)

	// First use freezes the registry.
	_, err := filetypes.ParseFile("x.json", filetypes.Input)
	qt.Assert(t, qt.IsNil(err))

	err = encodingregistry.Register(encodingregistry.Encoding{
		Name:       "kvlate",
		Extensions: []string{".kvlate"},
		NewDecoder: newKVDecoder,
	})
	qt.Assert(t, qt.ErrorMatches(err, `cannot register encoding "kvlate": file types already in use.*`))
	var late *encodingregistry.LateRegistrationError
	qt.Assert(t, qt.IsTrue(errors.As(err, &late)))
	qt.Assert(t, qt.Equals(late.Encoding, "kvlate"))
}

// TestConcurrency exercises parallel registration and
// resolution under the race detector: concurrent Register calls are
// serialized, resolution is race-free against them, and every
// registration either fully succeeds or fails with a deterministic
// error.
func TestConcurrency(t *testing.T) {
	reset(t)

	const n = 32
	var wg sync.WaitGroup
	errs := make([]error, n)
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs[i] = encodingregistry.Register(encodingregistry.Encoding{
				Name:       fmt.Sprintf("kvp%d", i),
				Extensions: []string{fmt.Sprintf(".kvp%d", i)},
				Info:       encodingregistry.FileInfo{Form: "data"},
				NewDecoder: newKVDecoder,
			})
		}()
	}
	wg.Wait()
	for i, err := range errs {
		qt.Assert(t, qt.IsNil(err), qt.Commentf("registration %d", i))
	}

	// Parallel resolution over the frozen registry, mixing built-in
	// and registered encodings, racing with late Register attempts.
	var wg2 sync.WaitGroup
	for i := range n {
		wg2.Add(1)
		go func() {
			defer wg2.Done()
			f, err := filetypes.ParseFile(fmt.Sprintf("x.kvp%d", i), filetypes.Input)
			if err == nil && f.Encoding != build.Encoding(fmt.Sprintf("kvp%d", i)) {
				err = fmt.Errorf("resolved to %q", f.Encoding)
			}
			errs[i] = err
		}()
		wg2.Add(1)
		go func() {
			defer wg2.Done()
			// Racing late registration: must either succeed (if it
			// wins the freeze race) or fail with LateRegistrationError;
			// never a torn state.
			err := encodingregistry.Register(encodingregistry.Encoding{
				Name:       fmt.Sprintf("kvq%d", i),
				Extensions: []string{fmt.Sprintf(".kvq%d", i)},
				NewDecoder: newKVDecoder,
			})
			var late *encodingregistry.LateRegistrationError
			if err != nil && !errors.As(err, &late) {
				t.Errorf("unexpected racing registration error: %v", err)
			}
		}()
	}
	wg2.Wait()
	for i, err := range errs {
		qt.Assert(t, qt.IsNil(err), qt.Commentf("resolution %d", i))
	}
}

// TestBuiltinEncodingNameConflict pins that a registration cannot claim
// the name of a built-in encoding that is not also a top-level tag.
// "binarypb" is the one such name, so a name check consulting only the
// qualifier/tag space would miss it and half-absorb the registration:
// its declared file-info would be dropped while its codec would go live.
func TestBuiltinEncodingNameConflict(t *testing.T) {
	reset(t)

	err := encodingregistry.Register(encodingregistry.Encoding{
		Name:       "binarypb",
		Extensions: []string{".bpb"},
		NewDecoder: newKVDecoder,
	})
	qt.Assert(t, qt.ErrorMatches(err, `cannot register encoding "binarypb": name already registered by built-in encoding "binarypb"`))
	var conflict *encodingregistry.ConflictError
	qt.Assert(t, qt.IsTrue(errors.As(err, &conflict)))
	qt.Assert(t, qt.Equals(conflict.BuiltIn, true))
}

// TestRegisterUnusableDeclarations pins rejection of declarations that
// are structurally valid but cannot actually be used.
func TestRegisterUnusableDeclarations(t *testing.T) {
	dec := newKVDecoder

	t.Run("name-with-plus", func(t *testing.T) {
		reset(t)
		err := encodingregistry.Register(encodingregistry.Encoding{Name: "a+b", NewDecoder: dec})
		qt.Assert(t, qt.ErrorMatches(err, `cannot register encoding "a\+b": name must not contain .*`))
	})
	t.Run("name-with-equals", func(t *testing.T) {
		reset(t)
		err := encodingregistry.Register(encodingregistry.Encoding{Name: "a=b", NewDecoder: dec})
		qt.Assert(t, qt.ErrorMatches(err, `cannot register encoding "a=b": name must not contain .*`))
	})
	t.Run("self-referential-tag", func(t *testing.T) {
		reset(t)
		err := encodingregistry.Register(encodingregistry.Encoding{
			Name:       "foo",
			NewDecoder: dec,
			Tags:       map[string]encodingregistry.TagDecl{"foo": {}},
		})
		qt.Assert(t, qt.ErrorMatches(err, `cannot register encoding "foo": tag "foo" .*`))
	})
	t.Run("tag-string-and-bool", func(t *testing.T) {
		reset(t)
		err := encodingregistry.Register(encodingregistry.Encoding{
			Name:       "foo",
			NewDecoder: dec,
			Tags:       map[string]encodingregistry.TagDecl{"x": {}},
			BoolTags:   map[string]encodingregistry.BoolTagDecl{"x": {}},
		})
		qt.Assert(t, qt.ErrorMatches(err, `cannot register encoding "foo": tag "x" declared as both .*`))
	})
	t.Run("extension-dot-only", func(t *testing.T) {
		reset(t)
		err := encodingregistry.Register(encodingregistry.Encoding{
			Name:       "foo",
			Extensions: []string{"."},
			NewDecoder: dec,
		})
		qt.Assert(t, qt.ErrorMatches(err, `cannot register encoding "foo": extension "\." .*`))
	})
	t.Run("extension-multiple-dots", func(t *testing.T) {
		reset(t)
		err := encodingregistry.Register(encodingregistry.Encoding{
			Name:       "foo",
			Extensions: []string{".tar.gz"},
			NewDecoder: dec,
		})
		qt.Assert(t, qt.ErrorMatches(err, `cannot register encoding "foo": extension "\.tar\.gz" must name one suffix .*`))
	})
	t.Run("extension-path-fragment", func(t *testing.T) {
		reset(t)
		err := encodingregistry.Register(encodingregistry.Encoding{
			Name:       "foo",
			Extensions: []string{".foo/bar"},
			NewDecoder: dec,
		})
		qt.Assert(t, qt.ErrorMatches(err, `cannot register encoding "foo": extension "\.foo/bar" must name one suffix .*`))
	})
	for _, name := range []string{"", "a+b", "a=b", "a:b"} {
		t.Run("invalid-string-tag-"+name, func(t *testing.T) {
			reset(t)
			err := encodingregistry.Register(encodingregistry.Encoding{
				Name:       "foo",
				NewDecoder: dec,
				Tags:       map[string]encodingregistry.TagDecl{name: {}},
			})
			qt.Assert(t, qt.IsNotNil(err))
			qt.Assert(t, qt.StringContains(err.Error(), "tag"))
		})
	}
	for _, name := range []string{"", "a+b", "a=b", "a:b"} {
		t.Run("invalid-bool-tag-"+name, func(t *testing.T) {
			reset(t)
			err := encodingregistry.Register(encodingregistry.Encoding{
				Name:       "foo",
				NewDecoder: dec,
				BoolTags:   map[string]encodingregistry.BoolTagDecl{name: {}},
			})
			qt.Assert(t, qt.IsNotNil(err))
			qt.Assert(t, qt.StringContains(err.Error(), "tag"))
		})
	}
}

// TestRegistrationRejectsIncompatibleComposition checks that all three
// public validation paths reject declarations whose individual names and
// values are valid but whose interpretation, form, and aspects cannot be
// unified during resolution.
func TestRegistrationRejectsIncompatibleComposition(t *testing.T) {
	dec := newKVDecoder
	streamFalse := false
	tests := []struct {
		name    string
		info    encodingregistry.FileInfo
		perMode map[encodingregistry.Mode]encodingregistry.FileInfo
	}{
		{
			name: "interpretation-form",
			info: encodingregistry.FileInfo{Interpretation: "jsonschema", Form: "data"},
		},
		{
			name: "interpretation-aspect",
			info: encodingregistry.FileInfo{
				Interpretation: "pb",
				Form:           "data",
				Aspects:        map[encodingregistry.Aspect]bool{encodingregistry.AspectStream: streamFalse},
			},
		},
		{
			name: "per-mode-composition",
			info: encodingregistry.FileInfo{Interpretation: "jsonschema"},
			perMode: map[encodingregistry.Mode]encodingregistry.FileInfo{
				encodingregistry.Export: {Form: "data"},
			},
		},
	}
	entrypoints := []struct {
		name string
		call func(encodingregistry.Encoding) error
	}{
		{"Register", encodingregistry.Register},
		{"RegisterWithoutFullValidation", encodingregistry.RegisterWithoutFullValidation},
		{"Validate", encodingregistry.Validate},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			for _, entrypoint := range entrypoints {
				t.Run(entrypoint.name, func(t *testing.T) {
					reset(t)
					err := entrypoint.call(encodingregistry.Encoding{
						Name:       "badcomposition",
						Extensions: []string{".badcomposition"},
						Info:       test.info,
						PerMode:    test.perMode,
						NewDecoder: dec,
					})
					qt.Assert(t, qt.IsNotNil(err))
					qt.Assert(t, qt.StringContains(err.Error(), "interpretation"))
					qt.Assert(t, qt.StringContains(err.Error(), "conflicts"))
				})
			}
		})
	}

	// A compatible interpretation/form pair remains valid.
	qt.Assert(t, qt.IsNil(encodingregistry.Validate(encodingregistry.Encoding{
		Name:       "goodcomposition",
		Info:       encodingregistry.FileInfo{Interpretation: "jsonschema", Form: "schema"},
		NewDecoder: dec,
	})))
}

// TestRegisterWithoutFullValidation checks that a prevalidated (trusted)
// registration resolves identically to the same encoding registered the
// normal way, in all four modes.
func TestRegisterWithoutFullValidation(t *testing.T) {
	reset(t)
	e := encodingregistry.Encoding{
		Name:       "pc",
		Extensions: []string{".pc"},
		Info:       encodingregistry.FileInfo{Form: "data"},
		NewDecoder: newKVDecoder,
	}
	err := encodingregistry.RegisterWithoutFullValidation(e)
	qt.Assert(t, qt.IsNil(err))

	for _, mode := range []filetypes.Mode{filetypes.Input, filetypes.Export, filetypes.Def, filetypes.Eval} {
		f, err := filetypes.ParseFile("x.pc", mode)
		qt.Assert(t, qt.IsNil(err))
		qt.Assert(t, qt.Equals(f.Encoding, build.Encoding("pc")))
		fi, err := filetypes.FromFile(f, mode)
		qt.Assert(t, qt.IsNil(err))
		qt.Assert(t, qt.Equals(fi.Form, build.Data))
	}
}

// TestValidateDeclaration checks that Validate accepts a well-formed
// declaration and rejects a malformed one, so a consumer's build-time
// test can guard a prevalidated registration.
func TestValidateDeclaration(t *testing.T) {
	ok := encodingregistry.Encoding{Name: "vd", Info: encodingregistry.FileInfo{Form: "data"}, NewDecoder: newKVDecoder}
	qt.Assert(t, qt.IsNil(encodingregistry.Validate(ok)))

	bad := encodingregistry.Encoding{Name: "vd", Info: encodingregistry.FileInfo{Form: "nope"}, NewDecoder: newKVDecoder}
	qt.Assert(t, qt.IsNotNil(encodingregistry.Validate(bad)))
}

// TestValidateReportsBuiltinConflicts checks that Validate reports
// collisions with built-in names and extensions as ConflictError. The
// built-in file types are compile-time constants, so a build-time
// Validate can catch such conflicts before shipping; conflicts with
// other runtime registrations remain visible only to Register, keeping
// Validate side-effect-free and independent of registry state.
func TestValidateReportsBuiltinConflicts(t *testing.T) {
	reset(t)
	dec := newKVDecoder

	// A built-in encoding name is a conflict at validation time.
	var conflict *encodingregistry.ConflictError
	err := encodingregistry.Validate(encodingregistry.Encoding{
		Name:       "json",
		NewDecoder: dec,
	})
	qt.Assert(t, qt.IsTrue(errors.As(err, &conflict)))
	qt.Assert(t, qt.Equals(conflict.Kind, "name"))
	qt.Assert(t, qt.IsTrue(conflict.BuiltIn))

	// So is a built-in extension.
	err = encodingregistry.Validate(encodingregistry.Encoding{
		Name:       "kvfresh",
		Extensions: []string{".json"},
		NewDecoder: dec,
	})
	qt.Assert(t, qt.IsTrue(errors.As(err, &conflict)))
	qt.Assert(t, qt.Equals(conflict.Kind, "extension"))
	qt.Assert(t, qt.Equals(conflict.Owner, "json"))
	qt.Assert(t, qt.IsTrue(conflict.BuiltIn))

	// A Validate-accepted declaration with a fresh name still
	// registers cleanly.
	ok := encodingregistry.Encoding{
		Name:       "kvfresh",
		Extensions: []string{".kvfresh"},
		Info:       encodingregistry.FileInfo{Form: "data"},
		NewDecoder: dec,
	}
	qt.Assert(t, qt.IsNil(encodingregistry.Validate(ok)))
	qt.Assert(t, qt.IsNil(encodingregistry.Register(ok)))

	// Validate does not consult runtime registrations: the same
	// declaration still validates after registration, while Register
	// now reports the conflict.
	qt.Assert(t, qt.IsNil(encodingregistry.Validate(ok)))
	err = encodingregistry.Register(ok)
	qt.Assert(t, qt.IsTrue(errors.As(err, &conflict)))
	qt.Assert(t, qt.IsFalse(conflict.BuiltIn))
}
