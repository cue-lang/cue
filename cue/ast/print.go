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

package ast

import (
	"fmt"
	gotoken "go/token"
	"io"
	"reflect"
	"strings"

	"cuelang.org/go/cue/token"
)

func DebugPrint(w io.Writer, node Node) {
	d := &debugPrinter{w: w, seen: make(map[reflect.Value]bool)}
	d.value(reflect.ValueOf(node))
	d.newline()
}

type debugPrinter struct {
	w     io.Writer
	level int

	seen map[reflect.Value]bool
}

func (d *debugPrinter) printf(format string, args ...any) {
	fmt.Fprintf(d.w, format, args...)
}

func (d *debugPrinter) newline() {
	fmt.Fprintf(d.w, "\n%s", strings.Repeat("\t", d.level))
}

var (
	typeTokenPos   = reflect.TypeOf((*token.Pos)(nil)).Elem()
	typeTokenToken = reflect.TypeOf((*token.Token)(nil)).Elem()
)

func (d *debugPrinter) value(v reflect.Value) {
	if d.seen[v] {
		panic("encountered a cycle; stopping") // otherwise we might never finish
	}
	d.seen[v] = true
	v = indirectElem(v)
	if !v.IsValid() { // indirecting a nil interface/pointer gives a zero value
		d.printf("nil")
		return
	}
	t := v.Type()
	switch t {
	case typeTokenPos, typeTokenToken: // simple types which can stringify themselves
		d.printf("%q", v.Interface())
		return
	}

	switch t.Kind() {
	default: // We assume all other kinds are basic in practice, like string or bool.
		d.printf("%#v", v.Interface())
	case reflect.Slice:
		d.printf("%s{", t)
		d.level++
		for i := 0; i < v.Len(); i++ {
			d.newline()
			ev := v.Index(i)
			d.value(ev)
		}
		d.level--
		d.newline()
		d.printf("}")
	case reflect.Struct:
		d.printf("%s{", t)
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
			d.newline()
			d.printf("%s: ", f.Name)
			d.value(v.Field(i))
		}
		val := v.Addr().Interface()
		if val, ok := val.(Node); ok {
			comments := val.Comments()
			if len(comments) > 0 {
				d.newline()
				d.printf("Comments: ")
				d.value(reflect.ValueOf(comments))
			}
		}
		d.level--
		d.newline()
		d.printf("}")
	}
}

// indirectElem is like reflect.Indirect, but also for interfaces.
func indirectElem(v reflect.Value) reflect.Value {
	for v.Kind() == reflect.Pointer || v.Kind() == reflect.Interface {
		v = v.Elem()
	}
	return v
}
