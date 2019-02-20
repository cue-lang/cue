package partial

def: *1 | int
sum: 1 | 2

b: {
	idx: a[str] // should resolve to top-level `a`
	str: string
}
b a b: 4
a: {
	b: 3
	c: 4
}
c: b & {str: "b"}
