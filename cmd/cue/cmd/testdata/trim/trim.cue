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

	sList: [{a: 8, b: string}, {a: 9, b: "foo" | string}]
	rList: [{a: "a"}]
	rcList: [{a: "a", c: b}]

	t <Name>: {
		x: 0..5
	}
}

foo bar: {
	_value: "here"

	a: 4
	b: "foo"
	c: 45
	e: string
	f: ">> here <<"

	// The template does not require that this field be an integer (it may be
	// a float), and thus this field specified additional information and
	// cannot be removed.
	n: int

	struct: {a: 3.0}

	list: ["foo", float]

	sList: [{a: 8, b: "foo"}, {b: "foo"}]
	rList: [{a: string}]
	rcList: [{a: "a", c: "foo"}]
}

foo baz: {}

foo multipath: {
	t <Name>: {
		// Combined with the other template, we know the value must be 5 and
		// thus the entry below can be eliminated.
		x: 5..8
	}

	t u: {
		x: 5
	}
}

// TODO: top-level fields are currently not removed.
group: {
	comp "\(k)": v for k, v in foo

	comp bar: {
		a:  4
		aa: 8 // new value
	}

	comp baz: {} // removed: fully implied by comprehension above
}
