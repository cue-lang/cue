@experiment(try)

a?: int
b: {
	try {v: a?} else {v: 0}
}
