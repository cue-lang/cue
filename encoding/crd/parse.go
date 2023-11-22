/*
Copyright 2023 Stefan Prodan

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

	http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/
package crd

import (
	"fmt"

	"cuelang.org/go/cue"
	apiextensions "cuelang.org/go/encoding/crd/k8s/apiextensions/v1"
	"cuelang.org/go/encoding/yaml"
	goyaml "gopkg.in/yaml.v3"
)

// Splits a YAML file containing one or more YAML documents into its elements
func splitFile(ctx *cue.Context, filename string, data []byte) ([]cue.Value, error) {
	// The filename provided here is only used in error messages
	yf, err := yaml.Extract(filename, data)
	if err != nil {
		return nil, fmt.Errorf("input is not valid yaml: %w", err)
	}

	val := ctx.BuildFile(yf)

	var all []cue.Value
	switch val.IncompleteKind() {
	case cue.StructKind:
		all = append(all, val)
	case cue.ListKind:
		iter, _ := val.List()
		for iter.Next() {
			all = append(all, iter.Value())
		}
	default:
		return nil, fmt.Errorf("input does not appear to be one or multiple YAML documents: %s", val)
	}

	return all, nil
}

// // Unmarshals a YAML file containing one or more CustomResourceDefinitions into a list of CRD objects
// func parseFile(ctx *cue.Context, filename string) ([]*apiextensions.CustomResourceDefinition, error) {
// 	data, err := os.ReadFile(filename)
// 	if err != nil {
// 		return nil, err
// 	}

// 	// The filename provided here is only used in error messages
// 	yf, err := yaml.Extract(filename, data)
// 	if err != nil {
// 		return nil, fmt.Errorf("input is not valid yaml: %w", err)
// 	}

// 	return parseMultiple(ctx.BuildFile(yf))
// }

// // Parses a cue.Value containing one or more CustomResourceDefinitions into a list of CRD objects
// func parseMultiple(val cue.Value) ([]*apiextensions.CustomResourceDefinition, error) {
// 	var all []cue.Value
// 	switch val.IncompleteKind() {
// 	case cue.StructKind:
// 		all = append(all, val)
// 	case cue.ListKind:
// 		iter, _ := val.List()
// 		for iter.Next() {
// 			all = append(all, iter.Value())
// 		}
// 	default:
// 		return nil, fmt.Errorf("input does not appear to be one or multiple CRDs: %s", val)
// 	}

// 	// Make return value list
// 	ret := make([]*apiextensions.CustomResourceDefinition, 0, len(all))

// 	// Iterate over each CRD
// 	for _, singleval := range all {
// 		obj, err := parseSingle(singleval)
// 		if err != nil {
// 			return ret, err
// 		}

// 		ret = append(ret, obj)
// 	}

// 	return ret, nil
// }

// Unmarshals YAML data for a single containing one or more CustomResourceDefinitions
// into a list of CRD objects
func parseCRD(val cue.Value) (*apiextensions.CustomResourceDefinition, error) {
	// Encode the CUE value as YAML bytes
	d, err := yaml.Encode(val)
	if err != nil {
		return nil, err
	}

	// Decode into a v1.CustomResourceDefinition
	obj := &apiextensions.CustomResourceDefinition{}
	err = goyaml.Unmarshal(d, obj)
	if err != nil {
		return nil, err
	}

	return obj, nil
}
