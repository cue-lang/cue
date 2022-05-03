package openapi

import (
	"regexp"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/ast"
)

type ResponseObjectBuilder struct {
	ctx         *buildContext
	description string
	mediaTypes  *OrderedMap
}

func newResponseBuilder(c *buildContext) *ResponseObjectBuilder {
	return &ResponseObjectBuilder{ctx: c, mediaTypes: &OrderedMap{}}
}

func isMediaType(s string) bool {
	mediaTypes := map[string]bool{"application/json": true,
		"application/xml":                   true,
		"application/x-www-form-urlencoded": true,
		"multipart/form-data":               true,
		"text/plain":                        true,
		"text/html":                         true,
		"application/pdf":                   true,
		"image/png":                         true}

	_, ok := mediaTypes[s]
	return ok
}

func (rb *ResponseObjectBuilder) mediaType(v cue.Value) {
	schema := &OrderedMap{}
	schemaStruct := rb.ctx.build("schema", v.Lookup("schema"))

	schema.Set("schema", schemaStruct)

	label, _ := v.Label()
	rb.mediaTypes.Set(label, schema)
}

func (rb *ResponseObjectBuilder) buildResponse(v cue.Value) *ast.StructLit {

	response := &OrderedMap{}

	description, err := v.Lookup("description").String()
	if err != nil {
		description = ""
	}
	rb.description = description

	contentStruct := v.Lookup("content")
	for i, _ := contentStruct.Value().Fields(cue.Definitions(false)); i.Next(); {
		label := i.Label()
		matched, _ := regexp.MatchString(`([^\s]+)[/]([^\s]+)`, label)

		if !matched {
			continue
		}
		rb.mediaType(i.Value())

	}

	//rb.mediaTypes.Set("description", description)

	response.Set("description", description)
	if rb.mediaTypes.len() != 0 {
		response.Set("content", rb.mediaTypes)
	}

	return (*ast.StructLit)(response)
}

func Response(v cue.Value, c *buildContext) *ast.StructLit {
	return newResponseBuilder(c).buildResponse(v)
}
