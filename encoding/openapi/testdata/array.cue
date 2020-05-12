import "list"

#Arrays: {
	bar?: [...#MyEnum]
	foo?: [...#MyStruct]

	baz?: list.UniqueItems()

	qux?: list.MinItems(1) & list.MaxItems(3)
}

#Arrays: {
	bar?: [...#MyEnum]
	foo?: [...#MyStruct]
}

// MyStruct
#MyStruct: {
	a?: int
	e?: [...#MyEnum]
	e?: [...#MyEnum]
}

// MyEnum
#MyEnum: *"1" | "2" | "3"
