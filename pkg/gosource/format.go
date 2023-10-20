package gosource

import (
	"bytes"
	"crypto/rand"
	"fmt"
	"go/ast"
	goast "go/ast"
	goformat "go/format"
	goparser "go/parser"
	gotoken "go/token"
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
		case *ast.Ident:
			// TODO we're being too general here. We can't always substitute an expression value
			// anywhere an identifier is allowed: we should only hit this case in non-l-value context.

			_, index, suffix, ok := splitID(n.Name)
			if !ok {
				break
			}
			if suffix != "" {
				walkErr = fmt.Errorf("additional text following interpolation in expression context")
				return false
			}
			n1, err := goExpr(args[index])
			if err != nil {
				walkErr = fmt.Errorf("cannot convert interpolation expression %d to Go expression: %v", index, err)
				return false
			}
			done[index] = true
			c.Replace(n1)
		case *ast.BasicLit:
			if n.Kind != gotoken.STRING {
				break
			}
			var buf strings.Builder
			lit := n.Value
			for {
				before, index, after, ok := splitID(lit)
				if !ok {
					break
				}
				buf.WriteString(before)
				s, err := toString(args[index])
				if err != nil {
					walkErr = fmt.Errorf("cannot convert interpolation expression %d to string: %v", index, err)
					return false
				}
				if strings.HasPrefix(n.Value, "`") {
					if strings.Contains(s, "`") {
						walkErr = fmt.Errorf("TODO support escaping backquotes inside backquotes (str %q)", s)
						return false
					}
					buf.WriteString(s)
				} else {
					q := strconv.Quote(s)
					buf.WriteString(q[1 : len(q)-1])
				}
				lit = after
				done[index] = true
			}
			buf.WriteString(lit)
			// TODO avoid the allocation when there's no splitting to be done.
			n.Value = buf.String()
		}
		return true
	}, nil).(*ast.File)
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
