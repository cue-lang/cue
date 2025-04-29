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

//go:build ignore

package main

import (
	"bytes"
	"cmp"
	_ "embed"
	"fmt"
	goformat "go/format"
	"iter"
	"log"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"text/template"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/build"
	"cuelang.org/go/cue/cuecontext"
	"cuelang.org/go/cue/format"
	"cuelang.org/go/internal/filetypes"
	"cuelang.org/go/internal/filetypes/internal"
	"cuelang.org/go/internal/filetypes/internal/genfunc"
	"cuelang.org/go/internal/filetypes/internal/genstruct"
)

type tmplParams struct {
	TagTypes                   map[string]filetypes.TagType
	ToFileParams               *genToFileParams
	ToFileResult               *genToFileResult
	SubsidiaryBoolTagFuncCount int
	SubsidiaryTagFuncCount     int
	Data                       string
}

var (
	//go:embed types_gen.go.tmpl
	typesGenCode string

	//go:embed types.cue
	typesCUE string
)

var tmpl = template.Must(template.New("types_gen.go.tmpl").Parse(typesGenCode))

type tagInfo struct {
	name  string
	typ   filetypes.TagType
	value cue.Value // only set when kind is filetypes.TagTopLevel
}

// fileResult represents a possible result for toFile.
type fileResult struct {
	bits uint64

	mode     string
	fileVal  cue.Value
	filename string
	file     *build.File

	subsidiaryTags     cue.Value
	subsidiaryBoolTags cue.Value

	subsidiaryBoolTagFuncIndex int // valid if subsidiaryBoolTags.Exists
	subsidiaryTagFuncIndex     int // valid if subsidiaryTags.Exists

	err internal.ErrorKind

	tags []string
}

func (r *fileResult) appendRecord(data []byte, paramStruct *genToFileParams, resultStruct *genToFileResult) []byte {
	recordSize := paramStruct.Size() + resultStruct.Size()
	data = slices.Grow(data, recordSize)
	data = data[:len(data)+recordSize]
	record := data[len(data)-recordSize:]

	// Write the key part of the record.
	param := slices.Clip(record[:paramStruct.Size()])
	paramStruct.FileExt.Put(param, fileExt(r.filename))
	paramStruct.Tags.Put(param, genstruct.ElemsFromBits(r.bits, r.tags))
	var mode filetypes.Mode
	switch r.mode {
	case "input":
		mode = filetypes.Input
	case "export":
		mode = filetypes.Export
	case "def":
		mode = filetypes.Def
	case "eval":
		mode = filetypes.Eval
	default:
		panic(fmt.Errorf("unknown mode %q", r.mode))
	}
	paramStruct.Mode.Put(param, mode)

	result := slices.Clip(record[paramStruct.Size():])
	// Write the result part of the record.
	if r.err != internal.ErrNoError {
		resultStruct.Error.Put(result, r.err)
		return data
	}
	resultStruct.Encoding.Put(result, r.file.Encoding)
	resultStruct.Interpretation.Put(result, r.file.Interpretation)
	resultStruct.Form.Put(result, r.file.Form)
	if r.subsidiaryBoolTags.Exists() {
		resultStruct.SubsidiaryBoolTagFuncIndex.Put(result, r.subsidiaryBoolTagFuncIndex+1)
	}
	if r.subsidiaryTags.Exists() {
		resultStruct.SubsidiaryTagFuncIndex.Put(result, r.subsidiaryTagFuncIndex+1)
	}
	return data
}

func main() {
	if err := generate(); err != nil {
		log.Fatal(err)
	}
}

var top cue.Value

func generate() error {
	ctx := cuecontext.New()
	top = ctx.CompileString("_")
	rootVal := ctx.CompileString(typesCUE, cue.Filename("types.cue"))

	if err := generateToFile(rootVal); err != nil {
		return err
	}
	return nil
}

func generateToFile(rootVal cue.Value) error {
	count := 0
	errCount := 0
	tags, topLevelTags, _ := allTags(rootVal)

	results := slices.Collect(allCombinations(rootVal, topLevelTags, tags))
	subsidiaryTagsByCUE := make(map[string]cueValue)
	subsidiaryTagKeysMap := make(map[string]bool)
	subsidiaryBoolTagsByCUE := make(map[string]cueValue)
	subsidiaryBoolTagKeysMap := make(map[string]bool)
	for i, r := range results {
		if v := r.subsidiaryBoolTags; v.Exists() {
			results[i].subsidiaryBoolTagFuncIndex = addCUELogic(v, subsidiaryBoolTagsByCUE, subsidiaryBoolTagKeysMap)
		}
		if v := r.subsidiaryTags; v.Exists() {
			results[i].subsidiaryTagFuncIndex = addCUELogic(v, subsidiaryTagsByCUE, subsidiaryTagKeysMap)
		}
	}
	subsidiaryBoolTagKeys := slices.Sorted(maps.Keys(subsidiaryBoolTagKeysMap))
	subsidiaryTagKeys := slices.Sorted(maps.Keys(subsidiaryTagKeysMap))
	toFileParams := newToFileParamsStruct(
		topLevelTags,
		// Note: add ".unknown" as a proxy for any unknown file extension,
		// and make sure that the empty file extension is also present
		// even though it's not mentioned in the extensions struct.
		append(allKeys[string](rootVal, "all", "extensions"), ".unknown", ""),
	)
	toFileResult := newToFileResultStruct(
		append(allKeys[build.Encoding](rootVal, "all", "encodings"), ""),
		append(allKeys[build.Interpretation](rootVal, "all", "interpretations"), ""),
		append(allKeys[build.Form](rootVal, "all", "forms"), ""),
		len(subsidiaryBoolTagsByCUE),
		len(subsidiaryTagsByCUE),
	)

	tagTypes := make(map[string]filetypes.TagType)
	for name, info := range tags {
		tagTypes[name] = info.typ
	}
	params := tmplParams{
		ToFileParams:               toFileParams,
		ToFileResult:               toFileResult,
		TagTypes:                   tagTypes,
		Data:                       "fileInfoDataBytes",
		SubsidiaryBoolTagFuncCount: len(subsidiaryBoolTagsByCUE),
		SubsidiaryTagFuncCount:     len(subsidiaryTagsByCUE),
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, params); err != nil {
		return err
	}

	// Now generate the subsidiary tag logic; we generate
	// a type for each class of subsidiary tag, containing all the possible
	// tags for that class. Then we generate a function for each
	// distinct piece of CUE logic that implements that logic
	// in Go.
	genfunc.GenerateGoTypeForFields(&buf, "subsidiaryTags", subsidiaryTagKeys, "string")
	genfunc.GenerateGoTypeForFields(&buf, "subsidiaryBoolTags", subsidiaryBoolTagKeys, "bool")

	for _, k := range slices.Sorted(maps.Keys(subsidiaryTagsByCUE)) {
		v := subsidiaryTagsByCUE[k]
		genfunc.GenerateGoFuncForCUEStruct(&buf, fmt.Sprintf("unifySubsidiaryTags_%d", v.index), "subsidiaryTags", v.v, subsidiaryTagKeys, "string")
	}

	for _, k := range slices.Sorted(maps.Keys(subsidiaryBoolTagsByCUE)) {
		v := subsidiaryBoolTagsByCUE[k]
		genfunc.GenerateGoFuncForCUEStruct(&buf, fmt.Sprintf("unifySubsidiaryBoolTags_%d", v.index), "subsidiaryBoolTags", v.v, subsidiaryBoolTagKeys, "bool")
	}

	data, err := goformat.Source(buf.Bytes())
	if err != nil {
		if err := os.WriteFile("types_gen.go", buf.Bytes(), 0o666); err != nil {
			return err
		}
		return fmt.Errorf("malformed source in types_gen.go: %v", err)
	}
	if err := os.WriteFile("types_gen.go", data, 0o666); err != nil {
		return err
	}

	var recordData []byte
	for _, r := range results {
		count++
		if r.err != internal.ErrNoError {
			errCount++
		}
		recordData = r.appendRecord(recordData, toFileParams, toFileResult)
	}
	genstruct.SortRecords(recordData, toFileParams.Size()+toFileResult.Size(), toFileParams.Size())
	if err := os.WriteFile("fileinfo.dat", recordData, 0o666); err != nil {
		return err
	}
	return nil
}

// cueValue holds a CUE value and an index that will be used
// to distinguish that value in the generated source.
type cueValue struct {
	v     cue.Value
	index int
}

// addCUELogic records the given CUE value as something that
// we will need to generate Go logic for into byCUE,
// and also adds any struct fields into keys.
//
// It returns the index recorded for the logic.
func addCUELogic(v cue.Value, byCUE map[string]cueValue, keys map[string]bool) int {
	data, err := format.Node(v.Syntax(cue.Raw()))
	if err != nil {
		panic(fmt.Errorf("cannot format CUE: %v", err))
	}
	for name := range structFields(v, cue.Optional(true)) {
		keys[name] = true
	}
	src := string(data)
	if v, ok := byCUE[src]; ok {
		return v.index
	}
	index := len(byCUE)
	byCUE[src] = cueValue{
		v:     v,
		index: index,
	}
	return index
}

func allCombinations(rootVal cue.Value, topLevelTags []string, tagInfo map[string]tagInfo) iter.Seq[fileResult] {
	return func(yield func(fileResult) bool) {
		var filenames []string
		for ext := range structFields(lookup(rootVal, "modes", "input", "extensions")) {
			filename := ext
			if filename != "-" {
				filename = "x" + filename
			}
			filenames = append(filenames, filename)
		}
		filenames = append(filenames, "x.unknown", "withoutextension")

		for r := range tagCombinations(top, topLevelTags, tagInfo) {
			for mode, modeVal := range structFields(lookup(rootVal, "modes")) {
				fileVal := r.fileVal.Unify(lookup(modeVal, "FileInfo"))
				for _, filename := range filenames {
					r.mode = mode
					r.filename = filename
					r.file, r.fileVal, r.err = toFile1(modeVal, fileVal, filename)
					r.subsidiaryBoolTags = lookup(r.fileVal, "boolTags")
					r.subsidiaryTags = lookup(r.fileVal, "tags")
					if !yield(r) {
						return
					}
				}
			}
		}
	}
}

func toFile1(modeVal, fileVal cue.Value, filename string) (*build.File, cue.Value, internal.ErrorKind) {
	if !lookup(fileVal, "encoding").Exists() {
		if ext := fileExt(filename); ext != "" {
			extFile := lookup(modeVal, "extensions", ext)
			if !extFile.Exists() {
				return nil, cue.Value{}, internal.ErrUnknownFileExtension
			}
			fileVal = fileVal.Unify(extFile)
		} else {
			return nil, cue.Value{}, internal.ErrNoEncodingSpecified
		}
	}

	// Note that the filename is only filled in the Go value, and not the CUE value.
	// This makes no difference to the logic, but saves a non-trivial amount of evaluator work.
	f := &build.File{Filename: filename}
	if err := fileVal.Decode(f); err != nil {
		return nil, cue.Value{}, internal.ErrCouldNotDetermineFileType
	}
	return f, fileVal, internal.ErrNoError
}

// allTags returns all tags that can be used and their types;
// It also returns slices of the top level and subsidiary tag names.
func allTags(rootVal cue.Value) (_ map[string]tagInfo, topLevel []string, subsid []string) {
	tags := make(map[string]tagInfo)
	add := func(name string, typ filetypes.TagType, v cue.Value) {
		if other, ok := tags[name]; ok {
			if typ != other.typ {
				panic("tag redefinition")
			}
			return
		}
		info := tagInfo{
			name:  name,
			typ:   typ,
			value: v,
		}
		if typ == filetypes.TagTopLevel {
			topLevel = append(topLevel, name)
		} else {
			subsid = append(subsid, name)
		}
		tags[name] = info
	}
	addSubsidiary := func(v cue.Value) {
		for tagName := range structFields(lookup(v, "boolTags")) {
			add(tagName, filetypes.TagSubsidiaryBool, cue.Value{})
		}
		for tagName := range structFields(lookup(v, "tags")) {
			add(tagName, filetypes.TagSubsidiaryString, cue.Value{})
		}
	}
	for tagName, v := range structFields(lookup(rootVal, "tagInfo")) {
		add(tagName, filetypes.TagTopLevel, v)
		addSubsidiary(v)
	}
	addSubsidiary(lookup(rootVal, "interpretations"))
	addSubsidiary(lookup(rootVal, "forms"))
	sort.Strings(topLevel)
	sort.Strings(subsid)
	return tags, topLevel, subsid
}

func tagCombinations(initial cue.Value, topLevelTags []string, tagInfo map[string]tagInfo) iter.Seq[fileResult] {
	return func(yield func(fileResult) bool) {
		if len(topLevelTags) > 64 {
			// As we use uint64 to represent the tag space, if there
			// are more than 64 of them, our logic fails. In the future
			// we could use a more sophisticated bitset representation.
			// As of the time of writing, there are only 20, so there's reasonable
			// headroom for now.
			panic(fmt.Errorf("too many tags (%d, max %d)", len(topLevelTags), 64))
		}
		type bitsValue struct {
			bits uint64
			v    cue.Value
		}
		evaluate := func(v bitsValue, tagIndex int, _ int) (bitsValue, bool) {
			v.v = v.v.Unify(tagInfo[topLevelTags[tagIndex]].value)
			v.bits |= 1 << tagIndex
			return v, v.v.Validate() == nil
		}

		for v := range walkSpace(len(topLevelTags), 1, bitsValue{0, initial}, evaluate) {
			if !yield(fileResult{
				bits:    v.bits,
				fileVal: v.v,
				tags:    topLevelTags,
			}) {
				return
			}
		}
	}
}

func (ts fileResult) String() string {
	if ts.bits == 0 {
		return "<none>"
	}
	var buf strings.Builder
	for i, tag := range ts.tags {
		if ts.bits&(1<<i) != 0 {
			if buf.Len() > 0 {
				buf.WriteByte('|')
			}
			buf.WriteString(tag)
		}
	}
	return buf.String()
}

func (ts fileResult) Compare(ts1 fileResult) int {
	return cmp.Compare(ts.String(), ts1.String())
}

// structFields returns an iterator over the names of all the fields
// in v and their values.
func structFields(v cue.Value, opts ...cue.Option) iter.Seq2[string, cue.Value] {
	return func(yield func(string, cue.Value) bool) {
		if !v.Exists() {
			return
		}
		iter, err := v.Fields(opts...)
		if err != nil {
			return
		}
		for iter.Next() {
			if !yield(iter.Selector().Unquoted(), iter.Value()) {
				break
			}
		}
	}
}

func newToFileParamsStruct(topLevelTags, fileExts []string) *genToFileParams {
	r := &genToFileParams{}
	r.Mode = genstruct.AddInt(&r.Struct, filetypes.NumModes, "Mode")
	// Note: "" is a member of the set: we'll default to that if the extension isn't
	// part of the known set.
	r.FileExt = genstruct.AddEnum(&r.Struct, fileExts, ".unknown", "allFileExts", "string", nil)
	r.Tags = genstruct.AddSet(&r.Struct, topLevelTags, "allTopLevelTags")
	return r
}

type genToFileParams struct {
	genstruct.Struct
	Tags    genstruct.Accessor[iter.Seq[string]]
	FileExt genstruct.Accessor[string]
	Mode    genstruct.Accessor[filetypes.Mode]
}

type genToFileResult struct {
	genstruct.Struct
	Encoding       genstruct.Accessor[build.Encoding]
	Interpretation genstruct.Accessor[build.Interpretation]
	Form           genstruct.Accessor[build.Form]

	// Note: the indexes below are one more than the actual index
	// so that we can use the zero value to communicate "no tags".
	SubsidiaryTagFuncIndex     genstruct.Accessor[int]
	SubsidiaryBoolTagFuncIndex genstruct.Accessor[int]

	Error genstruct.Accessor[internal.ErrorKind]
}

func newToFileResultStruct(
	encodings []build.Encoding,
	interpretations []build.Interpretation,
	forms []build.Form,
	subsidiaryBoolTagFuncCount int,
	subsidiaryTagFuncCount int,
) *genToFileResult {
	r := &genToFileResult{}
	r.Encoding = genstruct.AddEnum(&r.Struct, encodings, "", "allEncodings", "build.Encoding", nil)
	r.Interpretation = genstruct.AddEnum(&r.Struct, interpretations, "", "allInterpretations", "build.Interpretation", nil)
	r.Form = genstruct.AddEnum(&r.Struct, forms, "", "allForms", "build.Form", nil)
	r.Error = genstruct.AddInt(&r.Struct, internal.NumErrorKinds, "internal.ErrorKind")
	r.SubsidiaryTagFuncIndex = genstruct.AddInt(&r.Struct, subsidiaryTagFuncCount, "int")
	r.SubsidiaryBoolTagFuncIndex = genstruct.AddInt(&r.Struct, subsidiaryBoolTagFuncCount, "int")

	return r
}

type dimspace[V any] struct {
	evaluate      func(v V, dim, item int) (V, bool)
	numDimensions int
	numValues     int
	yield         func(V) bool
}

// walkSpace explores the values that are possible to reach from the given initial
// value within the given number of dimensions (numDimentions), where each point in space
// can have the given number of possible item values (numItems).
// It calls evaluate to derive further values as it walks the space, and
// truncates the tree whereever evaluate returns false.
//
// Note that evaluate will always be called with arguments in the range [0, numDimensions)
// and [0, numItems].
//
// Note also that this exploration relies on the property that evaluate is commutative;
// that is, for a given point in the space, the result does not depend on the path
// taken to reach that point.
func walkSpace[V any](numDimensions, numValues int, initial V, evaluate func(v V, dim, item int) (V, bool)) iter.Seq[V] {
	return func(yield func(V) bool) {
		b := &dimspace[V]{
			evaluate:      evaluate,
			numDimensions: numDimensions,
			numValues:     numValues,
			yield:         yield,
		}
		b.walk(initial, 0)
	}
}

func (b *dimspace[V]) walk(v V, maxDim int) bool {
	if !b.yield(v) {
		return false
	}
	for i := maxDim; i < b.numDimensions; i++ {
		for j := range b.numValues {
			if v1, ok := b.evaluate(v, i, j); ok {
				if !b.walk(v1, i+1) {
					return false
				}
			}
		}
	}
	return true
}

func keys[K, V any](seq iter.Seq2[K, V]) iter.Seq[K] {
	return func(yield func(K) bool) {
		for k := range seq {
			if !yield(k) {
				return
			}
		}
	}
}

func lookup(v cue.Value, elems ...string) cue.Value {
	sels := make([]cue.Selector, len(elems))
	for i := range elems {
		sels[i] = cue.Str(elems[i])
	}
	return v.LookupPath(cue.MakePath(sels...))
}

func allKeys[T ~string](v cue.Value, elems ...string) []T {
	return slices.Sorted(
		seqMap(keys(structFields(lookup(v, elems...))), func(s string) T {
			return T(s)
		}),
	)
}

func fileExt(f string) string {
	if f == "-" {
		return "-"
	}
	e := filepath.Ext(f)
	if e == "" || e == filepath.Base(f) {
		return ""
	}
	return e
}

func seqMap[T1, T2 any](it iter.Seq[T1], f func(T1) T2) iter.Seq[T2] {
	return func(yield func(T2) bool) {
		for t := range it {
			if !yield(f(t)) {
				return
			}
		}
	}
}
