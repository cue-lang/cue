package jsonschema

import (
	"iter"

	"cuelang.org/go/encoding/json"
)

// JSONPointerFromTokens returns a JSON Pointer formed from
// the unquoted tokens in the given sequence. Any
// slash (/) or tilde (~) characters will be escaped appropriately.
//
// Deprecated: Use json.PointerFromTokens instead.
func JSONPointerFromTokens(tokens iter.Seq[string]) string {
	return string(json.PointerFromTokens(tokens))
}

// JSONPointerTokens returns a sequence of all the
// unquoted path elements (tokens) of the given JSON
// Pointer.
//
// Deprecated: Use json.Pointer.Tokens instead.
func JSONPointerTokens(p string) iter.Seq[string] {
	return json.Pointer(p).Tokens()
}
