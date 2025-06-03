package jsonschema

import (
	"iter"
	"strings"
)

// TODO this file contains functionality that mimics the JSON Pointer functionality
// in https://pkg.go.dev/github.com/go-json-experiment/json/jsontext#Pointer;
// perhaps use it when it moves into the stdlib as json/v2.

var (
	jsonPtrEsc   = strings.NewReplacer("~", "~0", "/", "~1")
	jsonPtrUnesc = strings.NewReplacer("~0", "~", "~1", "/")
)

func jsonPointerFromTokens(tokens iter.Seq[string]) string {
	var buf strings.Builder
	for tok := range tokens {
		buf.WriteByte('/')
		buf.WriteString(jsonPtrEsc.Replace(tok))
	}
	return buf.String()
}

func jsonPointerTokens(p string) iter.Seq[string] {
	return func(yield func(string) bool) {
		needUnesc := strings.IndexByte(p, '~') >= 0
		for len(p) > 0 {
			p = strings.TrimPrefix(p, "/")
			i := min(uint(strings.IndexByte(p, '/')), uint(len(p)))
			var ok bool
			if needUnesc {
				ok = yield(jsonPtrUnesc.Replace(p[:i]))
			} else {
				ok = yield(p[:i])
			}
			if !ok {
				return
			}
			p = p[i:]
		}
	}
}
