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

	mode               string
	fileVal            cue.Value
	filename           string
	file               *build.File
	subsidiaryTags     cue.Value
	subsidiaryBoolTags cue.Value
	err                errorKind

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

	results := slices.Collect(allCombinations(rootVal, topLevelTags, tags))
	subsidiaryTagsByCUE := make(map[string]cue.Value)
	subsidiaryTagKeys := make(map[string]bool)
	//subsidiaryTagValues := make(map[string]bool)
	subsidiaryBoolTagsByCUE := make(map[string]cue.Value)
	subsidiaryBoolTagKeys := make(map[string]bool)
	for _, r := range results {
		if v := r.subsidiaryBoolTags; v.Exists() {
			data, err := format.Node(v.Syntax())
			if err != nil {
				return err
			}
			subsidiaryBoolTagsByCUE[string(data)] = v
			for name := range structFields(v, cue.Optional(true)) {
				subsidiaryBoolTagKeys[name] = true
			}
		}
		if v := r.subsidiaryTags; v.Exists() {
			data, err := format.Node(v.Syntax())
			if err != nil {
				return err
			}
			subsidiaryTagsByCUE[string(data)] = v
			for name := range structFields(v, cue.Optional(true)) {
				subsidiaryTagKeys[name] = true
				// TODO add values to subsidiaryTagValues
			}
		}
	}
	log.Printf("subsidiaryTags: %d variants; keys %v", len(subsidiaryTagsByCUE), subsidiaryTagKeys)
	for c := range subsidiaryTagsByCUE {
		log.Printf("- %s", c)
	}
	log.Printf("subsidiaryBoolTags: %d variants; keys %v", len(subsidiaryBoolTagsByCUE), subsidiaryBoolTagKeys)
	for c := range subsidiaryBoolTagsByCUE {
		log.Printf("- %s", c)
	}

	var recordData []byte
	for _, r := range results {
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

		for r := range tagCombinations(top, topLevelTags, tagInfo) {
			for mode, modeVal := range structFields(lookup(rootVal, "modes")) {
				r.fileVal = r.fileVal.Unify(lookup(modeVal, "FileInfo"))
				r.subsidiaryBoolTags = lookup(r.fileVal, "boolTags")
				r.subsidiaryTags = lookup(r.fileVal, "tags")
				r.mode = mode
				for _, filename := range filenames {
					r.filename = filename
					r.file, r.err = toFile1(modeVal, r.fileVal, filename)
					if !yield(r) {
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
	r.FileExt = genstruct.AddEnum(&r.Struct, fileExts, "", "allFileExts", "string", nil)
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
