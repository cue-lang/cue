@experiment(explicitopen)

// This file models ordering of the dependencies between JSON Schema
// constraints via the data that each constraint produces or consumes.
//
// The implied edges are:
//   producer → data node → consumer

#ConstraintsGraph

// Schema for the whole of this file.
#ConstraintsGraph: {
	// internalNodes are producer/consumer names that are not real constraints
	// (for example internal helper combinators) and should be ignored when
	// computing phases.
	internalNodes!: [...string]

	// constraints lists all known JSON Schema keywords and their implementation
	// metadata (function name + supported versions).
	constraints!: [...#Constraint]

	// dataNodes declares the intermediate data items that constraints
	// produce/consume, keyed by a stable node name.
	dataNodes!: [string]: #DataNode
}

#Constraint: {
	// key is the JSON Schema keyword name (e.g. "items", "$ref").
	key!: string

	// fn is the Go constraint function name; if omitted, the generator
	// treats this as constraintTODO.
	fn?: string

	// versions is the versionSet expression string used in constraints.go.
	versions: string
}

#DataNode: {
	// description explains the semantics of the data node.
	description!: string

	// stateField identifies the state field(s) the data node maps to.
	stateField!: string

	// producers lists constraint keywords that write this data node.
	producers!: [...string]

	// consumers lists constraint keywords that read this data node.
	consumers!: [...string]

	// notes are optional human-readable caveats about the dependency.
	notes?: [...string]
}


internalNodes: ["ifThenElse"]

constraints: [
	{key: "$anchor", versions: "vfrom(VersionDraft2019_09)"},
	{key: "$comment", fn: "constraintComment", versions: "vfrom(VersionDraft7)"},
	{key: "$defs", fn: "constraintAddDefinitions", versions: "allVersions"},
	{key: "$dynamicAnchor", versions: "vfrom(VersionDraft2020_12)"},
	{key: "$dynamicRef", versions: "vfrom(VersionDraft2020_12)"},
	{key: "$id", fn: "constraintID", versions: "vfrom(VersionDraft6)"},
	{key: "$recursiveAnchor", versions: "vbetween(VersionDraft2019_09, VersionDraft2020_12)"},
	{key: "$recursiveRef", versions: "vbetween(VersionDraft2019_09, VersionDraft2020_12)"},
	{key: "$ref", fn: "constraintRef", versions: "allVersions|openAPI|k8sAPI"},
	{key: "$schema", fn: "constraintSchema", versions: "allVersions"},
	{key: "$vocabulary", versions: "vfrom(VersionDraft2019_09)"},
	{key: "additionalItems", fn: "constraintAdditionalItems", versions: "vto(VersionDraft2019_09)"},
	{key: "additionalProperties", fn: "constraintAdditionalProperties", versions: "allVersions|openAPILike"},
	{key: "allOf", fn: "constraintAllOf", versions: "allVersions|openAPILike"},
	{key: "anyOf", fn: "constraintAnyOf", versions: "allVersions|openAPILike"},
	{key: "const", fn: "constraintConst", versions: "vfrom(VersionDraft6)"},
	{key: "contains", fn: "constraintContains", versions: "vfrom(VersionDraft6)"},
	{key: "contentEncoding", fn: "constraintContentEncoding", versions: "vfrom(VersionDraft7)"},
	{key: "contentMediaType", fn: "constraintContentMediaType", versions: "vfrom(VersionDraft7)"},
	{key: "contentSchema", versions: "vfrom(VersionDraft2019_09)"},
	{key: "default", fn: "constraintDefault", versions: "allVersions|openAPILike"},
	{key: "definitions", fn: "constraintAddDefinitions", versions: "allVersions"},
	{key: "dependencies", fn: "constraintDependencies", versions: "allVersions"},
	{key: "dependentRequired", fn: "constraintDependencies", versions: "vfrom(VersionDraft2019_09)"},
	{key: "dependentSchemas", fn: "constraintDependencies", versions: "vfrom(VersionDraft2019_09)"},
	{key: "deprecated", fn: "constraintDeprecated", versions: "vfrom(VersionDraft2019_09)|openAPI"},
	{key: "description", fn: "constraintDescription", versions: "allVersions|openAPILike"},
	{key: "discriminator", versions: "openAPI"},
	{key: "else", fn: "constraintElse", versions: "vfrom(VersionDraft7)"},
	{key: "enum", fn: "constraintEnum", versions: "allVersions|openAPILike"},
	{key: "example", versions: "openAPILike"},
	{key: "examples", fn: "constraintExamples", versions: "vfrom(VersionDraft6)"},
	{key: "exclusiveMaximum", fn: "constraintExclusiveMaximum", versions: "allVersions|openAPILike"},
	{key: "exclusiveMinimum", fn: "constraintExclusiveMinimum", versions: "allVersions|openAPILike"},
	{key: "externalDocs", versions: "openAPILike"},
	{key: "format", fn: "constraintFormat", versions: "allVersions|openAPILike"},
	{key: "id", fn: "constraintID", versions: "vto(VersionDraft4)"},
	{key: "if", fn: "constraintIf", versions: "vfrom(VersionDraft7)"},
	{key: "items", fn: "constraintItems", versions: "allVersions|openAPILike"},
	{key: "maxContains", fn: "constraintMaxContains", versions: "vfrom(VersionDraft2019_09)"},
	{key: "maxItems", fn: "constraintMaxItems", versions: "allVersions|openAPILike"},
	{key: "maxLength", fn: "constraintMaxLength", versions: "allVersions|openAPILike"},
	{key: "maxProperties", fn: "constraintMaxProperties", versions: "allVersions|openAPILike"},
	{key: "maximum", fn: "constraintMaximum", versions: "allVersions|openAPILike"},
	{key: "minContains", fn: "constraintMinContains", versions: "vfrom(VersionDraft2019_09)"},
	{key: "minItems", fn: "constraintMinItems", versions: "allVersions|openAPILike"},
	{key: "minLength", fn: "constraintMinLength", versions: "allVersions|openAPILike"},
	{key: "minProperties", fn: "constraintMinProperties", versions: "allVersions|openAPILike"},
	{key: "minimum", fn: "constraintMinimum", versions: "allVersions|openAPILike"},
	{key: "multipleOf", fn: "constraintMultipleOf", versions: "allVersions|openAPILike"},
	{key: "not", fn: "constraintNot", versions: "allVersions|openAPILike"},
	{key: "nullable", fn: "constraintNullable", versions: "openAPILike"},
	{key: "oneOf", fn: "constraintOneOf", versions: "allVersions|openAPILike"},
	{key: "pattern", fn: "constraintPattern", versions: "allVersions|openAPILike"},
	{key: "patternProperties", fn: "constraintPatternProperties", versions: "allVersions"},
	{key: "prefixItems", fn: "constraintPrefixItems", versions: "vfrom(VersionDraft2020_12)"},
	{key: "properties", fn: "constraintProperties", versions: "allVersions|openAPILike"},
	{key: "propertyNames", fn: "constraintPropertyNames", versions: "vfrom(VersionDraft6)"},
	{key: "readOnly", versions: "vfrom(VersionDraft7)|openAPI"},
	{key: "required", fn: "constraintRequired", versions: "allVersions|openAPILike"},
	{key: "then", fn: "constraintThen", versions: "vfrom(VersionDraft7)"},
	{key: "title", fn: "constraintTitle", versions: "allVersions|openAPILike"},
	{key: "type", fn: "constraintType", versions: "allVersions|openAPILike"},
	{key: "unevaluatedItems", versions: "vfrom(VersionDraft2019_09)"},
	{key: "unevaluatedProperties", versions: "vfrom(VersionDraft2019_09)"},
	{key: "uniqueItems", fn: "constraintUniqueItems", versions: "allVersions|openAPILike"},
	{key: "writeOnly", versions: "vfrom(VersionDraft7)|openAPI"},
	{key: "xml", versions: "openAPI"},
	{key: "x-kubernetes-embedded-resource", fn: "constraintEmbeddedResource", versions: "k8s"},
	{key: "x-kubernetes-group-version-kind", fn: "constraintGroupVersionKind", versions: "k8sAPI"},
	{key: "x-kubernetes-int-or-string", fn: "constraintIntOrString", versions: "k8s"},
	{key: "x-kubernetes-list-map-keys", fn: "constraintIgnore", versions: "k8s"},
	{key: "x-kubernetes-list-type", fn: "constraintIgnore", versions: "k8s"},
	{key: "x-kubernetes-map-type", fn: "constraintIgnore", versions: "k8s"},
	{key: "x-kubernetes-patch-merge-key", fn: "constraintIgnore", versions: "k8s"},
	{key: "x-kubernetes-patch-strategy", fn: "constraintIgnore", versions: "k8s"},
	{key: "x-kubernetes-preserve-unknown-fields", fn: "constraintPreserveUnknownFields", versions: "k8s"},
	{key: "x-kubernetes-validations", versions: "k8s"},
]

dataNodes: {
	SchemaVersion: {
		description: "Active JSON Schema version used for keyword gating."
		stateField:  "s.schemaVersion"
		producers:   ["$schema"]
		// This is a global dependency. Consumers are all constraints.
		consumers: [
			for c in constraints
			if c.key != "$schema" && c.fn != _|_ {
				c.key
			}
		]
	}

	SchemaBaseURI: {
		description: "Base URI for resolving $ref and nested subschemas."
		stateField:  "s.id / schemaRoot().id"
		producers:   ["$id", "id"]
		consumers: [
			"$ref",
			"$defs",
			"definitions",
			"allOf",
			"anyOf",
			"oneOf",
			"not",
			"if",
			"then",
			"else",
			"items",
			"prefixItems",
			"additionalItems",
			"contains",
			"properties",
			"patternProperties",
			"additionalProperties",
			"propertyNames",
			"dependencies",
			"dependentSchemas",
			"dependentRequired",
		]
	}

	MinContains: {
		description: "Minimum number of elements matching contains."
		stateField:  "s.minContains"
		producers:   ["minContains"]
		consumers:   ["contains"]
	}

	MaxContains: {
		description: "Maximum number of elements matching contains."
		stateField:  "s.maxContains"
		producers:   ["maxContains"]
		consumers:   ["contains"]
	}

	ExclusiveMinimumFlag: {
		description: "Legacy boolean exclusiveMinimum flag (pre-draft6)."
		stateField:  "s.exclusiveMin"
		producers:   ["exclusiveMinimum"]
		consumers:   ["minimum"]
	}

	ExclusiveMaximumFlag: {
		description: "Legacy boolean exclusiveMaximum flag (pre-draft6)."
		stateField:  "s.exclusiveMax"
		producers:   ["exclusiveMaximum"]
		consumers:   ["maximum"]
	}

	ListItemsIsArray: {
		description: "Whether items keyword used array form (legacy tuple)."
		stateField:  "s.listItemsIsArray"
		producers:   ["items"]
		consumers:   ["additionalItems"]
	}

	ListPrefixItems: {
		description: "PrefixItems list literal (used by items/additionalItems)."
		stateField:  "s.list"
		producers:   ["prefixItems"]
		consumers:   ["items", "additionalItems"]
	}

	ObjectFields: {
		description: "Accumulating struct literal for object constraints."
		stateField:  "s.obj / obj.Elts"
		producers: [
			"properties",
			"patternProperties",
			"dependencies",
			"dependentSchemas",
			"dependentRequired",
			"additionalProperties",
			"x-kubernetes-embedded-resource",
		]
		consumers: [
			"required",
		]
		notes: [
			"dependencies currently inject placeholder fields into obj.Elts,",
			"so additionalProperties observes them (likely semantically wrong,",
			"but reflects current behavior).",
		]
	}

	PatternExclusions: {
		description: "Pattern exclusions derived from patternProperties."
		stateField:  "s.patterns"
		producers:   ["patternProperties"]
		consumers:   ["additionalProperties"]
	}

	PreserveUnknownFields: {
		description: "Kubernetes preserve-unknown-fields flag."
		stateField:  "s.preserveUnknownFields"
		producers:   ["x-kubernetes-preserve-unknown-fields"]
		consumers:   ["properties", "additionalProperties"]
	}

	K8sKind: {
		description: "Kubernetes kind injected into properties."
		stateField:  "s.k8sResourceKind"
		producers:   ["x-kubernetes-group-version-kind"]
		consumers:   ["properties"]
	}

	K8sAPIVersion: {
		description: "Kubernetes apiVersion injected into properties."
		stateField:  "s.k8sAPIVersion"
		producers:   ["x-kubernetes-group-version-kind"]
		consumers:   ["properties"]
	}

	IfConstraint: {
		description: "Stored 'if' subschema for if/then/else."
		stateField:  "s.ifConstraint"
		producers:   ["if"]
		consumers:   ["ifThenElse"]
	}

	ThenConstraint: {
		description: "Stored 'then' subschema for if/then/else."
		stateField:  "s.thenConstraint"
		producers:   ["then"]
		consumers:   ["ifThenElse"]
	}

	ElseConstraint: {
		description: "Stored 'else' subschema for if/then/else."
		stateField:  "s.elseConstraint"
		producers:   ["else"]
		consumers:   ["ifThenElse"]
	}
}
