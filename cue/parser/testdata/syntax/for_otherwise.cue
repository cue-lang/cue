// versions: v0.8.0..; outcome: error 7:3: otherwise requires @experiment(try)
// versions: v0.16.0..; experiments: try; outcome: ok
@experiment(try)

for x in [] {
	"\(x)": x
} otherwise {
	empty: true
}
