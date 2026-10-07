@experiment(try)

a?: int
b: {
	try _ = a? {
		f: 1
	}
}
