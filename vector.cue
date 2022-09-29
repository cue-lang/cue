t: {
	a: true // must be declared outside of the nested comprehensions.
	if true {
		b: {
			if a {
			}
		}
	}
}
