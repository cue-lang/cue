package jsonschema

// input holds the parsed YAML document, which may contain multiple
// Kubernetes resources, in which case it will be an array.
input!: _

// specs holds the resulting CRD specs: when there was only
// a single resource in the input, we'll get an array with one
// element.
// TODO(rog) replace comparisons to bottom with whatever the
// correct replacement will be.
specs: {
	[...]
	if (input & [...]) != _|_ {
		// It's an array: include only elements that look like CRDs.
		[
			for doc in input
			if (doc & {#crdlike, ...}) != _|_ {
				// Note: don't check for unification with #CRDSpec above because
				// we want it to fail if it doesn't unify with the entirety of #CRDSpec,
				// not just exclude the document.
				doc
				#CRDSpec
			},
		]
	}
	if (input & [...]) == _|_ {
		// It's a single document. Include it if it looks like a CRD.
		if (input & {#crdlike, ...}) != _|_ {
			[{input, #CRDSpec}]
		}
	}
}

#crdlike: {
	apiVersion!: "apiextensions.k8s.io/v1"
	kind!:       "CustomResourceDefinition"
} @go(crdLike)

// CRDSpec defines a subset of the CRD schema, suitable for filtering
// CRDs based on common criteria like group and name.
#CRDSpec: {
	#crdlike
	apiVersion!: "apiextensions.k8s.io/v1"
	kind!:       "CustomResourceDefinition"
	spec!: {
		group!: string
		names!: {
			kind!:     string
			plural!:   string
			singular!: string
		}
		scope!: "Namespaced" | "Cluster"
		versions!: [... {
			name!: string
			schema!: {
				openAPIV3Schema!: _ @go(,type="cuelang.org/go/cue".Value)
			}
		}]
	}
}
