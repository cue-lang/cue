package wellknown

// Each well-known type maps as defined by the file declaring it,
// even when another well-known file is imported after it.
#Types: {
	// Declared in wrappers.proto, yet mapped as non-nullable
	// by struct.proto, which is imported last.
	boolValue?:   bool         @protobuf(1,google.protobuf.BoolValue,name=bool_value)
	stringValue?: string       @protobuf(2,google.protobuf.StringValue,name=string_value)
	doubleValue?: null | float @protobuf(3,google.protobuf.DoubleValue,name=double_value)

	// Declared in struct.proto.
	struct?:    {...} @protobuf(4,google.protobuf.Struct)
	value?:     _     @protobuf(5,google.protobuf.Value)
	listValue?: [...] @protobuf(6,google.protobuf.ListValue,name=list_value)
	nullValue?: null  @protobuf(7,google.protobuf.NullValue,name=null_value)
}
