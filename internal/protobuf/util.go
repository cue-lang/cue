// Copyright 2019 CUE Authors
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

package protobuf

import (
	"strings"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/token"
	"github.com/emicklei/proto"
	"golang.org/x/xerrors"
)

// failf panics with a marked error that can be intercepted upon returning
// from parsing.
func failf(format string, args ...interface{}) {
	panic(protoError{xerrors.Errorf(format, args...)})
}

func fail(err error) {
	panic(protoError{err})
}

type protoError struct {
	error
}

var (
	newline    = token.Pos(token.Newline)
	newSection = token.Pos(token.NewSection)
)

func addComments(f ast.Node, i int, doc, inline *proto.Comment) bool {
	cg := comment(doc, true)
	if cg != nil && len(cg.List) > 0 && i > 0 {
		cg.List[0].Slash = newSection
	}
	f.AddComment(cg)
	f.AddComment(comment(inline, false))
	return doc != nil
}

func comment(c *proto.Comment, doc bool) *ast.CommentGroup {
	if c == nil || len(c.Lines) == 0 {
		return nil
	}
	cg := &ast.CommentGroup{}
	if doc {
		cg.Doc = true
	} else {
		cg.Line = true
		cg.Position = 10
	}
	for _, s := range c.Lines {
		cg.List = append(cg.List, &ast.Comment{Text: "// " + s})
	}
	return cg
}

func quote(s string) string {
	if !strings.ContainsAny(s, `"\`) {
		return s
	}
	esc := `\#`
	for strings.Contains(s, esc) {
		esc += "#"
	}
	return esc[1:] + `"` + s + `"` + esc[1:]
}

func labelName(s string) string {
	split := strings.Split(s, "_")
	for i := 1; i < len(split); i++ {
		split[i] = strings.Title(split[i])
	}
	return strings.Join(split, "")
}
