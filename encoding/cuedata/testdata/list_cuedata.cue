aList: [1, 2, 3, 1, 2, 3]
$$cue: "#aEnum: *\"1\" | \"2\" | \"3\"\naList: [...#aEnum]\naSqrExtra: [ for x in aList {\n\tx * x\n}, 100, 200]\naSqrEven: [ for x in aList if x rem 2 == 0 {\n\tx * x\n}]"
