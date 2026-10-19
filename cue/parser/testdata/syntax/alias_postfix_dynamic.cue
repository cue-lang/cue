// versions: v0.8.0..v0.17.0; outcome: error 4:4: postfix alias syntax requires @experiment(aliasv2)
// versions: v0.18.0..; outcome: ok
k: "foo"
(k)~X: 1
"quoted"~(Y): 2
z: X + Y
