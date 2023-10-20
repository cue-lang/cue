package sh

import (
	"bytes"
	"crypto/rand"
	"fmt"
	"strconv"
	"strings"
	"sync"

	"cuelang.org/go/cue"
	shsyntax "mvdan.cc/sh/v3/syntax"
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
	p := shsyntax.NewParser(shsyntax.KeepComments(true))
	f, err := p.Parse(&buf, "")
	if err != nil {
		return "", fmt.Errorf("cannot parse shell syntax: %v", err)
	}
	qkind := make([]quoteKind, len(args))
	var visit func(n shsyntax.Node) bool
	visit = func(n shsyntax.Node) bool {
		switch n := n.(type) {
		case *shsyntax.Lit:
			if n == nil {
				panic("nil lit!")
			}
			if _, index, _, ok := splitID(n.Value); ok {
				qkind[index] = unquoted
			}
		case *shsyntax.SglQuoted:
			if _, index, _, ok := splitID(n.Value); ok {
				qkind[index] = sglQuoted
			}
		case *shsyntax.DblQuoted:
			for _, part := range n.Parts {
				if lit, ok := part.(*shsyntax.Lit); ok {
					if _, index, _, ok := splitID(lit.Value); ok {
						qkind[index] = dblQuoted
					}
				} else {
					shsyntax.Walk(part, visit)
				}
			}
			return false
		}
		return true
	}
	shsyntax.Walk(f, visit)
	for i, k := range qkind {
		if k == unknown {
			return "", fmt.Errorf("interpolation %d in unexpected place: cannot replace", i)
		}
	}
	buf.Reset()
	for i := range strs {
		// TODO bytes interpolations
		s, err := strs[i].String()
		if err != nil {
			return "", err
		}
		buf.WriteString(s)
		if i >= len(args) {
			continue
		}
		vs, err := toString(args[i])
		if err != nil {
			return "", fmt.Errorf("cannot convert index %d to string: %v", i, err)
		}
		switch qkind[i] {
		case unquoted:
			s, err := shsyntax.Quote(vs, shsyntax.LangBash)
			if err != nil {
				return "", fmt.Errorf("cannot quote string %q: %v", vs, err)
			}
			buf.WriteString(s)
		case sglQuoted:
			for _, r := range vs {
				if r == '\'' {
					buf.WriteString(`'"'"'`)
				} else {
					buf.WriteRune(r)
				}
			}
		case dblQuoted:
			for _, r := range vs {
				switch r {
				case '"', '\\', '`', '$':
					buf.WriteByte('\\')
				}
				buf.WriteRune(r)
			}
		default:
			panic("unreachable")
		}
	}
	return buf.String(), nil
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

type quoteKind byte

const (
	unknown quoteKind = iota
	unquoted
	sglQuoted
	dblQuoted
	// TODO here doc, comment, ... ?
)

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
