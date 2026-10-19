// versions: v0.8.0..v0.17.0; outcome: ok
// versions: v0.18.0..; outcome: error 3:2: old-style alias syntax (=) is not allowed with @experiment(aliasv2); use postfix syntax (~X or ~(K,V))
X=a: {b: 1}
c: X.b
