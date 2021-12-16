x: [1, 2, 3]
a: {
	c:     "barbaz"
	$$cue: "b: strings.ToTitle(\"foobar\")"
}
$$cue: "import (\n\t\"list\"\n\t\"strings\"\n)\nx: list.MaxItems(3)"
