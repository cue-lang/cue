// versions: v0.8.0..; outcome: error 5:4: function syntax requires @experiment(functions)
// versions: v0.18.0..; experiments: functions; outcome: ok
@experiment(functions)

f: func(a: int @doc(x)) -> int: a
