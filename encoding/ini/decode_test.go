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

package ini_test

import (
	"strings"
	"testing"

	"github.com/go-quicktest/qt"

	"cuelang.org/go/cue/ast/astutil"
	"cuelang.org/go/cue/format"
	"cuelang.org/go/encoding/ini"
)

func TestDecoder(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
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
				port: 5432
			}
			server: {
				host: "0.0.0.0"
				port: 8080
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
			version:  1.0
			database: {
				host: "localhost"
				port: 5432
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
					min: 5
					max: 20
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
			firewall_inbound: {
				"*_netbiosudprule1":                 "UDP/137:*.*.*"
				"securemode:true$*_netbiosudprule1": ""
				"sku:nonstdhw$*_netbiosudprule1":    ""
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
				name:     "John Doe"
				greeting: "Hello World"
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
				pool_size: 10
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
				port: 3306
				name: "mydb"
				credentials: {
					username: "dbuser"
					password: "dbpass"
				}
			}
			server: {
				host: "0.0.0.0"
				port: 443
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
		wantErr: "duplicate key: key",
	}, {
		name: "DuplicateSection",
		input: `
			[section]
			key1 = value1

			[section]
			key2 = value2
			`,
		wantErr: "duplicate section: section",
	}, {
		name: "MissingClosingBracket",
		input: `
			[section
			key = value
			`,
		wantErr: "missing closing bracket for section header",
	}, {
		name: "EmptySectionName",
		input: `
			[]
			key = value
			`,
		wantErr: "empty section name",
	}, {
		name: "InvalidLine",
		input: `
			[section]
			not a valid line
			`,
		wantErr: "invalid line",
	}, {
		name: "SecondDecodeReturnsEOF",
		input: `
			key = value
			`,
		wantCUE: `
			key: "value"
			`,
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			input := unindent(test.input)
			dec := ini.NewDecoder("test.ini", strings.NewReader(input))
			cueExpr, err := dec.Decode()

			if test.wantErr != "" {
				qt.Assert(t, qt.ErrorMatches(err, ".*"+test.wantErr+".*"))
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
