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
	"strings"
	"text/template"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/build"
	"cuelang.org/go/cue/cuecontext"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/format"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/internal/filetypes"
	"cuelang.org/go/internal/filetypes/internal"
	"cuelang.org/go/internal/filetypes/internal/genfunc"
	"cuelang.org/go/internal/filetypes/internal/genstruct"
)

type tmplParams struct {
	TagTypes                   map[string]filetypes.TagType
	ToFileDepParams            *genToFileDepParams
	ToFileIndepParams          *genToFileIndepParams
	ToFileResult               *genToFileResult
	ToFileResultIndex          *genResultIndex
	FromFileParams             *genFromFileParams
	FromFileResult             *genFromFileResult
	SubsidiaryBoolTagFuncCount int
	SubsidiaryTagFuncCount     int
	// Generated is used by the generation code to avoid
	// generating the same global identifier twice.
	Generated map[string]bool
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

// resultBytes returns the encoded result record for r, as referenced by index
// from the extension-dependent and extension-independent lookup tables.
func (r *fileResult) resultBytes(resultStruct *genToFileResult) []byte {
	result := make([]byte, resultStruct.Size())
	if r.err != internal.ErrNoError {
		resultStruct.Error.Put(result, r.err)
		return result
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
	return result
}

// modeFromString converts a mode name as used in types.cue to its [filetypes.Mode] value.
func modeFromString(s string) filetypes.Mode {
	switch s {
	case "input":
		return filetypes.Input
	case "export":
		return filetypes.Export
	case "def":
		return filetypes.Def
	case "eval":
		return filetypes.Eval
	default:
		panic(fmt.Errorf("unknown mode %q", s))
	}
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

	toFile, err := generateToFile(rootVal)
	if err != nil {
		return err
	}
	fromFile, err := generateFromFile(rootVal)
	if err != nil {
		return err
	}
	if err := generateCode(toFile, fromFile); err != nil {
		return err
	}
	return nil
}

// toFileInfo holds the information needed to generate the toFile implementation code.
type toFileInfo struct {
	depParams               *genToFileDepParams
	indepParams             *genToFileIndepParams
	resultStruct            *genToFileResult
	resultIndex             *genResultIndex
	tagTypes                map[string]filetypes.TagType
	subsidiaryBoolTagsByCUE map[string]cueValue
	subsidiaryTagsByCUE     map[string]cueValue
	subsidiaryBoolTagKeys   []string
	subsidiaryTagKeys       []string
}

// fromFileInfo holds the information needed to generate the fromFile implementation code.
type fromFileInfo struct {
	paramsStruct *genFromFileParams
	resultStruct *genFromFileResult
}

func generateToFile(rootVal cue.Value) (toFileInfo, error) {
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
	// Note: add ".unknown" as a proxy for any unknown file extension,
	// and make sure that the empty file extension is also present
	// even though it's not mentioned in the extensions struct.
	fileExts := append(allKeys[string](rootVal, "all", "extensions"), ".unknown", "")
	toFileResult := newToFileResultStruct(
		append(allKeys[build.Encoding](rootVal, "all", "encodings"), ""),
		append(allKeys[build.Interpretation](rootVal, "all", "interpretations"), ""),
		append(allKeys[build.Form](rootVal, "all", "forms"), ""),
		len(subsidiaryBoolTagsByCUE),
		len(subsidiaryTagsByCUE),
	)

	// Intern the result records. The cross product of all combinations yields
	// many thousands of records, but only a couple hundred distinct results, so
	// we store each distinct result once and reference it by index.
	var resultsData []byte
	resultIndexByBytes := make(map[string]int)
	resultIndexOf := func(r *fileResult) int {
		b := r.resultBytes(toFileResult)
		if i, ok := resultIndexByBytes[string(b)]; ok {
			return i
		}
		i := len(resultIndexByBytes)
		resultIndexByBytes[string(b)] = i
		resultsData = append(resultsData, b...)
		return i
	}

	// Group results by (mode, tag set). The file extension only affects the
	// result when the tags do not already determine an encoding, so for most
	// groups every extension yields the same result.
	type groupKey struct {
		mode filetypes.Mode
		bits uint64
	}
	type extResult struct {
		ext   string
		index int
	}
	type groupInfo struct {
		tags    []string
		results []extResult
	}
	groups := make(map[groupKey]*groupInfo)
	for i := range results {
		r := &results[i]
		gk := groupKey{mode: modeFromString(r.mode), bits: r.bits}
		gi := groups[gk]
		if gi == nil {
			gi = &groupInfo{tags: r.tags}
			groups[gk] = gi
		}
		gi.results = append(gi.results, extResult{ext: fileExt(r.filename), index: resultIndexOf(r)})
	}

	depParams := newToFileDepParamsStruct(topLevelTags, fileExts)
	indepParams := newToFileIndepParamsStruct(topLevelTags)
	resultIndex := newResultIndexStruct(len(resultIndexByBytes))

	// Emit one record per group into the extension-independent table when every
	// extension in the group shares the same result; otherwise emit one record
	// per extension into the extension-dependent table.
	// The emission order does not matter: SortRecords below imposes a total
	// order on each table by its (distinct) key.
	var depData, indepData []byte
	for gk, gi := range groups {
		extIndependent := true
		for _, er := range gi.results[1:] {
			if er.index != gi.results[0].index {
				extIndependent = false
				break
			}
		}
		if extIndependent {
			indepData = appendIndepRecord(indepData, indepParams, resultIndex, gk.mode, gi.tags, gk.bits, gi.results[0].index)
		} else {
			for _, er := range gi.results {
				depData = appendDepRecord(depData, depParams, resultIndex, gk.mode, gi.tags, gk.bits, er.ext, er.index)
			}
		}
	}

	genstruct.SortRecords(depData, depParams.Size()+resultIndex.Size(), depParams.Size())
	genstruct.SortRecords(indepData, indepParams.Size()+resultIndex.Size(), indepParams.Size())
	if err := os.WriteFile("fileinfo_dep.dat", depData, 0o666); err != nil {
		return toFileInfo{}, err
	}
	if err := os.WriteFile("fileinfo_indep.dat", indepData, 0o666); err != nil {
		return toFileInfo{}, err
	}
	if err := os.WriteFile("fileinfo_results.dat", resultsData, 0o666); err != nil {
		return toFileInfo{}, err
	}

	tagTypes := make(map[string]filetypes.TagType)
	for name, info := range tags {
		tagTypes[name] = info.typ
	}

	return toFileInfo{
		depParams:               depParams,
		indepParams:             indepParams,
		resultStruct:            toFileResult,
		resultIndex:             resultIndex,
		tagTypes:                tagTypes,
		subsidiaryBoolTagsByCUE: subsidiaryBoolTagsByCUE,
		subsidiaryTagsByCUE:     subsidiaryTagsByCUE,
		subsidiaryBoolTagKeys:   subsidiaryBoolTagKeys,
		subsidiaryTagKeys:       subsidiaryTagKeys,
	}, nil
}

// growRecord extends data by a record of keySize+valSize bytes and returns the
// grown slice together with sub-slices addressing the new record's key and
// value parts.
func growRecord(data []byte, keySize, valSize int) (grown, key, val []byte) {
	recordSize := keySize + valSize
	data = slices.Grow(data, recordSize)
	data = data[:len(data)+recordSize]
	record := data[len(data)-recordSize:]
	return data, slices.Clip(record[:keySize]), slices.Clip(record[keySize:])
}

// appendDepRecord appends an extension-dependent lookup record, keyed by
// (mode, file extension, tag set), referencing the result at resultIdx.
func appendDepRecord(data []byte, p *genToFileDepParams, idx *genResultIndex, mode filetypes.Mode, tags []string, bits uint64, ext string, resultIdx int) []byte {
	data, key, val := growRecord(data, p.Size(), idx.Size())
	p.Mode.Put(key, mode)
	p.FileExt.Put(key, ext)
	p.Tags.Put(key, genstruct.ElemsFromBits(bits, tags))
	idx.Index.Put(val, resultIdx)
	return data
}

// appendIndepRecord appends an extension-independent lookup record, keyed by
// (mode, tag set), referencing the result at resultIdx.
func appendIndepRecord(data []byte, p *genToFileIndepParams, idx *genResultIndex, mode filetypes.Mode, tags []string, bits uint64, resultIdx int) []byte {
	data, key, val := growRecord(data, p.Size(), idx.Size())
	p.Mode.Put(key, mode)
	p.Tags.Put(key, genstruct.ElemsFromBits(bits, tags))
	idx.Index.Put(val, resultIdx)
	return data
}

func generateCode(
	toFile toFileInfo,
	fromFile fromFileInfo,
) error {
	params := tmplParams{
		ToFileDepParams:            toFile.depParams,
		ToFileIndepParams:          toFile.indepParams,
		ToFileResult:               toFile.resultStruct,
		ToFileResultIndex:          toFile.resultIndex,
		FromFileParams:             fromFile.paramsStruct,
		FromFileResult:             fromFile.resultStruct,
		TagTypes:                   toFile.tagTypes,
		SubsidiaryBoolTagFuncCount: len(toFile.subsidiaryBoolTagsByCUE),
		SubsidiaryTagFuncCount:     len(toFile.subsidiaryTagsByCUE),
		Generated:                  make(map[string]bool),
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
	genfunc.GenerateGoTypeForFields(&buf, "subsidiaryTags", toFile.subsidiaryTagKeys, "string")
	genfunc.GenerateGoTypeForFields(&buf, "subsidiaryBoolTags", toFile.subsidiaryBoolTagKeys, "bool")

	for _, k := range slices.Sorted(maps.Keys(toFile.subsidiaryTagsByCUE)) {
		v := toFile.subsidiaryTagsByCUE[k]
		genfunc.GenerateGoFuncForCUEStruct(&buf, fmt.Sprintf("unifySubsidiaryTags_%d", v.index), "subsidiaryTags", v.v, toFile.subsidiaryTagKeys, "string")
	}

	for _, k := range slices.Sorted(maps.Keys(toFile.subsidiaryBoolTagsByCUE)) {
		v := toFile.subsidiaryBoolTagsByCUE[k]
		genfunc.GenerateGoFuncForCUEStruct(&buf, fmt.Sprintf("unifySubsidiaryBoolTags_%d", v.index), "subsidiaryBoolTags", v.v, toFile.subsidiaryBoolTagKeys, "bool")
	}

	data, err := goformat.Source(buf.Bytes())
	if err != nil {
		if err := os.WriteFile("types_gen.go", buf.Bytes(), 0o666); err != nil {
			return err
		}
		return fmt.Errorf("malformed source in types_gen.go:%v", err)
	}
	if err := os.WriteFile("types_gen.go", data, 0o666); err != nil {
		return err
	}
	return nil
}

func generateFromFile(rootVal cue.Value) (fromFileInfo, error) {
	allEncodings := append(allKeys[build.Encoding](rootVal, "all", "encodings"), "")
	allInterpretations := append(allKeys[build.Interpretation](rootVal, "all", "interpretations"), "")
	allForms := append(allKeys[build.Form](rootVal, "all", "forms"), "")
	paramsStruct := newFromFileParamsStruct(
		allEncodings,
		allInterpretations,
		allForms,
	)
	resultStruct := newFromFileResult(
		allEncodings,
		allInterpretations,
		allForms,
	)
	var recordData []byte
	for mode := range filetypes.NumModes {
		for _, encoding := range allEncodings {
			for _, interpretation := range allInterpretations {
				for _, form := range allForms {
					f := &build.File{
						Encoding:       encoding,
						Interpretation: interpretation,
						Form:           form,
					}
					fi, err := fromFileOrig(rootVal, f, mode)
					if err != nil {
						continue
					}
					recordData = appendFromFileRecord(recordData, paramsStruct, resultStruct, mode, f, fi)
				}
			}
		}
	}
	genstruct.SortRecords(recordData, paramsStruct.Size()+resultStruct.Size(), paramsStruct.Size())
	if err := os.WriteFile("fromfile.dat", recordData, 0o666); err != nil {
		return fromFileInfo{}, err
	}

	return fromFileInfo{
		paramsStruct: paramsStruct,
		resultStruct: resultStruct,
	}, nil
}

func appendFromFileRecord(
	data []byte,
	paramsStruct *genFromFileParams,
	resultStruct *genFromFileResult,
	mode filetypes.Mode,
	f *build.File,
	fi *filetypes.FileInfo,
) []byte {
	recordSize := paramsStruct.Size() + resultStruct.Size()
	data = slices.Grow(data, recordSize)
	data = data[:len(data)+recordSize]
	record := data[len(data)-recordSize:]

	// Write the key part of the record.
	param := slices.Clip(record[:paramsStruct.Size()])
	paramsStruct.Mode.Put(param, mode)
	paramsStruct.Encoding.Put(param, f.Encoding)
	paramsStruct.Interpretation.Put(param, f.Interpretation)
	paramsStruct.Form.Put(param, f.Form)

	result := slices.Clip(record[paramsStruct.Size():])
	resultStruct.Encoding.Put(result, fi.Encoding)
	resultStruct.Interpretation.Put(result, fi.Interpretation)
	resultStruct.Form.Put(result, fi.Form)
	resultStruct.Aspects.Put(result, fi.Aspects())
	return data
}

func fromFileOrig(rootVal cue.Value, b *build.File, mode filetypes.Mode) (*filetypes.FileInfo, error) {
	modeVal := lookup(rootVal, "modes", mode.String())
	fileVal := lookup(modeVal, "FileInfo")
	if b.Encoding == "" {
		return nil, errors.Newf(token.NoPos, "no encoding specified")
	}
	fileVal = fileVal.FillPath(cue.MakePath(cue.Str("encoding")), b.Encoding)
	if b.Interpretation != "" {
		fileVal = fileVal.FillPath(cue.MakePath(cue.Str("interpretation")), b.Interpretation)
	}
	if b.Form != "" {
		fileVal = fileVal.FillPath(cue.MakePath(cue.Str("form")), b.Form)
	}
	var errs errors.Error
	var interpretation string
	if b.Form != "" {
		fileVal, errs = unifyWith(errs, fileVal, rootVal, "forms", string(b.Form))
		if errs != nil {
			return nil, errs
		}
		interpretation, _ = lookup(fileVal, "interpretation").String()
		// may leave some encoding-dependent options open in data mode.
	} else {
		interpretation, _ = lookup(fileVal, "interpretation").String()
		if interpretation != "" {
			// always sets form=*schema
			fileVal, errs = unifyWith(errs, fileVal, rootVal, "interpretations", interpretation)
		}
	}
	if interpretation == "" {
		encoding, err := lookup(fileVal, "encoding").String()
		if err != nil {
			return nil, err
		}
		fileVal, errs = unifyWith(errs, fileVal, modeVal, "encodings", encoding)
	}

	fi := &filetypes.FileInfo{}
	if err := fileVal.Decode(fi); err != nil {
		return nil, errors.Wrapf(err, token.NoPos, "could not parse arguments")
	}
	fi.Filename = b.Filename
	return fi, errs
}

// unifyWith returns the equivalent of `v1 & v2[field][value]`.
func unifyWith(errs errors.Error, v1, v2 cue.Value, field, value string) (cue.Value, errors.Error) {
	v1 = v1.Unify(lookup(v2, field, value))
	if err := v1.Err(); err != nil {
		errs = errors.Append(errs,
			errors.Newf(token.NoPos, "unknown %s %s", field, value))
	}
	return v1, errs
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
	slices.Sort(topLevel)
	slices.Sort(subsid)
	return tags, topLevel, subsid
}

func tagCombinations(initial cue.Value, topLevelTags []string, tagInfo map[string]tagInfo) iter.Seq[fileResult] {
	return func(yield func(fileResult) bool) {
		if len(topLevelTags) > 64 {
			panic("too many tags")
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

type genFromFileParams struct {
	genstruct.Struct
	Mode           genstruct.Accessor[filetypes.Mode]
	Encoding       genstruct.Accessor[build.Encoding]
	Interpretation genstruct.Accessor[build.Interpretation]
	Form           genstruct.Accessor[build.Form]
}

type genFromFileResult struct {
	genstruct.Struct
	Encoding       genstruct.Accessor[build.Encoding]
	Interpretation genstruct.Accessor[build.Interpretation]
	Form           genstruct.Accessor[build.Form]
	Aspects        genstruct.Accessor[internal.Aspects]
}

func newFromFileParamsStruct(
	encodings []build.Encoding,
	interpretations []build.Interpretation,
	forms []build.Form,
) *genFromFileParams {
	r := &genFromFileParams{}
	r.Mode = genstruct.AddInt(&r.Struct, filetypes.NumModes, "Mode")
	r.Encoding = genstruct.AddEnum(&r.Struct, encodings, "", "allEncodings", "build.Encoding", nil)
	r.Interpretation = genstruct.AddEnum(&r.Struct, interpretations, "", "allInterpretations", "build.Interpretation", nil)
	r.Form = genstruct.AddEnum(&r.Struct, forms, "", "allForms", "build.Form", nil)
	return r
}

func newFromFileResult(
	encodings []build.Encoding,
	interpretations []build.Interpretation,
	forms []build.Form,
) *genFromFileResult {
	r := &genFromFileResult{}
	r.Encoding = genstruct.AddEnum(&r.Struct, encodings, "", "allEncodings", "build.Encoding", nil)
	r.Interpretation = genstruct.AddEnum(&r.Struct, interpretations, "", "allInterpretations", "build.Interpretation", nil)
	r.Form = genstruct.AddEnum(&r.Struct, forms, "", "allForms", "build.Form", nil)
	r.Aspects = genstruct.AddInt(&r.Struct, internal.AllAspects, "internal.Aspects")
	return r
}

// genToFileDepParams is the key layout for the extension-dependent lookup
// table: (mode, file extension, tag set).
type genToFileDepParams struct {
	genstruct.Struct
	Mode    genstruct.Accessor[filetypes.Mode]
	FileExt genstruct.Accessor[string]
	Tags    genstruct.Accessor[iter.Seq[string]]
}

func newToFileDepParamsStruct(topLevelTags, fileExts []string) *genToFileDepParams {
	r := &genToFileDepParams{}
	r.Mode = genstruct.AddInt(&r.Struct, filetypes.NumModes, "Mode")
	// Note: "" is a member of the set: we'll default to that if the extension isn't
	// part of the known set.
	r.FileExt = genstruct.AddEnum(&r.Struct, fileExts, ".unknown", "allFileExts", "string", nil)
	r.Tags = genstruct.AddSet(&r.Struct, topLevelTags, "allTopLevelTags")
	return r
}

// genToFileIndepParams is the key layout for the extension-independent lookup
// table: (mode, tag set).
type genToFileIndepParams struct {
	genstruct.Struct
	Mode genstruct.Accessor[filetypes.Mode]
	Tags genstruct.Accessor[iter.Seq[string]]
}

func newToFileIndepParamsStruct(topLevelTags []string) *genToFileIndepParams {
	r := &genToFileIndepParams{}
	r.Mode = genstruct.AddInt(&r.Struct, filetypes.NumModes, "Mode")
	r.Tags = genstruct.AddSet(&r.Struct, topLevelTags, "allTopLevelTags")
	return r
}

// genResultIndex is the value layout for both lookup tables: an index into the
// interned result records.
type genResultIndex struct {
	genstruct.Struct
	Index genstruct.Accessor[int]
}

func newResultIndexStruct(numResults int) *genResultIndex {
	r := &genResultIndex{}
	r.Index = genstruct.AddInt(&r.Struct, numResults-1, "int")
	return r
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
// value within the given number of dimensions (numDimensions), where each point in space
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
		seqMap(keys(structFields(lookup(v, elems...))), fromString[T]),
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

func fromString[T ~string](s string) T {
	return T(s)
}
