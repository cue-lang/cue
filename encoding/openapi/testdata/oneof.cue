T :: {
	shared: int
}
T :: {} | {
	exact: string
} | {
	regex: string
}
T :: {} | {
	count: int
} | {
	amount: int
}
T :: {
	shared2: int
}

MyInt :: int

Foo: {
	include: T
	exclude: [...T]
	count: MyInt
}

Incompatible :: {
	shared: int
} | {
	shared: int
	extra1: int
} | {
	shared: int
	extra2: int
}

WithMap :: {
	shared: [string]: int
} | {
	shared: [string]: int
	extra: int
} | {
	shared: string // incompatible
	extra:  int
}

Embed :: {
	a?: int

	close({}) |
	close({b: int}) |
	close({c: int})

	// TODO: maybe support builtin to write this as
	// oneof({},
	// {b: int},
	// {c: int})
}
