// versions: v0.8.0..; outcome: error 7:3: else requires @experiment(try)
// versions: v0.16.0..; experiments: try; outcome: ok
@experiment(try)

if true {
	a: 1
} else {
	b: 2
}
