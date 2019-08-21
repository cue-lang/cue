MyStruct: {
	mediumNum: int32
	smallNum:  int8

	float:  float32
	double: float64

	deprecatedField: string @protobuf(5,deprecated)
}
