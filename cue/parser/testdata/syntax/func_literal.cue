// versions: v0.8.0..; outcome: error 5:6: function syntax requires @experiment(functions)
// versions: v0.18.0..; experiments: functions; outcome: ok
@experiment(functions)

add: func(a: int, b: int) -> int: a + b
c:   add(1, 2)
