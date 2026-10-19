// versions: v0.8.0..; outcome: error 5:4: closed literals require @experiment(openlists)
// versions: v0.18.0..; experiments: openlists; outcome: ok
@experiment(openlists)

a: #{b: 1}
