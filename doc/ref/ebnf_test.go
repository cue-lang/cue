package ref

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
	"reflect"
	"strings"
	"testing"
	"text/tabwriter"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/text"
	"golang.org/x/exp/ebnf"
)

// TestEBNF ensures that the CUE spec contains valid ebnf
func TestEBNF(t *testing.T) {
	src, err := ioutil.ReadFile("spec.md")
	if err != nil {
		t.Fatal(err)
	}
	md := goldmark.New(goldmark.WithRendererOptions(html.WithUnsafe()))
	reader := text.NewReader(src)
	doc := md.Parser().Parse(reader)
	spec := new(bytes.Buffer)
	ast.Walk(doc, func(n ast.Node, entering bool) (res ast.WalkStatus, err error) {
		res = ast.WalkContinue
		if !entering {
			return
		}
		switch n := n.(type) {
		case *ast.FencedCodeBlock:
			var info string
			if n.Info != nil {
				info = string(n.Info.Text(src))
			}
			if info != "ebnf" {
				return
			}
			for i := 0; i < n.Lines().Len(); i++ {
				line := n.Lines().At(i)
				fmt.Fprintf(spec, "%s", line.Value(src))
			}
		}
		return
	})
	if testing.Verbose() {
		tw := tabwriter.NewWriter(os.Stdout, 0, 0, 1, ' ', tabwriter.AlignRight)
		lines := strings.Split(spec.String(), "\n")
		for i, line := range lines {
			fmt.Fprintf(tw, "%v:\t %s\n", i+1, line)
		}
		if err := tw.Flush(); err != nil {
			t.Fatal(err)
		}
	}
	grammar, err := ebnf.Parse("spec", spec)
	if err != nil {
		t.Fatal(err)
	}
	err = ebnf.Verify(grammar, "SourceFile")
	if err != nil {
		var buf bytes.Buffer
		fmt.Fprintln(&buf, "errors:")
		errs := reflect.ValueOf(err)
		for i := 0; i < errs.Len(); i++ {
			fmt.Fprintf(&buf, "%v\n", errs.Index(i))
		}
		t.Fatal(buf.String())
	}
}
