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

// For reference, the Kubernetes-related format strings
// are defined here:
// https://github.com/kubernetes/apiextensions-apiserver/blob/aca9073a80bee92a0b77741b9c7ad444c49fe6be/pkg/apis/apiextensions/v1beta1/types_jsonschema.go#L73

var formatFuncs = sync.OnceValue(func() map[string]formatFuncInfo {
	return map[string]formatFuncInfo{
		"binary":                {openAPI, formatTODO},
		"bsonobjectid":          {k8s, formatTODO},
		"byte":                  {openAPI | k8s, formatTODO},
		"cidr":                  {k8s, formatTODO},
		"creditcard":            {k8s, formatTODO},
		"data":                  {openAPI, formatTODO},
		"date":                  {vfrom(VersionDraft7) | openAPI | k8s, formatDate},
		"date-time":             {allVersions | openAPI | k8s, formatDateTime},
		"datetime":              {k8s, formatDateTime},
		"double":                {openAPI | k8s, formatTODO},
		"duration":              {vfrom(VersionDraft2019_09) | k8s, formatTODO},
		"email":                 {allVersions | openAPI | k8s, formatTODO},
		"float":                 {openAPI | k8s, formatTODO},
		"hexcolor":              {k8s, formatTODO},
		"hostname":              {allVersions | openAPI | k8s, formatTODO},
		"idn-email":             {vfrom(VersionDraft7), formatTODO},
		"idn-hostname":          {vfrom(VersionDraft7), formatTODO},
		"int32":                 {openAPI | k8s, formatInt32},
		"int64":                 {openAPI | k8s, formatInt64},
		"ipv4":                  {allVersions | openAPI | k8s, formatTODO},
		"ipv6":                  {allVersions | openAPI | k8s, formatTODO},
		"iri":                   {vfrom(VersionDraft7), formatURI},
		"iri-reference":         {vfrom(VersionDraft7), formatURIReference},
		"isbn":                  {k8s, formatTODO},
		"isbn10":                {k8s, formatTODO},
		"isbn13":                {k8s, formatTODO},
		"json-pointer":          {vfrom(VersionDraft6), formatTODO},
		"mac":                   {k8s, formatTODO},
		"password":              {openAPI | k8s, formatTODO},
		"regex":                 {vfrom(VersionDraft7), formatRegex},
		"relative-json-pointer": {vfrom(VersionDraft7), formatTODO},
		"rgbcolor":              {k8s, formatTODO},
		"ssn":                   {k8s, formatTODO},
		"time":                  {vfrom(VersionDraft7), formatTODO},
		"uint32":                {k8s, formatUint32},
		"uint64":                {k8s, formatUint64},
		// TODO we should probably disallow non-ASCII URIs (IRIs) but
		// this is good enough for now.
		"uri":           {allVersions | openAPI | k8s, formatURI},
		"uri-reference": {vfrom(VersionDraft6), formatURIReference},
		"uri-template":  {vfrom(VersionDraft6), formatTODO},
		"uuid":          {vfrom(VersionDraft2019_09) | k8s, formatTODO},
		"uuid3":         {k8s, formatTODO},
		"uuid4":         {k8s, formatTODO},
		"uuid5":         {k8s, formatTODO},
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
		if s.cfg.StrictKeywords && !s.schemaVersion.is(openAPILike) {
			s.errf(n, "unknown format %q", formatStr)
		}
		return
	}
	if !s.schemaVersion.is(finfo.versions) {
		if s.cfg.StrictKeywords && !s.schemaVersion.is(openAPILike) {
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

func formatUint32(n cue.Value, s *state) {
	s.add(n, numType, ast.NewIdent("uint32"))
}

func formatUint64(n cue.Value, s *state) {
	s.add(n, numType, ast.NewIdent("uint64"))
}

func formatTODO(n cue.Value, s *state) {}
