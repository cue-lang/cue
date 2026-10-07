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
