// versions: v0.8.0..v0.17.0; outcome: ok
// versions: v0.18.0..; outcome: error 3:5: old-style alias syntax (=) is not allowed with @experiment(aliasv2); use postfix syntax (~X or ~(K,V))
a: X={b: 1}
c: X.b
