@experiment(try)

x: {y?: int}
l: [1]
b: {
	try {v: x.y? + l[0]?}
}
