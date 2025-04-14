//go:build ignore

package main

import (
	"bytes"
	"cmp"
	_ "embed"
	"fmt"
	"go/format"
	"iter"
	"log"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"text/template"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/build"
	"cuelang.org/go/cue/cuecontext"
	"cuelang.org/go/internal/filetypes"
	"cuelang.org/go/internal/filetypes/internal/genstruct"
)

type tmplParams struct {
	TagTypes     map[string]filetypes.TagType
	ToFileParams *genToFileParams
	ToFileResult *genToFileResult
	Data         string
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

	mode           string
	fileVal        cue.Value
	filename       string
	file           *build.File
	subsidiaryTags map[string]bool
	err            errorKind

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
	if r.err != errNoError {
		resultStruct.Error.Put(result, r.err)
		return data
	}
	resultStruct.Encoding.Put(result, r.file.Encoding)
	resultStruct.Interpretation.Put(result, r.file.Interpretation)
	resultStruct.Form.Put(result, r.file.Form)
	return data
}

type errorKind int

const (
	errNoError errorKind = iota
	errUnknownFileExtension
	errCouldNotDetermineFileType
	errNoEncodingSpecified
	numErrors
)

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
	count := 0
	errCount := 0
	tags, topLevelTags, subsidiaryTags := allTags(rootVal)

	tagTypes := make(map[string]filetypes.TagType)
	for name, info := range tags {
		tagTypes[name] = info.typ
	}
	toFileParams := newToFileParamsStruct(
		topLevelTags,
		append(allKeys[string](rootVal, "all", "extensions"), ""),
	)
	toFileResult := newToFileResultStruct(
		append(allKeys[build.Encoding](rootVal, "all", "encodings"), ""),
		append(allKeys[build.Interpretation](rootVal, "all", "interpretations"), ""),
		append(allKeys[build.Form](rootVal, "all", "forms"), ""),
		subsidiaryTags,
	)
	params := tmplParams{
		ToFileParams: toFileParams,
		ToFileResult: toFileResult,
		TagTypes:     tagTypes,
		Data:         "fileInfoDataBytes",
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, params); err != nil {
		return err
	}
	data, err := format.Source(buf.Bytes())
	if err != nil {
		fmt.Fprintf(os.Stderr, "malformed source:\n%s\n", buf.Bytes())
		return fmt.Errorf("malformed source: %v", err)
	}
	if err := os.WriteFile("types_gen.go", data, 0o666); err != nil {
		return err
	}

	var recordData []byte
	for r := range allCombinations(rootVal, topLevelTags, tags) {
		count++
		if r.err != errNoError {
			errCount++
		}
		recordData = r.appendRecord(recordData, toFileParams, toFileResult)
	}
	genstruct.SortRecords(recordData, toFileParams.Size()+toFileResult.Size(), toFileParams.Size())
	fmt.Printf("got %d possibilities; %d errors\n", count, errCount)
	fmt.Printf("recordData length %d; record length %d\n", len(recordData), toFileParams.Size()+toFileResult.Size())
	if err := os.WriteFile("fileinfo.dat", recordData, 0o666); err != nil {
		return err
	}

	//fmt.Printf("mode %s; got %d combinations {\n", name, len(found))
	//		for _, t := range e.found {
	//			fmt.Printf("%s: {\n", e.bitString(t.bits))
	//			fmt.Printf("\tinfo %#v\n", t.info)
	//			fmt.Printf("\tfile: %#v\n", t.info.File)
	//			fmt.Printf("}\n")
	//		}
	return nil
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
		filenames = append(filenames, "other")

		for tags := range tagCombinations(top, topLevelTags, tagInfo) {
			for mode, modeVal := range structFields(lookup(rootVal, "modes")) {
				tags.fileVal = tags.fileVal.Unify(lookup(modeVal, "FileInfo"))
				tags.subsidiaryTags = make(map[string]bool)
				for tagName := range structFields(lookup(tags.fileVal, "tags")) {
					tags.subsidiaryTags[tagName] = true
				}
				for tagName := range structFields(lookup(tags.fileVal, "boolTags")) {
					tags.subsidiaryTags[tagName] = true
				}
				for _, filename := range filenames {
					tags1 := tags
					tags1.mode = mode
					tags1.filename = filename
					tags1.file, tags1.err = toFile1(modeVal, tags.fileVal, filename)
					if !yield(tags1) {
						return
					}
				}
			}
		}
	}
}

func toFile1(modeVal, fileVal cue.Value, filename string) (*build.File, errorKind) {
	if !lookup(fileVal, "encoding").Exists() {
		if ext := fileExt(filename); ext != "" {
			extFile := lookup(modeVal, "extensions", ext)
			if !extFile.Exists() {
				return nil, errUnknownFileExtension
			}
			fileVal = fileVal.Unify(extFile)
		} else {
			return nil, errNoEncodingSpecified
		}
	}

	// Note that the filename is only filled in the Go value, and not the CUE value.
	// This makes no difference to the logic, but saves a non-trivial amount of evaluator work.
	f := &build.File{Filename: filename}
	if err := fileVal.Decode(f); err != nil {
		return nil, errCouldNotDetermineFileType
	}
	return f, errNoError
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
			panic("too many tags")
		}
		e := &tagEnumerator{
			tags:    topLevelTags,
			tagInfo: tagInfo,
			yield:   yield,
		}

		e.walk(initial, 0, 0)
	}
}

type tagEnumerator struct {
	count   int
	tags    []string
	tagInfo map[string]tagInfo
	yield   func(fileResult) bool
}

func (e *tagEnumerator) bitsFor(tags ...string) uint64 {
	r := uint64(0)
	for _, name := range tags {
		found := -1
		for i, t := range e.tags {
			if t == name {
				found = i
			}
		}
		if found == -1 {
			panic(name + " not found")
		}
		r |= 1 << found
	}
	return r
}

func (e *tagEnumerator) walk(v cue.Value, bits uint64, maxBit int) bool {
	if !e.yield(fileResult{
		bits:    bits,
		fileVal: v,
		tags:    e.tags,
	}) {
		return false
	}
	for i := maxBit; i < len(e.tags); i++ {
		e.count++
		bit := uint64(1) << i
		current := bits | bit
		v1 := v.Unify(e.tagInfo[e.tags[i]].value)
		if err := v1.Validate(); err != nil {
			continue
		}
		if !e.walk(v1, current, i+1) {
			return false
		}
	}
	return true
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
func structFields(v cue.Value) iter.Seq2[string, cue.Value] {
	return func(yield func(string, cue.Value) bool) {
		if !v.Exists() {
			return
		}
		iter, err := v.Fields()
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

type genToFileParams struct {
	genstruct.Struct
	Tags    genstruct.Accessor[iter.Seq[string]]
	FileExt genstruct.Accessor[string]
	Mode    genstruct.Accessor[filetypes.Mode]
}

func newToFileParamsStruct(topLevelTags, fileExts []string) *genToFileParams {
	r := &genToFileParams{}
	r.Mode = genstruct.AddInt(&r.Struct, filetypes.NumModes, "Mode")
	// Note: "" is a member of the set: we'll default to that if the extension isn't
	// part of the known set.
	r.FileExt = genstruct.AddEnum(&r.Struct, fileExts, "", "allFileExts", "string", nil)
	r.Tags = genstruct.AddSet(&r.Struct, topLevelTags, "allTopLevelTags")
	return r
}

type genToFileResult struct {
	genstruct.Struct
	Encoding       genstruct.Accessor[build.Encoding]
	Interpretation genstruct.Accessor[build.Interpretation]
	Form           genstruct.Accessor[build.Form]
	SubsidiaryTags genstruct.Accessor[iter.Seq[string]]
	Error          genstruct.Accessor[errorKind]
}

func newToFileResultStruct(
	encodings []build.Encoding,
	interpretations []build.Interpretation,
	forms []build.Form,
	subsidiaryTags []string,
) *genToFileResult {
	r := &genToFileResult{}
	r.Encoding = genstruct.AddEnum(&r.Struct, encodings, "", "allEncodings", "build.Encoding", nil)
	r.Interpretation = genstruct.AddEnum(&r.Struct, interpretations, "", "allInterpretations", "build.Interpretation", nil)
	r.Form = genstruct.AddEnum(&r.Struct, forms, "", "allForms", "build.Form", nil)
	r.SubsidiaryTags = genstruct.AddSet(&r.Struct, subsidiaryTags, "allSubsidiaryTags")
	r.Error = genstruct.AddInt(&r.Struct, numErrors, "int")

	return r
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
