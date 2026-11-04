// versions: v0.8.0..v0.17.0; outcome: error 4:7: postfix alias syntax requires language version v0.18.0 or @experiment(aliasv2)
// versions: v0.18.0..; outcome: ok
k: "foo"
"\(k)"~X: 1
y: X
