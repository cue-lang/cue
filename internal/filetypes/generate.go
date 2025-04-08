//go:build ignore

package main

import (
	"cmp"
	_ "embed"
	"fmt"
	"iter"
	"log"
	"slices"
	"strings"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/internal/filetypes"
)

var (
	encodings = []string{
		"cue",
		"json",
		"yaml",
		"toml",
		"jsonl",
		"text",
		"binary",
		"proto",
		"textproto",
		"pb",
		"code",
	}

	interpretations = []string{
		"auto",
		"jsonschema",
		"openapi",
		"pb",
	}

	forms = []string{
		"full",
		"schema",
		"struct",
		"final",
		"graph",
		"dag",
		"data",
	}
)

//type field struct {
//	o0 int
//	o1 int
//}
//
//// generated code:
//
////go:embed fileinfo.dat
//var fileInfoDataBytes []byte
//
//var fileInfoData = func() *big.Int {
//	var n big.Int
//	n.SetBytes(fileInfoDataBytes)
//}()
//
//
//const (
//	stringsData =  "cuejsonyaml..."
//)
//
//var encodingOffets = []int{
//	// offset into strings data for a given encoding value
//}
//
//func fileInfoForTags(tags []string) (_ *FileInfo, unused  []string, _ error) {
//	make bitmask from all the tags in _tags_, adding any unrecognised ones to the `unused` slice
//	offset := binary search for that bitmask in the bits->index map
//	var f *FileInfo
//	setString(&f.Encoding, offset, stringsData[encodingOffsets[getnum(offset, offset+encodingBitlen)]]
//	f.Encoding = build.Encoding(stringsData[encodingOffset[getnum(offset, offset+encodingBitlen)]])
//}
//
//func setString[T ~string](dst *T, o0, o1 int, table []int) {
//	n := 0
//	for i := o0; i < o1; i++ {
//		n |= fileInfoData.Bit(i) << i
//	}
//	*dst = T(stringsData[table[n*2]:table[n*2+1]])
//}
//
//func setBool(dst *bool, o int) {
//	if fileInfoData.Bit(o) != 0 {
//		*dst = true
//	}
//}
//

//go:embed types.cue
var typesCUE string

type tagInfo struct {
	name  string
	value cue.Value
}

type tags struct {
	bits uint64
	val  cue.Value
	info filetypes.FileInfo
}

type tagEnumerator struct {
	count int
	tags  []tagInfo
	found []tags

	search uint64
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
	v := ctx.CompileString(typesCUE, cue.Filename("types.cue"))
	tagInfoV := v.LookupPath(cue.MakePath(cue.Str("tagInfo")))

	for name, mode := range structFields(v.LookupPath(cue.MakePath(cue.Str("modes")))) {
		e, err := tagCombinations(mode, tagInfoV)
		if err != nil {
			return nil
		}

		slices.SortFunc(e.found, func(t1, t2 tags) int {
			return cmp.Compare(e.bitString(t1.bits), e.bitString(t2.bits))
		})

		fmt.Printf("%d bits; mode %s; %d searched; got %d combinations {\n", len(e.tags), name, e.count, len(e.found))
		//		for _, t := range e.found {
		//			fmt.Printf("%s: {\n", e.bitString(t.bits))
		//			fmt.Printf("\tinfo %#v\n", t.info)
		//			fmt.Printf("\tfile: %#v\n", t.info.File)
		//			fmt.Printf("}\n")
		//		}
		break
	}
	return nil
}

func tagCombinations(root, tagInfoV cue.Value) (*tagEnumerator, error) {
	var tags []tagInfo
	byName := make(map[string]int)
	for name, v := range structFields(tagInfoV) {
		byName[name] = len(tags)
		tags = append(tags, tagInfo{
			name:  name,
			value: v,
		})
	}
	if len(tags) > 64 {
		return nil, fmt.Errorf("too many tags")
	}
	e := &tagEnumerator{
		tags: tags,
	}
	e.search = e.bitsFor("jsonschema")
	log.Printf("searchFor %#v", e.search)

	e.walk(root, 0, 0)
	return e, nil
}

func (e *tagEnumerator) bitsFor(tags ...string) uint64 {
	r := uint64(0)
	for _, name := range tags {
		found := -1
		for i, t := range e.tags {
			if t.name == name {
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

func (e *tagEnumerator) walk(v cue.Value, bits uint64, maxBit int) {
	for i := maxBit; i < len(e.tags); i++ {
		e.count++
		bit := uint64(1) << i
		v1 := v.Unify(e.tags[i].value)
		if err := v1.Validate(); err != nil {
			if (bits | bit) == e.search {
				log.Printf("error: %v", errors.Details(err, nil))
			}
			continue
		}

		foundBits := bits | bit
		var info filetypes.FileInfo
		if err := v1.Decode(&info); err != nil {
			panic(err)
		}
		e.found = append(e.found, tags{
			bits: foundBits,
			val:  v1,
			info: info,
		})
		e.walk(v1, foundBits, i+1)
	}
}

func (e *tagEnumerator) bitString(x uint64) string {
	if x == 0 {
		return "<none>"
	}
	var buf strings.Builder
	for i, tag := range e.tags {
		if x&(1<<i) != 0 {
			if buf.Len() > 0 {
				buf.WriteByte('|')
			}
			buf.WriteString(tag.name)
		}
	}
	return buf.String()
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
