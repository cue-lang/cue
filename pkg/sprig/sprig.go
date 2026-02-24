// Copyright 2025 The CUE Authors
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

// Package sprig provides Sprig-compatible string functions.
package sprig

import (
	"strings"

	"github.com/Masterminds/goutils"
	"github.com/huandu/xstrings"
)

// Title returns a copy of s with the first letter of each word capitalized.
func Title(s string) string {
	return strings.Title(s) //nolint:staticcheck // matches Sprig behavior
}

// Untitle returns a copy of s with the first letter of each word lowercased.
func Untitle(s string) string {
	return goutils.Uncapitalize(s)
}

// Substr returns the substring of s from byte index start to end.
// If start is negative, it is treated as 0.
// If end is negative or greater than len(s), the substring extends to the end.
func Substr(start, end int, s string) string {
	if start < 0 {
		return s[:end]
	}
	if end < 0 || end > len(s) {
		return s[start:]
	}
	return s[start:end]
}

// Nospace returns s with all whitespace characters removed.
func Nospace(s string) string {
	return goutils.DeleteWhiteSpace(s)
}

// Trunc truncates s to c bytes.
// If c is negative, it removes |c| bytes from the beginning instead.
func Trunc(n int, s string) string {
	if n < 0 && len(s)+n > 0 {
		return s[len(s)+n:]
	}
	if n >= 0 && len(s) > n {
		return s[:n]
	}
	return s
}

// Abbrev abbreviates s to at most width characters, ending with "...".
// If width is less than 4, s is returned unchanged.
func Abbrev(width int, s string) string {
	if width < 4 {
		return s
	}
	r, _ := goutils.Abbreviate(s, width)
	return r
}

// Abbrevboth abbreviates s from both sides.
// left is the offset from the left; right is the max width.
func Abbrevboth(left, right int, s string) string {
	if right < 4 || left > 0 && right < 7 {
		return s
	}
	r, _ := goutils.AbbreviateFull(s, left, right)
	return r
}

// Initials returns the first letter of each whitespace-delimited word in s.
func Initials(s string) string {
	return goutils.Initials(s)
}

// Wrap wraps s at the specified width using newlines.
func Wrap(width int, s string) string {
	return goutils.Wrap(s, width)
}

// WrapWith wraps s at the specified width using the given separator string.
func WrapWith(width int, sep, s string) string {
	return goutils.WrapCustom(s, width, sep, true)
}

// Indent prepends the given number of spaces to every line in s.
func Indent(spaces int, s string) string {
	pad := strings.Repeat(" ", spaces)
	return pad + strings.ReplaceAll(s, "\n", "\n"+pad)
}

// Nindent is like Indent but prepends a newline before the indented text.
func Nindent(spaces int, s string) string {
	return "\n" + Indent(spaces, s)
}

// Snakecase converts s to snake_case.
func Snakecase(s string) string {
	return xstrings.ToSnakeCase(s)
}

// Camelcase converts s to PascalCase (matching Sprig's camelcase behavior).
func Camelcase(s string) string {
	return xstrings.ToPascalCase(s)
}

// Kebabcase converts s to kebab-case.
func Kebabcase(s string) string {
	return xstrings.ToKebabCase(s)
}

// Swapcase swaps the case of each letter in s.
func Swapcase(s string) string {
	return goutils.SwapCase(s)
}

// Plural returns one if count is 1, otherwise many.
func Plural(one, many string, count int) string {
	if count == 1 {
		return one
	}
	return many
}
