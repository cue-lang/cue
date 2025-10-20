// Package anyunique provides canonicalization of values under a
// caller-defined equivalence relation.
//
// A [Store] holds a set of unique values of a specific type T. Calling
// [Store.Make] with two values that are equivalent according to the provided
// [Hasher] returns [U] values that are identical. [U] is a lightweight
// wrapper around the canonical value; use [U.Get] to obtain the
// underlying T.
//
// The zero [U] represents the zero value of T. Make returns the zero
// [U] when called with the zero value of T: it will never try to hash
// the zero value.
//
// [Store.WriteHash] writes a short representation of a canonicalized
// value to a [maphash.Hash]. It is useful when hashing structures that
// themselves contain canonicalized values, avoiding re-hashing the full
// value graph.
//
// NOTE this package assumes that T values are treated as immutable.
// That is, after calling [Store.Make] a value must not change.
package anyunique

import "hash/maphash"

// A Hasher defines a hash function and an equivalence relation over
// values of type T.
//
// Hash must write a hash of its argument to the provided *maphash.Hash,
// and Equal must report whether two values are equivalent. Hash and
// Equal must be consistent: if Equal(x, y) is true then Hash must
// produce the same output for x and y.
//
// See https://go-review.googlesource.com/c/go/+/657296/11/src/hash/maphash/hasher.go
type Hasher[T any] interface {
	//
	Hash(*maphash.Hash, T)
	Equal(x, y T) bool
}

// New returns a new store holding a set of unique values
// of type T, using h to determine whether values are the
// same.
//
// The equivalence relation and hash are supplied by the given [Hasher].
func New[T comparable, H Hasher[T]](h H) *Store[T, H] {
	s := &Store[T, H]{
		h:       h,
		seed:    maphash.MakeSeed(),
		hashes:  make(map[T]uint64),
		entries: make(map[uint64][]T),
	}
	return s
}

// Store holds a set of unique values of type T.
type Store[T comparable, H Hasher[T]] struct {
	h       H
	seed    maphash.Seed
	entries map[uint64][]T
	hashes  map[T]uint64
}

// U represents a unique value of type T. If two values of type U[T]
// originating from the same [Store] compare equal, they are guaranteed
// to be equal according to the equality criteria that the store was
// created with.
type U[T comparable] struct {
	x T
}

// Get returns the actual value held in u.
func (u U[T]) Get() T {
	return u.x
}

// WriteHash writes a short representation of x to h.
// This allows callers to avoid hashing an tree of values
// when hashing a value that itself contains other U[T] items.
func (s *Store[T, H]) WriteHash(h *maphash.Hash, x U[T]) {
	z := isZero(x)
	maphash.WriteComparable(h, z)
	if !z {
		// TODO we _could_ write two independent hashes here
		// if we were concerned about collisions.
		maphash.WriteComparable(h, s.hashes[x.Get()])
	}
}

// Make returns a unique value u such that u.Get() is equal to x
// according to the equality criteria defined by the store.
//
// It is assumed that values will not change after passing to Make: the
// caller must take care to preserve immutability.
func (s *Store[T, H]) Make(x T) U[T] {
	if isZero(x) {
		return U[T]{}
	}

	if _, ok := s.hashes[x]; ok {
		return U[T]{x}
	}
	var hasher maphash.Hash
	hasher.SetSeed(s.seed)
	s.h.Hash(&hasher, x)
	h := hasher.Sum64()
	entries := s.entries[h]
	for _, e := range entries {
		if s.h.Equal(x, e) {
			return U[T]{e}
		}
	}
	s.entries[h] = append(entries, x)
	s.hashes[x] = h
	return U[T]{x}
}

func isZero[T comparable](x T) bool {
	return x == *new(T)
}
