@experiment(try)

a?: int
b: {
	try x = a?
	{
		c: x
	}
}
