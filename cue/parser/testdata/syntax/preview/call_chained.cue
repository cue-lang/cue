@experiment(functions)

f: func(a: int, b: int) -> int: a + b
g: f(1, ...)(2)
l: [f]
h: l[0](1, 2)
