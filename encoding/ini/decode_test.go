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

	"cuelang.org/go/cue/ast/astutil"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/format"
	"cuelang.org/go/encoding/ini"
)

func TestDecoder(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		config  *ini.Config
		input   string
		wantCUE string
		wantErr string
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
		name: "NestedSections",
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
		name: "InlineComments",
		input: `
			[section]
			key1 = value1 ; this is an inline comment
			key2 = value2 # this is also an inline comment
			`,
		wantCUE: `
			section: {
				key1: "value1"
				key2: "value2"
			}
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
		name: "DeeplyNestedSections",
		input: `
			[a.b.c]
			key = value
			`,
		wantCUE: `
			a: b: c: key: "value"
			`,
	}, {
		name: "SectionWithSiblingAndNestedSection",
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
		name: "FullExample",
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
		name: "DuplicateSection",
		input: `
			[section]
			key1 = value1

			[section]
			key2 = value2
			`,
		wantErr: `
			duplicate section: section:
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
		name:   "CaseInsensitive/DuplicateKeys",
		config: &ini.Config{CaseSensitivity: ini.CaseLower},
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
		name:   "CaseInsensitive/DuplicateSections",
		config: &ini.Config{CaseSensitivity: ini.CaseLower},
		input: `
			[Section]
			key1 = value1

			[section]
			key2 = value2
			`,
		wantErr: `
			duplicate section: section:
			    test.ini:4:1
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
		name:   "CaseInsensitive/LowercasesKeysAndSections",
		config: &ini.Config{CaseSensitivity: ini.CaseLower},
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
		name:   "CaseInsensitive/LowercasesNestedSections",
		config: &ini.Config{CaseSensitivity: ini.CaseLower},
		input: `
			[Server.TLS]
			Cert = /path/to/cert
			`,
		wantCUE: `
			server: tls: cert: "/path/to/cert"
			`,
	}, {
		name:   "CaseInsensitive/MixedCaseKeys",
		config: &ini.Config{CaseSensitivity: ini.CaseLower},
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
		name:   "TypedValues/IntegerValues",
		config: &ini.Config{ValueTypes: ini.ValuesCUELiterals},
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
		config: &ini.Config{ValueTypes: ini.ValuesCUELiterals},
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
		config: &ini.Config{ValueTypes: ini.ValuesCUELiterals},
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
		config: &ini.Config{ValueTypes: ini.ValuesCUELiterals},
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
		config: &ini.Config{ValueTypes: ini.ValuesCUELiterals},
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
		config: &ini.Config{ValueTypes: ini.ValuesCUELiterals},
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
		config: &ini.Config{ValueTypes: ini.ValuesCUELiterals},
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
		config: &ini.Config{ValueTypes: ini.ValuesCUELiterals},
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
		config: &ini.Config{ValueTypes: ini.ValuesCUELiterals},
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
		config: &ini.Config{ValueTypes: ini.ValuesCUELiterals},
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
		config: &ini.Config{ValueTypes: ini.ValuesCUELiterals},
		input: `
			[section]
			val = ""
			`,
		wantCUE: `
			section: val: ""
			`,
	}, {
		name:   "TypedValues/UnclosedDoubleQuote",
		config: &ini.Config{ValueTypes: ini.ValuesCUELiterals},
		input: `
			[section]
			foo = "bar
			`,
		wantErr: `
			invalid quoted value: "bar:
			    test.ini:2:1
			`,
	}, {
		name:   "TypedValues/UnclosedSingleQuote",
		config: &ini.Config{ValueTypes: ini.ValuesCUELiterals},
		input: `
			[section]
			foo = 'bar
			`,
		wantErr: `
			invalid quoted value: 'bar:
			    test.ini:2:1
			`,
	}, {
		name:   "TypedValues/TrailingQuoteIsString",
		config: &ini.Config{ValueTypes: ini.ValuesCUELiterals},
		input: `
			[section]
			foo = baz"
			`,
		wantCUE: `
			section: foo: "baz\""
			`,
	}, {
		name:   "TypedValues/NumberLikeStringStaysString",
		config: &ini.Config{ValueTypes: ini.ValuesCUELiterals},
		input: `
			[section]
			foo = 34.bad
			`,
		wantCUE: `
			section: foo: "34.bad"
			`,
	}, {
		name:   "CombinedStrategies/CaseLowerAndTypedValues",
		config: &ini.Config{CaseSensitivity: ini.CaseLower, ValueTypes: ini.ValuesCUELiterals},
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
			if test.name == "SecondDecodeReturnsEOF" {
				_, err = dec.Decode()
				qt.Assert(t, qt.ErrorMatches(err, "EOF"))
			}

			wantCUE := unindent(test.wantCUE)

			wantFormatted, err := format.Source([]byte(wantCUE))
			qt.Assert(t, qt.IsNil(err), qt.Commentf("wantCUE:\n%s", wantCUE))

			rootCueFile, err := astutil.ToFile(cueExpr)
			qt.Assert(t, qt.IsNil(err))

			actualCue, err := format.Node(rootCueFile)
			qt.Assert(t, qt.IsNil(err))

			qt.Assert(t, qt.Equals(string(actualCue), string(wantFormatted)))
		})
	}
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
