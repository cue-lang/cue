// versions: v0.8.0..; outcome: error 5:18: else requires @experiment(try)
// versions: v0.16.0..; experiments: try; outcome: ok
@experiment(try)

l: [if false {1} else {2}]
m: [for x in [] {x} otherwise {0}]
