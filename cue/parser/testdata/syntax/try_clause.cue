// versions: v0.8.0..; outcome: ok
// versions: v0.16.0..; experiments: try; outcome: ok
@experiment(try)

a?: int
b: {
	try {
		c: a?
	}
}
