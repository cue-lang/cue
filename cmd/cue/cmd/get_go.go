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

package cmd

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"maps"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/spf13/cobra"
	"golang.org/x/tools/go/packages"

	cueast "cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/ast/astutil"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/format"
	"cuelang.org/go/cue/literal"
	"cuelang.org/go/cue/load"
	"cuelang.org/go/cue/parser"
	cuetoken "cuelang.org/go/cue/token"
	"cuelang.org/go/internal"
)

// TODO:
// Document:
// - Use ast package.
// - how to deal with "oneOf" or sum types?
// - generate cue files for cue field tags?
// - cue go get or cue get go
// - include generation report in doc_gen.cue or report.txt.
//   Possible enums:
//   package foo
//   Type: enumType

func newGoCmd(c *Command) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "go [flags] [packages]",
		Short: "add Go dependencies to the current module",
		Long: `go converts Go types into CUE definitions

The command "cue get go" is like "go get", but converts the retrieved Go
packages to CUE. The retrieved packages are put in the CUE module's pkg
directory at the import path of the corresponding Go package. The converted
definitions are available to any CUE file within the CUE module by using
this import path.

The Go type definitions are converted to CUE based on how they would be
interpreted by Go's encoding/json package. Definitions for a Go file foo.go
are written to a CUE file named foo_go_gen.cue.

It is safe for users to add additional files to the generated directories,
as long as their name does not end with _gen.*.


Rules of Converting Go types to CUE

Go structs are converted to cue structs adhering to the following conventions:

	- each struct follows a single codec: the first one in the priority list
	  given by the --codec flag, "json,yaml" by default, whose tag appears
	  on any of its fields, or else the first codec. Field names are
	  translated based on the tags of that codec.

	- the "jsonv2" codec reads "json" tags like the "json" codec, but follows
	  encoding/json/v2 rather than the v1 API of encoding/json. For example,
	  an "omitempty" option does not make a boolean or number optional, and
	  types which encoding/json/v2 rejects are dropped, such as time.Duration
	  or a struct whose json tags have invalid options.

	- the codec also decides how the fields are encoded: a field is optional
	  if its tag has an "omitempty" or "omitzero" option, a "json" tag with
	  a "string" option encodes a boolean, number, or string as a string,
	  and a pointer field is not nullable under "toml", as TOML has no null.

	- the fields of an embedded struct, or pointer to struct, are promoted
	  like encoding/json does, unless the field's tag gives it a name.
	  Under the "yaml" codec, they are only promoted with an "inline" option,
	  following libraries like gopkg.in/yaml.v3. For instance, the Go struct

	    type MyStruct struct {
			Common
			Field string
		}

	  translates to the CUE struct

		#MyStruct: {
			#Common
			Field: string
		}

	  When some of the promoted fields are hidden by other fields
	  with the same name, the remaining ones are added individually.
	  An "embed" option also promotes the fields of a named field,
	  or holds any other object members in a map or jsontext.Value.

	- a type that implements MarshalJSON, UnmarshalJSON, MarshalJSONTo,
	  UnmarshalJSONFrom, MarshalYAML, or UnmarshalYAML is translated to
	  top (_) to indicate it may be any value. For some Go core types for
	  which the implementation of these methods is known, like time.Time,
	  the type may be more specific. These methods can be omitted with the
	  --omit flag, as described below.

	- a type implementing MarshalText or UnmarshalText is represented as
	  the CUE type string

	- slices and arrays convert to CUE lists, except when the element type is
	  byte, in which case it translates to the CUE bytes type.
	  In the case of arrays, the length of the CUE value is constrained
	  accordingly, when possible.

	- Maps translate to a CUE struct, where all elements are constrained to
	  be of Go map element type. Like for JSON, map keys must be strings,
	  integers, floats, or types implementing MarshalText or UnmarshalText,
	  and are all translated to string labels.

	- Pointers translate to a sum type with the default value of null and
	  the Go type as an alternative value.

	- Field tags are translated to CUE's field attributes. In some cases,
	  the contents are rewritten to reflect the corresponding types in CUE.
	  The @go attribute is added if the field name or type definition differs
	  between the generated CUE and the original Go.


Omitting Declarations and Methods

The --omit flag leaves out the type and constant declarations matched by
any of its selectors written as pkg.Name. The package is given either by
its name, such as "v1", or by its import path, such as
"k8s.io/api/core/v1". Each element is a glob, such as "*" to match any
package, as understood by Go's path.Match. For example:

	--omit='*.Internal*'                 # declarations in any package
	--omit=v1.PodSpec                    # by package name
	--omit=k8s.io/api/core/v1.PodSpec    # by import path

References to omitted types are translated to top, and the fields of an
omitted embedded struct are added individually.

A selector written as pkg.Name.Method instead translates the matched types
as if they lacked the matched methods among those which make a type
translate to top or string, such as MarshalJSON or UnmarshalText.
This is useful when a type only implements them to validate its input,
or to encode it the same way. For example:

	--omit='v1.PodSpec.*'                # all encoding methods
	--omit=v1.PodSpec.UnmarshalJSON      # just one of them
	--omit='*.*.*YAML'                   # YAML methods of all types

Note how --omit=v1.PodSpec omits the type, whereas --omit='v1.PodSpec.*'
keeps the type and omits its encoding methods.
A selector which matches nothing is an error.


Native CUE Constraints

Native CUE constraints may be defined in separate cue files alongside the
generated files either in the original Go directory or in the generated
directory. These files can impose additional constraints on types and values
that are not otherwise expressible in Go. The package name for these CUE files
must be the same as that of the Go package.

For instance, for the type

	package foo

	type IP4String string

defined in the Go package, one could add a cue file foo.cue with the following
contents to allow IP4String to assume only valid IP4 addresses:

	package foo

	// IP4String defines a valid IP4 address.
	#IP4String: =~#"^\#(byte)\.\#(byte)\.\#(byte)\.\#(byte)$"#

	// byte defines string allowing integer values of 0-255.
	byte = #"([01]?\d?\d|2[0-4]\d|25[0-5])"#


The "cue get go" command copies any cue files in the original Go package
directory that has a package clause with the same name as the Go package to the
destination directory, replacing its .cue ending with _gen.cue.

Alternatively, the additional native constraints can be added to the generated
package, as long as the file name does not end with _gen.cue.
Running cue get go again to regenerate the package will never overwrite any
files not ending with _gen.*.


Constants and Enums

Go does not have an enum or sum type. Conventionally, a type that is supposed
to be an enum is followed by a const block with the allowed values for that
type. However, as that is only a guideline and not a hard rule, these cases
cannot be translated to CUE disjunctions automatically.

Constant values, however, are generated in a way that makes it easy to convert
a type to a proper enum using native CUE constraints. For instance, the Go type

	package foo

	type Switch int

	const (
		Off Switch = iota
		On
	)

translates into the following CUE definitions:

	package foo

	#Switch: int // #enumSwitch

	#enumSwitch: Off | On

	Off: 0
	On:  1

This definition allows any integer value for #Switch, while the #enumSwitch
value defines all defined constants for Switch and thus all valid values if
#Switch were to be interpreted as an enum type. To turn #Switch into an enum,
include the following constraint in, say, enum.cue, in either the original
source directory or the generated directory:

	package foo

	// limit the valid values for Switch to those existing as constants with
	// the same type.
	#Switch: #enumSwitch

This tells CUE that only the values enumerated by #enumSwitch are valid values
for #Switch. Note that there are now two definitions of #Switch. CUE handles
this in the usual way by unifying the two definitions, in which case the more
restrictive enum interpretation of #Switch remains.


Alternatives

Go types cannot express enums, sum types, defaults, or most constraints,
so converting them to CUE is lossy. Use this command only when the schemas
you depend on are solely defined as Go types.

When the schemas are also defined in a format such as JSON Schema or OpenAPI,
"cue import" gives much more precise results. Schemas for many well-known
projects, such as Kubernetes or GitHub Actions, are already imported this way
and published as curated modules in the Central Registry. See:

	https://cue.dev/getting-started/schema-library/

When writing or maintaining the schemas yourself, write them in CUE and
generate Go types from them with "cue exp gengotypes".
`,
		// - TODO: interpret cuego's struct tags and annotations.

		RunE: mkRunE(c, extract),
	}

	cmd.Flags().BoolP(string(flagVerbose), "v", false,
		"print information about progress")

	cmd.Flags().StringArray(string(flagOmit), nil,
		"comma-separated selectors of declarations or methods to omit, such as pkg.Name or pkg.Name.Method")

	cmd.Flags().StringP(string(flagExclude), "e", "",
		"comma-separated list of regexps of identifiers to omit")
	cmd.Flags().MarkDeprecated(string(flagExclude), "use --omit instead")

	cmd.Flags().Bool(string(flagLocal), false,
		"generates files in the main module locally")

	cmd.Flags().StringP(string(flagPackage), "p", "", "package name for generated CUE files")

	cmd.Flags().String(string(flagOutFile), "", "generate one CUE file for a single Go package")

	cmd.Flags().String(string(flagCodec), defaultCodec,
		"comma-separated priority list of codecs, such as json, jsonv2, yaml, or toml")

	return cmd
}

const (
	flagExclude flagName = "exclude"
	flagOmit    flagName = "omit"
	flagLocal   flagName = "local"
	flagCodec   flagName = "codec"

	defaultCodec = "json,yaml"
)

// initExclusions records the declarations whose names match any of
// the comma-separated regular expressions in str as omitted.
func (e *extractor) initExclusions(str string) error {
	var exclusions []*regexp.Regexp
	for expr := range strings.SplitSeq(str, ",") {
		if expr == "" {
			continue
		}
		re, err := regexp.Compile(expr)
		if err != nil {
			return fmt.Errorf("invalid --%s regexp %q: %v", flagExclude, expr, err)
		}
		exclusions = append(exclusions, re)
	}
	if len(exclusions) == 0 {
		return nil
	}
	for obj := range e.genDecls() {
		for _, re := range exclusions {
			if re.MatchString(obj.Name()) {
				e.omitted[obj] = true
			}
		}
	}
	return nil
}

type extractor struct {
	cmd *Command

	allPkgs map[string]*packages.Package
	done    map[string]bool

	// per package
	pkg         *packages.Package
	orig        map[types.Type]*ast.StructType
	usedPkgs    map[string]bool
	consts      map[types.Type][]string
	k8sSemantic bool

	// per file
	cmap     ast.CommentMap
	pkgNames map[string]pkgInfo

	// omits holds the --omit selectors, along with the packages they matched in.
	omits []omitSelector
	// omitted holds the declarations matched by --omit or --exclude.
	omitted map[types.Object]bool
	// omittedMethods holds the methods of named types matched by --omit.
	omittedMethods map[omittedMethod]bool

	codecs []*codec

	// jsonV2Errors records why encoding/json/v2 rejects a struct type,
	// or the empty string if it does not.
	jsonV2Errors map[*types.Struct]string

	// Caches for [extractor.ownEncoding] and [extractor.structEncoding].
	ownEncodings    map[types.Type]ownEncoding
	structEncodings map[*types.Struct]*structEncoding
}

// codec describes how a Go encoding library selected via --codec
// encodes struct fields.
type codec struct {
	// name is how the codec is selected via --codec.
	name string

	// tagKey is the struct tag key which the codec reads.
	tagKey string

	// inlineOption is whether the fields of an embedded struct are only
	// promoted into its parent when its tag has an "inline" option.
	inlineOption bool

	// noNull is whether the encoding has no null value.
	noNull bool

	// stringKinds are the kinds of basic types which a "string" tag option
	// encodes as a string.
	stringKinds types.BasicInfo

	// jsonV2 is whether the codec follows encoding/json/v2
	// rather than the v1 API of encoding/json.
	jsonV2 bool
}

// knownCodecs lists the codecs with their own semantics. Any other struct tag
// key follows encoding/json for embedding and omission, without a "string" option.
var knownCodecs = []*codec{
	{name: "json", tagKey: "json", stringKinds: types.IsBoolean | types.IsNumeric | types.IsString},
	{name: "jsonv2", tagKey: "json", stringKinds: types.IsNumeric, jsonV2: true},
	// YAML libraries such as gopkg.in/yaml.v3.
	{name: "yaml", tagKey: "yaml", inlineOption: true},
	// TOML libraries such as github.com/BurntSushi/toml.
	{name: "toml", tagKey: "toml", noNull: true},
}

func lookupCodec(name string) *codec {
	for _, c := range knownCodecs {
		if c.name == name {
			return c
		}
	}
	return &codec{name: name, tagKey: name}
}

type pkgInfo struct {
	id   string
	name string
}

func (e *extractor) logf(format string, args ...any) {
	if flagVerbose.Bool(e.cmd) {
		fmt.Fprintf(e.cmd.Stderr(), format+"\n", args...)
	}
}

func (e *extractor) usedPkg(pkg string) {
	e.usedPkgs[pkg] = true
}

var (
	typeAny    = types.Universe.Lookup("any").Type()    // any
	typeByte   = types.Universe.Lookup("byte").Type()   // byte
	typeBytes  = types.NewSlice(typeByte)               // []byte
	typeString = types.Universe.Lookup("string").Type() // string
	typeError  = types.Universe.Lookup("error").Type()  // error
)

// Note that we can leave positions, packages, and parameter/result names empty.
// They are not used by go/types.Implements.

func typeMethod(name string, params, results []types.Type) *types.Func {
	return types.NewFunc(token.NoPos, nil, name, typeSignature(params, results))
}

func typeSignature(params, results []types.Type) *types.Signature {
	paramVars := make([]*types.Var, len(params))
	for i, param := range params {
		paramVars[i] = types.NewParam(token.NoPos, nil, "", param)
	}
	resultVars := make([]*types.Var, len(results))
	for i, result := range results {
		resultVars[i] = types.NewParam(token.NoPos, nil, "", result)
	}
	return types.NewSignatureType(
		nil,
		nil,
		nil,
		types.NewTuple(paramVars...),
		types.NewTuple(resultVars...),
		false,
	)
}

// Note that we record these interfaces without names, so they will show up in
// the logs like "interface{MarshalJSON() ([]uint8, error)}" rather than
// encoding/json.Marshaler. We could construct named types if need be.

var toTop = []*types.Interface{
	// json.Marshaler: interface { MarshalJSON() ([]byte, error) }
	types.NewInterfaceType([]*types.Func{
		typeMethod("MarshalJSON", nil, []types.Type{typeBytes, typeError}),
	}, nil).Complete(),

	// json.Unmarshaler: interface { UnmarshalJSON([]byte) error }
	types.NewInterfaceType([]*types.Func{
		typeMethod("UnmarshalJSON", []types.Type{typeBytes}, []types.Type{typeError}),
	}, nil).Complete(),

	// yaml.Marshaler: interface { MarshalYAML() (any, error) }
	types.NewInterfaceType([]*types.Func{
		typeMethod("MarshalYAML", nil, []types.Type{typeAny, typeError}),
	}, nil).Complete(),

	// yaml.Unmarshaler: interface { UnmarshalYAML(func(any) error) error }
	types.NewInterfaceType([]*types.Func{
		typeMethod("UnmarshalYAML", []types.Type{
			typeSignature([]types.Type{typeAny}, []types.Type{typeError}),
		}, []types.Type{typeError}),
	}, nil).Complete(),
}

var toString = []*types.Interface{
	// encoding.TextMarshaler: interface { MarshalText() ([]byte, error) }
	types.NewInterfaceType([]*types.Func{
		typeMethod("MarshalText", nil, []types.Type{typeBytes, typeError}),
	}, nil).Complete(),

	// encoding.TextUnmarshaler: interface { UnmarshalText([]byte) error }
	types.NewInterfaceType([]*types.Func{
		typeMethod("UnmarshalText", []types.Type{typeBytes}, []types.Type{typeError}),
	}, nil).Complete(),
}

// TODO:
// - consider not including types with any dropped fields.

func extract(cmd *Command, args []string) error {
	if flagLocal.IsSet(cmd) && flagOutFile.IsSet(cmd) {
		return errors.New("--local and --outfile are mutually exclusive")
	}

	// TODO the CUE load using "." (below) assumes that a CUE module and a Go
	// module will exist within the same directory (more precisely a Go module
	// could be nested within a CUE module), such that the module path in any
	// subdirectory below the current directory will be the same.  This seems an
	// entirely reasonable restriction, but also one that we should enforce.
	//
	// Enforcing this restriction also makes --local entirely redundant.

	// command specifies a Go package(s) that belong to the main module
	// and where for some reason the
	// determine module root:
	binst := loadFromArgs([]string{"."}, nil)[0]

	// TODO: require explicitly set root.
	root := binst.Root

	cfg := &packages.Config{
		Mode: packages.NeedName | packages.NeedCompiledGoFiles |
			packages.NeedImports | packages.NeedDeps | packages.NeedTypes |
			packages.NeedSyntax | packages.NeedTypesInfo | packages.NeedModule,
	}
	pkgs, err := packages.Load(cfg, args...)
	if err != nil {
		return err
	}
	if packages.PrintErrors(pkgs) > 0 {
		return ErrPrintedError
	}

	if flagOutFile.IsSet(cmd) && len(pkgs) != 1 {
		return errors.New("--outfile only allows for one package to be specified")
	}

	e := extractor{
		cmd:            cmd,
		allPkgs:        map[string]*packages.Package{},
		orig:           map[types.Type]*ast.StructType{},
		omitted:        map[types.Object]bool{},
		omittedMethods: map[omittedMethod]bool{},
	}

	for name := range strings.SplitSeq(flagCodec.String(cmd), ",") {
		c := lookupCodec(name)
		for _, c2 := range e.codecs {
			if c.tagKey == c2.tagKey {
				return fmt.Errorf("codecs %s and %s both read %q tags", c2.name, c.name, c.tagKey)
			}
		}
		e.codecs = append(e.codecs, c)
	}
	e.jsonV2Errors = make(map[*types.Struct]string)
	e.ownEncodings = make(map[types.Type]ownEncoding)
	e.structEncodings = make(map[*types.Struct]*structEncoding)

	e.done = map[string]bool{}

	for _, p := range pkgs {
		e.done[p.PkgPath] = true
		e.addPackage(p)
	}
	if err := e.initExclusions(flagExclude.String(cmd)); err != nil {
		return err
	}
	if err := e.initOmissions(flagOmit.StringArray(cmd)); err != nil {
		return err
	}

	for _, p := range pkgs {
		if err := e.extractPkg(root, p); err != nil {
			return err
		}
	}
	return nil
}

func (e *extractor) addPackage(p *packages.Package) {
	if pkg, ok := e.allPkgs[p.PkgPath]; ok {
		if p != pkg {
			panic(fmt.Sprintf("duplicate package %s", p.PkgPath))
		}
		return
	}
	e.allPkgs[p.PkgPath] = p
	for _, pkg := range p.Imports {
		e.addPackage(pkg)
	}
}

func (e *extractor) recordTypeInfo(p *packages.Package) {
	for _, f := range p.Syntax {
		for n := range ast.Preorder(f) {
			switch n := n.(type) {
			case *ast.StructType:
				e.orig[p.TypesInfo.TypeOf(n)] = n
			}
		}
	}
}

func (e *extractor) extractPkg(root string, p *packages.Package) error {
	e.pkg = p
	e.k8sSemantic = false

outer:
	for _, f := range p.Syntax {
		for _, c := range f.Comments {
			if strings.Contains(c.Text(), "+k8s:openapi-gen=true") {
				e.k8sSemantic = true
				break outer
			}
		}
	}
	e.logf("--- Package %s - Kubernetes semantics: %t", p.PkgPath, e.k8sSemantic)

	e.recordTypeInfo(p)

	e.consts = map[types.Type][]string{}

	for _, f := range p.Syntax {
		for _, d := range f.Decls {
			switch d := d.(type) {
			case *ast.GenDecl:
				e.recordConsts(d)
			}
		}
	}

	pkg := p.PkgPath
	dir := filepath.Join(load.GenPath(root), filepath.FromSlash(pkg))

	isMain := flagLocal.Bool(e.cmd) && p.Module != nil && p.Module.Main
	if isMain {
		dir = p.Module.Dir
		sub := p.PkgPath[len(p.Module.Path):]
		if sub != "" {
			dir = filepath.FromSlash(dir + sub)
		}
	}

	outFile := flagOutFile.String(e.cmd)
	if outFile == "" {
		if err := os.MkdirAll(dir, 0777); err != nil {
			return err
		}
	}

	e.usedPkgs = map[string]bool{}

	args := pkg
	if val := flagCodec.String(e.cmd); val != defaultCodec {
		args += " --" + string(flagCodec) + "=" + val
	}
	if omits := e.omitsFor(p); len(omits) > 0 {
		args += " --" + string(flagOmit) + "=" + strings.Join(omits, ",")
	}
	if val := flagExclude.String(e.cmd); val != "" {
		args += " --" + string(flagExclude) + "=" + val
	}

	pName := flagPackage.String(e.cmd)
	if pName == "" {
		pName = p.Name
	}

	cueFile, cuePkg := e.newFile(pName, args)
	// By default, we output one CUE file for each Go file as long as there's anything to generate.
	// When --outfile is used, we want to generate exactly one CUE file for an entire Go package,
	// so we instead keep joining declarations until we reach the last Go file, where we then write.
	for i, f := range p.Syntax {
		e.cmap = ast.NewCommentMap(p.Fset, f, f.Comments)

		e.pkgNames = map[string]pkgInfo{}

		for _, spec := range f.Imports {
			pkgPath, _ := strconv.Unquote(spec.Path.Value)
			pkg := p.Imports[pkgPath]

			info := pkgInfo{id: pkgPath, name: pkg.Name}
			if path.Base(pkgPath) != pkg.Name {
				info.id += ":" + pkg.Name
			}

			if spec.Name != nil {
				info.name = spec.Name.Name
			}

			e.pkgNames[pkgPath] = info
		}

		addDoc(f.Doc, cuePkg)
		lenPreamble := len(cueFile.Decls)

		for _, d := range f.Decls {
			switch d := d.(type) {
			case *ast.GenDecl:
				cueFile.Decls = append(cueFile.Decls, e.reportDecl(d)...)
			}
		}

		if outFile == "" && len(cueFile.Decls) == lenPreamble && f.Doc == nil {
			// By default, empty files are not generated.
			// Note that cueFile.Decls is never empty, as we add top-level docs and a package clause.
			continue
		}

		if outFile != "" && i != len(p.Syntax)-1 {
			// By default, we generate one CUE file per Go file.
			// With --outfile, we keep adding to one output file, and write it at the end.
			// TODO: using a second loop to write output files may be simpler.
			continue
		}

		if err := astutil.Sanitize(cueFile); err != nil {
			return err
		}

		b, err := format.Node(cueFile, format.Simplify())
		if err != nil {
			return err
		}

		dst := outFile
		if dst == "" {
			file := filepath.Base(p.CompiledGoFiles[i])
			file = strings.Replace(file, ".go", "_go", 1)
			file += "_gen.cue"
			dst = filepath.Join(dir, file)
		}
		if dst == "-" {
			_, err = os.Stdout.Write(b)
		} else {
			err = os.WriteFile(dst, b, 0666)
		}
		if err != nil {
			return err
		}
		cueFile, cuePkg = e.newFile(pName, args)
	}

	if !isMain {
		if err := e.importCUEFiles(p, dir, args); err != nil {
			return err
		}
	}

	if outFile == "" {
		// Note that we don't generate dependencies with --outfile.
		for pkgPath := range e.usedPkgs {
			if !e.done[pkgPath] {
				e.done[pkgPath] = true
				if err := e.extractPkg(root, e.allPkgs[pkgPath]); err != nil {
					return err
				}
			}
		}
	}

	return nil
}

func (e *extractor) newFile(pName, args string) (*cueast.File, *cueast.Package) {
	pkg := &cueast.Package{Name: e.ident(pName, false)}
	file := &cueast.File{Decls: []cueast.Decl{
		&cueast.CommentGroup{List: []*cueast.Comment{
			{Text: "// Code generated by cue get go. DO NOT EDIT."},
		}},
		&cueast.CommentGroup{List: []*cueast.Comment{
			{Text: "//cue:generate cue get go " + args},
		}},
		pkg,
	}}
	return file, pkg
}

func (e *extractor) importCUEFiles(p *packages.Package, dstDir, args string) error {
	// A package's Go files will often share directories,
	// for example all original source files will sit in the same directory.
	// Deduplicate the directories so we don't repeat work.
	var srcDirs []string
	for _, path := range p.CompiledGoFiles {
		srcDirs = append(srcDirs, filepath.Dir(path))
	}
	slices.Sort(srcDirs)
	srcDirs = slices.Compact(srcDirs)

	for _, srcDir := range srcDirs {
		entries, err := os.ReadDir(srcDir)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			name := entry.Name()
			path := filepath.Join(srcDir, name)
			name, ok := strings.CutSuffix(name, ".cue")
			if !ok {
				continue
			}
			f, err := parser.ParseFile(path, nil, parser.PackageClauseOnly)
			if err != nil {
				return err
			}

			if pkg := f.PackageName(); pkg != "" && pkg == p.Name {
				w := &bytes.Buffer{}
				fmt.Fprintf(w, "// Code generated by cue get go. DO NOT EDIT.\n\n")
				fmt.Fprintf(w, "//cue:generate cue get go %v\n\n", args)
				b, err := os.ReadFile(path)
				if err != nil {
					return err
				}
				w.Write(b)
				dst := filepath.Join(dstDir, name+"_gen.cue")
				if err := os.WriteFile(dst, w.Bytes(), 0666); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func (e *extractor) recordConsts(x *ast.GenDecl) {
	if x.Tok != token.CONST {
		return
	}
	for _, s := range x.Specs {
		v, ok := s.(*ast.ValueSpec)
		if !ok {
			continue
		}
		for _, n := range v.Names {
			if n.Name == "_" || e.omitted[e.pkg.TypesInfo.Defs[n]] {
				continue
			}
			typ := e.pkg.TypesInfo.TypeOf(n)
			e.consts[typ] = append(e.consts[typ], n.Name)
		}
	}
}

func (e *extractor) ident(name string, isDef bool) *cueast.Ident {
	if isDef {
		r, _ := utf8.DecodeRuneInString(name)
		name = "#" + name
		if !unicode.Is(unicode.Lu, r) {
			name = "_" + name
		}
	}
	return cueast.NewIdent(name)
}

func (e *extractor) def(doc *ast.CommentGroup, name string, value cueast.Expr, newline bool) *cueast.Field {
	f := &cueast.Field{
		Label: e.ident(name, true), // Go identifiers are always valid CUE identifiers.
		Value: value,
	}
	addDoc(doc, f)
	if newline {
		cueast.SetRelPos(f, cuetoken.NewSection)
	}
	return f
}

func (e *extractor) reportDecl(x *ast.GenDecl) (a []cueast.Decl) {
	switch x.Tok {
	case token.TYPE:
		for _, s := range x.Specs {
			v, ok := s.(*ast.TypeSpec)
			if !ok || e.omitted[e.pkg.TypesInfo.Defs[v.Name]] {
				continue
			}

			typ := e.pkg.TypesInfo.TypeOf(v.Name)
			enums := e.consts[typ]
			name := v.Name.Name
			mapNamed := false
			underlying := e.pkg.TypesInfo.TypeOf(v.Type)
			if b, ok := underlying.Underlying().(*types.Basic); ok && b.Kind() != types.String {
				mapNamed = true
			}

			switch tn, ok := e.pkg.TypesInfo.Defs[v.Name].(*types.TypeName); {
			case ok:
				if altType := e.altType(tn.Type()); altType != nil {
					// TODO: add the underlying tag as a Go tag once we have
					// proper string escaping for CUE.
					a = append(a, e.def(x.Doc, name, altType, true))
					break
				}
				fallthrough

			default:
				if !e.supportedType(nil, typ, e.codecs[0]) {
					e.logf("    Dropped declaration %v of unsupported type %v", name, typ)
					continue
				}
				if s := e.altType(typ); s != nil {
					a = append(a, e.def(x.Doc, name, s, true))
					break
				}

				f, _ := e.makeField(name, definition, none, underlying, x.Doc, true)
				a = append(a, f)
				cueast.SetRelPos(f, cuetoken.NewSection)

			}

			if len(enums) > 0 && ast.IsExported(name) {
				enumName := "#enum" + name
				cueast.AddComment(a[len(a)-1], internal.NewComment(false, enumName))

				// Constants are mapped as definitions.
				var exprs []cueast.Expr
				var named []cueast.Decl
				for _, v := range enums {
					label := cueast.NewString(v)

					x := e.ident(v, true)
					cueast.SetRelPos(x, cuetoken.Newline)
					exprs = append(exprs, x)

					if !mapNamed {
						continue
					}

					named = append(named, &cueast.Field{
						Label: label,
						Value: e.ident(v, true),
					})
				}

				addField := func(label string, exprs []cueast.Expr) {
					f := &cueast.Field{
						Label: cueast.NewIdent(label),
						Value: cueast.NewBinExpr(cuetoken.OR, exprs...),
					}
					cueast.SetRelPos(f, cuetoken.NewSection)
					a = append(a, f)
				}

				addField(enumName, exprs)
				if len(named) > 0 {
					f := &cueast.Field{
						Label: cueast.NewIdent("#values_" + name),
						Value: &cueast.StructLit{Elts: named},
					}
					cueast.SetRelPos(f, cuetoken.NewSection)
					a = append(a, f)
				}
			}
		}

	case token.CONST:
		// TODO: copy over comments for constant blocks.

		for k, s := range x.Specs {
			// TODO: determine type name and filter.
			v, ok := s.(*ast.ValueSpec)
			if !ok {
				continue
			}

			for i, name := range v.Names {
				if name.Name == "_" || e.omitted[e.pkg.TypesInfo.Defs[name]] {
					continue
				}
				f := e.def(v.Doc, name.Name, nil, k == 0)
				a = append(a, f)

				val := ""
				if i < len(v.Values) {
					if lit, ok := v.Values[i].(*ast.BasicLit); ok {
						val = lit.Value
					}
				}

				typ := e.pkg.TypesInfo.TypeOf(name)
				v := e.pkg.TypesInfo.Defs[v.Names[i]].(*types.Const).Val()
				var cv cueast.Expr
				switch v.Kind() {
				case constant.String:
					s := constant.StringVal(v)
					bl := &cueast.BasicLit{Kind: cuetoken.STRING}
					// Go strings may contain any bytes, even invalid UTF-8.
					// CUE strings may only contain valid UTF-8, because it has
					// bytes values for anything that may not be valid UTF-8.
					if utf8.ValidString(s) {
						bl.Value = literal.String.Quote(s)
					} else {
						bl.Value = literal.Bytes.Quote(s)
					}
					cv = bl

				default:
					// TODO(mvdan): replace this with switch cases for Bool/Int/Float
					sv := v.ExactString()
					var err error
					cv, err = parser.ParseExpr("", sv)
					if err != nil {
						panic(fmt.Errorf("failed to parse %v: %v", sv, err))
					}
				}

				// Use the original Go value if compatible with CUE (octal is okay)
				if b, ok := cv.(*cueast.BasicLit); ok {
					if b.Kind == cuetoken.INT && val != "" && val[0] != '\'' {
						b.Value = val
					}
					if b.Value != val {
						cueast.AddComment(cv, internal.NewComment(false, val))
					}
				}

				switch typ {
				case typeByte, typeString, typeError:
				default:
					if basic, ok := typ.(*types.Basic); ok && basic.Info()&types.IsUntyped != 0 {
						break // untyped basic types do not make valid identifiers
					}
					cv = cueast.NewBinExpr(cuetoken.AND, e.makeType(typ, regular, none), cv)
				}

				f.Value = cv
			}
		}
	}
	return a
}

func shortTypeName(t types.Type) string {
	if n, ok := t.(*types.Named); ok {
		return n.Obj().String() // fully qualified, e.g. "foo/bar.Baz"
	}
	return t.String() // anonymous, e.g. "interface{Method() []byte}"
}

// ownEncoding describes whether a type encodes itself via its own methods.
type ownEncoding int

const (
	noOwnEncoding   ownEncoding = iota
	encodesAsTop                // e.g. via MarshalJSON
	encodesAsString             // e.g. via MarshalText
)

// altType returns the CUE type for typ if it encodes itself via its own methods.
func (e *extractor) altType(typ types.Type) cueast.Expr {
	switch e.ownEncoding(typ) {
	case encodesAsTop:
		return e.ident("_", false)
	case encodesAsString:
		return e.ident("string", false)
	}
	return nil
}

// ownEncoding reports whether typ encodes itself via its own methods, and how.
// The result is cached, as the same types are checked for many fields.
func (e *extractor) ownEncoding(typ types.Type) ownEncoding {
	enc, ok := e.ownEncodings[typ]
	if !ok {
		enc = e.findOwnEncoding(typ)
		e.ownEncodings[typ] = enc
	}
	return enc
}

func (e *extractor) findOwnEncoding(typ types.Type) ownEncoding {
	for _, iface := range toTop {
		if e.implements(typ, iface) {
			t := shortTypeName(typ)
			e.logf("    %v implements %s; setting type to _", t, iface)
			return encodesAsTop
		}
	}
	if name := e.jsontextMethod(typ); name != "" {
		e.logf("    %v has method %s; setting type to _", shortTypeName(typ), name)
		return encodesAsTop
	}
	for _, iface := range toString {
		if e.implements(typ, iface) {
			t := shortTypeName(typ)
			e.logf("    %v implements %s; setting type to string", t, iface)
			return encodesAsString
		}
	}
	return noOwnEncoding
}

// implements reports whether typ or *typ implements iface,
// which has a single encoding method, unless that method is omitted.
func (e *extractor) implements(typ types.Type, iface *types.Interface) bool {
	return !e.omitsMethod(typ, iface.Method(0).Name()) && implementsEither(typ, iface)
}

// implementsEither reports whether typ or *typ implements iface.
func implementsEither(typ types.Type, iface *types.Interface) bool {
	// Typically we would just need to check whether *T implements I,
	// as the method set of *T includes the method set of T,
	// but that doesn't work when T is an interface type.
	// See https://go.dev/ref/spec#Method_sets.
	//
	// TODO(mvdan): perhaps check with T when it's an interface,
	// and with *T otherwise, avoiding double calls to types.Implements.
	return types.Implements(typ, iface) || types.Implements(types.NewPointer(typ), iface)
}

// jsontextMethods lists the methods of json.MarshalerTo and json.UnmarshalerFrom,
// added in Go 1.27, along with the jsontext type they take a pointer to.
// Those interfaces use types from encoding/json/jsontext, which may not
// be available to us, so we cannot construct them like [toTop].
var jsontextMethods = []struct{ name, param string }{
	{"MarshalJSONTo", "Encoder"},
	{"UnmarshalJSONFrom", "Decoder"},
}

// jsontextMethod returns the name of the method of typ or *typ
// from [jsontextMethods], if any, unless that method is omitted.
func (e *extractor) jsontextMethod(typ types.Type) string {
	for _, m := range jsontextMethods {
		if !e.omitsMethod(typ, m.name) && hasJSONTextMethod(typ, m.name, m.param) {
			return m.name
		}
	}
	return ""
}

// hasJSONTextMethod reports whether typ or *typ has the method name
// which takes a pointer to the jsontext type param and returns an error.
func hasJSONTextMethod(typ types.Type, name, param string) bool {
	// Look up the method as if typ were addressable,
	// covering the method sets of both typ and *typ.
	obj, _, _ := types.LookupFieldOrMethod(typ, true, nil, name)
	fn, ok := obj.(*types.Func)
	if !ok {
		return false
	}
	sig := fn.Signature()
	if sig.Params().Len() != 1 || sig.Results().Len() != 1 ||
		!types.Identical(sig.Results().At(0).Type(), typeError) {
		return false
	}
	p, ok := sig.Params().At(0).Type().(*types.Pointer)
	return ok && isJSONTextType(p.Elem(), param)
}

// isStdPkg reports whether path is part of the Go standard library,
// which Go defines as having no dot in the first path element.
func isStdPkg(path string) bool {
	firstElem, _, _ := strings.Cut(path, "/")
	return !strings.Contains(firstElem, ".")
}

func addDoc(g *ast.CommentGroup, x cueast.Node) {
	doc := makeDoc(g, true)
	if doc != nil {
		cueast.AddComment(x, doc)
	}
}

func makeDoc(g *ast.CommentGroup, isDoc bool) *cueast.CommentGroup {
	if g == nil {
		return nil
	}

	a := []*cueast.Comment{}

	for _, comment := range g.List {
		c := comment.Text

		// Remove comment markers.
		// The parser has given us exactly the comment text.
		switch c[1] {
		case '/':
			// //-style comment (no newline at the end)
			a = append(a, &cueast.Comment{Text: c})

		case '*':
			/*-style comment */
			c = c[2 : len(c)-2]
			if len(c) > 0 && c[0] == '\n' {
				c = c[1:]
			}

			lines := strings.Split(c, "\n")

			// Find common space prefix
			i := 0
			line := lines[0]
			for ; i < len(line); i++ {
				if c := line[i]; c != ' ' && c != '\t' {
					break
				}
			}

			for _, l := range lines {
				for j := 0; j < i && j < len(l); j++ {
					if line[j] != l[j] {
						i = j
						break
					}
				}
			}

			// Strip last line if empty.
			if n := len(lines); n > 1 && len(lines[n-1]) < i {
				lines = lines[:n-1]
			}

			// Print lines.
			for _, l := range lines {
				if i >= len(l) {
					a = append(a, &cueast.Comment{Text: "//"})
					continue
				}
				a = append(a, &cueast.Comment{Text: "// " + l[i:]})
			}
		}
	}
	return &cueast.CommentGroup{Doc: isDoc, List: a}
}

// supportedType reports whether t can be encoded with the codec c,
// the one governing the struct field or declaration of type t.
func (e *extractor) supportedType(stack []types.Type, t types.Type, c *codec) (ok bool) {
	if s := e.altType(t); s != nil {
		// t implements a supported interface.
		return true
	}
	// handle recursive types
	if slices.Contains(stack, t) {
		return true
	}
	stack = append(stack, t)

	if named, ok := t.(*types.Named); ok {
		obj := named.Obj()
		pkg := obj.Pkg()

		// Redirect or drop Go standard library types.
		if pkg == nil {
			return true // error interface
		}
		switch pkg.Path() {
		case "time":
			switch obj.Name() {
			case "Duration":
				if c.jsonV2 {
					e.logf("    %v has no default representation in encoding/json/v2", obj)
					return false
				}
				return true
			case "Time", "Location", "Month", "Weekday":
				return true
			}
			return false
		case "math/big":
			switch obj.Name() {
			case "Int", "Float":
				return true
			}
			// case "net":
			// 	// TODO: IP, Host, SRV, etc.
			// case "url":
			// 	// TODO: URL and Values
		}
	}

	t = types.Unalias(t)
	switch t := t.(type) {
	case *types.Basic:
		return true
	case *types.Named:
		return e.supportedType(stack, t.Underlying(), c)
	case *types.TypeParam:
		return e.supportedType(stack, t.Underlying(), c)
	case *types.Pointer:
		return e.supportedType(stack, t.Elem(), c)
	case *types.Slice:
		return e.supportedType(stack, t.Elem(), c)
	case *types.Array:
		return e.supportedType(stack, t.Elem(), c)
	case *types.Map:
		if !e.supportedMapKey(t.Key()) {
			return false
		}
		if !e.supportedType(stack, t.Key(), c) {
			return false
		}
		return e.supportedType(stack, t.Elem(), c)
	case *types.Struct:
		if e.jsonV2Error(t) != "" {
			return false
		}
		// The fields of a struct follow its own codec.
		c := e.structCodec(t)
		// Eliminate structs with fields for which all fields are filtered.
		if t.NumFields() == 0 {
			return true
		}
		for f := range t.Fields() {
			if f.Exported() && e.supportedType(stack, f.Type(), c) {
				return true
			}
		}
	case *types.Interface:
		return true
	}
	return false
}

// supportedMapKey reports whether a map key type is supported by
// encoding/json: strings, integers, and types implementing
// encoding.TextMarshaler or encoding.TextUnmarshaler.
// Go 1.27 also added floats, and interfaces holding any of those.
// All are encoded as string labels in CUE.
func (e *extractor) supportedMapKey(t types.Type) bool {
	if hasBasicInfo(t, types.IsString|types.IsInteger|types.IsFloat) {
		return true
	}
	if i, ok := t.Underlying().(*types.Interface); ok && i.Empty() {
		return true
	}
	for _, iface := range toString {
		if e.implements(t, iface) {
			return true
		}
	}
	return false
}

type fieldKind int

const (
	regular fieldKind = iota
	definition
)

type fieldAttributes int

const (
	none fieldAttributes = 1 << iota
	required
	optional
	nullable
)

func (e *extractor) makeField(name string, kind fieldKind, attrs fieldAttributes, expr types.Type, doc *ast.CommentGroup, newline bool) (f *cueast.Field, typename string) {
	typ := e.makeType(expr, kind, attrs)
	var label cueast.Label
	if kind == definition {
		label = e.ident(name, true)
	} else {
		label = cueast.NewStringLabel(name)
	}
	f = &cueast.Field{Label: label, Value: typ}
	if doc := makeDoc(doc, newline); doc != nil {
		cueast.AddComment(f, doc)
		cueast.SetRelPos(doc, cuetoken.NewSection)
	}

	switch {
	case attrs&required == required:
		// TODO(required): use idiomatic CUE (token.NOT) if CUE version is
		// higher than a certain number? We may not be able to determine this
		// accurately enough.
	case attrs&optional == optional:
		f.Constraint = cuetoken.OPTION
	}

	b, _ := format.Node(typ, format.Simplify())
	return f, string(b)
}

func (e *extractor) makeType(typ types.Type, kind fieldKind, attrs fieldAttributes) (result cueast.Expr) {
	if attrs&nullable == nullable {
		return &cueast.BinaryExpr{
			X:  cueast.NewNull(),
			Op: cuetoken.OR,
			Y:  e.makeType2(typ, kind, attrs^nullable),
		}
	}
	return e.makeType2(typ, kind, attrs)
}

func (e *extractor) makeType2(typ types.Type, kind fieldKind, attrs fieldAttributes) (result cueast.Expr) {
	switch typ := types.Unalias(typ).(type) {
	case *types.Named:
		obj := typ.Obj()
		pkg := obj.Pkg()
		if pkg == nil {
			return e.ident("_", false)
		}
		// Check for builtin packages.
		switch {
		case pkg.Path() == "time" && obj.Name() == "Time":
			ref := e.ident(e.pkgNames[pkg.Path()].name, false)
			var name *cueast.Ident
			if ref.Name != "time" {
				name = e.ident(ref.Name, false)
			}
			ref.Node = cueast.NewImport(name, "time")
			return cueast.NewSel(ref, obj.Name())

		case pkg.Path() == "time" && obj.Name() == "Duration":
			// Go's time.Duration is an int64 to represent nanoseconds,
			// and even though most Go users would find the string representation
			// like "3s" or "40m" most reasonable and readable for constant values,
			// encoding/json is bound via Go's compatibility promise to encoding as integers.
			//
			// Since compatibility with JSON encoding and decoding in Go
			// is more important than readability, we use integers as well here.
			// Note that this means we aren't compatible with CUE's own time.Duration,
			// which is rather unfortunate, but we have to choose one or the other.
			//
			// The jsonv2 codec drops time.Duration instead, as encoding/json/v2
			// has no default representation for it; see https://go.dev/issue/71631.
			//
			// TODO(mvdan): once encoding/json/v2 chooses a representation,
			// could we generate types like 'int | *time.Duration'
			// and constants like '300 | *"300ns"' to support both at the same time?
			return e.ident("int", false)

		case pkg.Path() == "math/big" && obj.Name() == "Int":
			return e.ident("int", false)

		case pkg.Path() == "cuelang.org/go/cue" && obj.Name() == "Value":
			// A cue.Value can hold any CUE value, so it is translated to
			// top rather than generating CUE definitions for the
			// cuelang.org/go/cue package itself.
			return e.ident("_", false)

		default:
			// Any Go standard library type that hasn't been handled above is not supported;
			// fall back to whatever alternative type we can, or just "top".
			// Otherwise we would end up generating "std" packages which clash with our
			// own standard library, for example cue.mod/gen/time.
			//
			// Go defines standard library as "no dot in the first path element",
			// such that "foo/bar.baz" is still technically part of the standard library.
			// Note that "example" and "test" are reserved for users in Go, such that "test/foo"
			// is not part of Go's standard library, but such exceptions do not exist in CUE.
			// TODO: never place such packages under e.g. cue.mod/gen/example, as that clashes
			// with CUE's own standard library namespace at the moment.
			//
			// TODO: for cases where the Go std type could be supported, we could still generate
			// and import it under a non-std CUE package, such as cue.mod/gen/pkg.go.dev/time.
			// TODO: Doc?
			if isStdPkg(pkg.Path()) {
				if s := e.altType(obj.Type()); s != nil {
					return s
				}
				return e.ident("_", false)
			}
		}

		if !e.canReference(typ) {
			// Such as the type of a field promoted from another package,
			// whose hidden definition cannot be referenced from here.
			e.logf("    %v cannot be referenced; setting type to _", obj)
			return e.ident("_", false)
		}
		result = e.ident(obj.Name(), true)
		if pkg != e.pkg.Types {
			info := e.pkgNames[pkg.Path()]
			if info.name == "" {
				info.name = pkg.Name()
			}
			p := e.ident(info.name, false)
			var name *cueast.Ident
			if info.name != pkg.Name() {
				name = e.ident(info.name, false)
			}
			if info.id == "" {
				// This may happen if an alias is defined in a different file
				// within this package referring to yet another package.
				info.id = pkg.Path()
				if path.Base(info.id) != pkg.Name() {
					info.id += ":" + pkg.Name()
				}
			}
			p.Node = cueast.NewImport(name, info.id)
			// makeType is always called to describe a type, so whatever
			// this is referring to, it must be a definition.
			result = cueast.NewSel(p, "#"+obj.Name())
			e.usedPkg(pkg.Path())
		}

		// TODO(uhthomas): Fields with type parameters should not be
		// top.
		//
		// For example:
		//
		// 	type A[T any] struct {
		// 		SomeField T
		// 	}
		//
		// 	type B A[string]
		//
		// Should become:
		//
		// 	#A: SomeField: _
		//
		// 	#B: #A & {
		// 		_#T: string
		// 		SomeField: _#T
		// 	}
		//
		// Or maybe:
		//
		// 	#A: {
		// 		#T: _
		// 		SomeField: #T
		// 	}
		//
		// 	#B: #A & {
		// 		#T: string
		// 	}
		//
		// The values of x.TypeParams() and x.TypeArgs() may be helpful.

		// params := x.TypeParams()
		// args := x.TypeArgs()
		// if params.Len() > 0 {
		// 	var fields []any
		// 	for i := 0; i < params.Len(); i++ {
		// 		name := params.At(i).Obj().Name()
		// 		fields = append(fields, e.ident(name, true), e.makeType(args.At(i)))
		// 	}
		// 	return cueast.NewBinExpr(cuetoken.AND, result, cueast.NewStruct(fields...))
		// }

		return result
	case *types.Pointer:
		return e.makeType(typ.Elem(), kind, attrs)

	case *types.Struct:
		st := &cueast.StructLit{}
		e.addFields(typ, st)
		return st

	case *types.Slice:
		// Note that []byte is treated different from []uint8,
		// even though byte is an alias for the basic type uint8.
		// TODO: reconsider this; both encoding/json and the future v2
		// encode []uint8, or anything assignable to []byte, as bytes.
		if typ.Elem() == typeByte {
			return e.ident("bytes", false)
		}
		return cueast.NewList(&cueast.Ellipsis{Type: e.makeType(typ.Elem(), kind, attrs)})

	case *types.Array:
		if typ.Elem() == typeByte {
			// TODO: no way to constrain lengths of bytes for now, as regexps
			// operate on Unicode, not bytes. So we need
			//     fmt.Fprint(e.w, fmt.Sprintf("=~ '^\C{%d}$'", x.Len())),
			// but regexp does not support that.
			// But translate to bytes, instead of [...byte] to be consistent.
			return e.ident("bytes", false)
		} else {
			return &cueast.BinaryExpr{
				X: &cueast.BasicLit{
					Kind:  cuetoken.INT,
					Value: strconv.Itoa(int(typ.Len())),
				},
				Op: cuetoken.MUL,
				Y:  cueast.NewList(e.makeType(typ.Elem(), kind, attrs)),
			}
		}

	case *types.Map:
		if !e.supportedMapKey(typ.Key()) {
			panic(fmt.Sprintf("unsupported map key type %v", typ.Key()))
		}

		f := &cueast.Field{
			Label: cueast.NewList(e.ident("string", false)),
			Value: e.makeType(typ.Elem(), kind, e.fieldAttributesFromType(typ.Elem())),
		}
		cueast.SetRelPos(f, cuetoken.Blank)
		return &cueast.StructLit{
			Lbrace: cuetoken.Blank.Pos(),
			Elts:   []cueast.Decl{f},
			Rbrace: cuetoken.Blank.Pos(),
		}

	case *types.Basic:
		switch typ.Kind() {
		case types.Uintptr, types.UnsafePointer:
			return e.ident("uint64", false)
		case types.Byte:
			return e.ident("uint8", false)
		case types.Complex64, types.Complex128:
			return e.ident("_", false)
		}
		return e.ident(typ.Name(), false)

	case *types.Union:
		var exprs []cueast.Expr
		for term := range typ.Terms() {
			exprs = append(exprs, e.makeType(term.Type(), kind, attrs))
		}
		return cueast.NewBinExpr(cuetoken.OR, exprs...)

	case *types.Interface:
		// TODO(uhthomas): Should interfaces with methods (IsMethodSet)
		// be set to top?
		if !typ.IsComparable() {
			return e.ident("_", false)
		}

		// TODO(uhthomas): Simplify expressions.
		//
		// For example:
		//
		// 	int | (int | string)
		//
		// Should really become:
		//
		// 	int | string
		//
		var exprs []cueast.Expr
		for etyp := range typ.EmbeddedTypes() {
			exprs = append(exprs, e.makeType(etyp, kind, attrs))
		}
		return cueast.NewBinExpr(cuetoken.OR, exprs...)

	case *types.TypeParam:
		return e.makeType(typ.Constraint(), kind, attrs)

	default:
		// record error
		panic(fmt.Sprintf("unsupported type %T", typ))
	}
}

func (e *extractor) addAttr(f *cueast.Field, tag, body string) {
	if attrBodyNeedsQuoting(body) {
		body = literal.String.Quote(body)
	}
	s := fmt.Sprintf("@%s(%s)", tag, body)
	f.Attrs = append(f.Attrs, &cueast.Attribute{Text: s})
}

func attrBodyNeedsQuoting(s string) bool {
	// TODO(mvdan): just like we have [cueast.StringLabelNeedsQuoting],
	// we could add an API to tell whether an attribute body needs to be quoted.
	// For now, use the CUE parser for this purpose, which works okay.
	src := "@x(" + s + ")"
	f, err := parser.ParseFile("", src)
	if err != nil {
		return true
	}
	// Make sure we parsed exactly one attribute with the content we expect.
	if len(f.Decls) != 1 {
		return true
	}
	attr, ok := f.Decls[0].(*cueast.Attribute)
	if !ok || attr.Text != src {
		return true
	}
	return false
}

func (e *extractor) addFields(x *types.Struct, st *cueast.StructLit) {
	e.addFieldsAt(x, st, "", e.structEncoding(x), false)
}

// addFieldsAt adds the fields of x, found at the index path prefix
// within the struct being generated, to st.
// Only the fields encoded as described by enc are added.
// The fields are optional when viaNilable is set, as they are promoted
// via an embedded pointer which may be nil.
func (e *extractor) addFieldsAt(x *types.Struct, st *cueast.StructLit, prefix string, enc *structEncoding, viaNilable bool) {
	add := func(x cueast.Decl) {
		st.Elts = append(st.Elts, x)
	}

	// Structs from other packages, which we only see when their fields
	// are promoted into ours, have no docs available.
	docs := make([]*ast.CommentGroup, 0, x.NumFields())
	if s := e.orig[x]; s != nil {
		for _, f := range s.Fields.List {
			for range max(1, len(f.Names)) {
				docs = append(docs, f.Doc)
			}
		}
	}
	docs = docs[:x.NumFields()]
	count := 0
	for i := 0; i < x.NumFields(); i++ {
		f := x.Field(i)
		index := fmt.Sprint(prefix, i)
		typ, viaPointer, embedded := e.embedded(enc.codec, f, x.Tag(i))
		if !embedded && !ast.IsExported(f.Name()) {
			continue
		}
		if !e.supportedType(nil, f.Type(), enc.codec) {
			e.logf("    Dropped field %v for unsupported type %v", f.Name(), f.Type())
			continue
		}
		if embedded {
			nilable := viaNilable || viaPointer
			fields, isStruct := typ.Underlying().(*types.Struct)
			named, isNamed := types.Unalias(typ).(*types.Named)
			switch {
			case !isStruct && index == enc.fallback:
				add(e.makeFallback(typ, enc.names))
			case !isStruct && isFallback(typ):
				e.logf("    Dropped embedded field %v as another holds any other object members", f.Name())
			case !isStruct:
				e.logf("    Dropped embedded field %v for unsupported type %v", f.Name(), f.Type())
			case isNamed && !nilable && e.canReference(named) &&
				// The definition of a type which encodes itself is top or
				// string, which only fits when the struct being generated
				// encodes itself too. Otherwise, its fields are encoded.
				(enc.encodesItself || e.ownEncoding(named) == noOwnEncoding) &&
				e.embedsAsIs(fields, index+".", enc):
				embed := &cueast.EmbedDecl{Expr: e.makeType(named, regular, required)}
				if len(st.Elts) > 0 {
					cueast.SetRelPos(embed, cuetoken.NewSection)
				}
				add(embed)
			case !enc.within(index + "."):
				// None of its fields are encoded, such as when x embeds itself.
			default:
				if isNamed {
					e.logf("    Promoted the fields of %v individually", f.Name())
				}
				e.addFieldsAt(fields, st, index+".", enc, nilable)
			}
			continue
		}
		tag := x.Tag(i)
		if enc.codec.fieldName(tag) == "-" {
			continue
		}
		name, ok := enc.names[index]
		if !ok {
			e.logf("    Dropped field %v as it is hidden by another with the same name", f.Name())
			continue
		}

		doc := docs[i]

		// TODO: check referrers
		attrs, err := e.detectFieldAttributes(f, doc, tag, enc.codec)
		if err != nil {
			e.logf("error parsing field %q: %v", f.Name(), err)
			continue
		}
		if viaNilable {
			attrs = attrs&^required | optional
		}
		ftyp := f.Type()
		if e.isStringified(ftyp, tag, enc.codec) {
			ftyp = types.Typ[types.String]
		}
		field, cueType := e.makeField(name, regular, attrs, ftyp, doc, count > 0)
		add(field)

		if s := reflect.StructTag(tag).Get("cue"); s != "" {
			expr, err := parser.ParseExpr("get go", s)
			if err != nil {
				e.logf("error parsing struct tag %q:", s, err)
			}
			field.Value = cueast.NewBinExpr(cuetoken.AND, field.Value, expr)
		}

		// Add field tag to convert back to Go.
		typeName := f.Type().String()
		// simplify type names:
		for path, info := range e.pkgNames {
			typeName = strings.ReplaceAll(typeName, path+".", info.name+".")
		}
		typeName = strings.ReplaceAll(typeName, e.pkg.Types.Path()+".", "")

		cueStr := strings.ReplaceAll(cueType, "_#", "")
		cueStr = strings.ReplaceAll(cueStr, "#", "")

		// TODO: remove fields in @go attr that are the same as printed?
		if name != f.Name() || typeName != cueStr {
			buf := &strings.Builder{}
			if name != f.Name() {
				buf.WriteString(f.Name())
			}

			if typeName != cueStr {
				if strings.ContainsAny(typeName, `#"',()=`) {
					typeName = literal.String.Quote(typeName)
				}
				fmt.Fprint(buf, ",", typeName)
			}
			e.addAttr(field, "go", buf.String())
		}

		// Carry over protobuf field tags with modifications.
		// TODO: consider trashing the protobuf tag, as the Go versions are
		// lossy and will not allow for an accurate translation in some cases.
		tags := reflect.StructTag(tag)
		if t := tags.Get("protobuf"); t != "" {
			split := strings.Split(t, ",")
			split = slices.DeleteFunc(split, func(s string) bool {
				rest, ok := strings.CutPrefix(s, "name=")
				return ok && rest == name
			})

			// Put tag first, as type could potentially be elided and is
			// "more optional".
			if len(split) >= 2 {
				split[0], split[1] = split[1], split[0]
			}

			// Interpret as map?
			if len(split) > 2 && split[1] == "bytes" {
				tk := tags.Get("protobuf_key")
				tv := tags.Get("protobuf_val")
				if tk != "" && tv != "" {
					tk, _, _ = strings.Cut(tk, ",")
					tv, _, _ = strings.Cut(tv, ",")
					split[1] = fmt.Sprintf("map[%s]%s", tk, tv)
				}
			}

			e.addAttr(field, "protobuf", strings.Join(split, ","))
		}

		// Carry over XML tags.
		if t := reflect.StructTag(tag).Get("xml"); t != "" {
			e.addAttr(field, "xml", t)
		}

		// Carry over TOML tags.
		if t := reflect.StructTag(tag).Get("toml"); t != "" {
			e.addAttr(field, "toml", t)
		}

		// TODO: should we in general carry over any unknown tag verbatim?

		count++
	}
}

// canReference reports whether the definition for t can be referenced
// from the package being generated, which is not the case for omitted types,
// nor for unexported types from other packages, as their definitions are hidden.
func (e *extractor) canReference(t *types.Named) bool {
	obj := t.Obj()
	return !e.omitted[obj] && (obj.Pkg() == e.pkg.Types || obj.Exported())
}

// structEncoding describes which fields of a struct are encoded,
// following the rules of encoding/json for embedding.
type structEncoding struct {
	// codec is the codec which the struct is encoded with.
	codec *codec

	// names holds the names of the encoded fields,
	// keyed by their dot-separated index paths.
	names map[string]string

	// fallback is the index path of the embedded field which holds
	// any other object members, if any.
	fallback string

	// encodesItself is whether the struct has encoding methods of its own,
	// such as those promoted from its embedded fields.
	encodesItself bool
}

// within reports whether any encoded field is at the index path prefix.
func (enc *structEncoding) within(prefix string) bool {
	if strings.HasPrefix(enc.fallback, prefix) {
		return true
	}
	for index := range enc.names {
		if strings.HasPrefix(index, prefix) {
			return true
		}
	}
	return false
}

// embedsAsIs reports whether x, found at the index path prefix,
// can be embedded as its own definition: when x follows the same codec,
// all of the fields that x encodes on its own are also encoded at the prefix,
// and x has no fallback, which would constrain the other fields.
func (e *extractor) embedsAsIs(x *types.Struct, prefix string, enc *structEncoding) bool {
	sub := e.structEncoding(x)
	if sub.codec != enc.codec || sub.fallback != "" {
		return false
	}
	for index := range sub.names {
		if _, ok := enc.names[prefix+index]; !ok {
			return false
		}
	}
	return true
}

// structEncoding returns which fields of x are encoded.
// A field hides deeper fields with the same name. Of multiple fields with
// the same name at the same depth, a single one named by a tag hides the rest,
// or else they are all dropped. Fallbacks follow the same rules by depth.
// The struct follows the codec given by [extractor.structCodec],
// and so do any structs embedded in it.
// The result is cached, as embedded structs are checked for each parent.
func (e *extractor) structEncoding(x *types.Struct) *structEncoding {
	enc, ok := e.structEncodings[x]
	if !ok {
		enc = e.findStructEncoding(x, e.structCodec(x))
		e.structEncodings[x] = enc
	}
	return enc
}

func (e *extractor) findStructEncoding(x *types.Struct, c *codec) *structEncoding {
	type field struct {
		name   string
		index  string
		depth  int
		tagged bool
	}
	type embedding struct {
		x     *types.Struct
		index string
	}
	var fields, fallbacks []field
	// Visit the embedded structs by depth, each only at the first depth
	// it is found at, counting how many times it is found at that depth.
	next := []embedding{{x, ""}}
	nextCount := map[*types.Struct]int{x: 1}
	visited := map[*types.Struct]bool{x: true}
	for depth := 0; len(next) > 0; depth++ {
		current, count := next, nextCount
		next, nextCount = nil, map[*types.Struct]int{}
		for _, emb := range current {
			// If the same struct is embedded more than once at this depth,
			// its fields conflict with themselves.
			copies := min(count[emb.x], 2)
			for i := range emb.x.NumFields() {
				f := emb.x.Field(i)
				tag := emb.x.Tag(i)
				index := fmt.Sprint(emb.index, i)
				if typ, _, ok := e.embedded(c, f, tag); ok {
					st, ok := typ.Underlying().(*types.Struct)
					if !ok {
						if isFallback(typ) {
							for range copies {
								fallbacks = append(fallbacks, field{index: index, depth: depth})
							}
						}
						continue
					}
					nextCount[st]++
					if !visited[st] {
						visited[st] = true
						next = append(next, embedding{st, index + "."})
					}
					continue
				}
				if !ast.IsExported(f.Name()) {
					continue
				}
				name := c.fieldName(tag)
				if name == "-" {
					continue
				}
				tagged := name != ""
				if !tagged {
					name = f.Name()
				}
				for range copies {
					fields = append(fields, field{name, index, depth, tagged})
				}
			}
		}
	}
	byName := make(map[string][]field)
	for _, f := range fields {
		byName[f.name] = append(byName[f.name], f)
	}
	enc := &structEncoding{
		codec:         c,
		names:         make(map[string]string),
		encodesItself: e.ownEncoding(x) != noOwnEncoding,
	}
	for _, fs := range byName {
		// The fields were added in order of depth.
		fs = slices.DeleteFunc(fs, func(f field) bool { return f.depth > fs[0].depth })
		if len(fs) > 1 {
			fs = slices.DeleteFunc(fs, func(f field) bool { return !f.tagged })
		}
		if len(fs) == 1 {
			enc.names[fs[0].index] = fs[0].name
		}
	}
	if len(fallbacks) == 1 || (len(fallbacks) > 1 && fallbacks[0].depth != fallbacks[1].depth) {
		enc.fallback = fallbacks[0].index
	}
	return enc
}

// isFallback reports whether an embedded field of type typ can hold
// the object members not encoded by any of the other fields.
func isFallback(typ types.Type) bool {
	if isJSONTextType(typ, "Value") {
		return true
	}
	m, ok := typ.Underlying().(*types.Map)
	return ok && hasBasicInfo(m.Key(), types.IsString)
}

// makeFallback returns the CUE declaration for an embedded field of type typ,
// which must satisfy [isFallback], given the fields that are encoded.
func (e *extractor) makeFallback(typ types.Type, encoded map[string]string) cueast.Decl {
	if isJSONTextType(typ, "Value") {
		return &cueast.Ellipsis{}
	}
	m := typ.Underlying().(*types.Map)
	names := slices.Sorted(maps.Values(encoded))
	var label cueast.Expr = e.ident("string", false)
	if len(names) > 0 {
		var exprs []cueast.Expr
		for _, name := range names {
			exprs = append(exprs, &cueast.UnaryExpr{Op: cuetoken.NEQ, X: cueast.NewString(name)})
		}
		label = cueast.NewBinExpr(cuetoken.AND, exprs...)
	}
	return &cueast.Field{
		Label: cueast.NewList(label),
		Value: e.makeType(m.Elem(), regular, e.fieldAttributesFromType(m.Elem())),
	}
}

// hasBasicInfo reports whether t is a basic type with any of the given kinds.
func hasBasicInfo(t types.Type, info types.BasicInfo) bool {
	b, ok := t.Underlying().(*types.Basic)
	return ok && b.Info()&info != 0
}

// isJSONTextType reports whether t is the named type from encoding/json/jsontext.
func isJSONTextType(t types.Type, name string) bool {
	n, ok := types.Unalias(t).(*types.Named)
	return ok && n.Obj().Pkg() != nil &&
		n.Obj().Pkg().Path() == "encoding/json/jsontext" && n.Obj().Name() == name
}

// derefPointer returns the element type of t if it is a pointer.
func derefPointer(t types.Type) (_ types.Type, isPointer bool) {
	if p, ok := t.(*types.Pointer); ok {
		return p.Elem(), true
	}
	return t, false
}

// jsonV2Error returns why encoding/json/v2 rejects the struct type x,
// or the empty string if it does not, or x does not follow the jsonv2 codec.
func (e *extractor) jsonV2Error(x *types.Struct) string {
	c := e.structCodec(x)
	if !c.jsonV2 {
		return ""
	}
	msg, ok := e.jsonV2Errors[x]
	if !ok {
		msg = e.findJSONV2Error(x, c)
		e.jsonV2Errors[x] = msg
		if msg != "" {
			e.logf("    encoding/json/v2 rejects %v: %s", x, msg)
		}
	}
	return msg
}

func (e *extractor) findJSONV2Error(x *types.Struct, c *codec) string {
	names := make(map[string]bool)
	hasTag, hasField, fallbacks := false, false, 0
	for i := range x.NumFields() {
		f := x.Field(i)
		tag, tagged := reflect.StructTag(x.Tag(i)).Lookup(c.tagKey)
		hasTag = hasTag || tagged
		if tag == "-" {
			continue
		}
		if !f.Exported() && tagged {
			return fmt.Sprintf("unexported field %s has a json tag", f.Name())
		}
		name, opts, _ := strings.Cut(tag, ",")
		if strings.HasSuffix(tag, ",") {
			return fmt.Sprintf("field %s has a json tag with a trailing comma", f.Name())
		}
		var embed, stringOpt bool
		for opt := range strings.SplitSeq(opts, ",") {
			switch opt {
			case "", "omitzero", "omitempty", "case:ignore", "case:strict":
			case "string":
				stringOpt = true
			case "embed":
				embed = true
			default:
				// Unknown options are ignored, but not misspelled known ones.
				// A format option is only supported via a runtime option.
				key, _, _ := strings.Cut(opt, ":")
				for _, known := range []string{"omitzero", "omitempty", "string", "case", "embed", "format"} {
					if strings.EqualFold(key, known) {
						return fmt.Sprintf("field %s has an unsupported json tag option %q", f.Name(), opt)
					}
				}
			}
		}
		if embed && (name != "" || opts != "embed") {
			return fmt.Sprintf("field %s has other json tag options besides embed", f.Name())
		}
		typ, _ := derefPointer(f.Type())
		ownEncoding := e.ownEncoding(typ) != noOwnEncoding
		if stringOpt && !ownEncoding && !hasBasicInfo(typ, c.stringKinds) {
			return fmt.Sprintf("field %s has a json string option but is not a number", f.Name())
		}
		if !f.Exported() && !f.Anonymous() {
			continue
		}
		hasField = true
		_, isStruct := typ.Underlying().(*types.Struct)
		if (f.Anonymous() && name == "") || embed {
			if !isStruct && !embed {
				return fmt.Sprintf("embedded field %s is not a struct and has no json name", f.Name())
			}
			if ownEncoding && e.ownEncoding(x) == noOwnEncoding {
				return fmt.Sprintf("embedded field %s has its own marshal or unmarshal methods", f.Name())
			}
			if !isStruct {
				if !isFallback(typ) {
					return fmt.Sprintf("embedded field %s is not a struct, a map with string keys, nor a jsontext.Value", f.Name())
				}
				if fallbacks++; fallbacks > 1 {
					return "more than one embedded field holds other object members"
				}
			}
			continue
		}
		if name == "" {
			name = f.Name()
		}
		if names[name] {
			return fmt.Sprintf("more than one field is named %q", name)
		}
		names[name] = true
	}
	if !hasField && !hasTag && x.NumFields() > 0 {
		return "no exported fields and no json tags"
	}
	return ""
}

// isStringified reports whether a field of type typ with the given tag
// is encoded as a string due to a "string" tag option.
// encoding/json/v2 only allows the option for numbers.
func (e *extractor) isStringified(typ types.Type, tag string, codec *codec) bool {
	if codec.stringKinds == 0 || !hasFlag(tag, codec.tagKey, "string", 1) {
		return false
	}
	typ, _ = derefPointer(typ)
	return e.ownEncoding(typ) == noOwnEncoding && hasBasicInfo(typ, codec.stringKinds)
}

// embedded reports whether the codec promotes the fields of f,
// with the given struct tag, into its parent struct.
// If so, it returns the type of f, and whether that is via a pointer.
func (e *extractor) embedded(codec *codec, f *types.Var, tag string) (typ types.Type, viaPointer, ok bool) {
	typ, viaPointer = derefPointer(f.Type())
	if codec.inlineOption {
		return typ, viaPointer, f.Anonymous() && hasFlag(tag, codec.tagKey, "inline", 1)
	}
	// Like encoding/json, promote the fields of an embedded struct
	// or pointer to struct, unless the tag gives it a name.
	if codec.fieldName(tag) != "" {
		return nil, false, false
	}
	_, isStruct := typ.Underlying().(*types.Struct)
	if f.Anonymous() && isStruct {
		return typ, viaPointer, true
	}
	// Go 1.27 added the "embed" option to encoding/json, which does the same
	// for any exported field, and also allows a map with string keys or
	// a jsontext.Value to hold the object members not encoded by other fields.
	if !f.Exported() || !hasFlag(tag, codec.tagKey, "embed", 1) {
		return nil, false, false
	}
	return typ, viaPointer, isStruct || isFallback(typ)
}

func (e *extractor) fieldAttributesFromType(f types.Type) (attrs fieldAttributes) {
	switch f.(type) {
	case *types.Pointer:
		attrs |= optional

		// In k8s semantics a pointer doesn't count as nullable.
		if !e.k8sSemantic {
			attrs |= nullable
		}
	}
	return attrs
}

func (e *extractor) detectFieldAttributes(f *types.Var, doc *ast.CommentGroup, tag string, codec *codec) (fieldAttributes, error) {
	var attrs fieldAttributes
	// See k8s docs https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md
	for line := range strings.SplitSeq(doc.Text(), "\n") {
		before, _, _ := strings.Cut(strings.TrimSpace(line), "=")
		switch before {
		case "+optional":
			attrs |= optional
		case "+nullable":
			attrs |= nullable
		case "+required":
			attrs |= required
		}
	}

	if attrs&(required|optional) == required|optional {
		return 0, fmt.Errorf("field cannot be optional and required: %s", f)
	}
	if attrs&required == required {
		return attrs, nil
	}

	typeAttrs := e.fieldAttributesFromType(f.Type())
	if codec.noNull {
		// Encoders omit nil pointers instead.
		typeAttrs &^= nullable
	}
	attrs |= typeAttrs

	// Only the codec which governs the struct decides whether a field may be omitted.
	// Go 1.24 added the "omitzero" option to encoding/json, an improvement over "omitempty";
	// TOML and YAML libraries such as BurntSushi/toml and goccy/go-yaml support it too.
	// TODO: also when the type is a list or other kind of pointer.
	if hasFlag(tag, codec.tagKey, "omitzero", 1) ||
		(hasFlag(tag, codec.tagKey, "omitempty", 1) && e.omitsEmpty(f.Type(), codec)) {
		attrs |= optional
	}

	return attrs, nil
}

// omitsEmpty reports whether a field of type typ with the given tag
// may be omitted due to an "omitempty" tag option.
// encoding/json/v2 only omits values encoded as an empty JSON value,
// which booleans and numbers never are.
func (e *extractor) omitsEmpty(typ types.Type, codec *codec) bool {
	if !codec.jsonV2 || e.ownEncoding(typ) != noOwnEncoding {
		return true
	}
	return !hasBasicInfo(typ, types.IsBoolean|types.IsNumeric)
}

func hasFlag(tag, key, flag string, offset int) bool {
	if t := reflect.StructTag(tag).Get(key); t != "" {
		split := strings.Split(t, ",")
		if offset >= len(split) {
			return false
		}
		if slices.Contains(split[offset:], flag) {
			return true
		}
	}
	return false
}

// structCodec returns the codec from [extractor.codecs] which governs
// the fields of x: the first one whose tag key appears on any field of x,
// else the first codec. A struct is encoded by a single library,
// so its fields follow a single codec, even when their tags differ.
func (e *extractor) structCodec(x *types.Struct) *codec {
	for _, c := range e.codecs {
		for i := range x.NumFields() {
			if _, ok := reflect.StructTag(x.Tag(i)).Lookup(c.tagKey); ok {
				return c
			}
		}
	}
	return e.codecs[0]
}

// fieldName returns the field name given by the codec's tag, if any.
func (c *codec) fieldName(tag string) string {
	name, _, _ := strings.Cut(reflect.StructTag(tag).Get(c.tagKey), ",")
	return name
}
