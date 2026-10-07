src: {a: 1, b: 2}
for k, v in src {
	"\(k)2": v
}
if true {
	t: 1
}
l: [for x in [1, 2] if x > 1 let y = x {y}]
