l: [1, 2]
a: {
	for x in l
	if x > 1 {
		"\(x)": x
	}
}
b: [
	for x in l
	let y = x + 1
	if y > 2 {y},
]
