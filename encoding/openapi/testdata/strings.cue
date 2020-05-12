import "strings"

#MyType: {
	myString: strings.MinRunes(1) & strings.MaxRunes(5)

	myPattern: =~"foo.*bar"

	myAntiPattern: !~"foo.*bar"
}
