MyString: {
	exact: string
} | {
	regex: string
}

MyInt: int

Foo: {
	include: MyString
	exclude: [...MyString]
	count: MyInt
}
