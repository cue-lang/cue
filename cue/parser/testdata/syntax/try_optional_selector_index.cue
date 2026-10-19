// versions: v0.8.0..; outcome: ok
// versions: v0.16.0..; experiments: try; outcome: ok
@experiment(try)

x: {y?: int}
l: [1]
b: {
	try {v: x.y? + l[0]?}
}
