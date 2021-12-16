package cuedata

import (
	"log"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/literal"
	"cuelang.org/go/cue/parser"
	"cuelang.org/go/cue/token"
)

type Decoder struct {
}

func NewDecoder() *Decoder {
	return &Decoder{}
}

func (d *Decoder) RewriteFile(file *ast.File) error {
	var dec decoder
	file.Decls = dec.rewriteDecls(file.Decls)
	return dec.errs
}

type decoder struct {
	errs errors.Error
}

func (d *decoder) addErr(err errors.Error) {
	d.errs = errors.Append(d.errs, err)
}

func (d *decoder) addErrf(p token.Pos, schema cue.Value, format string, args ...interface{}) {
	format = "%s: " + format
	args = append([]interface{}{schema.Path()}, args...)
	d.addErr(errors.Newf(p, format, args...))
}

func (e *decoder) debug(format string, v ...interface{}) {
	if false {
		log.Printf(format, v...)
	}
}

func (d *decoder) rewriteDecls(decls []ast.Decl) (newDecls []ast.Decl) {
	d.debug("decoder.rewriteDecls()")
	newDecls = []ast.Decl{}
	for _, decl := range decls {
		switch dec := decl.(type) {
		case *ast.Field:
			sel := cue.Label(dec.Label)
			switch expr := dec.Value.(type) {
			case *ast.BasicLit:
				if sel.String() == syntaxLabel {
					s := expr.Value
					s, _ = literal.Unquote(s)
					f, err := parser.ParseFile("", s)
					if err != nil {
						d.addErr(errors.Wrapf(err, dec.Pos(), ""))
					} else {
						for _, inline := range f.Decls {
							newDecls = append(newDecls, inline)
						}
					}
					continue
				}
			case *ast.StructLit:
				expr.Elts = d.rewriteDecls(expr.Elts)
			}
			newDecls = append(newDecls, dec)
		default:
			newDecls = append(newDecls, dec)
		}
	}
	return newDecls
}
