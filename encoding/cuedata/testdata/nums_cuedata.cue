constant: 2
$$cue:    "import (\n\t\"strings\"\n\t\"math\"\n)\nseveral: 1 | 2 | 3 | 4\ninclusive: >=2 & <=3\nexclusive: uint & >2 & <3\nmulti: int & >=2 & <=3 | strings.MaxRunes(5)\nlegacy: >2 & <3\ncents: math.MultipleOf(0.05)\nneq: !=4"
