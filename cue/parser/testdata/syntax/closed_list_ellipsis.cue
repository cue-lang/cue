// versions: v0.8.0..; outcome: error 5:9: expected ']', found '...'
// versions: v0.18.0..; experiments: openlists; outcome: error 5:9: closed list literal cannot end in an ellipsis
@experiment(openlists)

a: #[1, ...]
