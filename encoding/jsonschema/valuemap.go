package jsonschema

import (
	"cuelang.org/go/cue"
	"cuelang.org/go/cue/token"
)

// valueMap holds a map of values indexed by schema position
// (a.k.a. JSON Pointer).
//
// It's designed so that it's cheap in the common case that a lookup
// returns false and that there are many more lookups than
// entries in the map.
//
// It does that by using the source position of the
// schema as a first probe. Determining the source location of a value
// is very cheap, and in most practical cases, JSON Schema is being
// extracted from concrete JSON where there will be a bijective mapping
// between source location and path.
type valueMap[T any] struct {
	byPos  map[token.Pos]bool
	byPath map[string]T
}

func newValueMap[T any]() *valueMap[T] {
	return &valueMap[T]{
		byPos:  make(map[token.Pos]bool),
		byPath: make(map[string]T),
	}
}

func (m *valueMap[T]) len() int {
	return len(m.byPath)
}

func (m *valueMap[T]) set(key cue.Value, v T) {
	m.byPos[key.Pos()] = true
	m.byPath[key.Path().String()] = v
}

func (m *valueMap[T]) get(key cue.Value) T {
	if !m.byPos[key.Pos()] {
		return *new(T)
	}
	return m.byPath[key.Path().String()]
}

func (m *valueMap[T]) lookup(key cue.Value) (T, bool) {
	if !m.byPos[key.Pos()] {
		return *new(T), false
	}
	v, ok := m.byPath[key.Path().String()]
	return v, ok
}
