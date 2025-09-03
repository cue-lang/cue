// Copyright 2023 CUE Authors
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

// Package openapi provides OpenAPI encoding and decoding functionality.
//
// This is an EXPERIMENTAL API.
package openapi

import (
	"cuelang.org/go/cue"
	"cuelang.org/go/encoding/openapi"
	"cuelang.org/go/internal/core/adt"
	"cuelang.org/go/internal/pkg"
	"cuelang.org/go/internal/value"
)

var (
	selfContainedPath    = cue.ParsePath("selfContained")
	expandReferencesPath = cue.ParsePath("expandReferences")
	infoPath             = cue.ParsePath("info")
)

// Marshal returns the OpenAPI encoding of schema for the given OpenAPI version.
// The optional config value can be used to make further adjustments.
//
// Experimental: this API may change.
func MarshalSchema(config cue.Value, schema pkg.Schema) (string, error) {
	ctx := value.OpContext(schema)
	return marshalSchema(ctx, config, schema)
}

func marshalSchema(_ *adt.OpContext, config cue.Value, schema pkg.Schema) (string, error) {
	selfContained, _ := config.LookupPath(selfContainedPath).Bool()
	expandReferences, _ := config.LookupPath(expandReferencesPath).Bool()

	version, err := config.LookupPath(cue.ParsePath("version")).String()
	if err != nil {
		return "", err
	}

	c := &openapi.Config{
		Version:          version,
		SelfContained:    selfContained,
		ExpandReferences: expandReferences,
	}

	if info := config.LookupPath(infoPath); info.Exists() {
		c.Info = info
	}

	b, err := openapi.Gen(schema, c)
	if err != nil {
		return "", err
	}
	return string(b), err
}
