package test

#Test: {
	// doc comment
	@protobuf(option (yoyo.foo)=true) // line comment
	@protobuf(option (yoyo.bar)=false)
	test?: int32 @protobuf(1,int32)
}
