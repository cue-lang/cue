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

package export

import (
	"strconv"
	"strings"
	"unicode/utf8"
)

// quote quotes the given string.
func quote(str string, quote byte) string {
	if strings.IndexByte(str, '\n') < 0 {
		buf := []byte{quote}
		buf = appendEscaped(buf, str, quote, true)
		buf = append(buf, quote)
		return string(buf)
	}
	buf := []byte{quote, quote, quote}
	buf = append(buf, multiSep...)
	buf = appendEscapeMulti(buf, str, quote)
	buf = append(buf, quote, quote, quote)
	return string(buf)
}

// TODO: consider the best indent strategy.
const multiSep = "\n        "

func appendEscapeMulti(buf []byte, str string, quote byte) []byte {
	// TODO(perf)
	a := strings.Split(str, "\n")
	for _, s := range a {
		buf = appendEscaped(buf, s, quote, true)
		buf = append(buf, multiSep...)
	}
	return buf
}

const lowerhex = "0123456789abcdef"

func appendEscaped(buf []byte, s string, quote byte, graphicOnly bool) []byte {
	for width := 0; len(s) > 0; s = s[width:] {
		r := rune(s[0])
		width = 1
		if r >= utf8.RuneSelf {
			r, width = utf8.DecodeRuneInString(s)
		}
		if width == 1 && r == utf8.RuneError {
			buf = append(buf, `\x`...)
			buf = append(buf, lowerhex[s[0]>>4])
			buf = append(buf, lowerhex[s[0]&0xF])
			continue
		}
		buf = appendEscapedRune(buf, r, quote, graphicOnly)
	}
	return buf
}

func appendEscapedRune(buf []byte, r rune, quote byte, graphicOnly bool) []byte {
	var runeTmp [utf8.UTFMax]byte
	if r == rune(quote) || r == '\\' { // always backslashed
		buf = append(buf, '\\')
		buf = append(buf, byte(r))
		return buf
	}
	// TODO(perf): IsGraphic calls IsPrint.
	if strconv.IsPrint(r) || graphicOnly && strconv.IsGraphic(r) {
		n := utf8.EncodeRune(runeTmp[:], r)
		buf = append(buf, runeTmp[:n]...)
		return buf
	}
	switch r {
	case '\a':
		buf = append(buf, `\a`...)
	case '\b':
		buf = append(buf, `\b`...)
	case '\f':
		buf = append(buf, `\f`...)
	case '\n':
		buf = append(buf, `\n`...)
	case '\r':
		buf = append(buf, `\r`...)
	case '\t':
		buf = append(buf, `\t`...)
	case '\v':
		buf = append(buf, `\v`...)
	default:
		switch {
		case r < ' ':
			// Invalid for strings, only bytes.
			buf = append(buf, `\x`...)
			buf = append(buf, lowerhex[byte(r)>>4])
			buf = append(buf, lowerhex[byte(r)&0xF])
		case r > utf8.MaxRune:
			r = 0xFFFD
			fallthrough
		case r < 0x10000:
			buf = append(buf, `\u`...)
			for s := 12; s >= 0; s -= 4 {
				buf = append(buf, lowerhex[r>>uint(s)&0xF])
			}
		default:
			buf = append(buf, `\U`...)
			for s := 28; s >= 0; s -= 4 {
				buf = append(buf, lowerhex[r>>uint(s)&0xF])
			}
		}
	}
	return buf
}
