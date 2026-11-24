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

package convert

import (
	"cmp"
	"reflect"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

// GoField is a field of a Go struct type, possibly promoted from an embedded
// struct.
type GoField struct {
	Name        string              // from a tag, or else the Go field name
	Index       []int               // for [reflect.Value.FieldByIndex]
	Indirect    bool                // promoted via an embedded pointer
	StructField reflect.StructField // the Go field itself
	JSON        JSONOptions         // from its json tag

	tagged bool // whether Name comes from a tag
}

func compareFieldByIndex(a, b GoField) int {
	for i, x := range a.Index {
		if i >= len(b.Index) {
			break
		}
		if c := cmp.Compare(x, b.Index[i]); c != 0 {
			return c
		}
	}
	return cmp.Compare(len(a.Index), len(b.Index))
}

// GoFields resolves the fields of the struct type t like encoding/json,
// in index order, with a breadth-first search over its embedded structs.
// It also returns the embedded fields whose fields were promoted.
//
// tagName names a field from its tags, or "" if they give no name, and
// reports whether to keep the field. An unnamed embedded struct, or pointer
// to one, has its fields promoted, and any other unnamed field takes its Go
// name. Of several fields with the same name, the shallowest wins, then a
// tagged one; a tie drops them all.
func GoFields(t reflect.Type, tagName func(reflect.StructField) (string, bool)) (fields []GoField, promoted []reflect.StructField) {
	type queued struct {
		index    []int
		typ      reflect.Type
		indirect bool
	}
	// Anonymous fields to explore at the current level and the next.
	current := []queued{}
	next := []queued{{typ: t}}

	// Count of queued names for current level and the next.
	var count, nextCount map[reflect.Type]int

	// Types already visited at an earlier level.
	visited := map[reflect.Type]bool{}

	for len(next) > 0 {
		current, next = next, current[:0]
		count, nextCount = nextCount, map[reflect.Type]int{}

		for _, f := range current {
			if visited[f.typ] {
				continue
			}
			visited[f.typ] = true

			// Scan f.typ for fields to include.
			for i := range f.typ.NumField() {
				sf := f.typ.Field(i)
				isUnexported := sf.PkgPath != ""
				if sf.Anonymous {
					t := sf.Type
					if t.Kind() == reflect.Pointer {
						t = t.Elem()
					}
					if isUnexported && t.Kind() != reflect.Struct {
						// Ignore embedded fields of unexported non-struct types.
						continue
					}
					// Do not ignore embedded fields of unexported struct types
					// since they may have exported fields.
				} else if isUnexported {
					// Ignore unexported non-embedded fields.
					continue
				}
				name, ok := tagName(sf)
				if !ok {
					continue
				}
				index := make([]int, len(f.index)+1)
				copy(index, f.index)
				index[len(f.index)] = i

				ft := sf.Type
				if ft.Name() == "" && ft.Kind() == reflect.Pointer {
					// Follow pointer.
					ft = ft.Elem()
				}

				// Record found field and index sequence.
				if name != "" || !sf.Anonymous || ft.Kind() != reflect.Struct {
					tagged := name != ""
					if name == "" {
						name = sf.Name
					}
					fields = append(fields, GoField{
						Name:        name,
						Index:       index,
						tagged:      tagged,
						Indirect:    f.indirect,
						StructField: sf,
						JSON:        parseJSONOptions(sf.Tag.Get("json")),
					})
					if count[f.typ] > 1 {
						// If there were multiple instances, add a second,
						// so that the annihilation code will see a duplicate.
						// It only cares about the distinction between 1 or 2,
						// so don't bother generating any more copies.
						fields = append(fields, fields[len(fields)-1])
					}
					continue
				}

				// Record new anonymous struct to explore in next round.
				promoted = append(promoted, sf)
				nextCount[ft]++
				if nextCount[ft] == 1 {
					indirect := f.indirect || ft != sf.Type
					next = append(next, queued{index: index, typ: ft, indirect: indirect})
				}
			}
		}
	}

	slices.SortFunc(fields, func(a, b GoField) int {
		// sort field by name, breaking ties with depth, then
		// breaking ties with "name came from tag", then
		// breaking ties with index sequence.
		if c := cmp.Compare(a.Name, b.Name); c != 0 {
			return c
		}
		if c := cmp.Compare(len(a.Index), len(b.Index)); c != 0 {
			return c
		}
		if a.tagged != b.tagged {
			if a.tagged {
				return -1
			} else {
				return 1
			}
		}
		return compareFieldByIndex(a, b)
	})

	// Delete all fields that are hidden by the Go rules for embedded fields,
	// except that fields with tags are promoted.

	// The fields are sorted in primary order of name, secondary order
	// of field index length. Loop over names; for each name, delete
	// hidden fields by choosing the one dominant field that survives.
	out := fields[:0]
	for advance, i := 0, 0; i < len(fields); i += advance {
		// One iteration per name.
		// Find the sequence of fields with the name of this first field.
		fi := fields[i]
		name := fi.Name
		for advance = 1; i+advance < len(fields); advance++ {
			fj := fields[i+advance]
			if fj.Name != name {
				break
			}
		}
		if advance == 1 { // Only one field with this name
			out = append(out, fi)
			continue
		}
		dominant, ok := dominantField(fields[i : i+advance])
		if ok {
			out = append(out, dominant)
		}
	}

	fields = out
	slices.SortFunc(fields, compareFieldByIndex)
	return fields, promoted
}

// dominantField looks through the fields, all of which are known to
// have the same name, to find the single field that dominates the
// others using Go's embedding rules, modified by the presence of
// tags. If there are multiple top-level fields, the boolean
// will be false: This condition is an error in Go and we skip all
// the fields.
func dominantField(fields []GoField) (GoField, bool) {
	// The fields are sorted in increasing index-length order, then by presence of tag.
	// That means that the first field is the dominant one. We need only check
	// for error cases: two fields at top level, either both tagged or neither tagged.
	if len(fields) > 1 && len(fields[0].Index) == len(fields[1].Index) && fields[0].tagged == fields[1].tagged {
		return GoField{}, false
	}
	return fields[0], true
}

// JSONOptions are the options of a json struct tag which CUE supports.
type JSONOptions struct {
	OmitEmpty  bool // "omitempty"
	OmitZero   bool // "omitzero"
	CaseStrict bool // "case:strict", to decode only from the exact name
}

// parseJSONOptions parses the options of a json struct tag.
// Like [encoding/json], it only looks at the leading identifier of each,
// and giving both "case:strict" and "case:ignore" is not strict.
func parseJSONOptions(tag string) JSONOptions {
	var o JSONOptions
	_, opts, _ := strings.Cut(tag, ",")
	var caseStrict, caseIgnore bool
	for opt := range strings.SplitSeq(opts, ",") {
		switch name, rest := cutTagIdent(opt); name {
		case "omitempty":
			o.OmitEmpty = true
		case "omitzero":
			o.OmitZero = true
		case "case":
			switch value, _ := cutTagIdent(strings.TrimPrefix(rest, ":")); value {
			case "strict":
				caseStrict = true
			case "ignore":
				caseIgnore = true
			}
		}
	}
	o.CaseStrict = caseStrict && !caseIgnore
	return o
}

// cutTagIdent cuts the letters, digits, and underscores which s starts with.
func cutTagIdent(s string) (ident, rest string) {
	rest = strings.TrimLeftFunc(s, func(r rune) bool {
		return r == '_' || unicode.IsLetter(r) || unicode.IsNumber(r)
	})
	return s[:len(s)-len(rest)], rest
}

// JSONTagName names a field by its json tag alone for [GoFields], like
// [encoding/json] as of Go 1.28 (https://go.dev/cl/828884): up to the first
// comma, or no name if that includes a backslash, quote, or backtick.
// Invalid UTF-8 is replaced by [utf8.RuneError].
func JSONTagName(sf reflect.StructField) (string, bool) {
	return jsonTagName(sf.Tag.Get("json"))
}

func jsonTagName(tag string) (string, bool) {
	if tag == "-" {
		return "", false
	}
	n := strings.IndexAny(tag, ",\\'\"`")
	switch {
	case n < 0:
		n = len(tag)
	case tag[n] != ',':
		return "", true
	}
	name := tag[:n]
	if !utf8.ValidString(name) {
		name = string([]rune(name))
	}
	return name, true
}
