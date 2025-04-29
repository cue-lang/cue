package genfunc

import (
	"bytes"
	_ "embed"
	"fmt"
	"iter"
	"strings"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/format"
	"cuelang.org/go/cue/token"
)

// GenerateGoTypeForFields writes to buf a definition for a Go struct type named
// structName with the given field names, all of which are of the same member type.
// It also generates unmarshalFromMap to marshalToMap methods to convert to
// and from map[string]memberType values.
func GenerateGoTypeForFields(buf *bytes.Buffer, structName string, fields []string, memberType string) {
	fmt.Fprintf(buf, "type %s struct {\n", structName)
	for _, name := range fields {
		fmt.Fprintf(buf, "\t%s opt[%s]\n", name, memberType)
	}
	fmt.Fprintf(buf, "}\n")

	emitf(buf, "func (t *%s) unmarshalFromMap(m map[string] %s) error {", structName, memberType)
	for _, name := range fields {
		emitf(buf, "if x, ok := m[%q]; ok {", name)
		emitf(buf, "\tt.%s = some(x)", name)
		emitf(buf, "}")
	}
	// TODO error when a key isn't allowed
	emitf(buf, "return nil")
	emitf(buf, "}")
	emitf(buf, "func (t %s) marshalToMap() map[string] %s {", structName, memberType)
	emitf(buf, "\tm := make(map[string] %s)", memberType)
	for _, name := range fields {
		emitf(buf, "if t.%s.IsPresent() {", name)
		emitf(buf, "\tm[%[1]q] = t.%[1]s.Value()", name)
		emitf(buf, "}")
	}
	emitf(buf, "return m")
	emitf(buf, "}")
}

// GenerateGoFuncForCUEStruct writes to buf a function definition that will unify a Go struct
// as defined by [GenerateGoTypeForFields] with the given CUE value, of the form:
//
//	func funcName(t typeName) (typeName, error)
//
// It only understands an extremely limited subset of CUE, as driven by the usage
// inside internal/filetypes/types.cue:
// - no cyclic dependencies
// - only a single scalar type for all fields in the struct
// - all fields are known ahead of time
//
// and many more restrictions. It should fail when the CUE falls outside those restrictions.
func GenerateGoFuncForCUEStruct(buf *bytes.Buffer, funcName, structName string, e cue.Value, keys []string, typeName string) {
	g := &funcGenerator{
		buf:        buf,
		structName: structName,
		generated:  make(map[string]bool),
		generating: make(map[string]bool),
		scope:      make(map[string]ast.Expr),
		typeName:   typeName,
	}
	for name, v := range structFields(e) {
		//log.Printf("syntax for %s: %s", name, dump(v.Syntax(cue.Raw())))
		g.scope[name] = simplify(v.Syntax(cue.Raw()).(ast.Expr))
	}
	g.emitf("// %s unifies %s values according to the following CUE logic:", funcName, structName)
	g.emitf("// %s", strings.ReplaceAll(dump(e.Syntax(cue.Raw())), "\n", "\n// "))
	g.emitf("func %[1]s(t %[2]s) (%[2]s, error) {", funcName, g.structName)
	g.emitf("\tvar r %s", g.structName)
	for _, name := range keys {
		g.generateField(name)
	}
	g.emitf("\treturn r, nil")
	g.emitf("}")
}

type funcGenerator struct {
	structName string
	scope      map[string]ast.Expr
	buf        *bytes.Buffer
	generated  map[string]bool
	generating map[string]bool
	typeName   string
}

func (g *funcGenerator) generateField(fieldName string) {
	// TODO fail when there's a recursive dependency.
	if g.generated[fieldName] {
		return
	}
	g.generated[fieldName] = true
	if g.generating[fieldName] {
		// Recursive reference.
		g.emitf("error: recursive reference to field %v", fieldName)
		return
	}
	g.generating[fieldName] = true
	defer func() {
		delete(g.generating, fieldName)
	}()
	x := g.scope[fieldName]
	if x == nil {
		g.emitf("if t.%s.IsPresent() {", fieldName)
		g.emitf("\treturn %s{}, fmt.Errorf(\"field %%q not allowed\", %q)", g.structName, fieldName)
		g.emitf("}")
		return
	}
	var binExpr *ast.BinaryExpr
	var unaryExpr *ast.UnaryExpr
	var ident *ast.Ident

	switch {
	case isLiteral(x):
		g.emitf("r.%s = some(%s)", fieldName, dump(x))
		g.emitf("if t.%[1]s.IsPresent() && t.%[1]s.Value() != r.%[1]s.Value() {", fieldName)
		g.emitf("\treturn %[1]s{}, fmt.Errorf(\"conflict on %s; %%#v provided but need %%#v\", t.%[2]s.Value(), r.%[2]s.Value())", g.structName, fieldName)
		g.emitf("}")
	case match(x, &binExpr) && binExpr.Op == token.OR &&
		match(binExpr.X, &unaryExpr) && unaryExpr.Op == token.MUL &&
		match(binExpr.Y, &ident) && ident.Name == g.typeName:
		// *reference | bool

		g.emitf("r.%s = %s", fieldName, g.exprFor(unaryExpr.X))
		g.emitf("if t.%s.IsPresent() {", fieldName)
		g.emitf("\tr.%s = t.%s", fieldName, fieldName)
		g.emitf("}")
	default:
		data, err := format.Node(x)
		if err != nil {
			panic(err)
		}
		g.emitf("error: cannot cope with field %s = %s", fieldName, data)
	}
}

func (g *funcGenerator) exprFor(e ast.Expr) string {
	var binExpr *ast.BinaryExpr
	var ident *ast.Ident
	switch {
	case match(e, &ident) && g.scope[ident.Name] != nil:
		// Ensure that we've evaluated the field we're referring
		// to before we use it.
		g.generateField(ident.Name)
		return fmt.Sprintf("r.%s", ident.Name)
	case isLiteral(e):
		return fmt.Sprintf("some(%s)", dump(e))
	case match(e, &binExpr) && binExpr.Op == token.AND:
		switch {
		case g.isTypeName(binExpr.Y):
			// foo & bool
			return g.exprFor(binExpr.X)
		case g.isTypeName(binExpr.X):
			// bool & foo
			return g.exprFor(binExpr.Y)
		}
	}
	return fmt.Sprintf("error: cannot build expr for %s", dump(e))
}

func (g *funcGenerator) isTypeName(x ast.Expr) bool {
	var ident *ast.Ident
	return match(x, &ident) && ident.Name == g.typeName
}

func emitf(buf *bytes.Buffer, f string, a ...any) {
	fmt.Fprintf(buf, f, a...)
	if !strings.HasSuffix(f, "\n") {
		buf.WriteByte('\n')
	}
}

func (g *funcGenerator) emitf(f string, a ...any) {
	emitf(g.buf, f, a...)
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
