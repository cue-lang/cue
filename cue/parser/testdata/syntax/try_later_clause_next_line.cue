// versions: v0.8.0..; outcome: ok
// versions: v0.16.0..; experiments: try; outcome: ok
@experiment(try)

a?: int
y: [1]
x: {
	for v in y try
	{b: v}
}
z: [
	for v in y try
	{c: v},
]
