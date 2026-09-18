// Copyright 2020 CUE Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package filetypes

import (
	"maps"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"

	"cuelang.org/go/cue/build"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/internal/filetypes/internal"
	cuepath "cuelang.org/go/pkg/path"
)

// Mode indicate the base mode of operation and indicates a different set of
// defaults.
type Mode int

const (
	Input Mode = iota // The default
	Export
	Def
	Eval
	NumModes
)

// checkMode rejects a Mode outside the defined constants before it is
// used to index the per-mode registry arrays, so a caller error
// surfaces as an error rather than an index-out-of-range panic. The
// earlier lookup tables degraded to a lookup miss for such modes.
func checkMode(m Mode) error {
	if m < 0 || m >= NumModes {
		return errors.Newf(token.NoPos, "invalid mode %d", int(m))
	}
	return nil
}

func (m Mode) String() string {
	switch m {
	default:
		return "input"
	case Eval:
		return "eval"
	case Export:
		return "export"
	case Def:
		return "def"
	}
}

type FileInfo = internal.FileInfo

// ParseArgs converts a sequence of command line arguments representing
// files into a sequence of build file specifications.
//
// The arguments are of the form
//
//	file* (spec: file+)*
//
// where file is a filename and spec is itself of the form
//
//	tag[=value]('+'tag[=value])*
//
// A file type spec applies to all its following files and until a next spec
// is found.
//
// Examples:
//
//	json: foo.data bar.data json+schema: bar.schema
func ParseArgs(args []string) (files []*build.File, err error) {
	qualifier := ""
	hasFiles := false

	sc := &scope{}
	for i, s := range args {
		scope, file, found := cutScope(s)
		switch {
		case !found: // just a filename, like "foo.yaml"
			if file == "" {
				return nil, errors.Newf(token.NoPos, "empty file name")
			}
			f, err := toFile(Input, sc, file)
			if err != nil {
				return nil, err
			}
			files = append(files, f)
			hasFiles = true

		case scope == "":
			return nil, errors.Newf(token.NoPos, "empty filetype prefix in %q", s)
		case file != "":
			return nil, errors.Newf(token.NoPos,
				"cannot combine file type and file name; did you mean %q?",
				scope+": "+file)

		default: // just a scope, like "json:"
			switch {
			case i == len(args)-1:
				qualifier = scope
				fallthrough
			case qualifier != "" && !hasFiles:
				return nil, errors.Newf(token.NoPos, "scoped qualifier %q without file", qualifier+":")
			}
			sc, err = parseScope(scope)
			if err != nil {
				return nil, err
			}
			qualifier = scope
			hasFiles = false
		}
	}

	return files, nil
}

// DefaultTagsForInterpretation returns any tags that would be set by default
// in the given interpretation in the given mode.
func DefaultTagsForInterpretation(interp build.Interpretation, mode Mode) map[string]bool {
	if interp == "" {
		return nil
	}

	// This should never fail if called with a legitimate build.Interpretation constant.
	f, err := toFile(mode, &scope{
		topLevel: map[string]bool{
			string(interp): true,
		},
	}, "-")
	if err != nil {
		panic(err)
	}
	return f.BoolTags
}

func cutScope(s string) (scope, file string, found bool) {
	if cuepath.IsAbs(s, cuepath.Windows) || cuepath.IsAbs(s, cuepath.Unix) {
		// Absolute paths on Windows can begin with a volume name, like `C:\foo\bar`;
		// do not confuse that for a scope prefix.
		// Note that we use [cuepath.IsAbs] for consistent behavior across platforms.
		//
		// We also check for Unix, so that `/foo:colons.json` is treated
		// as an absolute filename rather than a `/foo` scope prefix on `colons.json`.
	} else if before, after, ok := strings.Cut(s, ":"); ok {
		return before, after, true
	}
	return "", s, false // Just a filename
}

// ParseFile parses a single-argument file specifier, such as when a file is
// passed to a command line argument.
//
// Example:
//
//	cue eval -o yaml:foo.data
func ParseFile(s string, mode Mode) (*build.File, error) {
	scope, file, found := cutScope(s)
	if found && scope == "" {
		return nil, errors.Newf(token.NoPos, "empty filetype prefix in %q", s)
	}

	if file == "" {
		if s != "" {
			return nil, errors.Newf(token.NoPos, "empty file name in %q", s)
		}
		return nil, errors.Newf(token.NoPos, "empty file name")
	}

	return ParseFileAndType(file, scope, mode)
}

// ParseFileAndType parses a file and type combo.
func ParseFileAndType(file, scope string, mode Mode) (*build.File, error) {
	sc, err := parseScope(scope)
	if err != nil {
		return nil, err
	}
	return toFile(mode, sc, file)
}

// scope holds attributes that influence encoding and decoding.
// Together with the mode and the file name, they determine
// a number of properties of the encoding process.
//
// A parsed scope is cached and shared process-wide (parseScope returns
// the same *scope for the same qualifier string, and emptyScope for
// ""), so a scope and its maps must be treated as immutable once
// parseScope has returned it; resolution only ever reads them.
type scope struct {
	topLevel         map[string]bool
	subsidiaryBool   map[string]bool
	subsidiaryString map[string]string

	// canonicalKey is the resolution-cache identity for top-level and
	// Boolean subsidiary tags. String-valued subsidiary tags are excluded
	// because their value space is unbounded and disables result caching.
	canonicalKey string
}

var emptyScope = &scope{}

const (
	// Keep the parsed-scope cache small in both entry count and retained
	// bytes. Qualifiers longer than this remain valid but are parsed on
	// every use. strings.Clone below prevents a short substring from
	// retaining an arbitrarily large caller-owned backing string.
	maxParsedScopeCacheEntries = 1024
	maxParsedScopeKeyBytes     = 256
)

// boundedCache combines a lock-free read path with mutex-protected FIFO
// admission. Once full, it evicts the oldest admitted key so a workload change
// can replace stale entries while retained state remains strictly bounded.
// Values must be immutable because a reader may still observe an entry while
// a concurrent miss evicts it.
type boundedCache[K comparable, V any] struct {
	values sync.Map // K -> V

	mu    sync.Mutex
	keys  []K
	next  int
	limit int
}

func newBoundedCache[K comparable, V any](limit int) *boundedCache[K, V] {
	if limit <= 0 {
		panic("filetypes: bounded cache requires a positive limit")
	}
	return &boundedCache[K, V]{limit: limit}
}

func (c *boundedCache[K, V]) Load(key K) (V, bool) {
	value, ok := c.values.Load(key)
	if !ok {
		var zero V
		return zero, false
	}
	return value.(V), true
}

// LoadOrStore returns the existing value for key, or stores and returns value.
// Admission and eviction are serialized so the FIFO and map cannot diverge.
func (c *boundedCache[K, V]) LoadOrStore(key K, value V) (actual V, loaded bool) {
	if actual, loaded := c.Load(key); loaded {
		return actual, true
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if actual, loaded := c.Load(key); loaded {
		return actual, true
	}
	if len(c.keys) < c.limit {
		c.keys = append(c.keys, key)
	} else {
		c.values.Delete(c.keys[c.next])
		c.keys[c.next] = key
		c.next = (c.next + 1) % c.limit
	}
	c.values.Store(key, value)
	return value, false
}

func (c *boundedCache[K, V]) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.values.Clear()
	c.keys = nil
	c.next = 0
}

func (c *boundedCache[K, V]) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.keys)
}

func (c *boundedCache[K, V]) Range(f func(K, V) bool) {
	c.values.Range(func(key, value any) bool {
		return f(key.(K), value.(V))
	})
}

var parsedScopeCache = newBoundedCache[string, *scope](maxParsedScopeCacheEntries)

func parseScope(scopeStr string) (*scope, error) {
	if scopeStr == "" {
		return emptyScope, nil
	}
	cacheable := len(scopeStr) <= maxParsedScopeKeyBytes
	if cacheable {
		if cached, ok := parsedScopeCache.Load(scopeStr); ok {
			return cached, nil
		}
		// The maps in scope retain substrings of their input. Clone before
		// parsing so a cached short qualifier cannot retain a much larger
		// string from ParseFile's combined qualifier-and-filename input.
		scopeStr = strings.Clone(scopeStr)
	}
	sc, err := parseScopeUncached(scopeStr)
	if err != nil {
		return nil, err
	}
	// String-valued subsidiary tags have an unbounded value space and
	// are intentionally kept out of every resolution-related cache.
	if !cacheable || len(sc.subsidiaryString) != 0 {
		return sc, nil
	}
	return cacheParsedScope(scopeStr, sc), nil
}

func parseScopeUncached(scopeStr string) (*scope, error) {
	builtins := tagTypes()
	sc := scope{
		topLevel:         make(map[string]bool),
		subsidiaryBool:   make(map[string]bool),
		subsidiaryString: make(map[string]string),
	}
	for tag := range strings.SplitSeq(scopeStr, "+") {
		tagName, tagVal, hasValue := strings.Cut(tag, "=")
		typ := builtins[tagName]
		if typ == TagUnknown {
			// Dynamically registered qualifiers and subsidiary tags
			// extend the built-in classification (registry.go).
			typ = dynSnapshot().tagKinds[tagName]
		}
		switch typ {
		case TagTopLevel:
			if hasValue {
				return nil, errors.Newf(token.NoPos, "cannot specify value for tag %q", tagName)
			}
			sc.topLevel[tagName] = true
		case TagSubsidiaryBool:
			if hasValue {
				t, err := strconv.ParseBool(tagVal)
				if err != nil {
					return nil, errors.Newf(token.NoPos, "invalid boolean value for tag %q", tagName)
				}
				sc.subsidiaryBool[tagName] = t
			} else {
				sc.subsidiaryBool[tagName] = true
			}
		case TagSubsidiaryString:
			if !hasValue {
				return nil, errors.Newf(token.NoPos, "tag %q must have value (%s=<value>)", tagName, tagName)
			}
			if old, ok := sc.subsidiaryString[tagName]; ok && old != tagVal {
				return nil, errors.Newf(token.NoPos, "conflicting values for tag %q: %q and %q", tagName, old, tagVal)
			}
			sc.subsidiaryString[tagName] = tagVal
		default:
			return nil, errors.Newf(token.NoPos, "unknown filetype %s", tagName)
		}
	}
	sc.canonicalKey = canonicalScopeKey(&sc)
	return &sc, nil
}

func cacheParsedScope(raw string, sc *scope) *scope {
	actual, _ := parsedScopeCache.LoadOrStore(raw, sc)
	return actual
}

// clearParsedScopeCache is only used while tests or benchmarks have
// exclusive control of the process-wide registry.
func clearParsedScopeCache() {
	parsedScopeCache.Clear()
}

// canonicalScopeKey returns an order- and spelling-independent cache
// identity for the bounded-valued portion of sc: top-level tag membership and
// Boolean subsidiary tags. String-valued subsidiary tags are excluded because
// their values are arbitrary and any scope carrying one bypasses the result
// cache. Length-prefixing names avoids delimiter ambiguities, including for
// dynamically registered tags.
func canonicalScopeKey(sc *scope) string {
	if sc.canonicalKey != "" {
		return sc.canonicalKey
	}
	if len(sc.topLevel) == 0 && len(sc.subsidiaryBool) == 0 {
		return ""
	}
	var b strings.Builder
	writeName := func(kind byte, name string) {
		b.WriteByte(kind)
		b.WriteString(strconv.Itoa(len(name)))
		b.WriteByte(':')
		b.WriteString(name)
	}
	for _, name := range slices.Sorted(maps.Keys(sc.topLevel)) {
		writeName('t', name)
	}
	for _, name := range slices.Sorted(maps.Keys(sc.subsidiaryBool)) {
		writeName('b', name)
		if sc.subsidiaryBool[name] {
			b.WriteByte('1')
		} else {
			b.WriteByte('0')
		}
	}
	return b.String()
}

// fileExt is like filepath.Ext except we don't treat file names starting with "." as having an extension
// unless there's also another . in the name.
//
// It also treats "-" as a special case, so we treat stdin/stdout as
// a regular file.
func fileExt(f string) string {
	if f == "-" {
		return "-"
	}
	e := filepath.Ext(f)
	if e == "" || e == filepath.Base(f) {
		return ""
	}
	return e
}
