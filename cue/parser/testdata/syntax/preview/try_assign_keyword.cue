@experiment(try)

a?: int
b: {
	try package = a? {
		v: package
	}
}
