@experiment(try)

a?: int
l: [1]
b: {
	try for x in l {
		c: x
	}
	try if a? > 1 {
		d: 1
	}
	try let y = a? {
		e: y
	}
}
