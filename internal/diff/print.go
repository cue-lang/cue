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

package diff

import (
	"fmt"
	"io"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/errors"
)

// Print the differences between two structs represented by an edit script.
func Print(w io.Writer, es *EditScript) error {
	p := printer{
		w:       w,
		margin:  2,
		context: 2,
	}
	p.script(es)
	return p.errs
}

type printer struct {
	w         io.Writer
	context   int
	margin    int
	indent    int
	prefix    string
	hasPrefix bool
	hasPrint  bool
	errs      errors.Error
}

func (p *printer) writeRaw(b []byte) {
	if len(b) == 0 {
		return
	}
	if !p.hasPrefix {
		io.WriteString(p.w, p.prefix)
		p.hasPrefix = true
	}
	if !p.hasPrint {
		fmt.Fprintf(p.w, "% [1]*s", p.indent+p.margin-len(p.prefix), "")
		p.hasPrint = true
	}
	p.w.Write(b)
}

func (p *printer) Write(b []byte) (n int, err error) {
	i, last := 0, 0
	for ; i < len(b); i++ {
		if b[i] != '\n' {
			continue
		}
		p.writeRaw(b[last:i])
		last = i + 1
		io.WriteString(p.w, "\n")
		p.hasPrefix = false
		p.hasPrint = false
	}
	p.writeRaw(b[last:])
	return len(b), nil
}

func (p *printer) write(b []byte) {
	_, _ = p.Write(b)
}

func (p *printer) printLen(align int, str string) {
	fmt.Fprintf(p, "% -[1]*s", align, str)
}

func (p *printer) println(s string) {
	fmt.Fprintln(p, s)
}

func (p *printer) printf(format string, args ...interface{}) {
	fmt.Fprintf(p, format, args...)
}

func (p *printer) script(e *EditScript) {
	switch e.x.Kind() {
	case cue.StructKind:
		p.printStruct(e)
	case cue.ListKind:
		p.printList(e)
	default:
		p.printElem("-", e.x)
		p.printElem("+", e.y)
	}
}

func (p *printer) findRun(es *EditScript, i int) (start, end int) {
	lastEnd := i

	for ; i < es.Len() && es.edits[i].kind == Identity; i++ {
	}
	start = i

	// Find end of run
	include := p.context
	for ; i < es.Len(); i++ {
		e := es.edits[i]
		if e.kind != Identity {
			include = p.context + 1
			continue
		}
		if include--; include == 0 {
			break
		}
	}

	if i-start > 0 {
		// Adjust start of run
		if s := start - p.context; s > lastEnd {
			start = s
		} else {
			start = lastEnd
		}
	}
	return start, i
}

func (p *printer) printStruct(es *EditScript) {
	// TODO: consider not printing outer curlies, or make it an option.
	// if p.indent > 0 {
	p.println("{")
	defer p.println("}")
	// }
	p.indent += 4
	defer func() {
		p.indent -= 4
	}()

	var start, i int
	for i < es.Len() {
		lastEnd := i
		// Find provisional start of run.
		start, i = p.findRun(es, i)

		p.printSkipped(start - lastEnd)
		p.printFieldRun(es, start, i)
	}
	p.printSkipped(es.Len() - i)
}

func (p *printer) printList(es *EditScript) {
	p.println("[")
	p.indent += 4
	defer func() {
		p.indent -= 4
		p.println("]")
	}()

	x := getElems(es.x)
	y := getElems(es.y)

	var start, i int
	for i < es.Len() {
		lastEnd := i
		// Find provisional start of run.
		start, i = p.findRun(es, i)

		p.printSkipped(start - lastEnd)
		p.printElemRun(es, x, y, start, i)
	}
	p.printSkipped(es.Len() - i)
}

func getElems(x cue.Value) (a []cue.Value) {
	for i, _ := x.List(); i.Next(); {
		a = append(a, i.Value())
	}
	return a
}

func (p *printer) printSkipped(n int) {
	if n > 0 {
		p.printf("... // %d identical elements\n", n)
	}
}

func (p *printer) printValue(v cue.Value) {
	// TODO: have indent option.
	s := fmt.Sprintf("%+v", v)
	io.WriteString(p, s)
}

func (p *printer) printFieldRun(es *EditScript, start, end int) {
	// Determine max field len.
	for i := start; i < end; i++ {
		e := es.edits[i]

		switch e.kind {
		case UniqueX:
			p.printField("-", es, es.LabelX(i), es.ValueX(i))

		case UniqueY:
			p.printField("+", es, es.LabelY(i), es.ValueY(i))

		case Modified:
			if e.sub != nil {
				io.WriteString(p, es.LabelX(i))
				io.WriteString(p, " ")
				p.script(e.sub)
				break
			}
			// TODO: show per-line differences for multiline strings.
			p.printField("-", es, es.LabelX(i), es.ValueX(i))
			p.printField("+", es, es.LabelY(i), es.ValueY(i))

		case Identity:
			// TODO: write on one line
			p.printField("", es, es.LabelX(i), es.ValueX(i))
		}
	}
}

func (p *printer) printField(prefix string, es *EditScript, label string, v cue.Value) {
	p.prefix = prefix
	io.WriteString(p, label)
	io.WriteString(p, " ")
	p.printValue(v)
	io.WriteString(p, "\n")
	p.prefix = ""
}

func (p *printer) printElemRun(es *EditScript, x, y []cue.Value, start, end int) {
	for _, e := range es.edits[start:end] {
		switch e.kind {
		case UniqueX:
			p.printElem("-", x[e.XPos()])

		case UniqueY:
			p.printElem("+", y[e.YPos()])

		case Modified:
			if e.sub != nil {
				p.script(e.sub)
				break
			}
			// TODO: show per-line differences for multiline strings.
			p.printElem("-", x[e.XPos()])
			p.printElem("+", y[e.YPos()])

		case Identity:
			// TODO: write on one line
			p.printElem("", x[e.XPos()])
		}
	}
}

func (p *printer) printElem(prefix string, v cue.Value) {
	p.prefix = prefix
	p.printValue(v)
	io.WriteString(p, ",\n")
	p.prefix = ""
}
