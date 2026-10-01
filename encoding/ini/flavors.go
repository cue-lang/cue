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

package ini

// A flavor is a complete [Config] value, so combining one with an explicit
// option is an ordinary assignment and there is no merge rule to learn:
//
//	cfg := ini.GitConfig()
//	cfg.Comments = ini.CommentsWholeLine
//
// Every flavor is defined entirely in terms of the [Config] fields; no
// behavior hides behind a flavor name.

// GitConfig returns the flavor of git-config(1), pinned by
// testdata/flavors/git.txtar. It sets:
//
//   - [Config.Comments] to [CommentsAnywhere]
//   - [Config.Quotes] to [QuotesEscaped], for git's quoting and escapes
//   - [Config.Case] to [CaseLower], since git folds section and key
//     names but not quoted subsection names
//   - [Config.DottedSections] and [Config.QuotedSubsections], for
//     [a.b] and [a "b"]
//   - [Config.DuplicateKeys] to [DuplicatesList], for multi-valued keys
//   - [Config.Continuations] to [ContinuationsBackslash]
//   - [Config.BareKeys] to [BareKeysTrue]
//
// Values are left as strings, since git types a value only where the
// caller asks for one, as in "git config --type=bool".
//
// It reads every value as git-config does. A key may hold characters other
// than letters, digits and "-", which git rejects, and a property on the
// same line as its section header is an error, where git reads it.
func GitConfig() Config {
	return Config{
		Comments:          CommentsAnywhere,
		Quotes:            QuotesEscaped,
		Case:              CaseLower,
		DottedSections:    true,
		QuotedSubsections: true,
		DuplicateKeys:     DuplicatesList,
		Continuations:     ContinuationsBackslash,
		BareKeys:          BareKeysTrue,
	}
}

// PythonConfig returns the flavor of Python's configparser with its
// default settings, pinned by testdata/flavors/python.txtar. It sets:
//
//   - [Config.Delimiters] to "=:"
//   - [Config.Case] to [CaseLowerKeys], since optionxform folds keys
//     while section names keep their case
//   - [Config.TrailingHeaderText], since a header ends at its last "]"
//   - [Config.DuplicateSections] to [DuplicateSectionsError], as strict
//     mode rejects a repeated section
//   - [Config.Continuations] to [ContinuationsIndented]
//
// Every other option keeps its default, which is what configparser does:
// no inline comments, literal quotes, a repeated key an error, and a
// bare key an error. Values are read raw, as with interpolation=None, so
// "%(name)s" is kept as written.
//
// It differs from configparser in one respect: a section name loses the
// spaces around it, as in "[ name ]", where configparser keeps them.
func PythonConfig() Config {
	return Config{
		Delimiters:         "=:",
		Case:               CaseLowerKeys,
		TrailingHeaderText: true,
		DuplicateSections:  DuplicateSectionsError,
		Continuations:      ContinuationsIndented,
	}
}

// SystemdConfig returns the flavor of systemd unit files, pinned by
// testdata/flavors/systemd.txtar. It sets:
//
//   - [Config.DuplicateKeys] to [DuplicatesList], for list settings such
//     as Environment
//   - [Config.Continuations] to [ContinuationsBackslashSpace], which is
//     the form systemd.syntax(7) describes: the backslash becomes a space
//     and a comment block between the two lines is skipped
//
// Every other option keeps its default: systemd preserves case, treats
// quotes literally, and only recognizes a comment on a line of its own.
// Quotes and escapes within a value are left to the setting that reads
// it, as systemd itself does.
func SystemdConfig() Config {
	return Config{
		DuplicateKeys: DuplicatesList,
		Continuations: ContinuationsBackslashSpace,
	}
}

// WindowsConfig returns the flavor of the Windows profile API, as
// GetPrivateProfileString reads it, pinned by
// testdata/flavors/windows.txtar. It sets:
//
//   - [Config.Quotes] to [QuotesStripped], since a value enclosed in
//     double or single quotes loses them while a backslash, as in a path,
//     stays literal
//   - [Config.Case] to [CaseInsensitive], since names are looked up
//     regardless of case
//   - [Config.TrailingHeaderText], since a header ends at its last "]"
//   - [Config.DuplicateKeys] to [DuplicatesFirst], since a lookup finds
//     the first occurrence of a key
//   - [Config.DuplicateSections] to [DuplicateSectionsFirst], since a
//     lookup reads a section's keys from its first header only
//
// Every other option keeps its default, as the profile API has no
// delimiter but "=", no inline comments, continuations, nested sections,
// or bare keys.
//
// It differs from Windows in one respect: the input is UTF-8, where
// Windows also reads UTF-16 and the system code page.
func WindowsConfig() Config {
	return Config{
		Quotes:             QuotesStripped,
		Case:               CaseInsensitive,
		TrailingHeaderText: true,
		DuplicateKeys:      DuplicatesFirst,
		DuplicateSections:  DuplicateSectionsFirst,
	}
}
