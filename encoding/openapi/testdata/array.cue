Arrays: {
	bar?: [...MyEnum]
	foo?: [...MyStruct]
}

Arrays: {
	bar?: [...MyEnum]
	foo?: [...MyStruct]
}

// MyStruct
MyStruct: {
	a?: int
	e?: [...MyEnum]
	e?: [...MyEnum]
}

// MyEnum
MyEnum: *"1" | "2" | "3"
