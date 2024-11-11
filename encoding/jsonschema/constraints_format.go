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
)

type formatFuncInfo struct {
	versions versionSet
	f        func(n cue.Value, s *state)
}

var formatFuncs = sync.OnceValue(func() map[string]formatFuncInfo {
	return map[string]formatFuncInfo{
		"binary":                {openAPI, formatTODO},
		"byte":                  {openAPI, formatTODO},
		"data":                  {openAPI, formatTODO},
		"date":                  {vfrom(VersionDraft7) | openAPI, formatDate},
		"date-time":             {allVersions | openAPI, formatDateTime},
		"double":                {openAPI, formatTODO},
		"duration":              {vfrom(VersionDraft2019_09), formatTODO},
		"email":                 {allVersions | openAPI, formatTODO},
		"float":                 {openAPI, formatTODO},
		"hostname":              {allVersions | openAPI, formatTODO},
		"idn-email":             {vfrom(VersionDraft7), formatTODO},
		"idn-hostname":          {vfrom(VersionDraft7), formatTODO},
		"int32":                 {openAPI, formatInt32},
		"int64":                 {openAPI, formatInt64},
		"ipv4":                  {allVersions | openAPI, formatTODO},
		"ipv6":                  {allVersions | openAPI, formatTODO},
		"iri":                   {vfrom(VersionDraft7), formatURI},
		"iri-reference":         {vfrom(VersionDraft7), formatURIReference},
		"json-pointer":          {vfrom(VersionDraft6), formatTODO},
		"password":              {openAPI, formatTODO},
		"regex":                 {vfrom(VersionDraft7), formatRegex},
		"relative-json-pointer": {vfrom(VersionDraft7), formatTODO},
		"time":                  {vfrom(VersionDraft7), formatTODO},
		// TODO we should probably disallow non-ASCII URIs (IRIs) but
		// this is good enough for now.
		"uri":           {allVersions | openAPI, formatURI},
		"uri-reference": {vfrom(VersionDraft6), formatURIReference},
		"uri-template":  {vfrom(VersionDraft6), formatTODO},
		"uuid":          {vfrom(VersionDraft2019_09), formatTODO},
	}
})

func constraintFormat(key string, n cue.Value, s *state) {
	formatStr, ok := s.strValue(n)
	if !ok {
		return
	}
	// Note: OpenAPI 3.0 says "the format property is an open
	// string-valued property, and can have any value" so even when
	// StrictKeywords is true, we do not generate an error if we're
	// using OpenAPI. TODO it would still be nice to have a mode
	// that allows the use to find likely spelling mistakes in
	// format values in OpenAPI.
	finfo, ok := formatFuncs()[formatStr]
	if !ok {
		// TODO StrictKeywords isn't exactly right here, but in general
		// we want unknown formats to be ignored even when StrictFeatures
		// is enabled, and StrictKeywords is closest to what we want.
		// Perhaps we should have a "lint" mode?
		if s.cfg.StrictKeywords && s.schemaVersion != VersionOpenAPI {
			s.errf(n, "unknown format %q", formatStr)
		}
		return
	}
	if !finfo.versions.contains(s.schemaVersion) {
		if s.cfg.StrictKeywords && s.schemaVersion != VersionOpenAPI {
			s.errf(n, "format %q is not recognized in schema version %v", formatStr, s.schemaVersion)
		}
		return
	}
	finfo.f(n, s)
}

func formatURI(n cue.Value, s *state) {
	s.add(n, stringType, ast.NewSel(s.addImport(n, "net"), "AbsURL"))
}

func formatURIReference(n cue.Value, s *state) {
	s.add(n, stringType, ast.NewSel(s.addImport(n, "net"), "URL"))
}

func formatDateTime(n cue.Value, s *state) {
	// TODO this is a bit stricter than the spec, because the spec
	// allows lower-case "T" and "Z", and leap seconds, but
	// it's not bad for now.
	s.add(n, stringType, ast.NewSel(s.addImport(n, "time"), "Time"))
}

func formatDate(n cue.Value, s *state) {
	// TODO it might be nice to have a dedicated `time.Date` validator rather
	// than using `time.Format`.
	s.add(n, stringType, ast.NewCall(ast.NewSel(s.addImport(n, "time"), "Format"), ast.NewString("2006-01-02")))
}

func formatRegex(n cue.Value, s *state) {
	// TODO this is a bit stricter than the spec, because the spec
	// allows Perl idioms such as back-references.
	s.add(n, stringType, ast.NewSel(s.addImport(n, "regexp"), "Valid"))
}

func formatInt32(n cue.Value, s *state) {
	s.add(n, numType, ast.NewIdent("int32"))
}

func formatInt64(n cue.Value, s *state) {
	s.add(n, numType, ast.NewIdent("int64"))
}

func formatTODO(n cue.Value, s *state) {}
