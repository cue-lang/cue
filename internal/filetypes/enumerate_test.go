package filetypes

import (
	"sort"
	"strings"
	"testing"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
)

type tagInfo struct {
	name  string
	value cue.Value
}

func TestEnumerate(t *testing.T) {
	ctx := cuecontext.New()
	v := ctx.CompileString(typesCUE, cue.Filename("types.cue"))
	tagInfoV := v.LookupPath(cue.MakePath(cue.Str("tagInfo")))

	var tags []tagInfo
	iter, err := tagInfoV.Fields()
	if err != nil {
		t.Fatal(err)
	}
	for iter.Next() {
		tags = append(tags, tagInfo{
			name:  iter.Selector().Unquoted(),
			value: iter.Value(),
		})
	}
	if len(tags) > 64 {
		t.Fatal("too many tags")
	}
	e := &tagEnumerator{
		tags: tags,
	}

	e.walk(ctx.CompileString("_"), 0, 0)
	names := []string{}
	for _, tag := range e.found {
		names = append(names, e.bitString(tag.bits))
	}
	sort.Strings(names)
	t.Logf("got %d combinations:\n%s", len(e.found), strings.Join(names, "\n"))
}

type tags struct {
	bits uint64
	val  cue.Value
}

type tagEnumerator struct {
	tags  []tagInfo
	found []tags
}

func (e *tagEnumerator) walk(v cue.Value, bits uint64, maxBit int) {
	for i := maxBit; i < len(e.tags); i++ {
		bit := uint64(1) << i
		v1 := v.Unify(e.tags[i].value)
		if err := v1.Validate(); err != nil {
			continue
		}

		foundBits := bits | bit
		e.found = append(e.found, tags{
			bits: foundBits,
			val:  v1,
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
