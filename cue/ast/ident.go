// Copyright 2019 CUE Authors
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

package ast

import (
	"strconv"
	"unicode"
	"unicode/utf8"

	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/token"
)

func isLetter(ch rune) bool {
	return 'a' <= ch && ch <= 'z' || 'A' <= ch && ch <= 'Z' || ch >= utf8.RuneSelf && unicode.IsLetter(ch)
}

func isDigit(ch rune) bool {
	// TODO(mpvl): Is this correct?
	return '0' <= ch && ch <= '9' || ch >= utf8.RuneSelf && unicode.IsDigit(ch)
}

// QuoteIdent quotes an identifier, if needed, and reports
// an error if the identifier is invalid.
func QuoteIdent(ident string) (string, error) {
	if ident != "" && ident[0] == '`' {
		if _, err := strconv.Unquote(ident); err != nil {
			return "", errors.Newf(token.NoPos, "invalid quoted identifier %q", ident)
		}
		return ident, nil
	}

	// TODO: consider quoting keywords
	// switch ident {
	// case "for", "in", "if", "let", "true", "false", "null":
	// 	goto escape
	// }

	for _, r := range ident {
		if isLetter(r) || isDigit(r) || r == '_' {
			continue
		}
		if r == '-' {
			goto escape
		}
		return "", errors.Newf(token.NoPos, "invalid character '%s' in identifier", string(r))
	}

	return ident, nil

escape:
	return "`" + ident + "`", nil
}

// ParseIdent unquotes a possibly quoted identifier and validates
// if the result is valid.
func ParseIdent(n *Ident) (string, error) {
	ident := n.Name
	if ident == "" {
		return "", errors.Newf(n.Pos(), "empty identifier")
	}
	quoted := false
	if ident[0] == '`' {
		u, err := strconv.Unquote(ident)
		if err != nil {
			return "", errors.Newf(n.Pos(), "invalid quoted identifier")
		}
		ident = u
		quoted = true
	}

	for _, r := range ident {
		if isLetter(r) || isDigit(r) || r == '_' {
			continue
		}
		if r == '-' && quoted {
			continue
		}
		return "", errors.Newf(n.Pos(), "invalid character '%s' in identifier", string(r))
	}

	return ident, nil
}
