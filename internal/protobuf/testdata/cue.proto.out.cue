package proto

FieldOptions required?: bool @protobuf(1)

google.protobuf.FieldOptions: {
	val?: string       @protobuf(123456)
	opt?: FieldOptions @protobuf(1069)
}
