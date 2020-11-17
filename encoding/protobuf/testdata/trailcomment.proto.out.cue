// Issue #586
package foo

#Bar: {
	{} | {
		a: string @protobuf(1)

		// hello world

	} | {
		b: string @protobuf(2)

		// hello world

	}
	c?: int32 @protobuf(3)

	// hello world

}
