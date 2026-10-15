package labels

// Field names are converted from snake_case to camelCase.
#Names: {
	fooBar?:  string @protobuf(1,string,name=foo_bar)
	fooÜber?: string @protobuf(2,string,name=foo_über)
	fooǅx?:   string @protobuf(3,string,name=foo_ǆx)
}
