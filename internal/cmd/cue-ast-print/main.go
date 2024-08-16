// Copyright 2023 The CUE Authors
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

// cue-ast-print parses a CUE file and prints its syntax tree, for example:
//
//	cue-ast-print file.cue
package main

import (
	"flag"
	"log"
	"os"

	"fmt"
	gotoken "go/token"
	"io"
	"reflect"
	"strings"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/parser"
	"cuelang.org/go/cue/token"
)

func main() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: cue-ast-print [file.cue]\n")
		os.Exit(2)
	}
	flag.Parse()
	var filename string
	var src any
	switch flag.NArg() {
	case 0:
		filename = "<stdin>"
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			log.Fatal(err)
		}
		src = data
	case 1:
		filename = flag.Arg(0)
	default:
		flag.Usage()
	}
	file, err := parser.ParseFile(filename, src, parser.ParseComments)
	if err != nil {
		panic(err)
	}
	debugPrint(os.Stdout, file)
}

func debugPrint(w io.Writer, node ast.Node) {
	d := &debugPrinter{w: w}
	d.value(reflect.ValueOf(node), nil)
	d.newline()
}

type debugPrinter struct {
	w     io.Writer
	level int
}

func (d *debugPrinter) printf(format string, args ...any) {
	fmt.Fprintf(d.w, format, args...)
}

func (d *debugPrinter) newline() {
	fmt.Fprintf(d.w, "\n%s", strings.Repeat("\t", d.level))
}

var (
	typeTokenPos   = reflect.TypeFor[token.Pos]()
	typeTokenToken = reflect.TypeFor[token.Token]()
)

func (d *debugPrinter) value(v reflect.Value, impliedType reflect.Type) {
	// Skip over interface types.
	if v.Kind() == reflect.Interface {
		v = v.Elem()
	}

	// Indirecting a nil interface/pointer gives a zero value;
	// stop as calling reflect.Value.Type on an invalid type would panic.
	if !v.IsValid() {
		d.printf("nil")
		return
	}
	// We print the original pointer type if there was one.
	origType := v.Type()
	v = reflect.Indirect(v)

	t := v.Type()
	switch t {
	// Simple types which can stringify themselves.
	case typeTokenPos, typeTokenToken:
		d.printf("%s(%q)", t, v)
		return
	}

	switch t.Kind() {
	default:
		// We assume all other kinds are basic in practice, like string or bool.
		if t.PkgPath() != "" {
			// Mention defined and non-predeclared types, for clarity.
			d.printf("%s(%#v)", t, v)
		} else {
			d.printf("%#v", v)
		}
	case reflect.Slice:
		if origType != impliedType {
			d.printf("%s", origType)
		}
		d.printf("{")
		if v.Len() > 0 {
			d.level++
			for i := 0; i < v.Len(); i++ {
				d.newline()
				ev := v.Index(i)
				// Note: a slice literal implies the type of its elements
				// so we can avoid mentioning the type
				// of each element if it matches.
				d.value(ev, t.Elem())
			}
			d.level--
			d.newline()
		}
		d.printf("}")
	case reflect.Struct:
		if origType != impliedType {
			d.printf("%s", origType)
		}
		d.printf("{")
		printed := false
		d.level++
		for i := 0; i < v.NumField(); i++ {
			f := t.Field(i)
			if !gotoken.IsExported(f.Name) {
				continue
			}
			switch f.Name {
			// These fields are cyclic, and they don't represent the syntax anyway.
			case "Scope", "Node", "Unresolved":
				continue
			}
			printed = true
			d.newline()
			d.printf("%s: ", f.Name)
			d.value(v.Field(i), nil)
		}
		val := v.Addr().Interface()
		if val, ok := val.(ast.Node); ok {
			// Comments attached to a node aren't a regular field, but are still useful.
			// The majority of nodes won't have comments, so skip them when empty.
			if comments := ast.Comments(val); len(comments) > 0 {
				printed = true
				d.newline()
				d.printf("Comments: ")
				d.value(reflect.ValueOf(comments), nil)
			}
		}
		d.level--
		if printed {
			d.newline()
		}
		d.printf("}")
	}
}
