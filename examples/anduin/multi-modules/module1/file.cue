package module1

import "math"

data: {
	// Simple labels don't need to be quoted.
	one:       1
	two:       2
	piPlusOne: math.Pi + 1

	// Field names must be quoted if they contain
	// special characters, such as hyphen and space.
	"quoted field names": {
		"two-and-a-half":    2.5
		"three point three": 3.3
		"four^four":         math.Pow(4, 4)
	}

	aList: [
		1,
		2,
		3,
	]
}
