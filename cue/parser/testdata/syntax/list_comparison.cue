// versions: v0.8.0..; outcome: ok
l: [1, 2]
a: 1
x: [for v in l if v == 1 {v}]
y: [a == 1, "s" =~ "s", "s" !~ "t", a != 2]
