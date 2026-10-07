@experiment(functions)

l: [1]
f: func(s: {x: int}) -> int: s.x + 1
g: func(a: [...int]): a[0]
h: func(a: int) -> l[0]
k: func(s: {x: [...int]}): s.x...
