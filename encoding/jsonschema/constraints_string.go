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

package jsonschema

import (
	"sync"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/token"
)

// String constraints

func constraintContentEncoding(key string, n cue.Value, s *state) {
	// TODO: only mark as used if it generates something.
	// 7bit, 8bit, binary, quoted-printable and base64.
	// RFC 2054, part 6.1.
	// https://tools.ietf.org/html/rfc2045
	// TODO: at least handle bytes.
}

func constraintContentMediaType(key string, n cue.Value, s *state) {
	// TODO: only mark as used if it generates something.
}

func constraintMaxLength(key string, n cue.Value, s *state) {
	max := s.number(n)
	strings := s.addImport(n, "strings")
	s.add(n, stringType, ast.NewCall(ast.NewSel(strings, "MaxRunes"), max))
}

func constraintMinLength(key string, n cue.Value, s *state) {
	min := s.number(n)
	strings := s.addImport(n, "strings")
	s.add(n, stringType, ast.NewCall(ast.NewSel(strings, "MinRunes"), min))
}

func constraintPattern(key string, n cue.Value, s *state) {
	str, ok := s.regexpValue(n)
	if !ok {
		return
	}
	s.add(n, stringType, &ast.UnaryExpr{Op: token.MAT, X: str})
}

type formatFuncInfo struct {
	versions versionSet
	f        func(s *state)
}

var formatFuncs = sync.OnceValue(func() map[string]formatFuncInfo {
	return map[string]formatFuncInfo{
		"binary":                {openAPI, formatTODO},
		"byte":                  {openAPI, formatTODO},
		"data":                  {openAPI, formatTODO},
		"date":                  {vfrom(VersionDraft7), formatTODO},
		"date-time":             {allVersions | openAPI, formatTODO},
		"double":                {openAPI, formatTODO},
		"duration":              {vfrom(VersionDraft2019_09), formatTODO},
		"email":                 {allVersions | openAPI, formatTODO},
		"float":                 {openAPI, formatTODO},
		"hostname":              {allVersions | openAPI, formatTODO},
		"idn-email":             {vfrom(VersionDraft7), formatTODO},
		"idn-hostname":          {vfrom(VersionDraft7), formatTODO},
		"int32":                 {openAPI, formatTODO},
		"int64":                 {openAPI, formatTODO},
		"ipv4":                  {allVersions | openAPI, formatTODO},
		"ipv6":                  {allVersions | openAPI, formatTODO},
		"iri":                   {vfrom(VersionDraft7), formatTODO},
		"iri-reference":         {vfrom(VersionDraft7), formatTODO},
		"json-pointer":          {vfrom(VersionDraft6), formatTODO},
		"password":              {openAPI, formatTODO},
		"regex":                 {vfrom(VersionDraft7), formatTODO},
		"relative-json-pointer": {vfrom(VersionDraft7), formatTODO},
		"time":                  {vfrom(VersionDraft7), formatTODO},
		"uri":                   {allVersions | openAPI, formatTODO},
		"uri-reference":         {vfrom(VersionDraft6), formatTODO},
		"uri-template":          {vfrom(VersionDraft6), formatTODO},
		"uuid":                  {vfrom(VersionDraft2019_09), formatTODO},
	}
})

func constraintFormat(key string, n cue.Value, s *state) {
	formatStr, ok := s.strValue(n)
	if !ok {
		return
	}
	finfo, ok := formatFuncs()[formatStr]
	if !ok {
		// TODO StrictKeywords isn't exactly right here, but in general
		// we want unknown formats to be ignored even when StrictFeatures
		// is enabled, and StrictKeywords is closest to what we want.
		// Perhaps we should have a "lint" mode?
		if s.cfg.StrictKeywords {
			s.errf(n, "unknown format %q", formatStr)
		}
		return
	}
	if !finfo.versions.contains(s.schemaVersion) {
		if s.cfg.StrictKeywords {
			s.errf(n, "format %q is not recognized in schema version %v", formatStr, s.schemaVersion)
		}
		return
	}
	finfo.f(s)
}

func formatTODO(s *state) {}
