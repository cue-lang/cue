// Copyright 2026 The CUE Authors
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

package ini_test

import (
	"strings"
	"testing"

	"github.com/go-quicktest/qt"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/ast/astutil"
	"cuelang.org/go/cue/cuecontext"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/format"
	"cuelang.org/go/encoding/ini"
)

func TestDecoder(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		config  ini.Config
		input   string
		wantCUE string
		wantErr string
		// noEncode, when set, is why the decoded value has no spelling
		// under config, so that encoding it fails rather than reading
		// back.
		noEncode string
	}{{
		name:    "Empty",
		input:   "",
		wantCUE: "",
	}, {
		name: "CommentsOnly",
		input: `
			; This is a comment
			# This is also a comment
			`,
		wantCUE: "",
	}, {
		name: "GlobalProperties",
		input: `
			key1 = value1
			key2 = value2
			`,
		wantCUE: `
			key1: "value1"
			key2: "value2"
			`,
	}, {
		name: "SimpleSection",
		input: `
			[section]
			key1 = value1
			key2 = value2
			`,
		wantCUE: `
			section: {
				key1: "value1"
				key2: "value2"
			}
			`,
	}, {
		name: "MultipleSections",
		input: `
			[database]
			host = localhost
			port = 5432

			[server]
			host = 0.0.0.0
			port = 8080
			`,
		wantCUE: `
			database: {
				host: "localhost"
				port: "5432"
			}
			server: {
				host: "0.0.0.0"
				port: "8080"
			}
			`,
	}, {
		name: "GlobalAndSections",
		input: `
			app_name = MyApp
			version = 1.0

			[database]
			host = localhost
			port = 5432
			`,
		wantCUE: `
			app_name: "MyApp"
			version:  "1.0"
			database: {
				host: "localhost"
				port: "5432"
			}
			`,
	}, {
		name: "FlatSectionsByDefault/DottedSectionNameIsLiteral",
		input: `
			[database.pool]
			min = 5
			max = 20
			`,
		wantCUE: `
			"database.pool": {
				min: "5"
				max: "20"
			}
			`,
	}, {
		name:   "NestedSections",
		config: ini.Config{DottedSections: true},
		input: `
			[database.pool]
			min = 5
			max = 20

			[database.credentials]
			user = admin
			password = secret
			`,
		wantCUE: `
			database: {
				pool: {
					min: "5"
					max: "20"
				}
				credentials: {
					user:     "admin"
					password: "secret"
				}
			}
			`,
	}, {
		name: "KeysWithSpecialCharacters",
		input: `
			[Firewall_Inbound]
			*_NetbiosUDPRule1=UDP/137:*.*.*
			SecureMode:true$*_NetbiosUDPRule1=
			SKU:nonstdhw$*_NetbiosUDPRule1=
			`,
		wantCUE: `
			Firewall_Inbound: {
				"*_NetbiosUDPRule1":                 "UDP/137:*.*.*"
				"SecureMode:true$*_NetbiosUDPRule1": ""
				"SKU:nonstdhw$*_NetbiosUDPRule1":    ""
			}
			`,
	}, {
		name: "QuotedValues",
		input: `
			[section]
			name = "John Doe"
			greeting = 'Hello World'
			`,
		wantCUE: `
			section: {
				name:     "\"John Doe\""
				greeting: "'Hello World'"
			}
			`,
	}, {
		name: "Comments/WholeLineByDefault",
		input: `
			[Service]
			ExecStart=/bin/echo one ; /bin/echo two
			flags = -a #b
			color = #fff
			`,
		wantCUE: `
			Service: {
				ExecStart: "/bin/echo one ; /bin/echo two"
				flags:     "-a #b"
				color:     "#fff"
			}
			`,
	}, {
		name:   "Comments/Inline",
		config: ini.Config{Comments: ini.CommentsInline},
		input: `
			[section]
			key1 = value1 ; this is an inline comment
			key2 = value2 # this is also an inline comment
			color = #fff
			`,
		wantCUE: `
			section: {
				key1:  "value1"
				key2:  "value2"
				color: "#fff"
			}
			`,
	}, {
		// git ends a value at any ";" or "#" outside quotes, so a value
		// starting with one is empty.
		name:   "Comments/Anywhere",
		config: ini.Config{Comments: ini.CommentsAnywhere},
		input: `
			[section]
			x = a#b
			color = #fff
			z = a;b
			quoted = "a#b";c
			`,
		wantCUE: `
			section: {
				x:      "a"
				color:  ""
				z:      "a"
				quoted: "\"a#b\""
			}
			`,
	}, {
		// Under git's quoting an escaped quote opens nothing, so a comment
		// after it still ends the value.
		name:   "Comments/Anywhere/EscapedQuote",
		config: ini.Config{Comments: ini.CommentsAnywhere, Quotes: ini.QuotesEscaped},
		input: `
			x = a\" # "b
			`,
		wantCUE: `
			x: "a\""
			`,
	}, {
		// A comment before the delimiter ends the line, leaving a bare key.
		name:   "Comments/BeforeTheDelimiter",
		config: ini.Config{Comments: ini.CommentsInline, BareKeys: ini.BareKeysTrue},
		input: `
			[core]
			autocrlf # a comment
			flag ; see a=b
			`,
		wantCUE: `
			core: {
				autocrlf: true
				flag:     true
			}
			`,
	}, {
		name:   "Comments/BeforeTheDelimiter/BareKeysOff",
		config: ini.Config{Comments: ini.CommentsInline},
		input: `
			flag ; see a=b
			`,
		wantErr: `
			invalid line: flag ; see a=b:
			    test.ini:1:1
			`,
	}, {
		name: "CommentsAndBlankLines",
		input: `
			; Database configuration
			[database]
			host = localhost

			# Connection pool settings
			pool_size = 10
			`,
		wantCUE: `
			database: {
				host:      "localhost"
				pool_size: "10"
			}
			`,
	}, {
		name: "EmptyValues",
		input: `
			[section]
			key1 =
			key2 =
			`,
		wantCUE: `
			section: {
				key1: ""
				key2: ""
			}
			`,
	}, {
		name: "ValuesWithSpecialCharacters",
		input: `
			[paths]
			home = /usr/local/bin
			url = https://example.com/path?query=1&other=2
			`,
		wantCUE: `
			paths: {
				home: "/usr/local/bin"
				url:  "https://example.com/path?query=1&other=2"
			}
			`,
	}, {
		name: "WhitespaceHandling",
		input: `
			[section]
			  key1  =  value1  
			key2=value2
			`,
		wantCUE: `
			section: {
				key1: "value1"
				key2: "value2"
			}
			`,
	}, {
		name:   "DeeplyNestedSections",
		config: ini.Config{DottedSections: true},
		input: `
			[a.b.c]
			key = value
			`,
		wantCUE: `
			a: b: c: key: "value"
			`,
	}, {
		name:   "SectionWithSiblingAndNestedSection",
		config: ini.Config{DottedSections: true},
		input: `
			[server]
			host = localhost

			[server.tls]
			cert = /path/to/cert
			key = /path/to/key
			`,
		wantCUE: `
			server: {
				host: "localhost"
				tls: {
					cert: "/path/to/cert"
					key:  "/path/to/key"
				}
			}
			`,
	}, {
		name:   "FullExample",
		config: ini.Config{DottedSections: true},
		input: `
			; Application configuration
			app_name = MyWebApp

			[database]
			host = db.example.com
			port = 3306
			name = mydb

			[database.credentials]
			username = dbuser
			password = dbpass

			[server]
			host = 0.0.0.0
			port = 443

			[logging]
			level = info
			file = /var/log/app.log
			`,
		wantCUE: `
			app_name: "MyWebApp"
			database: {
				host: "db.example.com"
				port: "3306"
				name: "mydb"
				credentials: {
					username: "dbuser"
					password: "dbpass"
				}
			}
			server: {
				host: "0.0.0.0"
				port: "443"
			}
			logging: {
				level: "info"
				file:  "/var/log/app.log"
			}
			`,
	}, {
		name: "DuplicateKey",
		input: `
			[section]
			key = value1
			key = value2
			`,
		wantErr: `
			duplicate key: key:
			    test.ini:3:1
			`,
	}, {
		name: "RepeatedSectionHeader/Merges",
		input: `
			[section]
			key1 = value1

			[section]
			key2 = value2
			`,
		wantCUE: `
			section: {
				key1: "value1"
				key2: "value2"
			}
			`,
	}, {
		name: "RepeatedSectionHeader/DuplicateKeyAcrossOccurrences",
		input: `
			[section]
			key = value1

			[section]
			key = value2
			`,
		wantErr: `
			duplicate key: key:
			    test.ini:5:1
			`,
	}, {
		name:   "DuplicateSections/Error",
		config: ini.Config{DuplicateSections: ini.DuplicateSectionsError},
		input: `
			[s]
			a = 1
			[s]
			b = 2
			`,
		wantErr: `
			duplicate section: s:
			    test.ini:3:1
			`,
	}, {
		// A section that a dotted header only passes through has not been
		// named by a header, so a first header naming it is no repeat.
		name:   "DuplicateSections/Error/PathThroughASection",
		config: ini.Config{DuplicateSections: ini.DuplicateSectionsError, DottedSections: true},
		input: `
			[a.b]
			x = 1
			[a]
			y = 2
			`,
		wantCUE: `
			a: {
				b: x: "1"
				y: "2"
			}
			`,
	}, {
		name:   "DuplicateSections/First",
		config: ini.Config{DuplicateSections: ini.DuplicateSectionsFirst},
		input: `
			[s]
			a = 1
			[t]
			c = 3
			[s]
			a = 9
			b = 2
			`,
		wantCUE: `
			s: a: "1"
			t: c: "3"
			`,
	}, {
		name:   "PropertyShadowsExistingSubsection",
		config: ini.Config{DottedSections: true},
		input: `
			[a.b]
			x = 1

			[a]
			b = 2
			`,
		wantErr: `
			property b conflicts with section of the same name:
			    test.ini:5:1
			`,
	}, {
		name:   "SubsectionShadowsExistingProperty",
		config: ini.Config{DottedSections: true},
		input: `
			[a]
			b = 1

			[a.b]
			x = 2
			`,
		wantErr: `
			section b conflicts with property of the same name:
			    test.ini:4:1
			`,
	}, {
		name: "MissingClosingBracket",
		input: `
			[section
			key = value
			`,
		wantErr: `
			missing closing bracket for section header:
			    test.ini:1:1
			`,
	}, {
		name: "EmptySectionName",
		input: `
			[]
			key = value
			`,
		wantErr: `
			empty section name:
			    test.ini:1:1
			`,
	}, {
		name: "InvalidLine",
		input: `
			[section]
			not a valid line
			`,
		wantErr: `
			invalid line: not a valid line:
			    test.ini:2:1
			`,
	}, {
		name: "EmptyKey",
		input: `
			[section]
			= value
			`,
		wantErr: `
			invalid line: = value:
			    test.ini:2:1
			`,
	}, {
		name: "DuplicateKeyGlobalScope",
		input: `
			key = value1
			key = value2
			`,
		wantErr: `
			duplicate key: key:
			    test.ini:2:1
			`,
	}, {
		name:   "Case/Lower/DuplicateAfterFolding",
		config: ini.Config{Case: ini.CaseLower},
		input: `
			[section]
			Key = value1
			key = value2
			`,
		wantErr: `
			duplicate key: key:
			    test.ini:3:1
			`,
	}, {
		name:   "Case/Lower/RepeatedSectionsMerge",
		config: ini.Config{Case: ini.CaseLower},
		input: `
			[Section]
			key1 = value1

			[section]
			key2 = value2
			`,
		wantCUE: `
			section: {
				key1: "value1"
				key2: "value2"
			}
			`,
	}, {
		name: "SecondDecodeReturnsEOF",
		input: `
			key = value
			`,
		wantCUE: `
			key: "value"
			`,
	}, {
		name:   "Case/Lower/KeysAndSections",
		config: ini.Config{Case: ini.CaseLower},
		input: `
			AppName = MyApp

			[Database]
			Host = localhost
			Port = 5432
			`,
		wantCUE: `
			appname: "MyApp"
			database: {
				host: "localhost"
				port: "5432"
			}
			`,
	}, {
		name:   "Case/Lower/NestedSections",
		config: ini.Config{Case: ini.CaseLower, DottedSections: true},
		input: `
			[Server.TLS]
			Cert = /path/to/cert
			`,
		wantCUE: `
			server: tls: cert: "/path/to/cert"
			`,
	}, {
		name:   "Case/Lower/MixedCaseKeys",
		config: ini.Config{Case: ini.CaseLower},
		input: `
			[section]
			camelCase = value1
			ALLCAPS = value2
			lower = value3
			`,
		wantCUE: `
			section: {
				camelcase: "value1"
				allcaps:   "value2"
				lower:     "value3"
			}
			`,
	}, {
		// A flavor is a starting point: assigning one field after the call
		// changes exactly that behavior.
		name:   "Override/GitWithoutInlineComments",
		config: override(ini.GitConfig(), func(c *ini.Config) { c.Comments = ini.CommentsWholeLine }),
		input: `
			[remote "origin"]
			fetch = +refs/heads/*:refs/remotes/origin/*
			fetch = +refs/tags/*:refs/tags/*
			[Core]
			excludesFile = ~/.gitignore ; the user-wide ignore list
			`,
		wantCUE: `
			remote: origin: fetch: ["+refs/heads/*:refs/remotes/origin/*", "+refs/tags/*:refs/tags/*"]
			core: excludesfile: "~/.gitignore ; the user-wide ignore list"
			`,
	}, {
		name:   "Override/PythonWithDuplicateLists",
		config: override(ini.PythonConfig(), func(c *ini.Config) { c.DuplicateKeys = ini.DuplicatesList }),
		input: `
			[options]
			install_requires: one
			install_requires: two
			`,
		wantCUE: `
			options: install_requires: ["one", "two"]
			`,
	}, {
		name:   "Override/PythonKeepsItsOtherOptions",
		config: override(ini.PythonConfig(), func(c *ini.Config) { c.DuplicateKeys = ini.DuplicatesList }),
		input: `
			[metadata]
			Name = mypkg
			`,
		wantCUE: `
			metadata: name: "mypkg"
			`,
	}, {name: "Delimiters/EqualsAndColon",
		config: ini.Config{Delimiters: "=:"},
		input: `
			[options]
			python_requires: >=3.9
			url = https://example.com:8080/x
			both: a = b
			`,
		wantCUE: `
			options: {
				python_requires: ">=3.9"
				url:             "https://example.com:8080/x"
				both:            "a = b"
			}
			`,
	}, {
		name:   "Delimiters/ColonOnly",
		config: ini.Config{Delimiters: ":"},
		input: `
			key: value
			equals = stays
			`,
		wantErr: `
			invalid line: equals = stays:
			    test.ini:2:1
			`,
	}, {
		name:   "Comments/Inline/QuotedValueKeepsIt",
		config: ini.Config{Comments: ini.CommentsInline, Quotes: ini.QuotesStripped},
		input: `
			kept = "one ; two"
			trailing = "one ; two" ; a comment
			color = #fff
			flags = -a #b
			`,
		wantCUE: `
			kept:     "one ; two"
			trailing: "one ; two"
			color:    "#fff"
			flags:    "-a"
			`,
	}, {name: "TypedValues/RejectedNumberForms",
		config: ini.Config{Values: ini.ValuesTyped},
		input: `
			hex = 0x10
			underscores = 1_000
			multiplier = 1M
			leadingZero = 010
			noIntegerPart = .5
			noFraction = 1.
			infinity = Inf
			`,
		wantCUE: `
			hex:           "0x10"
			underscores:   "1_000"
			multiplier:    "1M"
			leadingZero:   "010"
			noIntegerPart: ".5"
			noFraction:    "1."
			infinity:      "Inf"
			`,
	}, {
		name:   "TypedValues/AcceptedNumberForms",
		config: ini.Config{Values: ini.ValuesTyped},
		input: `
			zero = 0
			exponent = 1e3
			negativeExponent = 1.5E-3
			negative = -42
			positive = +1
			`,
		wantCUE: `
			zero:             0
			exponent:         1e3
			negativeExponent: 1.5E-3
			negative:         -42
			positive:         1
			`,
	}, {
		name:   "Booleans/TrueFalse",
		config: ini.Config{Values: ini.ValuesTyped, Booleans: ini.BooleansTrueFalse},
		input: `
			a = true
			b = FALSE
			c = yes
			d = on
			e = 1
			f = 0
			`,
		wantCUE: `
			a: true
			b: false
			c: "yes"
			d: "on"
			e: 1
			f: 0
			`,
	}, {
		name:   "Booleans/Extended",
		config: ini.Config{Values: ini.ValuesTyped, Booleans: ini.BooleansExtended},
		input: `
			a = true
			b = No
			c = yes
			d = OFF
			e = 1
			f = 0
			g = 2
			`,
		wantCUE: `
			a: true
			b: false
			c: true
			d: false
			e: true
			f: false
			g: 2
			`,
	}, {name: "BareKeys/Error",
		config: ini.Config{BareKeys: ini.BareKeysError},
		input: `
			[Core]
			sparseCheckout
			`,
		wantErr: `
			invalid line: sparseCheckout:
			    test.ini:2:1
			`,
	}, {
		name:   "BareKeys/Null",
		config: ini.Config{BareKeys: ini.BareKeysNull},
		input: `
			[Core]
			sparseCheckout
			empty =
			`,
		wantCUE: `
			Core: {
				sparseCheckout: null
				empty:          ""
			}
			`,
	}, {
		name:   "BareKeys/True",
		config: ini.Config{BareKeys: ini.BareKeysTrue},
		input: `
			[Core]
			sparseCheckout
			empty =
			`,
		wantCUE: `
			Core: {
				sparseCheckout: true
				empty:          ""
			}
			`,
	}, {
		name:   "BareKeys/True/Duplicate",
		config: ini.Config{BareKeys: ini.BareKeysTrue, DuplicateKeys: ini.DuplicatesList},
		input: `
			[Core]
			sparseCheckout
			sparseCheckout
			`,
		wantCUE: `
			Core: sparseCheckout: [true, true]
			`,
	}, {name: "Continuations/Backslash",
		config: ini.Config{Continuations: ini.ContinuationsBackslash},
		input: `
			[alias]
			lg = log --graph \
				--oneline \
				--decorate
			plain = log
			`,
		wantCUE: `
			alias: {
				lg:    "log --graph \t--oneline \t--decorate"
				plain: "log"
			}
			`,
	}, {
		name:   "Continuations/Backslash/ContinuationIsNotClassified",
		config: ini.Config{Continuations: ini.ContinuationsBackslash},
		input: `
			comment = a\
			; still the value
			header = b\
			[still the value]
			`,
		wantCUE: `
			comment: "a; still the value"
			header:  "b[still the value]"
			`,
	}, {
		name:   "Continuations/Backslash/CommentsAndHeadersDoNotContinue",
		config: ini.Config{Continuations: ini.ContinuationsBackslash},
		input: `
			; a comment ending in a backslash \
			[section]
			key = value
			`,
		wantCUE: `
			section: key: "value"
			`,
	}, {
		// A backslash with nothing after it ends the value, as git and
		// systemd read one, and git keeps the space before it.
		name:   "Continuations/Backslash/NothingAfterTheBackslash",
		config: ini.Config{Continuations: ini.ContinuationsBackslash},
		input: `
			key = value \
			`,
		wantCUE: `
			key: "value "
			`,
		noEncode: "a trailing space has no spelling under literal quotes",
	}, {
		// A backslash reached only by ignoring an inline comment is part of
		// the comment, so it continues nothing and the next property stands.
		name:   "Continuations/Backslash/BackslashInsideAnInlineComment",
		config: ini.Config{Continuations: ini.ContinuationsBackslash, Comments: ini.CommentsInline},
		input: `
			[core]
			a = one ; a comment \
			b = two
			`,
		wantCUE: `
			core: {
				a: "one"
				b: "two"
			}
			`,
	}, {
		// Inline comments apply to continued text too.
		name:   "Continuations/Backslash/ContinuedTextCarriesAComment",
		config: ini.Config{Continuations: ini.ContinuationsBackslash, Comments: ini.CommentsInline},
		input: `
			a = one\
			two ; a comment
			`,
		wantCUE: `
			a: "onetwo"
			`,
	}, {
		name:   "Continuations/Backslash/QuotedValueSpansLines",
		config: ini.Config{Continuations: ini.ContinuationsBackslash, Comments: ini.CommentsInline, Quotes: ini.QuotesEscaped},
		input: `
			[a]
			key = "one ; two \
				three"
			`,
		wantCUE: `
			a: key: "one ; two \tthree"
			`,
	}, {
		// A quote within the value opens a span when its match is on a
		// line the value continues onto, as git reads a double quote, while
		// one with no match anywhere stays an ordinary character.
		name:   "Continuations/Backslash/QuotedSpanWithinAValueSpansLines",
		config: ini.Config{Continuations: ini.ContinuationsBackslash, Comments: ini.CommentsInline, Quotes: ini.QuotesEscaped},
		input: `
			[alias]
			demo = !echo "one \
			 #two"
			semi = !echo "a ; b \
			 c" ; a comment
			apos = don't ; a comment \
			other = 1
			`,
		wantCUE: `
			alias: {
				demo:  "!echo one  #two"
				semi:  "!echo a ; b  c"
				apos:  "don't"
				other: "1"
			}
			`,
	}, {
		// An even run of trailing backslashes continues nothing, so the
		// next property stands. Both backslashes stay in the value, since
		// nothing unescapes an unquoted one.
		name:   "Continuations/Backslash/EscapedTrailingBackslash",
		config: ini.Config{Continuations: ini.ContinuationsBackslash, Comments: ini.CommentsInline},
		input: `
			[core]
			a = path\\
			b = two
			`,
		wantCUE: `
			core: {
				a: "path\\\\"
				b: "two"
			}
			`,
	}, {
		name:   "Continuations/Backslash/OddRunContinues",
		config: ini.Config{Continuations: ini.ContinuationsBackslash},
		input: `
			a = one\\\
			two
			`,
		wantCUE: `
			a: "one\\\\two"
			`,
	}, {
		// A comment may open the line a value continues onto, which ends the
		// value there, keeping the space before the backslash as git does.
		name:   "Continuations/Backslash/CommentOpensTheContinuedLine",
		config: ini.Config{Continuations: ini.ContinuationsBackslash, Comments: ini.CommentsInline},
		input: `
			[a]
			k = one \
			; a comment
			`,
		wantCUE: `
			a: k: "one "
			`,
		noEncode: "a trailing space has no spelling under literal quotes",
	}, {
		// A blank line after a backslash ends the value, as git and systemd
		// read one.
		name:   "Continuations/Backslash/BlankLineAfterTheBackslash",
		config: ini.Config{Continuations: ini.ContinuationsBackslash},
		input: `
			a = one\

			b = two
			`,
		wantCUE: `
			a: "one"
			b: "two"
			`,
	}, {
		// A value starts at its first character other than whitespace, even
		// one on a continuation line, as git reads it.
		name:   "Continuations/Backslash/EmptyFirstLine",
		config: ini.Config{Continuations: ini.ContinuationsBackslash},
		input: `
			x = \
			   foo
			`,
		wantCUE: `
			x: "foo"
			`,
	}, {
		name:   "Continuations/BackslashSpace",
		config: ini.Config{Continuations: ini.ContinuationsBackslashSpace},
		input: `
			[Service]
			ExecStart=/bin/echo one\
			two
			After=network.target \
			auditd.service
			`,
		wantCUE: `
			Service: {
				ExecStart: "/bin/echo one two"
				After:     "network.target  auditd.service"
			}
			`,
	}, {
		// systemd.syntax(7) ignores a comment block between a backslash and
		// the line it continues onto.
		name:   "Continuations/BackslashSpace/CommentBlockIsSkipped",
		config: ini.Config{Continuations: ini.ContinuationsBackslashSpace},
		input: `
			[Section C]
			KeyThree=value 3\
			# this line is ignored
			; this line is ignored too
			       value 3 continued
			`,
		wantCUE: `
			"Section C": KeyThree: "value 3        value 3 continued"
			`,
	}, {
		// Only a backslash ending the line continues the value, so one
		// followed by spaces stays in it, as systemd reads it.
		name:   "Continuations/BackslashSpace/SpaceAfterTheBackslash",
		config: ini.Config{Continuations: ini.ContinuationsBackslashSpace},
		input:  "a = one \\  \nb = two\n",
		wantCUE: `
			a: "one \\"
			b: "two"
			`,
		noEncode: "a trailing backslash continues the value under literal quotes",
	}, {
		name:   "Continuations/BackslashSpace/BlankLineAfterTheBackslash",
		config: ini.Config{Continuations: ini.ContinuationsBackslashSpace},
		input: `
			a = one \

			b = two
			`,
		wantCUE: `
			a: "one"
			b: "two"
			`,
	}, {
		// configparser ignores a comment line within a value, indented or
		// not, and resumes the value after it.
		name:   "Continuations/Indented/CommentWithinTheValue",
		config: ini.Config{Continuations: ini.ContinuationsIndented},
		input: `
			[s]
			indented = one
			    # explanation
			    two
			unindented = one
			# explanation
			    two
			`,
		wantCUE: `
			s: {
				indented:   "one\ntwo"
				unindented: "one\ntwo"
			}
			`,
	}, {name: "Continuations/Indented",
		config: ini.Config{Continuations: ini.ContinuationsIndented},
		input: `
			[metadata]
			long_description = the first line
			    the second line
			        the third line
			license = Apache-2.0
			`,
		wantCUE: `
			metadata: {
				long_description: "the first line\nthe second line\nthe third line"
				license:          "Apache-2.0"
			}
			`,
	}, {
		name:   "Continuations/Indented/InlineCommentInContinuedText",
		config: ini.Config{Continuations: ini.ContinuationsIndented, Comments: ini.CommentsInline},
		input: `
			key = first ; a comment
			    second ; another
			`,
		wantCUE: `
			key: "first\nsecond"
			`,
	}, {
		name:   "Continuations/Indented/QuotedSpanWithinAValueSpansLines",
		config: ini.Config{Continuations: ini.ContinuationsIndented, Comments: ini.CommentsInline},
		input: `
			key = one "two
			    three ; four" ; a comment
			apos = don't
			    stop ; a comment
			`,
		wantCUE: `
			key:  "one \"two\nthree ; four\""
			apos: "don't\nstop"
			`,
	}, {
		// A blank line is an empty line of the value when a continuation line
		// follows it, as configparser reads one; otherwise the value ends
		// before it.
		name:   "Continuations/Indented/BlankLineWithinTheValue",
		config: ini.Config{Continuations: ini.ContinuationsIndented},
		input: `
			key = first
			    second

			    third = 3
			other = one
			    two


			next = 2
			`,
		wantCUE: `
			key:   "first\nsecond\n\nthird = 3"
			other: "one\ntwo"
			next:  "2"
			`,
	}, {
		// configparser trims each line of a value, so an empty first line
		// leaves the value starting with a line break.
		name:   "Continuations/Indented/EmptyFirstLine",
		config: ini.Config{Continuations: ini.ContinuationsIndented},
		input: `
			install_requires =
			    foo

			    bar
			`,
		wantCUE: `
			install_requires: "\nfoo\n\nbar"
			`,
	}, {
		name:   "Continuations/Indented/SameIndentIsNotAContinuation",
		config: ini.Config{Continuations: ini.ContinuationsIndented},
		input: `
			[options]
			    python_requires = >=3.9
			    zip_safe = False
			`,
		wantCUE: `
			options: {
				python_requires: ">=3.9"
				zip_safe:        "False"
			}
			`,
	}, {
		name:   "Continuations/Indented/NoPrecedingProperty",
		config: ini.Config{Continuations: ini.ContinuationsIndented},
		input: `
			    orphan
			key = value
			`,
		wantErr: `
			invalid line: orphan:
			    test.ini:1:1
			`,
	}, {
		name:   "Continuations/Indented/HeaderEndsTheContinuation",
		config: ini.Config{Continuations: ini.ContinuationsIndented},
		input: `
			key = first
			[section]
			other = second
			`,
		wantCUE: `
			key: "first"
			section: other: "second"
			`,
	}, {name: "DuplicateKeys/Error",
		config: ini.Config{DuplicateKeys: ini.DuplicatesError},
		input: `
			[section]
			key = value1
			key = value2
			`,
		wantErr: `
			duplicate key: key:
			    test.ini:3:1
			`,
	}, {
		name:   "DuplicateKeys/List",
		config: ini.Config{DuplicateKeys: ini.DuplicatesList},
		input: `
			[section]
			once = a
			twice = b
			twice = c
			thrice = d
			thrice = e
			thrice = f
			`,
		wantCUE: `
			section: {
				once:   "a"
				twice: ["b", "c"]
				thrice: ["d", "e", "f"]
			}
			`,
	}, {
		name:   "DuplicateKeys/List/AcrossRepeatedHeaders",
		config: ini.Config{DuplicateKeys: ini.DuplicatesList},
		input: `
			[section]
			key = a

			[section]
			key = b
			`,
		wantCUE: `
			section: key: ["a", "b"]
			`,
	}, {
		name:   "DuplicateKeys/List/Typed",
		config: ini.Config{DuplicateKeys: ini.DuplicatesList, Values: ini.ValuesTyped},
		input: `
			port = 1
			port = 2
			`,
		wantCUE: `
			port: [1, 2]
			`,
	}, {
		name:   "DuplicateKeys/First",
		config: ini.Config{DuplicateKeys: ini.DuplicatesFirst},
		input: `
			[section]
			key = a
			key = b
			key = c
			`,
		wantCUE: `
			section: key: "a"
			`,
	}, {
		name:   "DuplicateKeys/Last",
		config: ini.Config{DuplicateKeys: ini.DuplicatesLast},
		input: `
			[section]
			key = a
			key = b
			key = c
			`,
		wantCUE: `
			section: key: "c"
			`,
	}, {name: "QuotedSubsections",
		config: ini.Config{QuotedSubsections: true},
		input: `
			[remote "origin"]
			url = https://example.com/x.git

			[remote "upstream"]
			url = https://example.com/y.git
			`,
		wantCUE: `
			remote: {
				origin: url:   "https://example.com/x.git"
				upstream: url: "https://example.com/y.git"
			}
			`,
	}, {
		name:   "QuotedSubsections/BaseIsNotSplitWithoutDottedSections",
		config: ini.Config{QuotedSubsections: true},
		input: `
			[a.b "c"]
			key = value
			`,
		wantCUE: `
			"a.b": c: key: "value"
			`,
	}, {
		name:   "QuotedSubsections/WithDottedSections",
		config: ini.Config{QuotedSubsections: true, DottedSections: true},
		input: `
			[a.b "c.d"]
			key = value
			`,
		wantCUE: `
			a: b: "c.d": key: "value"
			`,
		noEncode: "a quoted subsection name is written only at depth two, and a dotted header cannot hold a dot in a name",
	}, {
		name:   "QuotedSubsections/CaseLowerKeepsSubsectionCase",
		config: ini.Config{QuotedSubsections: true, Case: ini.CaseLower},
		input: `
			[URL "https://Example.COM/"]
			insteadOf = ex:
			`,
		wantCUE: `
			url: "https://Example.COM/": insteadof: "ex:"
			`,
	}, {
		name:   "QuotedSubsections/BracketInSubsectionName",
		config: ini.Config{QuotedSubsections: true},
		input: `
			[a "b]c"]
			key = value
			`,
		wantCUE: `
			a: "b]c": key: "value"
			`,
	}, {
		// Paths are followed one segment at a time, so a name holding the
		// byte a joined path would need cannot collide with a longer path.
		name:   "QuotedSubsections/SectionPathsCannotCollide",
		config: ini.Config{QuotedSubsections: true, DottedSections: true},
		input:  "[a.b]\nx = 1\n[a\x00b]\ny = 2\n",
		wantCUE: `
			a: b: x: "1"
			"a\u0000b": y: "2"
			`,
	}, {
		name:   "QuotedSubsections/RepeatedHeaderMerges",
		config: ini.Config{QuotedSubsections: true},
		input: `
			[a "b"]
			key1 = value1

			[a "b"]
			key2 = value2
			`,
		wantCUE: `
			a: b: {
				key1: "value1"
				key2: "value2"
			}
			`,
	}, {
		name:   "QuotedSubsections/MissingClosingQuote",
		config: ini.Config{QuotedSubsections: true},
		input: `
			[a "b]
			key = value
			`,
		wantErr: `
			missing closing bracket for section header:
			    test.ini:1:1
			`,
	}, {
		// An unescaped quote ends the subsection name, so a second quoted
		// part is text after it.
		name:   "QuotedSubsections/QuoteInSubsectionName",
		config: ini.Config{QuotedSubsections: true},
		input: `
			[a "b"c"d"]
			key = value
			`,
		wantErr: `
			text after subsection name: a "b"c"d":
			    test.ini:1:1
			`,
	}, {
		// A backslash escapes the character after it, as git reads a
		// subsection name, so a quote, a backslash, and a "]" may all
		// appear in one.
		name:   "QuotedSubsections/Escapes",
		config: ini.Config{QuotedSubsections: true},
		input: `
			[a "x\"y"]
			k = 1
			[b "x\\y"]
			k = 2
			[c "x\ty]"]
			k = 3
			`,
		wantCUE: `
			a: "x\"y": k: "1"
			b: "x\\y": k: "2"
			c: "xty]": k: "3"
			`,
	}, {
		name:   "QuotedSubsections/TextAfterSubsectionName",
		config: ini.Config{QuotedSubsections: true},
		input: `
			[a "b" c]
			key = value
			`,
		wantErr: `
			text after subsection name: a "b" c:
			    test.ini:1:1
			`,
	}, {
		name:   "QuotedSubsections/PropertyThenSubsection",
		config: ini.Config{QuotedSubsections: true},
		input: `
			[a]
			b = 1

			[a "b"]
			key = 2
			`,
		wantErr: `
			section b conflicts with property of the same name:
			    test.ini:4:1
			`,
	}, {
		name:   "QuotedSubsections/SubsectionThenProperty",
		config: ini.Config{QuotedSubsections: true},
		input: `
			[a "b"]
			key = 1

			[a]
			b = 2
			`,
		wantErr: `
			property b conflicts with section of the same name:
			    test.ini:5:1
			`,
	}, {name: "Case/Preserve",
		config: ini.Config{Case: ini.CasePreserve},
		input: `
			[Database]
			Host = localhost
			`,
		wantCUE: `
			Database: Host: "localhost"
			`,
	}, {
		name:   "Case/LowerKeys",
		config: ini.Config{Case: ini.CaseLowerKeys},
		input: `
			AppName = MyApp

			[Database]
			Host = localhost
			PORT = 5432
			`,
		wantCUE: `
			appname: "MyApp"
			Database: {
				host: "localhost"
				port: "5432"
			}
			`,
	}, {
		name:   "Case/LowerKeys/SectionsKeepTheirCase",
		config: ini.Config{Case: ini.CaseLowerKeys},
		input: `
			[Section]
			key1 = value1

			[section]
			key2 = value2
			`,
		wantCUE: `
			Section: key1: "value1"
			section: key2: "value2"
			`,
	}, {
		name:   "Case/LowerKeys/DuplicateAfterFolding",
		config: ini.Config{Case: ini.CaseLowerKeys},
		input: `
			[section]
			Key = value1
			KEY = value2
			`,
		wantErr: `
			duplicate key: key:
			    test.ini:3:1
			`,
	}, {
		// Names compare regardless of case, and keep the spelling they were
		// first written with.
		name:   "Case/Insensitive",
		config: ini.Config{Case: ini.CaseInsensitive, DuplicateKeys: ini.DuplicatesFirst},
		input: `
			[Network]
			Timeout = 30
			timeout = 5
			[NETWORK]
			Proxy = example.com
			`,
		wantCUE: `
			Network: {
				Timeout: "30"
				Proxy:   "example.com"
			}
			`,
	}, {
		name:   "Case/Insensitive/DuplicateAfterFolding",
		config: ini.Config{Case: ini.CaseInsensitive},
		input: `
			Key = 1
			KEY = 2
			`,
		wantErr: `
			duplicate key: KEY:
			    test.ini:2:1
			`,
	}, {
		name:   "Case/Insensitive/QuotedSubsectionIsNeverFolded",
		config: ini.Config{Case: ini.CaseInsensitive, QuotedSubsections: true},
		input: `
			[a "X"]
			k = 1
			[A "x"]
			k = 2
			`,
		wantCUE: `
			a: {
				X: k: "1"
				x: k: "2"
			}
			`,
	}, {
		name:   "TypedValues/IntegerValues",
		config: ini.Config{Values: ini.ValuesTyped},
		input: `
			port = 8080
			count = 0
			negative = -42
			`,
		wantCUE: `
			port:     8080
			count:    0
			negative: -42
			`,
	}, {
		name:   "TypedValues/FloatValues",
		config: ini.Config{Values: ini.ValuesTyped},
		input: `
			version = 1.0
			rate = 3.14
			`,
		wantCUE: `
			version: 1.0
			rate:    3.14
			`,
	}, {
		name:   "TypedValues/BooleanValues",
		config: ini.Config{Values: ini.ValuesTyped},
		input: `
			enabled = true
			disabled = false
			mixed_case = True
			upper = FALSE
			`,
		wantCUE: `
			enabled:    true
			disabled:   false
			mixed_case: true
			upper:      false
			`,
	}, {
		name:   "TypedValues/MixedTypes",
		config: ini.Config{Values: ini.ValuesTyped},
		input: `
			name = MyApp
			port = 443
			version = 2.1
			debug = true
			`,
		wantCUE: `
			name:    "MyApp"
			port:    443
			version: 2.1
			debug:   true
			`,
	}, {
		name:   "TypedValues/StringsThatLookLikeNumbers",
		config: ini.Config{Values: ini.ValuesTyped},
		input: `
			zip = 01onal
			phone = 555-1234
			`,
		wantCUE: `
			zip:   "01onal"
			phone: "555-1234"
			`,
	}, {
		name:   "TypedValues/UnquotedBackslashesAreLiteral",
		config: ini.Config{Values: ini.ValuesTyped},
		input: `
			[section]
			greeting = hello\nworld
			tab = col1\tcol2
			backslash = C:\\Users\\me
			`,
		wantCUE: `
			section: {
				greeting:  "hello\\nworld"
				tab:       "col1\\tcol2"
				backslash: "C:\\\\Users\\\\me"
			}
			`,
	}, {
		name:   "TypedValues/QuotedEscapeSequences",
		config: ini.Config{Values: ini.ValuesTyped, Quotes: ini.QuotesEscaped},
		input: `
			[section]
			greeting = "hello\nworld"
			tab = "col1\tcol2"
			`,
		wantCUE: `
			section: {
				greeting: "hello\nworld"
				tab:      "col1\tcol2"
			}
			`,
	}, {
		name:   "TypedValues/QuotedNumberStaysString",
		config: ini.Config{Values: ini.ValuesTyped, Quotes: ini.QuotesStripped},
		input: `
			[section]
			port = "8080"
			pi = "3.14"
			`,
		wantCUE: `
			section: {
				port: "8080"
				pi:   "3.14"
			}
			`,
	}, {
		name:   "TypedValues/QuotedBoolStaysString",
		config: ini.Config{Values: ini.ValuesTyped, Quotes: ini.QuotesStripped},
		input: `
			[section]
			enabled = "true"
			disabled = "false"
			`,
		wantCUE: `
			section: {
				enabled:  "true"
				disabled: "false"
			}
			`,
	}, {
		name:   "TypedValues/QuotedStringStripsQuotes",
		config: ini.Config{Values: ini.ValuesTyped, Quotes: ini.QuotesStripped},
		input: `
			[section]
			name = "John Doe"
			greeting = "hello world"
			`,
		wantCUE: `
			section: {
				name:     "John Doe"
				greeting: "hello world"
			}
			`,
	}, {
		name:   "TypedValues/EmptyQuotedString",
		config: ini.Config{Values: ini.ValuesTyped, Quotes: ini.QuotesStripped},
		input: `
			[section]
			val = ""
			`,
		wantCUE: `
			section: val: ""
			`,
	}, {
		// A value that does not end with its opening quote is not enclosed in
		// quotes, so it is kept as written, as the profile API reads it.
		name:   "Quotes/Stripped/Unterminated",
		config: ini.Config{Quotes: ini.QuotesStripped},
		input: `
			[section]
			foo = "bar
			title = 'tis the season
			`,
		wantCUE: `
			section: {
				foo:   "\"bar"
				title: "'tis the season"
			}
			`,
	}, {
		name:   "Quotes/Escaped/Unterminated",
		config: ini.Config{Quotes: ini.QuotesEscaped},
		input: `
			[section]
			foo = a "bar
			`,
		wantErr: `
			unterminated quoted value: a "bar:
			    test.ini:2:7
			`,
	}, {
		name:   "TypedValues/TrailingQuoteIsString",
		config: ini.Config{Values: ini.ValuesTyped},
		input: `
			[section]
			foo = baz"
			`,
		wantCUE: `
			section: foo: "baz\""
			`,
	}, {
		name:   "TypedValues/NumberLikeStringStaysString",
		config: ini.Config{Values: ini.ValuesTyped},
		input: `
			[section]
			foo = 34.bad
			`,
		wantCUE: `
			section: foo: "34.bad"
			`,
	}, {
		name:   "CombinedStrategies/CaseLowerAndTypedValues",
		config: ini.Config{Case: ini.CaseLower, Values: ini.ValuesTyped, Quotes: ini.QuotesStripped},
		input: `
			AppName = MyApp
			[Database]
			Port = 8080
			Debug = True
			Version = 2.1
			Name = "MyDB"
			`,
		wantCUE: `
			appname: "MyApp"
			database: {
				port:    8080
				debug:   true
				version: 2.1
				name:    "MyDB"
			}
			`,
	}, {
		name:   "Quotes/Stripped",
		config: ini.Config{Quotes: ini.QuotesStripped},
		input: `
			[section]
			name = "John Doe"
			greeting = 'Hello World'
			empty = ""
			inner = a "b" c
			trailing = baz"
			path = "C:\Users\me"
			escapes = "a\nb"
			`,
		wantCUE: `
			section: {
				name:     "John Doe"
				greeting: "Hello World"
				empty:    ""
				inner:    "a \"b\" c"
				trailing: "baz\""
				path:     "C:\\Users\\me"
				escapes:  "a\\nb"
			}
			`,
	}, {
		// Quotes and escapes are read as git reads them: a double quote
		// anywhere opens or closes a quoted part, the escapes apply
		// throughout the value, and a single quote is an ordinary character.
		name:   "Quotes/Escaped",
		config: ini.Config{Quotes: ini.QuotesEscaped},
		input: `
			[section]
			backslash = "a\\b"
			doubleQuote = "a\"b"
			newline = "a\nb"
			tab = "a\tb"
			backspace = "a\bb"
			unquoted = a\nb
			parts = !echo "one"  "two"
			spaces = " a "
			single = 'q'
			`,
		wantCUE: `
			section: {
				backslash:   "a\\b"
				doubleQuote: "a\"b"
				newline:     "a\nb"
				tab:         "a\tb"
				backspace:   "a\bb"
				unquoted:    "a\nb"
				parts:       "!echo one  two"
				spaces:      " a "
				single:      "'q'"
			}
			`,
	}, {
		// git knows no \r or \' escape.
		name:   "Quotes/Escaped/NoCarriageReturnEscape",
		config: ini.Config{Quotes: ini.QuotesEscaped},
		input: `
			[section]
			cr = "a\rb"
			`,
		wantErr: `
			unknown escape sequence: \r:
			    test.ini:2:6
			`,
	}, {
		name:   "Quotes/Escaped/UnknownEscape",
		config: ini.Config{Quotes: ini.QuotesEscaped},
		input: `
			[section]
			path = "C:\Users\me"
			`,
		wantErr: `
			unknown escape sequence: \U:
			    test.ini:2:8
			`,
	}, {
		name:   "Quotes/Escaped/TrailingBackslash",
		config: ini.Config{Quotes: ini.QuotesEscaped},
		input: `
			[section]
			foo = "bar\\\"
			`,
		wantErr: `
			unterminated quoted value: "bar\\\":
			    test.ini:2:7
			`,
	}, {
		// A quoted span protects its contents wherever it sits in the value,
		// not only when it opens the value.
		name:   "Quotes/Stripped/QuotedSpanWithinAValue",
		config: ini.Config{Quotes: ini.QuotesStripped, Comments: ini.CommentsInline},
		input: `
			[alias]
			show = !echo "one # two"
			semi = !echo "a ; b" ; a comment
			single = 'Hello ; World'
			`,
		wantCUE: `
			alias: {
				show:   "!echo \"one # two\""
				semi:   "!echo \"a ; b\""
				single: "Hello ; World"
			}
			`,
	}, {
		// An unmatched quote that does not open the value is an ordinary
		// character, so an apostrophe does not protect what follows it.
		name:   "Quotes/Stripped/ApostropheIsNotAQuote",
		config: ini.Config{Quotes: ini.QuotesStripped, Comments: ini.CommentsInline},
		input: `
			apos = don't ; a comment
			`,
		wantCUE: `
			apos: "don't"
			`,
	}, {
		// The closing quote may only arrive on a continuation line, so until
		// it does the whole remainder is quoted and holds no comment.
		name:   "Quotes/Stripped/UnfinishedQuoteHoldsNoComment",
		config: ini.Config{Quotes: ini.QuotesStripped, Comments: ini.CommentsInline},
		input: `
			[section]
			foo = "one ; two
			`,
		wantCUE: `
			section: foo: "\"one ; two"
			`,
	}, {
		name:   "Quotes/Literal/UnfinishedQuoteHoldsNoComment",
		config: ini.Config{Comments: ini.CommentsInline},
		input: `
			[section]
			foo = "one ; two
			`,
		wantCUE: `
			section: foo: "\"one ; two"
			`,
	}, {
		// A value that starts and ends with the same quote loses those two
		// characters, whatever lies between them, as the profile API reads
		// one.
		name:   "Quotes/Stripped/PartlyQuoted",
		config: ini.Config{Quotes: ini.QuotesStripped},
		input: `
			[section]
			two = "a" "b"
			tail = "a" c
			`,
		wantCUE: `
			section: {
				two:  "a\" \"b"
				tail: "\"a\" c"
			}
			`,
	}, {
		name: "HeaderTrailingText",
		input: `
			[a]b
			key = value
			`,
		wantErr: `
			unexpected text after section header: b:
			    test.ini:1:1
			`,
	}, {
		name: "HeaderTrailingComment/RejectedByDefault",
		input: `
			[s] ; c
			key = value
			`,
		wantErr: `
			unexpected text after section header: ; c:
			    test.ini:1:1
			`,
	}, {
		name:   "HeaderTrailingComment/AcceptedWithComments",
		config: ini.Config{Comments: ini.CommentsInline},
		input: `
			[s] ; c
			key = value
			`,
		wantCUE: `
			s: key: "value"
			`,
	}, {
		// configparser and the profile API end a header at its last "]" and
		// ignore what follows it.
		name:   "TrailingHeaderText",
		config: ini.Config{TrailingHeaderText: true},
		input: `
			[s] ; a comment
			a = 1
			[t] anything
			b = 2
			[c]d]
			e = 3
			`,
		wantCUE: `
			s: a: "1"
			t: b: "2"
			"c]d": e: "3"
			`,
	}, {
		name:  "LeadingBOM",
		input: "\ufeffkey = value\n",
		wantCUE: `
			key: "value"
			`,
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			input := unindent(test.input)
			dec := ini.NewDecoder("test.ini", strings.NewReader(input), test.config)
			cueExpr, err := dec.Decode()

			if test.wantErr != "" {
				gotErr := strings.TrimSuffix(errors.Details(err, nil), "\n")
				wantErr := unindent(test.wantErr)
				qt.Assert(t, qt.Equals(gotErr, wantErr))
				return
			}
			qt.Assert(t, qt.IsNil(err))

			// Verify second decode returns EOF.
			_, err = dec.Decode()
			qt.Assert(t, qt.ErrorMatches(err, "EOF"))

			wantCUE := unindent(test.wantCUE)

			wantFormatted, err := format.Source([]byte(wantCUE))
			qt.Assert(t, qt.IsNil(err), qt.Commentf("wantCUE:\n%s", wantCUE))

			rootCueFile, err := astutil.ToFile(cueExpr)
			qt.Assert(t, qt.IsNil(err))

			actualCue, err := format.Node(rootCueFile)
			qt.Assert(t, qt.IsNil(err))

			qt.Assert(t, qt.Equals(string(actualCue), string(wantFormatted)))

			// The decoded value, encoded under the same flavor, reads back.
			v := cuecontext.New().BuildExpr(cueExpr)
			out, err := encode(test.config, v)
			if test.noEncode != "" {
				qt.Assert(t, qt.ErrorMatches(err, `cannot write .*`), qt.Commentf("%s", test.noEncode))
				return
			}
			qt.Assert(t, qt.IsNil(err))
			roundTrip(t, test.config, v, out)
		})
	}
}

// TestPositions checks the positions a decoded field and its value carry.
func TestPositions(t *testing.T) {
	t.Parallel()

	dec := ini.NewDecoder("test.ini", strings.NewReader("[section]\nkey = value\n"), ini.Config{})
	expr, err := dec.Decode()
	qt.Assert(t, qt.IsNil(err))

	sec := expr.(*ast.StructLit).Elts[0].(*ast.Field)
	field := sec.Value.(*ast.StructLit).Elts[0].(*ast.Field)
	qt.Assert(t, qt.Equals(field.Label.Pos().String(), "test.ini:2:1"))
	qt.Assert(t, qt.Equals(field.Value.Pos().String(), "test.ini:2:7"))
}

// override applies fn to a copy of cfg, so that a table case can state a
// flavor plus the one option it changes.
func override(cfg ini.Config, fn func(*ini.Config)) ini.Config {
	fn(&cfg)
	return cfg
}

// unindent strips the common leading whitespace from a multi-line raw string,
// using the indentation of the last line (the line containing the closing backtick)
// as the prefix to remove. This matches the convention used in encoding/toml tests.
func unindent(s string) string {
	i := strings.LastIndexByte(s, '\n')
	if i < 0 {
		return s
	}
	prefix := s[i:]
	s = strings.ReplaceAll(s, prefix, "\n")
	s = strings.TrimPrefix(s, "\n")
	s = strings.TrimSuffix(s, "\n")
	return s
}
