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
	"os"

	"cuelang.org/go/cue"
	"cuelang.org/go/encoding/yaml"
	v1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset/scheme"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
)

// Unmarshals a YAML file containing one or more CustomResourceDefinitions
// into a list of CRD objects
func UnmarshalFile(ctx *cue.Context, filename string) ([]*v1.CustomResourceDefinition, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, err
	}

	// The filename provided here is only used in error messages
	yf, err := yaml.Extract(filename, data)
	if err != nil {
		return nil, fmt.Errorf("input is not valid yaml: %w", err)
	}
	crdv := ctx.BuildFile(yf)

	return Unmarshal(crdv)
}

func Unmarshal(crdv cue.Value) ([]*v1.CustomResourceDefinition, error) {
	var all []cue.Value
	switch crdv.IncompleteKind() {
	case cue.StructKind:
		all = append(all, crdv)
	case cue.ListKind:
		iter, _ := crdv.List()
		for iter.Next() {
			all = append(all, iter.Value())
		}
	default:
		return nil, fmt.Errorf("input does not appear to be one or multiple CRDs: %s", crdv)
	}

	// Make return value list
	ret := make([]*v1.CustomResourceDefinition, 0, len(all))

	// Iterate over each CRD
	for _, cueval := range all {
		obj, err := UnmarshalOne(cueval)
		if err != nil {
			return ret, err
		}

		ret = append(ret, obj)
	}

	return ret, nil
}

// Unmarshals YAML data for a single containing one or more CustomResourceDefinitions
// into a list of CRD objects
func UnmarshalOne(val cue.Value) (*v1.CustomResourceDefinition, error) {
	// Encode the CUE value as YAML bytes
	d, err := yaml.Encode(val)
	if err != nil {
		return nil, err
	}

	// Decode into a v1.CustomResourceDefinition
	obj := &v1.CustomResourceDefinition{}
	if err = runtime.DecodeInto(serializer.NewCodecFactory(scheme.Scheme).UniversalDecoder(), d, obj); err != nil {
		return nil, err
	}

	return obj, nil
}
