// versions: v0.8.0..; outcome: error 7:14: else requires @experiment(try)
// versions: v0.16.0..; experiments: try; outcome: ok
@experiment(try)

a?: int
b: {
	try {v: a?} else {v: 0}
}
