// versions: v0.8.0..; outcome: ok
x: true
l: [1]
if(x) {a: 1}
b: [if(x) {c: 1}]
d: {for v in l if(x) {e: v}}
f: {if[x][0] {g: 1}}
