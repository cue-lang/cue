package crd

import (
	"fmt"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
	"cuelang.org/go/encoding/yaml"
	v1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset/scheme"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
)

// Unmarshals a YAML file containing one or more CustomResourceDefinitions
// into a list of CRD objects
func Unmarshal(data []byte) ([]*v1.CustomResourceDefinition, error) {
	// The filename provided here is only used in error messages
	yf, err := yaml.Extract("crd.yaml", data)
	if err != nil {
		return nil, fmt.Errorf("input is not valid yaml: %w", err)
	}
	crdv := cuecontext.New().BuildFile(yf)

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

	// Create the "codec factory" that can decode CRDs
	codecs := serializer.NewCodecFactory(scheme.Scheme, serializer.EnableStrict)

	// Iterate over each CRD
	for _, cueval := range all {
		// Encode the CUE value as YAML bytes
		d, err := yaml.Encode(cueval)
		if err != nil {
			return ret, err
		}

		// Decode into a v1.CustomResourceDefinition
		obj := &v1.CustomResourceDefinition{}
		if err := runtime.DecodeInto(codecs.UniversalDecoder(), d, obj); err != nil {
			return ret, err
		}

		ret = append(ret, obj)
	}

	return ret, nil
}
