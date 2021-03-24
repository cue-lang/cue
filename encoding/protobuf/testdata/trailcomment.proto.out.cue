// Issue #586
package foo

#Bar: {
	{} | {
		a: string @protobuf(1,string)

		// hello world

	} | {
		b: string @protobuf(2,string)

		// hello world

	}
	c?: int32 @protobuf(3,int32)
	// hello world

}
