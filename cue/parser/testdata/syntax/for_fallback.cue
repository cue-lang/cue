// versions: v0.8.0..; outcome: error 7:3: fallback requires @experiment(try)
// versions: v0.16.0..; experiments: try; outcome: ok
@experiment(try)

for x in [] {
	"\(x)": x
} fallback {
	empty: true
}
