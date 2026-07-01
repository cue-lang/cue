// This file is the meta-schema used by [GenerateV2] to locate the Schema
// Object positions within an OpenAPI document. It is embedded into the binary
// and unified with the document being generated: every position typed as
// #SchemaObject acquires the hidden _openapiSchema marker, which the generator
// detects to know where to convert a # definition (or a raw JSON Schema value)
// into an OpenAPI schema.
//
// The meta-schema deliberately leaves everything open (...) so that unifying it
// with a real document neither rejects specification extensions (x-* fields and
// vendor sections) nor other content it does not model. It only needs to
// describe enough structure to reach the document-level schema positions;
// schemas nested within a schema (properties, items, allOf, …) are handled by
// the JSON Schema generator, not by this document walk.
//
// Versioning: the document structure differs between OpenAPI versions, so there
// is one #Document_* definition per supported version and the generator selects
// the one matching its configured version (see metaSchema in generate_v2.go).
// Most of the structure, in particular every place a Schema Object can appear
// below the top level, is shared between versions and factored into the
// building blocks below; only the version-specific document and components
// shapes differ. Today just 3.0 and 3.1 are modeled; a new version is added by
// introducing another #Document_* definition and extending metaSchema.
//
// This file has no package clause on purpose: it is compiled on its own, so
// the _openapiSchema marker lives in the anonymous package "_", and is detected
// via cue.Hid("_openapiSchema", "_").

// #SchemaObject marks a position where an OpenAPI Schema Object is expected. A
// Reference Object ({$ref: string}) is a valid schema, so no separate case is
// needed for it.
#SchemaObject: {
	_openapiSchema: true
	...
}

#MediaType: {
	schema?: #SchemaObject
	...
}

#Content: [string]: #MediaType

#Header: {
	schema?:  #SchemaObject
	content?: #Content
	...
}

#Parameter: {
	schema?:  #SchemaObject
	content?: #Content
	...
}

#RequestBody: {
	content?: #Content
	...
}

#Response: {
	content?: #Content
	headers?: [string]: #Header
	...
}

#Operation: {
	parameters?:  [...#Parameter]
	requestBody?: #RequestBody
	responses?: [string]: #Response
	callbacks?: [string]: #Callback
	...
}

#PathItem: {
	get?:        #Operation
	put?:        #Operation
	post?:       #Operation
	delete?:     #Operation
	options?:    #Operation
	head?:       #Operation
	patch?:      #Operation
	trace?:      #Operation
	parameters?: [...#Parameter]
	...
}

#Callback: [string]: #PathItem

// #Components_3_0 holds the component maps that hold schemas in OpenAPI 3.0.
#Components_3_0: {
	schemas?: [string]:       #SchemaObject
	responses?: [string]:     #Response
	parameters?: [string]:    #Parameter
	requestBodies?: [string]: #RequestBody
	headers?: [string]:       #Header
	...
}

// #Components_3_1 adds the pathItems component map introduced in OpenAPI 3.1.
#Components_3_1: {
	#Components_3_0
	pathItems?: [string]: #PathItem
	...
}

// #Document_3_0 describes an OpenAPI 3.0 document.
#Document_3_0: {
	paths?: [string]: #PathItem
	components?: #Components_3_0
	...
}

// #Document_3_1 describes an OpenAPI 3.1 document. It adds top-level webhooks
// and, via #Components_3_1, component path items.
#Document_3_1: {
	paths?: [string]:    #PathItem
	webhooks?: [string]: #PathItem
	components?: #Components_3_1
	...
}
