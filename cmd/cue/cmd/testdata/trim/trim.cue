package trim

foo <Name>: {
	_value: string

	a: 4
	b: string
	d: 8
	e: "foo"
	f: ">> \( _value) <<"
	n: 5

	list: ["foo", 8.0]

	struct: {a: 3.0}

	sList: [{a: 8, b: string}, {a: 9, b: *"foo" | string}]
	rList: [{a: "a"}]
	rcList: [{a: "a", c: b}]

	t <Name>: {
		x: >=0 & <=5
	}
}

foo bar: {
	_value: "here"
	b:      "foo"
	c:      45

	sList: [{b: "foo"}, {}]
}

foo baz: {}

foo multipath: {
	t <Name>: {
		// Combined with the other template, we know the value must be 5 and
		// thus the entry below can be eliminated.
		x: >=5 & <=8
	}

	t u: {
	}
}

// TODO: top-level fields are currently not removed.
group: {
	comp "\(k)": v for k, v in foo

	comp bar: {
		aa: 8 // new value
	}
}
