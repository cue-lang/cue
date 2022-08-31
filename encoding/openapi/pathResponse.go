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
	r, _ := regexp.Compile(`([^\s]+)[/]([^\s]+)`)
	for i, _ := contentStruct.Value().Fields(cue.Definitions(false)); i.Next(); {
		label := i.Label()
		matched := r.MatchString(label)

		if !matched {
			continue
		}
		rb.mediaType(i.Value())

	}

	response.Set("description", description)
	if rb.mediaTypes.len() != 0 {
		response.Set("content", rb.mediaTypes)
	}

	return (*ast.StructLit)(response)
}

func Response(v cue.Value, c *buildContext) *ast.StructLit {
	return newResponseBuilder(c).buildResponse(v)
}
