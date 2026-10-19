// versions: v0.8.0..v0.17.0; outcome: error 3:2: postfix alias syntax requires @experiment(aliasv2)
// versions: v0.18.0..; outcome: ok
a~(_, V): {b: 1}
c: V.b
x: {[string]~(K, _): {name: K}}
