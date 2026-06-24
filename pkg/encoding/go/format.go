package encoding_go

import (
	"bytes"
	"crypto/rand"
	"fmt"
	goast "go/ast"
	goformat "go/format"
	goparser "go/parser"
	gotoken "go/token"
	"reflect"
	"strconv"
	"strings"
	"sync"

	"golang.org/x/tools/go/ast/astutil"

	"cuelang.org/go/cue"
)

var getPrefix = sync.OnceValue(func() string {
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		panic(err)
	}
	return fmt.Sprintf("X%x_", buf[:])
})

func Format(strs []cue.Value, args []cue.Value) (string, error) {
	prefix := getPrefix()
	var buf bytes.Buffer
	for i := range strs {
		// TODO bytes interpolations
		s, err := strs[i].String()
		if err != nil {
			return "", err
		}
		buf.WriteString(s)
		if i < len(args) {
			fmt.Fprintf(&buf, "%s%dX", prefix, i)
		}
	}
	fset := gotoken.NewFileSet()
	f, err := goparser.ParseFile(fset, "", buf.Bytes(), goparser.ParseComments)
	if err != nil {
		// TODO map syntax errors to source locations within the actual CUE string literal.
		return "", fmt.Errorf("cannot parse file %q as Go: %v", buf.Bytes(), err)
	}
	done := make([]bool, len(args))
	var walkErr error
	f = astutil.Apply(f, func(c *astutil.Cursor) bool {
		switch n := c.Node().(type) {
		case *goast.Ident:
			_, index, suffix, ok := splitID(n.Name)
			if !ok {
				break
			}
			if suffix != "" {
				walkErr = fmt.Errorf("additional text following interpolation in expression context")
				return false
			}
			if !nodeFits(c, basicLitType) {
				// The interpolation appears in a position that grammatically
				// requires an identifier (for example a declaration name), so
				// an arbitrary expression cannot be substituted here. Leave it
				// unreplaced; it is reported as an unexpected place below.
				break
			}
			n1, err := goExpr(args[index])
			if err != nil {
				walkErr = fmt.Errorf("cannot convert interpolation expression %d to Go expression: %v", index, err)
				return false
			}
			done[index] = true
			c.Replace(n1)
		case *goast.BasicLit:
			if n.Kind != gotoken.STRING {
				break
			}
			repl, err := substituteStringLit(n, args, done, nodeFits(c, binaryExprType))
			if err != nil {
				walkErr = err
				return false
			}
			if repl != goast.Expr(n) {
				c.Replace(repl)
			}
		}
		return true
	}, nil).(*goast.File)
	if walkErr != nil {
		return "", walkErr
	}
	for i, isDone := range done {
		if !isDone {
			return "", fmt.Errorf("interpolation %d in unexpected place: cannot replace", i)
		}
	}
	var rbuf strings.Builder
	if err := goformat.Node(&rbuf, fset, f); err != nil {
		return "", fmt.Errorf("cannot format: %v", err)
	}
	return rbuf.String(), nil
}

// substituteStringLit returns the Go expression that should replace the string
// literal lit once its interpolation placeholders have been substituted with
// the string forms of the corresponding operands, recording each in done.
//
// An interpreted (double-quoted) literal is rewritten in place, escaping each
// operand so that it survives inside the surrounding quotes, and lit is
// returned. A raw (back-quoted) literal keeps its operands verbatim where it
// can, but a Go raw string literal cannot contain a backquote and silently
// discards carriage returns, so when an operand introduces either the literal
// must be re-expressed:
//
//   - A single-line literal becomes an interpreted literal.
//   - A multi-line literal is split into a concatenation (joined with +) of raw
//     fragments and interpreted segments, preserving the newline-bearing raw
//     fragments; this is only done when the position accepts an arbitrary
//     expression (canBreak), otherwise the interpreted form is used.
func substituteStringLit(lit *goast.BasicLit, args []cue.Value, done []bool, canBreak bool) (goast.Expr, error) {
	if !strings.HasPrefix(lit.Value, "`") {
		value := lit.Value
		var buf strings.Builder
		for {
			before, index, after, ok := splitID(value)
			if !ok {
				break
			}
			buf.WriteString(before)
			s, err := toString(args[index])
			if err != nil {
				return nil, fmt.Errorf("cannot convert interpolation expression %d to string: %v", index, err)
			}
			q := strconv.Quote(s)
			buf.WriteString(q[1 : len(q)-1])
			done[index] = true
			value = after
		}
		buf.WriteString(value)
		lit.Value = buf.String()
		return lit, nil
	}

	// Raw string literal. Split it into the literal fragments and the operand
	// strings so we can decide how to re-assemble them. len(frags) == len(ops)+1.
	multiline := strings.Contains(lit.Value, "\n")
	inner := lit.Value[1 : len(lit.Value)-1]
	var frags, ops []string
	needsBreak := false
	for {
		before, index, after, ok := splitID(inner)
		if !ok {
			break
		}
		s, err := toString(args[index])
		if err != nil {
			return nil, fmt.Errorf("cannot convert interpolation expression %d to string: %v", index, err)
		}
		// A backquote cannot appear in a raw string literal, and a carriage
		// return is silently discarded by one, so such an operand cannot be
		// inserted verbatim.
		if strings.ContainsAny(s, "`\r") {
			needsBreak = true
		}
		frags = append(frags, before)
		ops = append(ops, s)
		done[index] = true
		inner = after
	}
	frags = append(frags, inner)

	if !needsBreak {
		lit.Value = "`" + joinFrags(frags, ops) + "`"
		return lit, nil
	}
	if !multiline || !canBreak {
		lit.Value = strconv.Quote(joinFrags(frags, ops))
		return lit, nil
	}

	// Build a concatenation that keeps the newline-bearing raw fragments raw and
	// breaks the offending operands out into interpreted segments.
	var segs []goast.Expr
	var raw strings.Builder
	flush := func() {
		if raw.Len() == 0 {
			return
		}
		segs = append(segs, &goast.BasicLit{Kind: gotoken.STRING, Value: "`" + raw.String() + "`"})
		raw.Reset()
	}
	for i, frag := range frags {
		raw.WriteString(frag)
		if i >= len(ops) {
			break
		}
		if s := ops[i]; strings.ContainsAny(s, "`\r") {
			flush()
			segs = append(segs, &goast.BasicLit{Kind: gotoken.STRING, Value: strconv.Quote(s)})
		} else {
			raw.WriteString(s)
		}
	}
	flush()
	expr := segs[0]
	for _, seg := range segs[1:] {
		expr = &goast.BinaryExpr{X: expr, Op: gotoken.ADD, Y: seg}
	}
	return expr, nil
}

// joinFrags interleaves the n+1 literal fragments with the n operand strings.
func joinFrags(frags, ops []string) string {
	var buf strings.Builder
	for i, frag := range frags {
		buf.WriteString(frag)
		if i < len(ops) {
			buf.WriteString(ops[i])
		}
	}
	return buf.String()
}

var (
	basicLitType   = reflect.TypeOf((*goast.BasicLit)(nil))
	binaryExprType = reflect.TypeOf((*goast.BinaryExpr)(nil))
)

// nodeFits reports whether the node at c may be replaced by a node of the given
// concrete type t. astutil.Cursor.Replace stores the new node into the parent's
// field by reflection and panics unless the value is assignable to that field's
// static type. Probing with basicLitType distinguishes expression positions
// from concrete-Ident positions (declaration names, labels, selectors), where
// the grammar allows only an identifier; probing with binaryExprType further
// distinguishes positions that accept an arbitrary expression from those typed
// as a concrete *goast.BasicLit (such as an import path).
func nodeFits(c *astutil.Cursor, t reflect.Type) bool {
	parent := reflect.ValueOf(c.Parent())
	for parent.Kind() == reflect.Pointer {
		parent = parent.Elem()
	}
	if parent.Kind() != reflect.Struct {
		return false
	}
	field := parent.FieldByName(c.Name())
	if !field.IsValid() {
		return false
	}
	ft := field.Type()
	if c.Index() >= 0 {
		ft = ft.Elem()
	}
	return t.AssignableTo(ft)
}

func goExpr(v cue.Value) (goast.Node, error) {
	switch kind := v.Kind(); kind {
	case cue.StringKind:
		x, err := v.String()
		if err != nil {
			return nil, err
		}
		return &goast.BasicLit{
			Kind:  gotoken.STRING,
			Value: strconv.Quote(x),
		}, nil
	case cue.BoolKind:
		x, err := v.Bool()
		if err != nil {
			return nil, err
		}
		return &goast.Ident{
			Name: fmt.Sprint(x),
		}, nil
	case cue.IntKind:
		return &goast.BasicLit{
			Kind:  gotoken.INT,
			Value: fmt.Sprint(v),
		}, nil
	case cue.FloatKind:
		return &goast.BasicLit{
			Kind:  gotoken.FLOAT,
			Value: fmt.Sprint(v),
		}, nil
	default:
		return nil, fmt.Errorf("cannot form Go syntax for %v", v)
	}
}

func toString(v cue.Value) (string, error) {
	switch kind := v.Kind(); kind {
	case cue.StringKind:
		return v.String()
	case cue.BoolKind, cue.IntKind, cue.FloatKind:
		return fmt.Sprint(v), nil
	default:
		return "", fmt.Errorf("unsupported value kind %v", kind)
	}
}

func splitID(s string) (before string, index int, after string, ok bool) {
	s1, s2, ok := strings.Cut(s, getPrefix())
	if !ok {
		return "", 0, "", false
	}
	id, s2, ok := strings.Cut(s2, "X")
	if !ok {
		panic(fmt.Errorf("no terminator found for interpolation %q (s2 %q) (shouldn't happen)", s, s2))
	}
	index, err := strconv.Atoi(id)
	if err != nil || index < 0 {
		panic(fmt.Errorf("invalid index in interpolation string %q", s))
	}
	return s1, index, s2, true
}
