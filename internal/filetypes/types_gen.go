//go:build !bootstrap

package filetypes

import (
	_ "embed"
	"fmt"
	"log"
	"maps"
	"slices"
	"sync"

	"cuelang.org/go/cue/build"
	"cuelang.org/go/internal/filetypes/internal/genstruct"
)

//go:embed fileinfo.dat
var fileInfoDataBytes []byte

func init() {
	tagTypes = map[string]TagType{
		"auto":           TagTopLevel,
		"binary":         TagTopLevel,
		"code":           TagTopLevel,
		"cue":            TagTopLevel,
		"dag":            TagTopLevel,
		"data":           TagTopLevel,
		"go":             TagTopLevel,
		"graph":          TagTopLevel,
		"json":           TagTopLevel,
		"jsonl":          TagTopLevel,
		"jsonschema":     TagTopLevel,
		"lang":           TagSubsidiaryString,
		"openapi":        TagTopLevel,
		"pb":             TagTopLevel,
		"proto":          TagTopLevel,
		"schema":         TagTopLevel,
		"strict":         TagSubsidiaryBool,
		"strictFeatures": TagSubsidiaryBool,
		"strictKeywords": TagSubsidiaryBool,
		"text":           TagTopLevel,
		"textproto":      TagTopLevel,
		"toml":           TagTopLevel,
		"yaml":           TagTopLevel,
	}
}

var errInvalidTagCombination = fmt.Errorf("invalid tag combination")

var (
	allFileExts = []string{
		"-",
		".cue",
		".go",
		".json",
		".jsonl",
		".ldjson",
		".ndjson",
		".proto",
		".textpb",
		".textproto",
		".toml",
		".txt",
		".wasm",
		".yaml",
		".yml",
		"",
	}
	allFileExts_rev = genstruct.IndexMap(allFileExts)
)
var (
	allTopLevelTags = []string{
		"auto",
		"binary",
		"code",
		"cue",
		"dag",
		"data",
		"go",
		"graph",
		"json",
		"jsonl",
		"jsonschema",
		"openapi",
		"pb",
		"proto",
		"schema",
		"text",
		"textproto",
		"toml",
		"yaml",
	}
	allTopLevelTags_rev = genstruct.IndexMap(allTopLevelTags)
)

var (
	allEncodings = []build.Encoding{
		"binary",
		"binarypb",
		"code",
		"cue",
		"json",
		"jsonl",
		"proto",
		"text",
		"textproto",
		"toml",
		"yaml",
		"",
	}
	allEncodings_rev = genstruct.IndexMap(allEncodings)
)
var (
	allInterpretations = []build.Interpretation{
		"auto",
		"jsonschema",
		"openapi",
		"pb",
		"",
	}
	allInterpretations_rev = genstruct.IndexMap(allInterpretations)
)
var (
	allForms = []build.Form{
		"dag",
		"data",
		"final",
		"graph",
		"schema",
		"",
	}
	allForms_rev = genstruct.IndexMap(allForms)
)
var (
	allSubsidiaryTags = []string{
		"lang",
		"strict",
		"strictFeatures",
		"strictKeywords",
	}
	allSubsidiaryTags_rev = genstruct.IndexMap(allSubsidiaryTags)
)

func toFileGenerated(mode Mode, sc *scope, filename string) (*build.File, error) {
	dumpOnce.Do(dumpData)
	key := make([]byte, 5)
	genstruct.PutSet(key, 2, 3, allTopLevelTags_rev, maps.Keys(sc.topLevel))
	genstruct.PutEnum(key, 1, 1, allFileExts_rev, 15, fileExt(filename))
	genstruct.PutUint64(key, 0, 1, uint64(mode))

	data, ok := genstruct.FindRecord(fileInfoDataBytes, 5+5, key)
	if !ok {
		return nil, errInvalidTagCombination // TODO what error would be best?
	}

	switch e := int(genstruct.GetUint64(data, 4, 1)); e {
	default:
		return nil, fmt.Errorf("unknown error %d", e)
	case 1, 2, 3:
		return nil, fmt.Errorf("error %d", e)
	case 0:
		// no error
	}

	var f build.File
	f.Filename = filename
	f.Encoding = genstruct.GetEnum(data, 0, 1, allEncodings)
	f.Interpretation = genstruct.GetEnum(data, 1, 1, allInterpretations)
	f.Form = genstruct.GetEnum(data, 2, 1, allForms)
	// TODO check allowed tags
	return &f, nil
}

var dumpOnce sync.Once

func dumpData() {
	return
	recSize := 5 + 5
	i := 0
	for data := fileInfoDataBytes; len(data) > 0; data, i = data[recSize:], i+1 {
		data := data[:recSize:recSize]
		key := data[:5]
		//val := data[len(key):]
		tags := slices.Collect(genstruct.GetSet(key, 2, 3, allTopLevelTags))
		fileExt := genstruct.GetEnum(key, 1, 1, allFileExts)
		mode := Mode(genstruct.GetUint64(key, 0, 1))
		log.Printf("key %d: mode %v; fileExt %q; tags %q; %v", i, mode, fileExt, tags, key)
	}
}
